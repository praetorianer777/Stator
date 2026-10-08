package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/example"
)

// A block or mark added to the allowlist has to be given a way into a Word
// document here, or be named as one its parent writes.
func TestEveryNodeAndMarkOfTheAllowlistHasAMapping(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(document.Allowed.Nodes)) {
		_, block := blockWriters[name]
		_, inline := inlineWriters[name]
		_, part := parts[name]
		if !block && !inline && !part {
			t.Errorf("the allowlist's %q has no mapping to Word; add it to blockWriters or inlineWriters, and to docs/word.md", name)
		}
	}
	for _, table := range []map[string]bool{keys(blockWriters), keys(inlineWriters), keys(parts)} {
		for name := range table {
			if _, ok := document.Allowed.Nodes[name]; !ok {
				t.Errorf("%q is mapped to Word but is no node of the allowlist any more", name)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(document.Allowed.Marks)) {
		_, styled := markStyles[name]
		_, plain := unstyled[name]
		if !styled && !plain {
			t.Errorf("the allowlist's mark %q has no mapping to Word; add it to markStyles, or to unstyled with why", name)
		}
	}
}

func keys[V any](m map[string]V) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// pictureBytes is a small PNG of the given size.
func pictureBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// opened is a written document's parts by name.
type opened map[string][]byte

func open(t *testing.T, data []byte) opened {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := opened{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = b
	}
	for name, b := range out {
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			wellFormed(t, name, b)
		}
	}
	return out
}

func wellFormed(t *testing.T, name string, b []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	depth := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("%s is not well formed: %v", name, err)
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 {
		t.Fatalf("%s leaves %d elements open", name, depth)
	}
}

func (o opened) doc() string { return string(o["word/document.xml"]) }

func doc(blocks ...any) document.Node {
	raw, _ := json.Marshal(map[string]any{"type": "doc", "content": blocks})
	n, err := document.Parse(raw)
	if err != nil {
		panic(err)
	}
	return n
}

func text(s string, marks ...map[string]any) map[string]any {
	n := map[string]any{"type": "text", "text": s}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

func para(content ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": content}
}

func node(typ string, attrs map[string]any, content ...any) map[string]any {
	n := map[string]any{"type": typ}
	if attrs != nil {
		n["attrs"] = attrs
	}
	if len(content) > 0 {
		n["content"] = content
	}
	return n
}

func write(t *testing.T, body document.Node, r Reader) opened {
	t.Helper()
	data, err := Bytes(context.Background(), Page{ID: uuid.New(), Title: "Guide", Language: "en", Body: body}, r)
	if err != nil {
		t.Fatal(err)
	}
	return open(t, data)
}

// The example space's showcase holds every block and mark the editor
// offers; all of it comes out as a document Word opens.
func TestTheShowcaseBecomesADocumentWithEveryBlock(t *testing.T) {
	for _, lang := range example.Languages {
		f := example.Facts{
			Lang: lang, Key: "STATOR", Me: example.Person{ID: uuid.New(), Name: "Ada"},
			Today: "2026-10-06", Soon: "2026-10-09", NextWeek: "2026-10-13",
			Pages: map[string]uuid.UUID{}, Excerpts: map[string]uuid.UUID{},
			Files:    map[string]uuid.UUID{example.CoverFile: uuid.New(), example.ImageFile: uuid.New(), example.BoardFile: uuid.New(), example.DataFile: uuid.New()},
			Calendar: uuid.New(), Armature: &example.ArmatureFacts{Project: "CP", Issue: "CP-4"},
		}
		example.Walk(func(e example.Entry) {
			f.Pages[e.Name] = uuid.New()
			f.Excerpts[e.Name] = uuid.New()
		})
		made, _, err := example.Render(example.Showcase, f)
		if err != nil {
			t.Fatal(err)
		}
		root, err := document.Parse(made.Body)
		if err != nil {
			t.Fatal(err)
		}
		picture := pictureBytes(t, 40, 20)
		included := 0
		data, err := Bytes(context.Background(), Page{ID: uuid.New(), Title: made.Title, Language: lang, Body: root}, Reader{
			Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(picture)), nil
			},
			Include: func(_ context.Context, id uuid.UUID, _ string, via []uuid.UUID) (*Included, error) {
				included++
				if len(via) != 1 {
					t.Errorf("an include of the page itself is read via %v", via)
				}
				return &Included{Title: "Elsewhere", URL: "https://stator.example/s/X/p/" + id.String(), Body: doc(para(text("Included words.")))}, nil
			},
			BaseURL: "https://stator.example",
		})
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		parts := open(t, data)
		body := parts.doc()
		for _, want := range []string{`w:pStyle w:val="Heading1"`, `w:pStyle w:val="Heading2"`, `<w:numPr>`, `<w:tbl>`, `<w:drawing>`, `w:pStyle w:val="Code"`, `w:pStyle w:val="Formula"`, "Included words.", `w:anchor="_Heading1"`} {
			if !strings.Contains(body, want) {
				t.Errorf("the %s showcase has no %s", lang, want)
			}
		}
		if included == 0 {
			t.Errorf("the %s showcase's include was never read", lang)
		}
	}
}

