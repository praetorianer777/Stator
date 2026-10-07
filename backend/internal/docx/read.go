package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Reading a Word document is the export's mirror: Word's structures are
// mapped onto the allowlist, and what a page cannot hold is a warning for
// the author. docs/word.md lists how each comes in.

const (
	// MaxPartBytes bounds one part of a document as unpacked, whatever its
	// zip directory claims; the text of a long book is a few megabytes.
	MaxPartBytes int64 = 32 << 20
	// MaxPictureBytes bounds the pictures one document brings, as MaxBytes
	// bounds what an export carries.
	MaxPictureBytes = MaxBytes
	// maxParts bounds the entries of one package.
	maxParts = 10000
	// maxNesting bounds tables, text boxes and notes inside each other;
	// deeper ones come in as their text.
	maxNesting = 8
	// maxWarnings bounds what one document reports.
	maxWarnings = 30
)

var (
	// ErrNotWord refuses a file that is no Word document at all.
	ErrNotWord = errors.New("it is not a Word document; save it from Word as a .docx and import it again")
	// ErrLegacyWord refuses the older binary format, and documents locked with
	// a password, which Word keeps in the same container.
	ErrLegacyWord = errors.New("it is a Word 97-2003 document or one protected by a password; open it in Word, save it as a .docx without a password, and import that")
)

// TooLargeError refuses a document past one of the reader's bounds.
type TooLargeError struct{ What string }

func (e *TooLargeError) Error() string { return e.What }

// ReadOptions bound what a document brings in.
type ReadOptions struct {
	// FileLimit is the largest picture kept; a larger one is left out with a
	// warning. Zero keeps every picture.
	FileLimit int64
}

// Picture is a picture of the document, to become a file of the new page.
type Picture struct {
	Name string
	Data []byte
}

// Converted is a document read as a page: its title, if the document names
// one, its body, the pictures the body shows and what did not come across.
type Converted struct {
	Title    string
	Body     document.Node
	Pictures []Picture
	Warnings []string
}

// PictureRef is the stand-in id an image of the body carries for the
// picture at index i, until Bind puts the file's id in its place.
func PictureRef(i int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1)
}

// Bind is the body with each picture's stand-in replaced by its file's id.
func (c *Converted) Bind(ids []uuid.UUID) document.Node {
	refs := map[string]string{}
	for i, id := range ids {
		refs[PictureRef(i)] = id.String()
	}
	var walk func(n document.Node) document.Node
	walk = func(n document.Node) document.Node {
		if n.Type == "image" {
			if id, ok := refs[stringAttr(n, "attachmentId")]; ok {
				attrs := map[string]any{}
				for k, v := range n.Attrs {
					attrs[k] = v
				}
				attrs["attachmentId"] = id
				n.Attrs = attrs
			}
		}
		if len(n.Content) > 0 {
			content := make([]document.Node, len(n.Content))
			for i, child := range n.Content {
				content[i] = walk(child)
			}
			n.Content = content
		}
		return n
	}
	return walk(c.Body)
}

// pkg is the package's parts by name, without regard to case as the format says.
type pkg struct {
	files map[string]*zip.File
}

func openPackage(data []byte) (*pkg, error) {
	if bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}) {
		return nil, ErrLegacyWord
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotWord
	}
	if len(r.File) > maxParts {
		return nil, ErrNotWord
	}
	p := &pkg{files: map[string]*zip.File{}}
	for _, f := range r.File {
		p.files[strings.ToLower(strings.TrimPrefix(f.Name, "/"))] = f
	}
	return p, nil
}

// read is a part's bytes, nil for one that is not there.
func (p *pkg) read(name string) ([]byte, error) {
	f := p.files[strings.ToLower(strings.TrimPrefix(name, "/"))]
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, ErrNotWord
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxPartBytes+1))
	if err != nil {
		return nil, ErrNotWord
	}
	if int64(len(data)) > MaxPartBytes {
		return nil, &TooLargeError{What: fmt.Sprintf("its part %s unpacks to more than %d MB; split the document into smaller ones", name, MaxPartBytes>>20)}
	}
	return data, nil
}

// xml is a part read as its tree, nil for one that is not there.
func (p *pkg) xml(name string) (*elem, error) {
	data, err := p.read(name)
	if err != nil || data == nil {
		return nil, err
	}
	root, err := parseXML(data)
	if err != nil {
		return nil, fmt.Errorf("%w (%s: %v)", ErrNotWord, name, err)
	}
	return root, nil
}

// rel is a relationship of a part: where it points, and whether outside the package.
type rel struct {
	kind, target string
	external     bool
}

// rels reads the relationships of a part, their targets made paths of the package.
func (p *pkg) rels(part string) (map[string]rel, error) {
	dir, base := path.Split(part)
	root, err := p.xml(dir + "_rels/" + base + ".rels")
	if err != nil || root == nil {
		return map[string]rel{}, err
	}
	out := map[string]rel{}
	for _, r := range root.children {
		if r.name != "Relationship" {
			continue
		}
		target := r.attr("Target")
		external := strings.EqualFold(r.attr("TargetMode"), "External")
		if !external {
			if strings.HasPrefix(target, "/") {
				target = strings.TrimPrefix(target, "/")
			} else {
				target = path.Join(dir, target)
			}
		}
		out[r.attr("Id")] = rel{kind: r.attr("Type"), target: target, external: external}
	}
	return out, nil
}

