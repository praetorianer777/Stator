package example_test

import (
	"bytes"
	"image/png"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/example"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// notShown are the marks of the allowlist the showcase cannot show, and why:
// a person never inserts either, and neither survives into a published page.
var notShown = map[string]string{
	"hint":              "a template's placeholder, stripped from whatever is published",
	document.AnchorMark: "an inline comment's passage, made by commenting on a selection and kept out of every version",
}

// facts are what a site with files and an Armature fills the content with.
func facts(lang string) example.Facts {
	f := example.Facts{
		Lang: lang, Key: "STATOR",
		Me:    example.Person{ID: uuid.New(), Name: "Ada <Admin> & Co"},
		Today: "2026-10-06", Soon: "2026-10-09", NextWeek: "2026-10-13",
		Pages: map[string]uuid.UUID{}, Excerpts: map[string]uuid.UUID{},
		Files:    map[string]uuid.UUID{example.ImageFile: uuid.New(), example.DataFile: uuid.New()},
		Calendar: uuid.New(),
		Armature: &example.ArmatureFacts{Project: "CP", Issue: "CP-4"},
	}
	example.Walk(func(e example.Entry) {
		f.Pages[e.Name] = uuid.New()
		f.Excerpts[e.Name] = uuid.New()
	})
	return f
}

// bare are the facts of a site that keeps no files and has no Armature.
func bare(lang string) example.Facts {
	f := facts(lang)
	f.Files, f.Armature = map[string]uuid.UUID{}, nil
	return f
}

// pages names every file that becomes a page or a post.
func pages() []string {
	names := []string{example.Home}
	example.Walk(func(e example.Entry) {
		if e.Kind != page.KindFolder {
			names = append(names, e.Name)
		}
	})
	return append(names, example.Posts...)
}

func render(t *testing.T, name string, f example.Facts) (*example.Doc, document.Node) {
	t.Helper()
	doc, warnings, err := example.Render(name, f)
	if err != nil {
		t.Fatalf("%s in %s: %v", name, f.Lang, err)
	}
	if len(warnings) > 0 {
		t.Errorf("%s in %s does not come across as written: %v", name, f.Lang, warnings)
	}
	root, err := document.Parse(doc.Body)
	if err != nil {
		t.Fatal(err)
	}
	return doc, root
}

func kinds(n document.Node, into map[string]bool) {
	into[n.Type] = true
	for _, m := range n.Marks {
		into[m.Type] = true
	}
	for _, c := range n.Content {
		kinds(c, into)
	}
}

// A block or mark added to the editor is added to the allowlist, which the
// editor's own test holds it to; the showcase has to show it then too.
func TestTheShowcaseShowsEveryBlockAndMarkTheEditorOffers(t *testing.T) {
	for _, lang := range example.Languages {
		_, root := render(t, example.Showcase, facts(lang))
		shown := map[string]bool{}
		kinds(root, shown)
		for _, name := range slices.Sorted(maps.Keys(document.Allowed.Nodes)) {
			if !shown[name] {
				t.Errorf("the %s showcase shows no %q block; add one to content/%s/showcase.md with how to insert it", lang, name, lang)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(document.Allowed.Marks)) {
			if _, why := notShown[name]; !why && !shown[name] {
				t.Errorf("the %s showcase shows no %q mark; add one to content/%s/showcase.md with how to apply it", lang, name, lang)
			}
		}
	}
	for name := range notShown {
		if _, ok := document.Allowed.Marks[name]; !ok {
			t.Errorf("%q is excused from the showcase but is no mark of the allowlist any more", name)
		}
	}
}

// What the content cannot show without files or Armature it says in words.
func TestASiteWithoutFilesOrArmatureGetsSentencesInstead(t *testing.T) {
	for _, lang := range example.Languages {
		_, root := render(t, example.Showcase, bare(lang))
		shown := map[string]bool{}
		kinds(root, shown)
		for _, name := range []string{"image", "attachment", document.NodeAttachmentList, "armatureIssue", "armatureIssueBlock", "armatureIssueList", "armatureChart", "armatureRoadmap"} {
			if shown[name] {
				t.Errorf("the bare %s showcase shows a %q, which needs what the site lacks", lang, name)
			}
		}
		text := document.PlainText(root)
		for _, said := range map[string][]string{example.English: {"Armature", "file"}, example.German: {"Armature", "Datei"}}[lang] {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(said)) {
				t.Errorf("the bare %s showcase never mentions %s", lang, said)
			}
		}
	}
}

func TestEveryPageIsAPageStatorTakesInBothLanguages(t *testing.T) {
	for _, lang := range example.Languages {
		for _, f := range []example.Facts{facts(lang), bare(lang)} {
			for _, name := range pages() {
				doc, _ := render(t, name, f)
				title, err := example.Title(lang, name)
				if err != nil || doc.Title != title || title == "" || len([]rune(title)) > page.MaxTitleLength {
					t.Errorf("%s in %s is titled %q, read ahead as %q (%v)", name, lang, doc.Title, title, err)
				}
				if bytes.Contains(doc.Body, []byte("%%")) || bytes.Contains(doc.Body, []byte("{{")) {
					t.Errorf("%s in %s keeps a marker unread: %s", name, lang, doc.Body)
				}
				for _, l := range doc.Labels {
					if got, err := label.Normalize(l); err != nil || got != l {
						t.Errorf("%s in %s: label %q is not one as typed (%q, %v)", name, lang, l, got, err)
					}
				}
				if len(doc.Labels) > label.MaxPerPage {
					t.Errorf("%s in %s carries %d labels", name, lang, len(doc.Labels))
				}
			}
		}
	}
}

// The home page goes in with the space, as a template's home page does, so
// it holds nothing that only a publish settles.
func TestTheHomePageIsOneTheSpaceCanStartWith(t *testing.T) {
	for _, lang := range example.Languages {
		doc, root := render(t, example.Home, facts(lang))
		found := map[string]bool{}
		kinds(root, found)
		for _, k := range []string{"taskList", "taskItem", "mention", "hint", document.AnchorMark} {
			if found[k] {
				t.Errorf("the %s home page carries %s, which only a publish settles", lang, k)
			}
		}
		if len(doc.Labels) > 0 || doc.Comment != "" {
			t.Errorf("the %s home page asks for labels or a comment, which the space's creation does not make", lang)
		}
	}
}

// structure is a document's blocks as their types, in order and nested.
func structure(n document.Node) string {
	spec := document.Allowed.Nodes[n.Type]
	if spec.Inline {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Type)
	var inner []string
	for _, c := range n.Content {
		if s := structure(c); s != "" {
			inner = append(inner, s)
		}
	}
	if len(inner) > 0 {
		b.WriteString("(" + strings.Join(inner, " ") + ")")
	}
	return b.String()
}

func TestTheLanguagesSayTheSameInTheSameShape(t *testing.T) {
	for _, name := range pages() {
		_, en := render(t, name, facts(example.English))
		_, de := render(t, name, facts(example.German))
		if a, b := structure(en), structure(de); a != b {
			t.Errorf("%s is laid out differently in German:\n en %s\n de %s", name, a, b)
		}
		enDoc, _ := render(t, name, facts(example.English))
		deDoc, _ := render(t, name, facts(example.German))
		if len(enDoc.Labels) != len(deDoc.Labels) || (enDoc.Comment == "") != (deDoc.Comment == "") {
			t.Errorf("%s carries other labels or comments in German", name)
		}
	}
}

// Every file of the content is a page of the tree, a post or the home page,
// in each language, and the house style holds in all of them.
func TestTheContentIsEveryFileAndNoOther(t *testing.T) {
	want := []string{example.Home}
	example.Walk(func(e example.Entry) { want = append(want, e.Name) })
	want = append(want, example.Posts...)
	slices.Sort(want)
	for _, lang := range example.Languages {
		entries, err := fs.ReadDir(example.Content(), "content/"+lang)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, strings.TrimSuffix(e.Name(), ".md"))
			src, err := example.Source(lang, strings.TrimSuffix(e.Name(), ".md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, banned := range []string{"–", "—", "“", "”", "„", "‘", "’", "‚"} {
				if bytes.Contains(src, []byte(banned)) {
					t.Errorf("content/%s/%s holds %q; write ASCII dashes and quotes", lang, e.Name(), banned)
				}
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("content/%s holds %v, want %v", lang, got, want)
		}
	}
}

func TestThePictureIsAPNG(t *testing.T) {
	data, err := example.Picture()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() == 0 {
		t.Fatalf("the picture does not decode: %v", err)
	}
}

func TestTheLanguageIsAskedForElseThePersonsOwn(t *testing.T) {
	for _, tt := range []struct {
		asked, own, want string
		err              bool
	}{
		{"de", "", "de", false},
		{"en", "de", "en", false},
		{"", "de", "de", false},
		{"", "", "en", false},
		{"fr", "de", "", true},
	} {
		got, err := example.Language(tt.asked, tt.own)
		if got != tt.want || (err != nil) != tt.err {
			t.Errorf("Language(%q, %q) = %q, %v", tt.asked, tt.own, got, err)
		}
	}
}

func mentions(n document.Node, into *[]document.Node) {
	if n.Type == "mention" {
		*into = append(*into, n)
	}
	for _, c := range n.Content {
		mentions(c, into)
	}
}

func TestTheShowcasesTasksMentionTheirMaker(t *testing.T) {
	f := facts(example.English)
	doc, root := render(t, example.Showcase, f)
	if len(document.TasksIn(doc.Body)) == 0 {
		t.Fatal("the showcase has no tasks")
	}
	var found []document.Node
	mentions(root, &found)
	if len(found) == 0 {
		t.Fatal("the showcase mentions nobody")
	}
	for _, m := range found {
		if m.Attrs["id"] != f.Me.ID.String() || m.Attrs["label"] != f.Me.Name {
			t.Errorf("the showcase mentions %v, want its maker by name", m.Attrs)
		}
	}
}
