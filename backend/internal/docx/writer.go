package docx

import (
	"context"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Page geometry, in twentieths of a point: A4 portrait with margins of an
// inch, as Word's own A4 template has them.
const (
	pageWidth  = 11906
	pageHeight = 16838
	margin     = 1440
	textWidth  = pageWidth - 2*margin
	// cellMargin is Word's default space between a cell's border and its text.
	cellMargin = 108
	// listIndent is how far each level of a list sits in, and hanging how far
	// its marker hangs out of that.
	listIndent = 720
	hanging    = 360
	// maxListLevel is the deepest level Word numbers.
	maxListLevel = 8
	// minColumnWidth keeps a column that says nothing of its width readable.
	minColumnWidth = 600
)

// Drawing units: Word measures pictures in English Metric Units.
const (
	emuPerPixel = 9525
	emuPerTwip  = 635
)

type media struct {
	rid    string
	target string
	ext    string
	data   []byte
}

type relationship struct {
	id, kind, target string
	external         bool
}

// listNum is one numbering instance: a list numbered on its own, starting where it says.
type listNum struct {
	abstract int
	start    int
}

type writer struct {
	ctx      context.Context
	read     Reader
	words    words
	b        strings.Builder
	rels     []relationship
	media    []media
	nums     []listNum
	pictures int
	// embedded is each file read so far, nil for one left out, so a picture
	// shown twice is carried once.
	embedded map[string]*picture
	spent    int64
	err      error
	// anchors maps a heading's anchor to its bookmark, for links within the page.
	anchors  map[string]string
	headings []document.Heading
	heading  int
	bookmark int
	via      []uuid.UUID
}

func newWriter(ctx context.Context, p Page, r Reader) *writer {
	w := &writer{ctx: ctx, read: r, words: wordsFor(p.Language), anchors: map[string]string{}, embedded: map[string]*picture{}}
	w.headings = document.Headings(p.Body)
	for i, h := range w.headings {
		if _, taken := w.anchors[h.Anchor]; !taken {
			w.anchors[h.Anchor] = bookmarkName(i)
		}
	}
	if p.ID != uuid.Nil {
		w.via = []uuid.UUID{p.ID}
	}
	return w
}

// bookmarkName is a heading's bookmark: hidden, as Word's own leading underscore makes it.
func bookmarkName(i int) string { return "_Heading" + strconv.Itoa(i+1) }

// document is word/document.xml: the title, a line about the page, then its body.
func (w *writer) document(p Page) ([]byte, error) {
	w.b.WriteString(xml.Header)
	w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
	w.para(frame{width: textWidth}, paraProps{style: "Title"}, func() { w.text(p.Title, runProps{}) })
	w.meta(p)
	root := frame{width: textWidth, top: true}
	w.blocks(p.Body.Content, root, 0)
	w.b.WriteString(`<w:sectPr><w:pgSz w:w="` + itoa(pageWidth) + `" w:h="` + itoa(pageHeight) + `"/>`)
	w.b.WriteString(`<w:pgMar w:top="` + itoa(margin) + `" w:right="` + itoa(margin) + `" w:bottom="` + itoa(margin) + `" w:left="` + itoa(margin) + `" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`)
	w.b.WriteString(`</w:body></w:document>`)
	if w.err != nil {
		return nil, w.err
	}
	return []byte(w.b.String()), nil
}

// meta is the line under the title: the space, the version and its day, and
// where the page is read.
func (w *writer) meta(p Page) {
	parts := []string{}
	if p.Space != "" {
		parts = append(parts, p.Space)
	}
	if p.Version > 0 {
		parts = append(parts, w.words.version(p.Version, w.words.day(p.Modified)))
	}
	if len(parts) == 0 && p.URL == "" {
		return
	}
	w.para(frame{width: textWidth}, paraProps{style: "Subtitle"}, func() {
		w.text(strings.Join(parts, " · "), runProps{})
		if p.URL != "" {
			if len(parts) > 0 {
				w.text(" · ", runProps{})
			}
			w.hyperlink(p.URL, func() { w.text(w.words.open, runProps{style: "Hyperlink"}) })
		}
	})
}

// fail keeps the first error; what follows is still written, and thrown away.
func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// rel adds a relationship of the document and returns its id.
func (w *writer) rel(kind, target string, external bool) string {
	id := "rId" + strconv.Itoa(len(w.rels)+firstRel)
	w.rels = append(w.rels, relationship{id: id, kind: kind, target: target, external: external})
	return id
}

// firstRel leaves rId1 to rId3 to the styles, numbering and settings.
const firstRel = 4

func (w *writer) relationships() []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	b.WriteString(`<Relationship Id="rId1" Type="` + relStyles + `" Target="styles.xml"/>`)
	b.WriteString(`<Relationship Id="rId2" Type="` + relNumbering + `" Target="numbering.xml"/>`)
	b.WriteString(`<Relationship Id="rId3" Type="` + relSettings + `" Target="settings.xml"/>`)
	for _, r := range w.rels {
		b.WriteString(`<Relationship Id="` + r.id + `" Type="` + r.kind + `" Target="` + escape(r.target) + `"`)
		if r.external {
			b.WriteString(` TargetMode="External"`)
		}
		b.WriteString(`/>`)
	}
	b.WriteString(`</Relationships>`)
	return []byte(b.String())
}