// mainPart is where the document's body is, as the package's own relationships say.
func (p *pkg) mainPart() (string, error) {
	rels, err := p.rels("")
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if strings.HasSuffix(r.kind, "/officeDocument") && !r.external {
			return r.target, nil
		}
	}
	if p.files["word/document.xml"] != nil {
		return "word/document.xml", nil
	}
	return "", ErrNotWord
}

// Read reads a .docx as a page. A document it cannot read is refused with
// an error saying why; everything else comes in, with warnings for the rest.
func Read(data []byte, opts ReadOptions) (*Converted, error) {
	p, err := openPackage(data)
	if err != nil {
		return nil, err
	}
	main, err := p.mainPart()
	if err != nil {
		return nil, err
	}
	root, err := p.xml(main)
	if err != nil {
		return nil, err
	}
	if root == nil || root.child("body") == nil {
		return nil, ErrNotWord
	}
	c := &conv{pkg: p, part: main, opts: opts, pictureAt: map[string]int{}, warned: map[string]bool{}, bookmarks: map[string]*rnode{}}
	if c.rels, err = p.rels(main); err != nil {
		return nil, err
	}
	if err := c.readParts(); err != nil {
		return nil, err
	}
	title := c.properties()
	content := c.blocks(root.child("body").children, 0)
	if c.err != nil {
		return nil, c.err
	}
	content = c.withNotes(content)
	out := &Converted{Pictures: c.pictures}
	out.Title, content = c.entitle(title, content)
	body := document.Node{Type: "doc", Content: c.finish(content)}
	if len(body.Content) == 0 {
		body.Content = []document.Node{{Type: "paragraph"}}
	}
	out.Body = document.Normalize(body)
	out.Warnings = c.warnings
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out, nil
}

// readParts reads the parts the body leans on: styles, numbering, notes, and
// whether there are comments, headers or footers to report.
func (c *conv) readParts() error {
	for _, r := range c.rels {
		if r.external {
			continue
		}
		kind := r.kind[strings.LastIndex(r.kind, "/")+1:]
		switch kind {
		case "styles", "numbering", "footnotes", "endnotes", "comments", "header", "footer":
		default:
			continue
		}
		root, err := c.pkg.xml(r.target)
		if err != nil {
			return err
		}
		if root == nil {
			continue
		}
		switch kind {
		case "styles":
			c.styles = readStyles(root)
		case "numbering":
			c.numbering = readNumbering(root)
		case "footnotes", "endnotes":
			if c.notes == nil {
				c.notes = map[string]*elem{}
			}
			for _, n := range root.children {
				// The separators Word draws above notes are notes of a type.
				if (n.name == "footnote" || n.name == "endnote") && n.attr("type") == "" {
					c.notes[n.name+":"+n.attr("id")] = n
				}
			}
		case "comments":
			if n := len(root.findAll("comment", nil)); n > 0 {
				c.warn(fmt.Sprintf("The document's comments (%d) were left out; they stay in the Word document.", n))
			}
		case "header", "footer":
			if strings.TrimSpace(root.textOf()) != "" {
				c.warn("The headers and footers were left out, since a page has none.")
			}
		}
	}
	if c.styles.byID == nil {
		c.styles = readStyles(nil)
	}
	if c.numbering.nums == nil {
		c.numbering = readNumbering(nil)
	}
	return nil
}

// properties reads the title the document's properties give, and whether
// Stator wrote it, whose line under the title is no part of the page.
func (c *conv) properties() string {
	if app, _ := c.pkg.xml("docProps/app.xml"); app != nil {
		c.fromStator = strings.TrimSpace(app.find("Application").text) == "Stator"
	}
	core, _ := c.pkg.xml("docProps/core.xml")
	if core == nil {
		return ""
	}
	return clean(core.find("title").text)
}

// entitle settles the page's title: the document's title property, else its
// Title paragraph, else a leading heading of level 1 when it is the only one,
// which then leaves the body and lifts the headings below it a level, as a
// Markdown import does. Empty is a title the file's name gives.
func (c *conv) entitle(property string, content []*rnode) (string, []*rnode) {
	title := property
	if title == "" {
		title = c.titlePara
	}
	if len(content) > 0 && content[0].typ == "heading" && intOf(content[0].attrs["level"]) == 1 {
		first := strings.TrimSpace(inlineText(content[0]))
		ones := 0
		for _, n := range content {
			if n.typ == "heading" && intOf(n.attrs["level"]) == 1 {
				ones++
			}
		}
		if ones == 1 && (title == "" || first == title) {
			if title == "" {
				title = first
			}
			content = content[1:]
			for _, n := range content {
				lift(n)
			}
		}
	}
	return clean(title), content
}

func lift(n *rnode) {
	if n.typ == "heading" {
		n.attrs["level"] = max(intOf(n.attrs["level"])-1, 1)
	}
	for _, c := range n.content {
		lift(c)
	}
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func (c *conv) warn(sentence string) {
	if c.warned[sentence] || len(c.warnings) >= maxWarnings {
		return
	}
	c.warned[sentence] = true
	c.warnings = append(c.warnings, sentence)
}

func (c *conv) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}
