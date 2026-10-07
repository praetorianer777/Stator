package spaceio

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/space"
)

func TestTheDefaultsAreTheConfigurations(t *testing.T) {
	if DefaultWatchInterval != config.DefaultSpaceTransferCheck || DefaultExportTTL != config.DefaultSpaceExportTTL || DefaultMaxImportBytes != config.DefaultSpaceImportLimit {
		t.Fatalf("spaceio's defaults %s, %s, %d and config's %s, %s, %d disagree", DefaultWatchInterval, DefaultExportTTL, DefaultMaxImportBytes,
			config.DefaultSpaceTransferCheck, config.DefaultSpaceExportTTL, config.DefaultSpaceImportLimit)
	}
}

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

// Every id a document names is pointed at the import's, the space's key
// follows its new one, and what names nothing in the archive stays.
func TestRewritePointsADocumentAtTheImportsIds(t *testing.T) {
	v := ids(10)
	pageA, pageB, outside, file, thread, gone, cal, alice, bob, newAlice := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9]
	r := renames{
		pages: map[uuid.UUID]uuid.UUID{pageA: uuid.New(), pageB: uuid.New()}, files: map[uuid.UUID]uuid.UUID{file: uuid.New()},
		threads: map[uuid.UUID]uuid.UUID{thread: uuid.New()}, calendars: map[uuid.UUID]uuid.UUID{cal: uuid.New()},
		people: map[uuid.UUID]uuid.UUID{alice: newAlice}, oldKey: "OLD", newKey: "NEW",
	}
	body := `{"type":"doc","content":[
		{"type":"paragraph","content":[
			{"type":"text","text":"see","marks":[{"type":"link","attrs":{"href":"/s/OLD/p/` + pageA.String() + `/intro#top"}}]},
			{"type":"text","text":"there","marks":[{"type":"link","attrs":{"href":"/s/OTHER/p/` + outside.String() + `"}}]},
			{"type":"text","text":"file","marks":[{"type":"link","attrs":{"href":"/api/v1/attachments/` + file.String() + `?download=1"}}]},
			{"type":"mention","attrs":{"id":"` + alice.String() + `","label":"Alice"}},
			{"type":"mention","attrs":{"id":"` + bob.String() + `","label":"Bob"}},
			{"type":"text","text":"kept","marks":[{"type":"inlineComment","attrs":{"threadId":"` + thread.String() + `"}},{"type":"inlineComment","attrs":{"threadId":"` + gone.String() + `"}}]}
		]},
		{"type":"image","attrs":{"attachmentId":"` + file.String() + `","alt":null,"width":320}},
		{"type":"include","attrs":{"pageId":"` + pageB.String() + `","excerptId":null}},
		{"type":"calendar","attrs":{"calendarId":"` + cal.String() + `","project":null}},
		{"type":"blogPosts","attrs":{"space":"OLD","limit":5}},
		{"type":"recentlyUpdated","attrs":{"space":"ELSE","limit":5}}
	]}`
	out, err := r.rewrite(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"/s/NEW/p/" + r.pages[pageA].String() + "/intro#top",
		"/s/OTHER/p/" + outside.String(),
		"/api/v1/attachments/" + r.files[file].String() + "?download=1",
		`"id":"` + newAlice.String() + `"`,
		`"text":"@Bob"`,
		`"threadId":"` + r.threads[thread].String() + `"`,
		`"attachmentId":"` + r.files[file].String() + `"`,
		`"pageId":"` + r.pages[pageB].String() + `"`,
		`"calendarId":"` + r.calendars[cal].String() + `"`,
		`"space":"NEW"`, `"space":"ELSE"`, `"width":320`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the rewritten document lacks %s:\n%s", want, got)
		}
	}
	for _, gone := range []string{bob.String(), gone.String(), pageA.String(), file.String()} {
		if strings.Contains(got, gone) {
			t.Errorf("the rewritten document still names %s:\n%s", gone, got)
		}
	}
	if r.asText != 1 {
		t.Errorf("%d mentions became words, want 1", r.asText)
	}
	if err := document.Validate(out); err != nil {
		t.Errorf("the rewritten document is no longer valid: %v", err)
	}
	plain := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"1.50"}]}]}`)
	if same, _ := r.rewrite(plain); !bytes.Equal(same, plain) {
		t.Errorf("a document naming nothing changed: %s", same)
	}
}

func manifest() *Manifest {
	home := uuid.New()
	return &Manifest{Format: Format, Version: Version, Space: ArchiveSpace{Key: "DOCS", Name: "Docs", HomePage: home}, Pages: []uuid.UUID{home}}
}

func TestAManifestIsOursAndNotNewer(t *testing.T) {
	if err := CheckManifest(manifest()); err != nil {
		t.Fatalf("a manifest of ours is refused: %v", err)
	}
	for name, change := range map[string]func(*Manifest){
		"another format": func(m *Manifest) { m.Format = "zip" },
		"a newer one":    func(m *Manifest) { m.Version = Version + 1 },
		"no pages":       func(m *Manifest) { m.Pages = nil },
		"too many pages": func(m *Manifest) { m.Pages = make([]uuid.UUID, MaxPages+1) },
		"too many files": func(m *Manifest) { m.Counts.Files = MaxFiles + 1 },
	} {
		m := manifest()
		change(m)
		var inv *InvalidError
		if err := CheckManifest(m); !errors.As(err, &inv) || !strings.HasSuffix(inv.Message, ".") {
			t.Errorf("%s: answered %v, want a sentence", name, err)
		}
	}
}

func zipOf(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestAnArchiveWithoutAManifestIsRefusedInASentence(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no manifest":   {"readme.md": "# hello"},
		"no json":       {manifestPath: "{"},
		"other content": {manifestPath: `{"format":"something","version":1}`},
	} {
		_, err := readManifest(zipOf(t, files))
		var inv *InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%s: answered %v", name, err)
		}
	}
	data, _ := json.Marshal(manifest())
	if _, err := readManifest(zipOf(t, map[string]string{manifestPath: string(data)})); err != nil {
		t.Errorf("a good manifest is refused: %v", err)
	}
}

// A page is held to what the tables take before anything is written, and
// told by its title.
func TestCheckPageNamesWhatIsWrong(t *testing.T) {
	home := uuid.New()
	before := map[uuid.UUID]string{home: "Home"}
	good := func() *ArchivePage {
		return &ArchivePage{ID: uuid.New(), Parent: &home, Kind: "page", Rank: "m", Title: "Child", Mode: "draft", Width: "fixed",
			Versions: []PageVersion{{Number: 1, Title: "Child"}, {Number: 2, Title: "Child"}}}
	}
	if err := checkPage(good(), false, before); err != nil {
		t.Fatalf("a good page is refused: %v", err)
	}
	stranger := uuid.New()
	two := 2
	for name, change := range map[string]func(*ArchivePage){
		"a blank title":          func(p *ArchivePage) { p.Title = "  " },
		"an unknown kind":        func(p *ArchivePage) { p.Kind = "wiki" },
		"a post under a page":    func(p *ArchivePage) { p.Kind = "post" },
		"an orphan":              func(p *ArchivePage) { p.Parent = nil },
		"a parent listed later":  func(p *ArchivePage) { p.Parent = &stranger },
		"a gap in its versions":  func(p *ArchivePage) { p.Versions[1].Number = 3 },
		"a restore of the later": func(p *ArchivePage) { p.Versions[1].RestoredFrom = &two },
		"a folder with versions": func(p *ArchivePage) { p.Kind = "folder" },
		"an unknown width":       func(p *ArchivePage) { p.Width = "huge" },
		"an unknown restriction": func(p *ArchivePage) {
			p.Restrictions = []Restriction{{Kind: "comment", Subject: Subject{Type: "user", Person: "x"}}}
		},
	} {
		p := good()
		change(p)
		var inv *InvalidError
		if err := checkPage(p, false, before); !errors.As(err, &inv) || !strings.Contains(inv.Message, "Export the space again") {
			t.Errorf("%s: answered %v", name, err)
		}
	}
	h := good()
	h.Parent = nil
	if err := checkPage(h, true, before); err != nil {
		t.Errorf("a home page is refused: %v", err)
	}
	h.Kind = "folder"
	h.Versions = nil
	if err := checkPage(h, true, before); err == nil {
		t.Error("a folder as the home page passes")
	}
}

func TestMentionedFindsPeopleAndAssignees(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	body := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"` + a.String() + `","label":"A"}}]},
		{"type":"taskReport","attrs":{"assignee":"` + b.String() + `"}},{"type":"taskReport","attrs":{"assignee":"me"}}]}`
	got := mentioned(json.RawMessage(body))
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("mentioned found %v, want %v and %v", got, a, b)
	}
}