func TestHeadingsAreWordsOwnAndTheContentsLinkToThem(t *testing.T) {
	parts := write(t, doc(
		node("tableOfContents", map[string]any{"maxLevel": 2}),
		node("heading", map[string]any{"level": 1}, text("Setting up")),
		node("heading", map[string]any{"level": 2}, text("Install")),
		node("heading", map[string]any{"level": 3}, text("Deep")),
		para(text("back to the top", map[string]any{"type": "link", "attrs": map[string]any{"href": "#setting-up"}})),
	), Reader{})
	body := parts.doc()
	for _, want := range []string{
		`<w:pStyle w:val="Heading1"/></w:pPr><w:bookmarkStart w:id="0" w:name="_Heading1"/>`,
		`<w:pStyle w:val="Heading3"/>`,
		`<w:pStyle w:val="TOC2"/></w:pPr><w:hyperlink w:anchor="_Heading2"`,
		`<w:hyperlink w:anchor="_Heading1" w:history="1"><w:r><w:rPr><w:rStyle w:val="Hyperlink"/>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	if strings.Contains(body, `w:anchor="_Heading3"`) {
		t.Error("the contents list a heading below their level")
	}
	styles := string(parts["word/styles.xml"])
	if !strings.Contains(styles, `<w:name w:val="heading 1"/>`) || !strings.Contains(styles, `<w:outlineLvl w:val="0"/>`) {
		t.Error("the heading styles are not Word's own, which its navigation pane reads")
	}
}

func TestListsNestAndEachNumberedListCountsFromItsStart(t *testing.T) {
	item := func(content ...any) map[string]any { return node("listItem", nil, content...) }
	parts := write(t, doc(
		node("bulletList", nil,
			item(para(text("Outer")), node("orderedList", map[string]any{"start": 3, "type": "a"}, item(para(text("Inner"))))),
		),
		node("taskList", nil,
			node("taskItem", map[string]any{"checked": true}, para(text("Done"))),
			node("taskItem", map[string]any{"checked": false}, para(text("Open"))),
		),
	), Reader{})
	body := parts.doc()
	if !strings.Contains(body, `<w:ilvl w:val="0"/><w:numId w:val="1"/>`) || !strings.Contains(body, `<w:ilvl w:val="1"/><w:numId w:val="2"/>`) {
		t.Errorf("the nested list is not one level in: %s", body)
	}
	numbering := string(parts["word/numbering.xml"])
	if !strings.Contains(numbering, `<w:num w:numId="2"><w:abstractNumId w:val="2"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="3"/>`) {
		t.Errorf("the lettered list does not count from 3: %s", numbering)
	}
	if !strings.Contains(body, checkedBox+"</w:t><w:tab/>") || !strings.Contains(body, uncheckedBox+"</w:t><w:tab/>") {
		t.Error("the tasks have no boxes")
	}
}

func TestTablesKeepTheirHeaderMergesAndBackgrounds(t *testing.T) {
	cell := func(typ string, attrs map[string]any, s string) map[string]any {
		return node(typ, attrs, para(text(s)))
	}
	parts := write(t, doc(node("table", nil,
		node("tableRow", nil, cell("tableHeader", nil, "Name"), cell("tableHeader", map[string]any{"colspan": 2}, "Both")),
		node("tableRow", nil, cell("tableCell", map[string]any{"rowspan": 2, "background": "warning"}, "Tall"), cell("tableCell", nil, "b"), cell("tableCell", nil, "c")),
		node("tableRow", nil, cell("tableCell", nil, "e"), cell("tableCell", map[string]any{"align": "right"}, "f")),
	)), Reader{})
	body := parts.doc()
	for _, want := range []string{
		`<w:trPr><w:tblHeader/></w:trPr>`,
		`<w:gridSpan w:val="2"/>`,
		`<w:vMerge w:val="restart"/><w:shd w:val="clear" w:color="auto" w:fill="FEF3C7"/>`,
		`<w:vMerge/>`,
		`<w:jc w:val="right"/>`,
		`<w:b/><w:bCs/></w:rPr><w:t xml:space="preserve">Name</w:t>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the table lacks %s", want)
		}
	}
	if n := strings.Count(body, "<w:gridCol "); n != 3 {
		t.Errorf("the table has %d columns, want 3", n)
	}
	for i, row := range strings.Split(body, "<w:tr>")[1:] {
		if n := strings.Count(row, "<w:tc>"); n != 3 && !(i == 0 && n == 2) {
			t.Errorf("row %d has %d cells", i+1, n)
		}
	}
}

