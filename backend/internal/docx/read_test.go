package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// fixture is a Word document made in the test, part by part, so the suite
// carries no files from other programs.
type fixture struct {
	body      string
	styles    string
	numbering string
	footnotes string
	comments  string
	header    string
	title     string
	app       string
	// rels are more relationships of the document, from rId10 on.
	rels  []string
	media map[string][]byte
}

const fixtureNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml"`

func (f fixture) bytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, content string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, content)
	}
	put("[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)
	put("_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	put("word/document.xml", `<?xml version="1.0"?><w:document `+fixtureNS+`><w:body>`+f.body+`</w:body></w:document>`)
	rels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	part := func(id, kind, name, content, root string) {
		if content == "" {
			return
		}
		rels += `<Relationship Id="` + id + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/` + kind + `" Target="` + name + `"/>`
		put("word/"+name, `<?xml version="1.0"?><w:`+root+` `+fixtureNS+`>`+content+`</w:`+root+`>`)
	}
	part("rId1", "styles", "styles.xml", f.styles, "styles")
	part("rId2", "numbering", "numbering.xml", f.numbering, "numbering")
	part("rId3", "footnotes", "footnotes.xml", f.footnotes, "footnotes")
	part("rId4", "comments", "comments.xml", f.comments, "comments")
	part("rId5", "header", "header1.xml", f.header, "hdr")
	for _, r := range f.rels {
		rels += r
	}
	rels += `</Relationships>`
	put("word/_rels/document.xml.rels", rels)
	for name, data := range f.media {
		w, err := z.Create("word/media/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	if f.title != "" {
		put("docProps/core.xml", `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>`+f.title+`</dc:title></cp:coreProperties>`)
	}
	if f.app != "" {
		put("docProps/app.xml", `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>`+f.app+`</Application></Properties>`)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// read reads a fixture and holds what comes out to the allowlist, as a page's save would.
func read(t *testing.T, f fixture) *Converted {
	t.Helper()
	got, err := Read(f.bytes(t), ReadOptions{})
	if err != nil {
		t.Fatalf("the document was refused: %v", err)
	}
	valid(t, got.Body)
	return got
}

func valid(t *testing.T, body document.Node) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.Validate(raw); err != nil {
		t.Fatalf("the page is refused by the allowlist: %v\n%s", err, raw)
	}
}

// same fails unless the body is the blocks wanted, compared as JSON.
func same(t *testing.T, got document.Node, want ...any) {
	t.Helper()
	g, _ := json.Marshal(got.Content)
	w, _ := json.Marshal(doc(want...).Content)
	if string(g) != string(w) {
		t.Errorf("the page reads\n%s\nwant\n%s", g, w)
	}
}

func hasWarning(t *testing.T, got *Converted, part string) {
	t.Helper()
	for _, w := range got.Warnings {
		if strings.Contains(w, part) {
			return
		}
	}
	t.Errorf("no warning says %q: %q", part, got.Warnings)
}

func wp(props, runs string) string {
	if props != "" {
		props = "<w:pPr>" + props + "</w:pPr>"
	}
	return "<w:p>" + props + runs + "</w:p>"
}

func wr(props, words string) string {
	if props != "" {
		props = "<w:rPr>" + props + "</w:rPr>"
	}
	return `<w:r>` + props + `<w:t xml:space="preserve">` + words + `</w:t></w:r>`
}

func styled(id string) string { return `<w:pStyle w:val="` + id + `"/>` }

const headingStyles = `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Heading1"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Ueberschrift5"><w:name w:val="Ueberschrift 5"/><w:pPr><w:outlineLvl w:val="4"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Zitat"><w:name w:val="Quote"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="SourceCode"><w:name w:val="Source Code"/></w:style>` +
	`<w:style w:type="character" w:styleId="Strong"><w:name w:val="Strong"/><w:rPr><w:b/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:u w:val="single"/></w:rPr></w:style>`

func TestHeadingsComeFromOutlineLevelsAndTheTitleFromTheTitleParagraph(t *testing.T) {
	got := read(t, fixture{styles: headingStyles, body: wp(styled("Title"), wr("", "Release plan")) +
		wp(styled("Heading1"), wr("", "Goals")) +
		wp("", wr("", "Ship it.")) +
		wp(styled("Heading2"), wr("", "Dates")) +
		wp(styled("Ueberschrift5"), wr("", "Fine print")) +
		wp(`<w:outlineLvl w:val="1"/>`, wr("", "Set by hand")) +
		wp(styled("Heading1"), wr("", "Risks"))})
	if got.Title != "Release plan" {
		t.Errorf("the title is %q", got.Title)
	}
	h := func(level int, words string) map[string]any {
		return node("heading", map[string]any{"level": level}, text(words))
	}
	same(t, got.Body, h(1, "Goals"), para(text("Ship it.")), h(2, "Dates"), h(3, "Fine print"), h(2, "Set by hand"), h(1, "Risks"))
	hasWarning(t, got, "below level 3")
}

func TestAPropertyTitleWinsAndALoneLeadingHeadingBecomesTheTitle(t *testing.T) {
	got := read(t, fixture{styles: headingStyles, title: "  From   properties ", body: wp(styled("Heading1"), wr("", "Body"))})
	if got.Title != "From properties" {
		t.Errorf("the title is %q", got.Title)
	}
	same(t, got.Body, node("heading", map[string]any{"level": 1}, text("Body")))

	got = read(t, fixture{styles: headingStyles, body: wp(styled("Heading1"), wr("", "Handbook")) + wp(styled("Heading2"), wr("", "Start")) + wp("", wr("", "Words."))})
	if got.Title != "Handbook" {
		t.Errorf("the title is %q", got.Title)
	}
	same(t, got.Body, node("heading", map[string]any{"level": 1}, text("Start")), para(text("Words.")))

	got = read(t, fixture{body: wp("", wr("", "No title here."))})
	if got.Title != "" {
		t.Errorf("a document without a title is titled %q", got.Title)
	}
}

func TestRunsKeepTheirMarksAndLinks(t *testing.T) {
	got := read(t, fixture{
		styles: headingStyles,
		rels:   []string{`<Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/a" TargetMode="External"/>`},
		body: wp("",
			wr("<w:b/>", "bold ")+
				wr(`<w:rStyle w:val="Strong"/><w:i/>`, "both")+
				wr(`<w:rStyle w:val="Strong"/><w:b w:val="0"/>`, " plain")+
				wr("<w:strike/>", " gone")+
				wr(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/>`, "x := 1")+
				`<w:r><w:br/><w:t>next</w:t><w:br w:type="page"/><w:tab/><w:t>line</w:t></w:r>`+
				`<w:hyperlink r:id="rId10">`+wr(`<w:rStyle w:val="Hyperlink"/>`, "a link")+`</w:hyperlink>`+
				wr(`<w:u w:val="single"/>`, " under")+
				wr(`<w:vertAlign w:val="superscript"/>`, "2")+
				wr(`<w:vanish/>`, "secret")),
	})
	link := map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/a"}}
	bold, italic, strike, code := mark("bold"), mark("italic"), mark("strike"), mark("code")
	same(t, got.Body, para(text("bold ", bold), text("both", bold, italic), text(" plain"), text(" gone", strike), text("x := 1", code),
		node("hardBreak", nil), text("next line"), text("a link", link), text(" under2")))
	hasWarning(t, got, "underline")
	hasWarning(t, got, "Superscript")
	hasWarning(t, got, "Hidden text")
}

func mark(t string) map[string]any { return map[string]any{"type": t} }

func TestAFieldHyperlinkAndAnAnchorToAHeadingAreLinks(t *testing.T) {
	got := read(t, fixture{styles: headingStyles, body: wp(styled("Heading1"), `<w:bookmarkStart w:id="0" w:name="_Toc1"/>`+wr("", "Getting started")+`<w:bookmarkEnd w:id="0"/>`) +
		wp(styled("Heading1"), wr("", "Second")) +
		wp("", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> HYPERLINK "https://example.com/f" </w:instrText></w:r>`+
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`+wr("", "field")+`<w:r><w:fldChar w:fldCharType="end"/></w:r>`+
			wr("", " and ")+`<w:hyperlink w:anchor="_Toc1">`+wr("", "back")+`</w:hyperlink>`+
			wr("", " and ")+`<w:hyperlink w:anchor="nowhere">`+wr("", "lost")+`</w:hyperlink>`)})
	field := map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/f"}}
	back := map[string]any{"type": "link", "attrs": map[string]any{"href": "#getting-started"}}
	h := func(words string) map[string]any { return node("heading", map[string]any{"level": 1}, text(words)) }
	same(t, got.Body, h("Getting started"), h("Second"), para(text("field", field), text(" and "), text("back", back), text(" and lost")))
	hasWarning(t, got, "other than its headings")
}

const listNumbering = `<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
	`<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>` +
	`<w:num w:numId="3"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="5"/></w:lvlOverride></w:num>`

func item(n int, level int, words string) string {
	return wp(`<w:numPr><w:ilvl w:val="`+itoa(level)+`"/><w:numId w:val="`+itoa(n)+`"/></w:numPr>`, wr("", words))
}

func TestListsNestByLevelAndCountOnAcrossABreak(t *testing.T) {
	got := read(t, fixture{numbering: listNumbering, body: item(1, 0, "Apples") +
		item(1, 1, "Red") + item(1, 1, "Green") +
		wp(`<w:ind w:left="720"/>`, wr("", "More on apples.")) +
		item(1, 0, "Pears") +
		wp("", "") +
		item(2, 0, "First") + item(2, 0, "Second") +
		wp("", wr("", "A break.")) +
		item(2, 0, "Third") +
		item(3, 0, "Fifth")})
	li := func(content ...any) map[string]any { return node("listItem", nil, content...) }
	same(t, got.Body,
		node("bulletList", nil,
			li(para(text("Apples")),
				node("orderedList", map[string]any{"start": 1, "type": "a"}, li(para(text("Red"))), li(para(text("Green")))),
				para(text("More on apples."))),
			li(para(text("Pears")))),
		node("orderedList", map[string]any{"start": 1}, li(para(text("First"))), li(para(text("Second")))),
		para(text("A break.")),
		node("orderedList", map[string]any{"start": 3}, li(para(text("Third")))),
		node("orderedList", map[string]any{"start": 5}, li(para(text("Fifth")))))
}

func TestCheckBoxesAreTasks(t *testing.T) {
	got := read(t, fixture{body: wp(`<w:ind w:left="720" w:hanging="360"/>`, wr("", "☐")+`<w:r><w:tab/></w:r>`+wr("", "Write")) +
		wp(`<w:ind w:left="720" w:hanging="360"/>`, wr("", "☒ Review"))})
	task := func(done bool, words string) map[string]any {
		return node("taskItem", map[string]any{"checked": done}, para(text(words)))
	}
	same(t, got.Body, node("taskList", nil, task(false, "Write"), task(true, "Review")))
}

func cell(props, content string) string {
	return "<w:tc><w:tcPr>" + props + "</w:tcPr>" + content + "</w:tc>"
}

func TestTablesKeepHeadersAndMergedCells(t *testing.T) {
	got := read(t, fixture{body: `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol/><w:gridCol/><w:gridCol/></w:tblGrid>` +
		`<w:tr><w:trPr><w:tblHeader/></w:trPr>` + cell("", wp("", wr("<w:b/>", "Name"))) + cell(`<w:gridSpan w:val="2"/>`, wp("", wr("", "Dates"))) + `</w:tr>` +
		`<w:tr>` + cell(`<w:vMerge w:val="restart"/>`, wp("", wr("", "Ada"))) + cell(`<w:shd w:val="clear" w:fill="dcfce7"/>`, wp(`<w:jc w:val="center"/>`, wr("", "1815"))) + cell("", wp("", "")) + `</w:tr>` +
		`<w:tr>` + cell(`<w:vMerge/>`, wp("", "")) + cell("", wp("", wr("", "1852"))) + cell("", "") + `</w:tr>` +
		`</w:tbl>` +
		`<w:tbl><w:tr>` + cell("", wp("", wr("", "In a box."))) + `</w:tr></w:tbl>`})
	c := func(typ string, attrs map[string]any, content ...any) map[string]any {
		base := map[string]any{"colspan": 1, "rowspan": 1}
		for k, v := range attrs {
			base[k] = v
		}
		return node(typ, base, content...)
	}
	empty := map[string]any{"type": "paragraph"}
	same(t, got.Body,
		node("table", nil,
			node("tableRow", nil, c("tableHeader", nil, para(text("Name"))), c("tableHeader", map[string]any{"colspan": 2}, para(text("Dates")))),
			node("tableRow", nil, c("tableCell", map[string]any{"rowspan": 2}, para(text("Ada"))), c("tableCell", map[string]any{"background": "success", "align": "center"}, para(text("1815"))), c("tableCell", nil, empty)),
			node("tableRow", nil, c("tableCell", nil, para(text("1852"))), c("tableCell", nil, empty))),
		node("blockquote", nil, para(text("In a box."))))
}

func drawing(rid, descr string, cx int) string {
	return `<w:r><w:drawing><wp:inline><wp:extent cx="` + itoa(cx) + `" cy="9525"/><wp:docPr id="1" name="Picture 1" descr="` + descr + `"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:blipFill><a:blip r:embed="` + rid + `"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}

func TestPicturesBecomeFilesOfThePageWithTheirDescriptions(t *testing.T) {
	small := pictureBytes(t, 40, 20)
	got := read(t, fixture{
		rels: []string{
			`<Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>`,
			`<Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image2.emf"/>`,
		},
		media: map[string][]byte{"image1.png": small, "image2.emf": []byte("not a picture a browser shows")},
		body: wp("", wr("", "Before ")+drawing("rId10", "A red line", 40*emuPerPixel)+wr("", " after")) +
			wp("", drawing("rId10", "", 20*emuPerPixel)) +
			wp("", drawing("rId11", "Vector", 9525)),
	})
	if len(got.Pictures) != 1 || got.Pictures[0].Name != "image1.png" || !bytes.Equal(got.Pictures[0].Data, small) {
		t.Fatalf("the pictures are %+v", got.Pictures)
	}
	ref := PictureRef(0)
	same(t, got.Body, para(text("Before ")), node("image", map[string]any{"attachmentId": ref, "alt": "A red line"}), para(text(" after")),
		node("image", map[string]any{"attachmentId": ref, "width": 20}))
	hasWarning(t, got, "(EMF)")
	id := uuid.New()
	bound, _ := json.Marshal(got.Bind([]uuid.UUID{id}))
	if strings.Contains(string(bound), ref) || !strings.Contains(string(bound), id.String()) {
		t.Errorf("binding the files leaves %s", bound)
	}
}

func TestPicturesPastTheLimitsAreRefusedOrLeftOut(t *testing.T) {
	f := fixture{
		rels:  []string{`<Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/big.png"/>`},
		media: map[string][]byte{"big.png": pictureBytes(t, 400, 400)},
		body:  wp("", drawing("rId10", "Big", 9525)),
	}
	got, err := Read(f.bytes(t), ReadOptions{FileLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pictures) != 0 {
		t.Errorf("a picture over the file limit came in")
	}
	hasWarning(t, got, "larger than a file")
}

func TestNotesCommentsChangesAndBoxesAreKeptOrSaid(t *testing.T) {
	got := read(t, fixture{
		footnotes: `<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
			`<w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r>` + wr("", "The source.") + `</w:p></w:footnote>`,
		comments: `<w:comment w:id="0"><w:p>` + wr("", "Check this") + `</w:p></w:comment>`,
		header:   wp("", wr("", "Company confidential")),
		body: wp("", wr("", "Claim")+`<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="1"/></w:r>`+
			`<w:commentRangeStart w:id="0"/>`+`<w:ins w:id="1" w:author="A">`+wr("", " added")+`</w:ins>`+`<w:del w:id="2" w:author="A"><w:r><w:delText>removed</w:delText></w:r></w:del>`+`<w:commentRangeEnd w:id="0"/>`) +
			wp("", `<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:anchor><wp:extent cx="1" cy="1"/><wp:docPr id="2" name="Box"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp><wps:txbx><w:txbxContent>`+
				wp("", wr("", "Boxed words"))+`</w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:anchor></w:drawing></mc:Choice>`+
				`<mc:Fallback><w:pict><v:shape><v:textbox><w:txbxContent>`+wp("", wr("", "Boxed words"))+`</w:txbxContent></v:textbox></v:shape></w:pict></mc:Fallback></mc:AlternateContent></w:r>`) +
			wp("", `<w:r><w:drawing><wp:inline><wp:extent cx="1" cy="1"/><wp:docPr id="3" name="Chart"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"/></a:graphic></wp:inline></w:drawing></w:r>`),
	})
	same(t, got.Body, para(text("Claim[1] added")), para(text("Boxed words")), node("horizontalRule", nil),
		node("orderedList", map[string]any{"start": 1}, node("listItem", nil, para(text("The source.")))))
	for _, w := range []string{"comments (1)", "Tracked changes were accepted", "Text boxes", "Charts", "headers and footers"} {
		hasWarning(t, got, w)
	}
}

func TestCodeAndQuoteParagraphsJoin(t *testing.T) {
	got := read(t, fixture{styles: headingStyles, body: wp(styled("SourceCode"), wr("", "func main() {")) +
		wp(styled("SourceCode"), `<w:r><w:tab/><w:t>run()</w:t></w:r>`) +
		wp(styled("SourceCode"), wr("", "}")) +
		wp(styled("Zitat"), wr("", "To be.")) +
		wp(styled("Zitat"), wr("", "Or not.")) +
		wp(`<w:pBdr><w:bottom w:val="single" w:sz="6"/></w:pBdr>`, "")})
	same(t, got.Body, node("codeBlock", nil, text("func main() {\n\trun()\n}")),
		node("blockquote", nil, para(text("To be.")), para(text("Or not."))), node("horizontalRule", nil))
}

func TestWhatIsNoWordDocumentIsRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		"text":   []byte("just words"),
		"zip":    zipOf(t, map[string]string{"notes.txt": "hello"}),
		"broken": zipOf(t, map[string]string{"word/document.xml": "<w:document><w:body><w:p>"}),
	} {
		if _, err := Read(data, ReadOptions{}); !errors.Is(err, ErrNotWord) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Read([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, ReadOptions{}); !errors.Is(err, ErrLegacyWord) {
		t.Errorf("a .doc: %v", err)
	}
	deep := strings.Repeat("<w:sdt><w:sdtContent>", 150) + wp("", wr("", "x")) + strings.Repeat("</w:sdtContent></w:sdt>", 150)
	if _, err := Read(fixture{body: deep}.bytes(t), ReadOptions{}); !errors.Is(err, ErrNotWord) {
		t.Errorf("a document nested past every bound: %v", err)
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := z.Create(name)
		_, _ = io.WriteString(w, content)
	}
	_ = z.Close()
	return buf.Bytes()
}

// A page exported to Word and imported again is the page it was, for the
// blocks Word carries.
func TestAnExportedPageImportsBackAsItWas(t *testing.T) {
	pictureID := uuid.New()
	picture := pictureBytes(t, 40, 20)
	cellAttrs := func(extra map[string]any) map[string]any {
		out := map[string]any{"colspan": 1, "rowspan": 1}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	link := map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/docs"}}
	back := map[string]any{"type": "link", "attrs": map[string]any{"href": "#overview"}}
	li := func(content ...any) map[string]any { return node("listItem", nil, content...) }
	blocks := []any{
		node("tableOfContents", map[string]any{"maxLevel": 3}),
		node("heading", map[string]any{"level": 1}, text("Overview")),
		para(text("Plain, "), text("bold", mark("bold")), text(" and ", mark("bold"), mark("italic")), text("struck", mark("strike")),
			text(" with "), text("code()", mark("code")), text(", a "), text("link", link), node("hardBreak", nil), text("and a second line.")),
		node("heading", map[string]any{"level": 2}, text("Steps")),
		node("bulletList", nil,
			li(para(text("One")), node("orderedList", map[string]any{"start": 1}, li(para(text("first"))), li(para(text("second"))))),
			li(para(text("Two")), para(text("More about two.")))),
		node("orderedList", map[string]any{"start": 3, "type": "a"}, li(para(text("third"))), li(para(text("fourth")))),
		node("taskList", nil, node("taskItem", map[string]any{"checked": true}, para(text("Done"))), node("taskItem", map[string]any{"checked": false}, para(text("Open")))),
		node("codeBlock", nil, text("make test\n  and check")),
		node("horizontalRule", nil),
		node("table", nil,
			node("tableRow", nil, node("tableHeader", cellAttrs(nil), para(text("Name"))), node("tableHeader", cellAttrs(nil), para(text("Role")))),
			node("tableRow", nil, node("tableCell", cellAttrs(map[string]any{"rowspan": 2}), para(text("Ada"))), node("tableCell", cellAttrs(map[string]any{"background": "accent"}), para(text("Author")))),
			node("tableRow", nil, node("tableCell", cellAttrs(nil), para(text("Reviewer")))),
			node("tableRow", nil, node("tableCell", cellAttrs(map[string]any{"colspan": 2, "align": "center"}), para(text("Both"))))),
		node("image", map[string]any{"attachmentId": pictureID.String(), "alt": "A red line"}),
		node("blockquote", nil, para(text("Quoted."))),
		node("panel", map[string]any{"kind": "warning"}, para(text("Careful."))),
		node("heading", map[string]any{"level": 3}, text("End")),
		para(text("Back to the "), text("overview", back), text(".")),
	}
	page := doc(blocks...)
	data, err := Bytes(context.Background(), Page{ID: uuid.New(), Title: "Guide", Space: "Docs", Version: 3, Language: "en", URL: "https://stator.example/s/DOCS/p/1/guide", Body: page},
		Reader{Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(picture)), nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(data, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Guide" {
		t.Errorf("the title came back as %q", got.Title)
	}
	if len(got.Pictures) != 1 || !bytes.Equal(got.Pictures[0].Data, picture) {
		t.Fatalf("the picture came back as %d pictures", len(got.Pictures))
	}
	if len(got.Warnings) != 0 {
		t.Errorf("an exported page warns %q", got.Warnings)
	}
	body := got.Bind([]uuid.UUID{pictureID})
	valid(t, body)
	same(t, body, blocks...)
}

func TestTheReaderNeverPanicsOnBrokenParts(t *testing.T) {
	bodies := []string{
		`<w:tbl/>`,
		`<w:tbl><w:tr><w:tc/></w:tr></w:tbl>`,
		`<w:tbl><w:tr>` + cell(`<w:vMerge/>`, "") + `</w:tr></w:tbl>`,
		wp(`<w:numPr><w:numId w:val="9"/></w:numPr>`, wr("", "unknown list")),
		wp("", `<w:hyperlink r:id="missing">`+wr("", "x")+`</w:hyperlink>`),
		wp("", `<w:r><w:fldChar w:fldCharType="end"/><w:fldChar w:fldCharType="separate"/></w:r>`),
		wp("", `<w:r><w:drawing/></w:r><w:r><w:sym w:char="zz"/></w:r>`),
		wp("", `<w:r><w:footnoteReference w:id="7"/></w:r>`),
	}
	for _, b := range bodies {
		got, err := Read(fixture{body: b}.bytes(t), ReadOptions{})
		if err != nil {
			t.Errorf("%s: %v", b, err)
			continue
		}
		valid(t, got.Body)
	}
}
