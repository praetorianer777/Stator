package docx

import (
	"bytes"
	"fmt"
	"image"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// inliner reads a paragraph's runs into inline nodes, and the pictures and
// text boxes among them into blocks that split the paragraph.
type inliner struct {
	c     *conv
	depth int
	code  bool
	items []*rnode
	// fields are the fields open, outermost first: a field's instruction is
	// read, never shown, and a hyperlink field links its result.
	fields    []*field
	bookmarks []string
}

type field struct {
	instr  strings.Builder
	result bool
	link   string
}

func (in *inliner) walk(children []*elem, link string) {
	c := in.c
	for _, e := range children {
		switch e.name {
		case "r":
			in.run(e, link)
		case "hyperlink":
			in.walk(e.children, c.hyperlink(e))
		case "ins":
			c.tracked()
			in.walk(e.children, link)
		case "moveTo", "smartTag", "customXml", "dir", "bdo":
			in.walk(e.children, link)
		case "sdt":
			in.walk(e.child("sdtContent").children, link)
		case "del", "moveFrom":
			c.tracked()
		case "fldSimple":
			href := fieldLink(e.attr("instr"))
			if href != "" && !strings.HasPrefix(href, bookmarkHref) {
				href = c.safeLink(href)
			}
			if href == "" {
				href = link
			}
			in.walk(e.children, href)
		case "bookmarkStart":
			if name := e.attr("name"); name != "" {
				in.bookmarks = append(in.bookmarks, name)
			}
		case "oMath", "oMathPara":
			c.warn("Equations were kept as their text, since a page writes formulas in TeX.")
			in.text(mathText(e), nil)
		case "AlternateContent":
			in.walk(choice(e), link)
		}
	}
}

// hyperlink is where a hyperlink points: an address, or a bookmark of the
// document, settled once the headings' anchors are known.
func (c *conv) hyperlink(e *elem) string {
	id := e.attrNS("relationships", "id")
	if id == "" {
		id = e.attr("id")
	}
	if r, ok := c.rels[id]; ok && r.external {
		href := r.target
		if a := e.attr("anchor"); a != "" {
			href += "#" + a
		}
		return c.safeLink(href)
	}
	if a := e.attr("anchor"); a != "" {
		return bookmarkHref + a
	}
	return ""
}

// safeLink is a link a page may hold, or none, said once.
func (c *conv) safeLink(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if len(href) > document.MaxHrefLength || !document.SafeHref(href) || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
		c.warn("Links that are not web or mail addresses were left out, their words kept.")
		return ""
	}
	return href
}

// fieldLink reads the address of a HYPERLINK field's instruction.
func fieldLink(instr string) string {
	fields := strings.Fields(instr)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "HYPERLINK") {
		return ""
	}
	rest := strings.TrimSpace(instr[strings.Index(strings.ToUpper(instr), "HYPERLINK")+len("HYPERLINK"):])
	if strings.HasPrefix(rest, `\l`) {
		return bookmarkHref + strings.Trim(strings.TrimSpace(rest[2:]), `"`)
	}
	if strings.HasPrefix(rest, `"`) {
		if end := strings.Index(rest[1:], `"`); end >= 0 {
			return rest[1 : end+1]
		}
	}
	return strings.Fields(rest)[0]
}

// inField says whether the run is part of a field's instruction.
func (in *inliner) inField() bool {
	for _, f := range in.fields {
		if !f.result {
			return true
		}
	}
	return false
}

func (in *inliner) fieldLinkOf() string {
	for i := len(in.fields) - 1; i >= 0; i-- {
		if in.fields[i].link != "" {
			return in.fields[i].link
		}
	}
	return ""
}

