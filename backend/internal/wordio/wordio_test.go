package wordio

import (
	"archive/zip"
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheWatchIntervalIsTheConfiguredDefault(t *testing.T) {
	if DefaultWatchInterval != config.DefaultWordImportCheck {
		t.Fatalf("wordio.DefaultWatchInterval %s and config.DefaultWordImportCheck %s disagree", DefaultWatchInterval, config.DefaultWordImportCheck)
	}
	if Lease <= RunLimit+CleanupLimit {
		t.Fatalf("a lease of %s lapses while an import of %s and its cleanup of %s may still run", Lease, RunLimit, CleanupLimit)
	}
}

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	_ = z.Close()
	return buf.Bytes()
}

// An upload keeps its Word documents in the order they came, opens its
// archives, names what is no document, and leaves hidden files and Word's
// lock files out.
func TestAnUploadIsCollectedIntoItsDocuments(t *testing.T) {
	got, err := collect([]File{
		{Path: "b.docx", Data: []byte("b")},
		{Path: "team.zip", Data: archive(t, map[string]string{"team/plan.docx": "p", "team/logo.png": "x", "team/.hidden.docx": "h", "__MACOSX/team/plan.docx": "m", "team/~$plan.docx": "lock"})},
		{Path: "notes.txt", Data: []byte("n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, d := range got.docs {
		paths = append(paths, d.Path)
	}
	if strings.Join(paths, ",") != "b.docx,team/plan.docx" {
		t.Errorf("the documents are %v", paths)
	}
	skipped := slices.Sorted(slices.Values(got.skipped))
	if strings.Join(skipped, ",") != "notes.txt,team/logo.png,team/~$plan.docx" {
		t.Errorf("the files passed over are %v", got.skipped)
	}

	if _, err := collect([]File{{Path: "a.txt", Data: []byte("a")}}); !errors.Is(err, ErrNoWord) {
		t.Errorf("an upload without a document: %v", err)
	}
	var bad *InvalidError
	if _, err := collect([]File{{Path: "../escape.docx", Data: []byte("x")}}); !errors.As(err, &bad) {
		t.Errorf("a path out of the folder: %v", err)
	}
	many := make([]File, MaxFiles+1)
	for i := range many {
		many[i] = File{Path: strings.Repeat("d", i+1) + ".docx", Data: []byte("x")}
	}
	if _, err := collect(many); !errors.As(err, &bad) || !strings.Contains(bad.Message, "more than 50") {
		t.Errorf("too many documents: %v", err)
	}
}

// Folders are pages, unless a document beside one has its name and holds
// its pages instead.
func TestFoldersOfDocumentsBecomeATree(t *testing.T) {
	docs := []File{{Path: "intro.docx"}, {Path: "guides/setup.docx"}, {Path: "guides/deep/more.docx"}, {Path: "team.docx"}, {Path: "team/alice.docx"}}
	roots := tree(docs, "")
	var lines []string
	walk(roots, 1, func(e, _ *entry, depth int) {
		lines = append(lines, strings.Repeat("  ", depth-1)+e.name+"|"+e.doc+"|"+e.dir)
	})
	want := []string{
		"intro.docx|intro.docx|",
		"guides||guides",
		"  setup.docx|guides/setup.docx|",
		"  deep||guides/deep",
		"    more.docx|guides/deep/more.docx|",
		"team.docx|team.docx|team",
		"  alice.docx|team/alice.docx|",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("the tree is\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if count(roots) != 7 {
		t.Errorf("the tree counts %d pages", count(roots))
	}
}

func TestPackedDocumentsReadBack(t *testing.T) {
	b := &batch{docs: []File{{Path: "a.docx", Data: []byte("one")}, {Path: "f/b.docx", Data: []byte("two")}}}
	packed, err := b.pack()
	if err != nil {
		t.Fatal(err)
	}
	got, err := readPacked(packed)
	if err != nil || len(got) != 2 || got[1].Path != "f/b.docx" || string(got[1].Data) != "two" {
		t.Errorf("read back %+v, %v", got, err)
	}
}

func TestTitlesComeFromNames(t *testing.T) {
	for in, want := range map[string]string{
		"release_notes-2026.docx": "release notes 2026",
		"Team Plan.DOCX":          "Team Plan",
		".docx":                   untitled,
		"folder":                  "folder",
	} {
		if got := titleFrom(in); got != want {
			t.Errorf("%q is titled %q, want %q", in, got, want)
		}
	}
	if got := sentence("the file x cannot be a page"); got != "The file x cannot be a page." {
		t.Errorf("a refusal reads %q", got)
	}
}
