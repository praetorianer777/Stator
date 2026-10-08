package spaceio

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/wikiread"
)

// smallHTMLExport is an HTML space export of three pages, built here in the
// shape docs/wiki-import.md describes.
func smallHTMLExport() map[string]string {
	page := func(title, content, labels string) string {
		return `<html><head><title>Team : ` + title + `</title></head><body><div id="main-content">` + content +
			`</div><div class="labels"><ul>` + labels + `</ul></div></body></html>`
	}
	return map[string]string{
		"index.html": `<html><head><title>Team</title></head><body><ul><li><a href="Home_1.html">Home</a><ul>
			<li><a href="B_3.html">B</a></li><li><a href="A_2.html">A</a></li></ul></li></ul></body></html>`,
		"Home_1.html":         page("Home", `<p>See <a href="A_2.html">A</a> and <img src="attachments/1/9.png" alt="logo"></p>`, `<li>Team Notes</li><li>!!</li>`),
		"A_2.html":            page("A", `<p>a</p>`, ""),
		"B_3.html":            page("B", `<p>b</p>`, ""),
		"attachments/1/9.png": "png bytes",
	}
}

// An export read is given the shapes of an archive: ranks in the export's
// order, labels as Stator writes them, and its documents rewritten for the
// new space as an archive's are.
func TestAnExportTakesTheShapesOfAnArchive(t *testing.T) {
	zr := zipOf(t, smallHTMLExport())
	from, err := detect(zr)
	if err != nil || from != SourceHTML {
		t.Fatalf("the export is detected as %q: %v", from, err)
	}
	m, src, err := readExport(zr, from, "TEAM", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Space.Name != "Team" || len(m.Pages) != 3 || m.Pages[0] != m.Space.HomePage || len(m.Grants) != 0 {
		t.Fatalf("the manifest reads %+v", m)
	}
	home, _ := src.page(m.Pages[0])
	b, _ := src.page(m.Pages[1])
	a, _ := src.page(m.Pages[2])
	if b.Title != "B" || a.Title != "A" || !(b.Rank < a.Rank) || *a.Parent != home.ID {
		t.Errorf("the children are %q at %s and %q at %s", b.Title, b.Rank, a.Title, a.Rank)
	}
	if len(home.Labels) != 1 || home.Labels[0].Name != "team-notes" {
		t.Errorf("the home page's labels are %+v", home.Labels)
	}
	if src.lostCount != 1 || src.listed[0].Kind != wikiread.LossLabel || src.listed[0].Detail != "!!" {
		t.Errorf("the losses are %+v", src.listed)
	}
	if len(home.Files) != 1 || home.Files[0].Version != 1 || home.Files[0].ContentType != "image/png" {
		t.Fatalf("the home page's files are %+v", home.Files)
	}
	if data, err := src.fileBytes(home.Files[0]); err != nil || string(data) != "png bytes" {
		t.Errorf("the picture reads %q, %v", data, err)
	}

	im := &importer{src: src, m: m, key: "TEAM", from: from}
	if err := im.scan(); err != nil {
		t.Fatalf("the export does not scan: %v", err)
	}
	body, err := im.checked(home, home.Body, "a body")
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "/s/TEAM/p/"+im.r.pages[a.ID].String()) || !strings.Contains(got, im.r.files[home.Files[0].ID].String()) {
		t.Errorf("the home page is not pointed at the new ids: %s", got)
	}
	if err := document.Validate(body); err != nil {
		t.Errorf("the home page is refused: %v", err)
	}
	if len(home.Versions) != 1 || !json.Valid(home.Versions[0].Body) {
		t.Errorf("the home page's versions are %+v", home.Versions)
	}
}

// What is neither an archive nor an export is refused in a sentence that
// says what to upload instead.
func TestAZipOfNeitherIsRefused(t *testing.T) {
	_, err := detect(zipOf(t, map[string]string{"readme.md": "# hi"}))
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(inv.Message, "HTML or XML") {
		t.Errorf("a zip of neither is refused with %v", err)
	}
	if from, _ := detect(zipOf(t, map[string]string{manifestPath: "{}"})); from != SourceArchive {
		t.Errorf("an archive is detected as %q", from)
	}
	if from, _ := detect(zipOf(t, map[string]string{"export/entities.xml": "<x/>"})); from != SourceXML {
		t.Errorf("an XML export in a folder is detected as %q", from)
	}
	_, _, err = readExport(zipOf(t, map[string]string{"entities.xml": "<export></export>"}), SourceXML, "K", "")
	if !errors.As(err, &inv) {
		t.Errorf("an empty XML export reads as %v", err)
	}
}