func (in *inliner) run(r *elem, link string) {
	c := in.c
	rPr := r.child("rPr")
	look := c.styles.look(rPr)
	style := rPr.child("rStyle").val()
	for _, e := range r.children {
		switch e.name {
		case "fldChar":
			switch e.attr("fldCharType") {
			case "begin":
				in.fields = append(in.fields, &field{})
			case "separate":
				if n := len(in.fields); n > 0 {
					f := in.fields[n-1]
					f.result = true
					if href := fieldLink(f.instr.String()); href != "" {
						if strings.HasPrefix(href, bookmarkHref) {
							f.link = href
						} else {
							f.link = c.safeLink(href)
						}
					}
				}
			case "end":
				if n := len(in.fields); n > 0 {
					in.fields = in.fields[:n-1]
				}
			}
			continue
		case "instrText":
			if n := len(in.fields); n > 0 && !in.fields[n-1].result {
				in.fields[n-1].instr.WriteString(e.text)
			}
			continue
		}
		if in.inField() {
			continue
		}
		href := link
		if href == "" {
			href = in.fieldLinkOf()
		}
		switch e.name {
		case "t":
			if look.hidden {
				c.warn("Hidden text was left out.")
				continue
			}
			if style == "FormulaChar" && strings.TrimSpace(e.text) != "" {
				in.items = append(in.items, &rnode{typ: document.NodeMathInline, attrs: map[string]any{"latex": e.text}})
				continue
			}
			in.text(e.text, in.marks(look, style, href))
		case "tab", "ptab":
			if in.code {
				in.text("\t", nil)
			} else {
				in.text(" ", in.marks(look, style, href))
			}
		case "br":
			// Page and column breaks only lay out Word's sheets.
			if t := e.attr("type"); t == "page" || t == "column" {
				continue
			}
			in.items = append(in.items, &rnode{typ: "hardBreak"})
		case "cr":
			in.items = append(in.items, &rnode{typ: "hardBreak"})
		case "noBreakHyphen":
			in.text("-", in.marks(look, style, href))
		case "sym":
			code, err := strconv.ParseUint(e.attr("char"), 16, 32)
			if err != nil {
				continue
			}
			// A symbol font's characters sit in the private area and mean
			// nothing in another font.
			if code >= 0xF000 && code <= 0xF0FF {
				c.warn("Symbols from a symbol font were left out.")
				continue
			}
			in.text(string(rune(code)), in.marks(look, style, href))
		case "drawing":
			in.blocks(c.drawing(e, in.depth))
		case "pict":
			in.blocks(c.vml(e, in.depth))
		case "object":
			c.warn("Embedded objects, such as spreadsheets, were left out.")
		case "AlternateContent":
			in.run(&elem{name: "r", children: append([]*elem{{name: "rPr", children: rPr.childrenOrNil()}}, choice(e)...)}, link)
		case "footnoteReference", "endnoteReference":
			kind := strings.TrimSuffix(e.name, "Reference")
			if note := c.notes[kind+":"+e.attr("id")]; note != nil {
				c.noteRefs = append(c.noteRefs, note)
				in.text("["+strconv.Itoa(len(c.noteRefs))+"]", nil)
			}
		case "ruby":
			in.walk(e.child("rubyBase").children, link)
		}
	}
}

func (e *elem) childrenOrNil() []*elem {
	if e == nil {
		return nil
	}
	return e.children
}

// marks are a run's marks, in the order the editor writes them; underline
// and raised or lowered text have none and are said once.
func (in *inliner) marks(look runLook, style, href string) []document.Mark {
	if in.code {
		return nil
	}
	var out []document.Mark
	if look.bold {
		out = append(out, document.Mark{Type: "bold"})
	}
	if look.italic {
		out = append(out, document.Mark{Type: "italic"})
	}
	if look.strike {
		out = append(out, document.Mark{Type: "strike"})
	}
	if look.mono {
		out = append(out, document.Mark{Type: "code"})
	}
	if href != "" {
		out = append(out, document.Mark{Type: "link", Attrs: map[string]any{"href": href}})
	}
	if look.underline && href == "" && !strings.EqualFold(style, "Hyperlink") {
		in.c.warn("Underlined text was kept without its underline, since a page has none.")
	}
	if look.shifted {
		in.c.warn("Superscript and subscript were kept as ordinary text.")
	}
	return out
}

// text adds words, joined to the words before when they look the same.
func (in *inliner) text(s string, marks []document.Mark) {
	if s == "" {
		return
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if n := len(in.items); n > 0 {
		last := in.items[n-1]
		if last.typ == "text" && !last.block && sameMarks(last.marks, marks) {
			last.text += s
			return
		}
	}
	in.items = append(in.items, &rnode{typ: "text", text: s, marks: marks})
}

func sameMarks(a, b []document.Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || stringOf(a[i].Attrs["href"]) != stringOf(b[i].Attrs["href"]) {
			return false
		}
	}
	return true
}

func (in *inliner) blocks(blocks []*rnode) {
	for _, b := range blocks {
		b.block = true
		in.items = append(in.items, b)
	}
}

func mathText(e *elem) string {
	var b strings.Builder
	var walk func(*elem)
	walk = func(x *elem) {
		for _, c := range x.children {
			if c.name == "t" {
				b.WriteString(c.text)
				continue
			}
			walk(c)
		}
	}
	walk(e)
	return b.String()
}

// drawing reads a DrawingML object: a picture, a shape or group whose text
// is kept, or a chart or diagram that is said to be left out.
func (c *conv) drawing(d *elem, depth int) []*rnode {
	frame := d.child("inline")
	if frame == nil {
		frame = d.child("anchor")
	}
	if frame == nil {
		return nil
	}
	docPr := frame.child("docPr")
	alt := strings.TrimSpace(docPr.attr("descr"))
	if alt == "" {
		alt = strings.TrimSpace(docPr.attr("title"))
	}
	widthPx := 0
	if cx, err := strconv.ParseInt(frame.child("extent").attr("cx"), 10, 64); err == nil && cx > 0 {
		widthPx = int((cx + emuPerPixel/2) / emuPerPixel)
	}
	data := frame.find("graphicData")
	uri := data.attr("uri")
	switch {
	case strings.HasSuffix(uri, "/picture"):
		if n := c.picture(data.find("blip"), alt, widthPx); n != nil {
			return []*rnode{n}
		}
		return nil
	case strings.HasSuffix(uri, "/chart"):
		c.warn("Charts were left out; paste each into the document as a picture and import it again to keep it.")
		return nil
	case strings.HasSuffix(uri, "/diagram"):
		c.warn("SmartArt diagrams were left out; paste each into the document as a picture and import it again to keep it.")
		return nil
	}
	return c.shapes(data, depth)
}