// frame is where blocks are written: how far in, how wide, inside how many
// lists, and the styling a table cell lends its paragraphs.
type frame struct {
	indent int
	width  int
	list   int
	bold   bool
	align  string
	// marker is the list marker the next paragraph carries, once.
	marker *marker
	// top is the page's own body, whose headings are bookmarked for its links.
	top bool
}

type marker struct {
	num   int
	level int
	// box is a task's check box, written as a character before its words.
	box  string
	used bool
}

// paraProps are a paragraph's properties, written in the order the schema wants them.
type paraProps struct {
	style    string
	keepNext bool
	border   string
	align    string
}

// para writes one paragraph in the frame, taking the frame's list marker
// if it has one waiting.
func (w *writer) para(f frame, pp paraProps, runs func()) {
	var props strings.Builder
	if pp.style != "" {
		props.WriteString(`<w:pStyle w:val="` + pp.style + `"/>`)
	}
	if pp.keepNext {
		props.WriteString("<w:keepNext/>")
	}
	m := f.marker
	if m != nil && m.used {
		m = nil
	}
	if m != nil && m.box == "" {
		props.WriteString(`<w:numPr><w:ilvl w:val="` + itoa(m.level) + `"/><w:numId w:val="` + itoa(m.num) + `"/></w:numPr>`)
	}
	props.WriteString(pp.border)
	switch {
	case m != nil:
		props.WriteString(`<w:ind w:left="` + itoa(f.indent) + `" w:hanging="` + itoa(hanging) + `"/>`)
	case f.indent > 0:
		props.WriteString(`<w:ind w:left="` + itoa(f.indent) + `"/>`)
	}
	align := pp.align
	if align == "" {
		align = f.align
	}
	switch align {
	case "center":
		props.WriteString(`<w:jc w:val="center"/>`)
	case "right":
		props.WriteString(`<w:jc w:val="right"/>`)
	}
	w.b.WriteString("<w:p>")
	if props.Len() > 0 {
		w.b.WriteString("<w:pPr>" + props.String() + "</w:pPr>")
	}
	if m != nil {
		m.used = true
		if m.box != "" {
			w.text(m.box+"\t", runProps{})
		}
	}
	if runs != nil {
		runs()
	}
	w.b.WriteString("</w:p>")
}

// runProps are a run's properties, written in the order the schema wants them.
type runProps struct {
	style  string
	bold   bool
	italic bool
	caps   bool
	strike bool
	color  string
	shade  string
}

func (rp runProps) write(b *strings.Builder) {
	if rp == (runProps{}) {
		return
	}
	b.WriteString("<w:rPr>")
	if rp.style != "" {
		b.WriteString(`<w:rStyle w:val="` + rp.style + `"/>`)
	}
	if rp.bold {
		b.WriteString("<w:b/><w:bCs/>")
	}
	if rp.italic {
		b.WriteString("<w:i/><w:iCs/>")
	}
	if rp.caps {
		b.WriteString("<w:caps/>")
	}
	if rp.strike {
		b.WriteString("<w:strike/>")
	}
	if rp.color != "" {
		b.WriteString(`<w:color w:val="` + rp.color + `"/>`)
	}
	if rp.shade != "" {
		b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="` + rp.shade + `"/>`)
	}
	b.WriteString("</w:rPr>")
}

// text writes text as runs: a line break, a tab and the words between them
// each as Word wants them.
func (w *writer) text(s string, rp runProps) {
	if s == "" {
		return
	}
	w.b.WriteString("<w:r>")
	rp.write(&w.b)
	start := 0
	flush := func(end int) {
		if end > start {
			w.b.WriteString(`<w:t xml:space="preserve">` + escape(s[start:end]) + `</w:t>`)
		}
	}
	for i, c := range s {
		switch c {
		case '\n':
			flush(i)
			w.b.WriteString("<w:br/>")
			start = i + 1
		case '\t':
			flush(i)
			w.b.WriteString("<w:tab/>")
			start = i + 1
		case '\r':
			flush(i)
			start = i + 1
		}
	}
	flush(len(s))
	w.b.WriteString("</w:r>")
}

// hyperlink wraps runs in a link: within the page to a heading's bookmark,
// anywhere else by address.
func (w *writer) hyperlink(href string, runs func()) {
	if anchor, ok := strings.CutPrefix(href, "#"); ok {
		if name, found := w.anchors[anchor]; found {
			w.b.WriteString(`<w:hyperlink w:anchor="` + name + `" w:history="1">`)
			runs()
			w.b.WriteString("</w:hyperlink>")
			return
		}
		runs()
		return
	}
	target := w.absolute(href)
	if target == "" {
		runs()
		return
	}
	id := w.rel(relHyperlink, target, true)
	w.b.WriteString(`<w:hyperlink r:id="` + id + `" w:history="1">`)
	runs()
	w.b.WriteString("</w:hyperlink>")
}

// absolute reads a link within Stator against its address; one that cannot
// leave the page, without an address to read it against, is no link.
func (w *writer) absolute(href string) string {
	if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
		if w.read.BaseURL == "" {
			return ""
		}
		return strings.TrimSuffix(w.read.BaseURL, "/") + href
	}
	return href
}

// escape makes text safe in an element or an attribute; a character XML
// cannot carry becomes the replacement character.
func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