func TestPicturesAreEmbeddedOnceAndWhatCannotBeReadIsSaid(t *testing.T) {
	shown, hidden := uuid.New(), uuid.New()
	reads := 0
	picture := pictureBytes(t, 4000, 1000)
	parts := write(t, doc(
		node("image", map[string]any{"attachmentId": shown.String(), "alt": "A wide chart"}),
		node(document.NodeGallery, map[string]any{"columns": 2},
			node(document.NodeGalleryImage, map[string]any{"attachmentId": shown.String(), "caption": "Again"}),
			node(document.NodeGalleryImage, map[string]any{"attachmentId": hidden.String()}),
		),
		node("image", map[string]any{"attachmentId": hidden.String(), "alt": "Secret plan"}),
	), Reader{Picture: func(_ context.Context, id uuid.UUID) (io.ReadCloser, error) {
		reads++
		if id == hidden {
			return nil, nil
		}
		return io.NopCloser(bytes.NewReader(picture)), nil
	}})
	if len(parts["word/media/picture1.png"]) == 0 || parts["word/media/picture2.png"] != nil {
		t.Error("the picture is not carried exactly once")
	}
	if reads != 2 {
		t.Errorf("the files were read %d times, want once each", reads)
	}
	body := parts.doc()
	if !strings.Contains(body, `descr="A wide chart"`) || !strings.Contains(body, "Again") {
		t.Error("the pictures lost their descriptions")
	}
	ext := regexp.MustCompile(`<wp:extent cx="(\d+)"`).FindStringSubmatch(body)
	if ext == nil || ext[1] != "5731510" {
		t.Errorf("the wide picture is %v EMU wide, want the text's width", ext)
	}
	if !strings.Contains(body, "A picture could not be included: Secret plan") || !strings.Contains(body, "One picture of this gallery could not be included.") {
		t.Error("the pictures left out are not said")
	}
	if !strings.Contains(string(parts["[Content_Types].xml"]), `<Default Extension="png" ContentType="image/png"/>`) {
		t.Error("the content types do not name PNG")
	}
}