// shapes reads what a shape, a group or a canvas holds: its pictures and
// the text of its text boxes; a shape with neither is said to be left out.
func (c *conv) shapes(e *elem, depth int) []*rnode {
	var out []*rnode
	found := false
	var walk func(x *elem)
	walk = func(x *elem) {
		for _, ch := range x.children {
			switch ch.name {
			case "txbxContent":
				found = true
				c.warn("Text boxes were kept as their text where they are anchored; their place on the sheet was not.")
				if depth+1 >= maxNesting {
					continue
				}
				out = append(out, c.blocks(ch.children, depth+1)...)
			case "pic":
				found = true
				if n := c.picture(ch.find("blip"), strings.TrimSpace(ch.find("cNvPr").attr("descr")), 0); n != nil {
					out = append(out, n)
				}
			default:
				walk(ch)
			}
		}
	}
	walk(e)
	if !found {
		c.warn("Shapes and drawings were left out, since a page draws none.")
	}
	return out
}

// vml reads the older drawing format Word still writes for some pictures and text boxes.
func (c *conv) vml(e *elem, depth int) []*rnode {
	var out []*rnode
	found := false
	var walk func(x *elem)
	walk = func(x *elem) {
		for _, ch := range x.children {
			switch ch.name {
			case "imagedata":
				found = true
				id := ch.attrNS("relationships", "id")
				if n := c.pictureByRel(id, strings.TrimSpace(x.attr("alt")), 0); n != nil {
					out = append(out, n)
				}
			case "txbxContent":
				found = true
				c.warn("Text boxes were kept as their text where they are anchored; their place on the sheet was not.")
				if depth+1 < maxNesting {
					out = append(out, c.blocks(ch.children, depth+1)...)
				}
			default:
				walk(ch)
			}
		}
	}
	walk(e)
	if !found {
		c.warn("Shapes and drawings were left out, since a page draws none.")
	}
	return out
}

func (c *conv) picture(blip *elem, alt string, widthPx int) *rnode {
	if blip == nil {
		return nil
	}
	id := blip.attrNS("relationships", "embed")
	if id == "" {
		if blip.attrNS("relationships", "link") != "" {
			c.warn("Pictures linked from outside the document were left out; insert them into it and import it again.")
		}
		return nil
	}
	return c.pictureByRel(id, alt, widthPx)
}

// pictureByRel reads a picture once, however often it is shown, as a file
// for the page; one a browser cannot show is said to be left out.
func (c *conv) pictureByRel(id, alt string, widthPx int) *rnode {
	r, ok := c.rels[id]
	if !ok || r.external {
		return nil
	}
	i, seen := c.pictureAt[r.target]
	if !seen {
		i = -1
		data, err := c.pkg.read(r.target)
		if err != nil {
			c.fail(err)
			return nil
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		switch {
		case data == nil:
		case err != nil || !browserFormats[format]:
			c.warn(fmt.Sprintf("Pictures in a format browsers do not show (%s) were left out; save them as PNG or JPEG in the document and import it again.", strings.ToUpper(strings.TrimPrefix(path.Ext(r.target), "."))))
		case c.opts.FileLimit > 0 && int64(len(data)) > c.opts.FileLimit:
			c.warn("Pictures larger than a file of a page may be were left out; make them smaller in the document and import it again.")
		default:
			c.pictureBytes += int64(len(data))
			if c.pictureBytes > MaxPictureBytes {
				c.fail(&TooLargeError{What: fmt.Sprintf("its pictures weigh more than %d MB; make them smaller in Word and import it again", MaxPictureBytes>>20)})
				return nil
			}
			i = len(c.pictures)
			c.pictures = append(c.pictures, Picture{Name: pictureName(r.target, format), Data: data})
			c.pictureWidths = append(c.pictureWidths, cfg.Width)
		}
		c.pictureAt[r.target] = i
	}
	if i < 0 {
		return nil
	}
	attrs := map[string]any{"attachmentId": PictureRef(i)}
	if alt != "" {
		if r := []rune(alt); len(r) > document.MaxAltLength {
			alt = string(r[:document.MaxAltLength])
		}
		attrs["alt"] = alt
	}
	// A picture shown at its own size is left to the page's width, as the editor leaves it.
	if natural := c.pictureWidths[i]; widthPx > 0 && (widthPx < natural-1 || widthPx > natural+1) {
		attrs["width"] = min(widthPx, document.MaxImageWidth)
	}
	return &rnode{typ: "image", attrs: attrs}
}

// browserFormats are the pictures a page shows as they are.
var browserFormats = map[string]bool{"png": true, "jpeg": true, "gif": true, "webp": true}

// pictureName names a picture's file after its part, with the extension its bytes call for.
func pictureName(target, format string) string {
	base := strings.TrimSuffix(path.Base(target), path.Ext(target))
	ext := map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif", "webp": ".webp"}[format]
	return base + ext
}