// snapshotOf is a small space read as the export reads one: a home page, a
// child with a picture and a folder, and a post.
func snapshotOf(t *testing.T) (*snapshot, map[string]uuid.UUID) {
	t.Helper()
	v := ids(6)
	home, child, folder, post, picture, author := v[0], v[1], v[2], v[3], v[4], v[5]
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s := &snapshot{exportedAt: now, exporter: author, space: &space.Space{ID: uuid.New(), Key: "DOCS", Name: "Docs <&>", HomePageID: home},
		byID: map[uuid.UUID]*ArchivePage{}, people: map[uuid.UUID]Person{author: {Ref: author.String(), Name: "Ada"}}, keys: map[uuid.UUID]string{}}
	add := func(p *ArchivePage) {
		s.pages = append(s.pages, p)
		s.byID[p.ID] = p
	}
	add(&ArchivePage{ID: home, Kind: "page", Title: "Welcome", Body: json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Read "},{"type":"text","text":"the child","marks":[{"type":"link","attrs":{"href":"/s/DOCS/p/` + child.String() + `/child"}}]}]},
		{"type":"childPages","attrs":{"scope":"children","depth":null,"sort":"tree"}},
		{"type":"calendar","attrs":{"calendarId":"` + uuid.New().String() + `","project":null}}]}`)})
	add(&ArchivePage{ID: child, Parent: &home, Kind: "page", Title: "Child <script>", Labels: []Label{{Name: "how-to"}},
		Files: []File{{ID: picture, Name: "chart one.png", Size: 3}},
		Body: json.RawMessage(`{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + picture.String() + `","alt":"A chart","width":null}},
			{"type":"paragraph","content":[{"type":"text","text":"<b>not bold</b>"}]},
			{"type":"include","attrs":{"pageId":"` + home.String() + `","excerptId":null}}]}`)})
	add(&ArchivePage{ID: folder, Parent: &home, Kind: "folder", Title: "Folder", Body: json.RawMessage(emptyDoc)})
	add(&ArchivePage{ID: post, Kind: "post", Title: "News", Body: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Out now"}]}]}`)})
	return s, map[string]uuid.UUID{"home": home, "child": child, "folder": folder, "post": post, "picture": picture}
}

// The HTML pages link to each other and their files by relative paths, run
// no script, and say what they cannot show offline.
func TestTheHTMLPagesReadOffline(t *testing.T) {
	s, id := snapshotOf(t)
	w := layout(s)
	if w.names[id["home"]] != indexPage || w.names[id["child"]] != "child-script.html" {
		t.Fatalf("the pages are named %v", w.names)
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, p := range s.pages {
		if err := w.writePage(zw, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		for _, f := range zr.File {
			if f.Name == name {
				rc, _ := f.Open()
				defer rc.Close()
				var out bytes.Buffer
				_, _ = out.ReadFrom(rc)
				return out.String()
			}
		}
		t.Fatalf("the export has no %s", name)
		return ""
	}
	index := read(indexPage)
	for _, want := range []string{
		`<title>Welcome - Docs &lt;&amp;&gt;</title>`, `href="child-script.html"`, `href="news.html"`, "script-src 'none'",
		`href="style.css"`, drawnNote, `id="contents"`,
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index.html lacks %s:\n%s", want, index)
		}
	}
	child := read("child-script.html")
	for _, want := range []string{
		`<img src="files/` + id["picture"].String() + `/chart`, `Child &lt;script&gt;`, `&lt;b&gt;not bold&lt;/b&gt;`,
		`<a href="index.html">Welcome</a>`, "how-to", "Read", `<a href="child-script.html">the child</a>`,
	} {
		if !strings.Contains(child, want) {
			t.Errorf("the child's page lacks %s:\n%s", want, child)
		}
	}
	if strings.Contains(child, "<script") || strings.Contains(index, "<script") {
		t.Error("a page holds a script")
	}
}

func TestDownloadNamesSayTheFormat(t *testing.T) {
	if downloadName("DOCS", FormatHTML) != "docs-html.zip" || downloadName("DOCS", FormatArchive) != "docs-space.zip" {
		t.Errorf("the names are %s and %s", downloadName("DOCS", FormatHTML), downloadName("DOCS", FormatArchive))
	}
}