func TestTooManyPicturesAreRefusedAndAFailedReadFails(t *testing.T) {
	body := doc(node("image", map[string]any{"attachmentId": uuid.NewString()}))
	big := Reader{Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) {
		return io.NopCloser(io.LimitReader(zeros{}, MaxBytes+1)), nil
	}}
	if _, err := Bytes(context.Background(), Page{Title: "Big", Body: body}, big); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a picture over the limit gave %v", err)
	}
	broken := errors.New("the store is down")
	failing := Reader{Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) { return nil, broken }}
	if _, err := Bytes(context.Background(), Page{Title: "Down", Body: body}, failing); !errors.Is(err, broken) {
		t.Errorf("a failed read gave %v", err)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestMarksAndLinksBecomeRunsAndHyperlinks(t *testing.T) {
	mark := func(typ string) map[string]any { return map[string]any{"type": typ} }
	link := func(href string) map[string]any {
		return map[string]any{"type": "link", "attrs": map[string]any{"href": href}}
	}
	parts := write(t, doc(para(
		text("bold", mark("bold")), text("italic", mark("italic")), text("gone", mark("strike")), text("x := 1", mark("code")),
		text("home", link("/s/DOC/p/1")), text(" page", link("/s/DOC/p/1"), mark("bold")),
		text("site", link("https://example.com/a?b=1&c=2")),
		node("mention", map[string]any{"id": uuid.NewString(), "label": "Ada"}),
		node(document.NodeStatus, map[string]any{"label": "Ready", "color": "success"}),
		node(document.NodeDate, map[string]any{"date": "2026-10-07"}),
		node(document.NodeMathInline, map[string]any{"latex": `e^{i\pi}`}),
		node("hardBreak", nil),
		text("<&> \"quoted\""),
	)), Reader{BaseURL: "https://stator.example/"})
	body := parts.doc()
	for _, want := range []string{
		`<w:b/><w:bCs/></w:rPr><w:t xml:space="preserve">bold</w:t>`,
		`<w:i/><w:iCs/></w:rPr><w:t xml:space="preserve">italic</w:t>`,
		`<w:strike/></w:rPr><w:t xml:space="preserve">gone</w:t>`,
		`<w:rStyle w:val="InlineCode"/></w:rPr><w:t xml:space="preserve">x := 1</w:t>`,
		`@Ada`, `October 7, 2026`, `e^{i\pi}`, `<w:br/>`, `&lt;&amp;&gt; &#34;quoted&#34;`,
		`<w:caps/>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	if n := strings.Count(body, "<w:hyperlink "); n != 2 {
		t.Errorf("%d hyperlinks, want one per run of a link", n)
	}
	rels := string(parts["word/_rels/document.xml.rels"])
	for _, want := range []string{`Target="https://stator.example/s/DOC/p/1" TargetMode="External"`, `Target="https://example.com/a?b=1&amp;c=2"`} {
		if !strings.Contains(rels, want) {
			t.Errorf("the relationships lack %s: %s", want, rels)
		}
	}
}

func TestBlocksWithoutWordsOfTheirOwnAreSaidInTheReadersLanguage(t *testing.T) {
	body := doc(
		node(document.NodeDiagram, map[string]any{"source": "graph TD; A --> B"}),
		node(document.NodeSketch, map[string]any{"scene": `{"elements":[]}`, "drawing": nil, "title": "Ablauf"}),
		node(document.NodeTaskReport, map[string]any{"space": "DOC", "due": "any", "state": "open"}),
		node(document.NodeInclude, map[string]any{"pageId": uuid.NewString()}),
		node("panel", map[string]any{"kind": "warning"}, para(text("Careful"))),
		node(document.NodeDecision, map[string]any{"state": "decided"}, text("Ship it")),
	)
	data, err := Bytes(context.Background(), Page{Title: "Seite", Language: "de", Body: body}, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	parts := open(t, data)
	got := parts.doc()
	for _, want := range []string{"Ein Diagramm", "graph TD; A --&gt; B", "Ein Bericht über Aufgaben in DOC.", "Hier zeigt Stator eine andere Seite", "Warnung", "Entschieden: ", "Eine Skizze, die Stator zeichnet: Ablauf."} {
		if !strings.Contains(got, want) {
			t.Errorf("the German document lacks %q", want)
		}
	}
	if !strings.Contains(string(parts["word/styles.xml"]), `<w:lang w:val="de-DE"/>`) {
		t.Error("Word would check the German document's spelling in another language")
	}
}

func TestAnIncludeTheReaderMayNotReadIsASentence(t *testing.T) {
	missing := uuid.New()
	parts := write(t, doc(node(document.NodeInclude, map[string]any{"pageId": missing.String()})), Reader{
		Include: func(context.Context, uuid.UUID, string, []uuid.UUID) (*Included, error) { return nil, nil },
	})
	if !strings.Contains(parts.doc(), "Another page is shown here to those who may read it.") {
		t.Error("an include the reader may not read is not said")
	}
}

func TestThePropertiesNameTheTitleAuthorAndDays(t *testing.T) {
	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	modified := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	data, err := Bytes(context.Background(), Page{
		Title: "Runbook <ops>", Space: "Operations", Author: "Ada", Editor: "Grace", Version: 4,
		Created: created, Modified: modified, Language: "en", URL: "https://stator.example/s/OPS/p/1", Body: doc(),
	}, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	parts := open(t, data)
	core := string(parts["docProps/core.xml"])
	for _, want := range []string{
		"<dc:title>Runbook &lt;ops&gt;</dc:title>", "<dc:creator>Ada</dc:creator>", "<cp:lastModifiedBy>Grace</cp:lastModifiedBy>",
		`<dcterms:created xsi:type="dcterms:W3CDTF">2026-09-01T08:00:00Z</dcterms:created>`,
		`<dcterms:modified xsi:type="dcterms:W3CDTF">2026-10-07T09:30:00Z</dcterms:modified>`,
		"<cp:revision>4</cp:revision>", "<dc:subject>Operations</dc:subject>",
	} {
		if !strings.Contains(core, want) {
			t.Errorf("core.xml lacks %s", want)
		}
	}
	if !strings.Contains(parts.doc(), "Operations · Version 4 of October 7, 2026") {
		t.Error("the line under the title does not say the space, version and day")
	}
	anonymous, err := Bytes(context.Background(), Page{Title: "Public", Body: doc()}, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	if core := string(open(t, anonymous)["docProps/core.xml"]); strings.Contains(core, "creator") || strings.Contains(core, "lastModifiedBy") {
		t.Errorf("a document for anybody names its writers: %s", core)
	}
}

func TestADocumentIsNamedAfterItsSpacePageAndDay(t *testing.T) {
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if got := FileName("DOCS", "Setting up, again", day); got != "DOCS-setting-up-again-2026-10-07.docx" {
		t.Errorf("got %q", got)
	}
	if got := FileName("", "Guide", day); got != "guide-2026-10-07.docx" {
		t.Errorf("a link's document is named %q", got)
	}
}
