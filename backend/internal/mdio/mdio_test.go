package mdio

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func files(pairs ...string) []File {
	var out []File
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, File{Path: pairs[i], Data: []byte(pairs[i+1])})
	}
	return out
}

func outline(entries []*entry) string {
	var b strings.Builder
	walk(entries, 1, func(e *entry, _ *entry, depth int) {
		b.WriteString(strings.Repeat("  ", depth-1) + e.title + " [" + e.md + "]\n")
	})
	return b.String()
}

func TestFoldersBecomePagesInTheOrderTheyCame(t *testing.T) {
	s := &Service{MaxBytes: DefaultMaxImportBytes}
	_, roots, err := s.prepare(files(
		"docs/README.md", "# Documentation\n",
		"docs/guide.md", "# The guide\n",
		"docs/guide/install.md", "Install it.\n",
		"docs/guide.files/shot.png", "png",
		"docs/api/endpoints.md", "# Endpoints\n",
		"docs/api/schema.json", "{}",
		"docs/pictures/only.png", "png",
		"docs/.hidden/secret.md", "# Secret\n",
		"__MACOSX/docs/._guide.md", "junk",
		"top_level-notes.md", "No heading.\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	want := "Documentation [docs/README.md]\n" +
		"  The guide [docs/guide.md]\n" +
		"    install [docs/guide/install.md]\n" +
		"  api []\n" +
		"    Endpoints [docs/api/endpoints.md]\n" +
		"top level notes [top_level-notes.md]\n"
	if got := outline(roots); got != want {
		t.Errorf("the pages are\n%s\nwant\n%s", got, want)
	}
}

func TestReferencedFilesGoOnThePageThatShowsThem(t *testing.T) {
	s := &Service{MaxBytes: DefaultMaxImportBytes}
	_, roots, err := s.prepare(files(
		"a.md", "![one](pics/one.png) [doc](../b.md) [two](pics/two%20x.pdf) ![again](./pics/one.png) [b](b.md)",
		"b.md", "# B\n",
		"pics/one.png", "1",
		"pics/two x.pdf", "2",
	))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(roots[0].files, ","); got != "pics/one.png,pics/two x.pdf" {
		t.Errorf("a.md needs %q", got)
	}
	if len(roots[1].files) != 0 {
		t.Errorf("b.md needs %v", roots[1].files)
	}
}

func TestAnUploadIsCheckedBeforeAnythingIsMade(t *testing.T) {
	s := &Service{MaxBytes: 64}
	var invalidErr *InvalidError
	var tooLarge *TooLargeError
	for name, c := range map[string]struct {
		in    []File
		check func(error) bool
	}{
		"nothing to import":  {files("a.png", "x"), func(err error) bool { return errors.Is(err, ErrNoMarkdown) }},
		"a path that leaves": {files("../a.md", "x"), func(err error) bool { return errors.As(err, &invalidErr) }},
		"too much":           {files("a.md", strings.Repeat("x", 65)), func(err error) bool { return errors.As(err, &tooLarge) }},
		"an unreadable zip":  {files("a.zip", "PK not really"), func(err error) bool { return errors.As(err, &invalidErr) }},
		"a zip that grows":   {[]File{{Path: "bomb.zip", Data: zipped(t, map[string]string{"a.md": strings.Repeat("a", 10_000)})}}, func(err error) bool { return errors.As(err, &tooLarge) }},
		"a zip that leaves":  {[]File{{Path: "evil.zip", Data: zipped(t, map[string]string{"../../evil.md": "x"})}}, func(err error) bool { return errors.As(err, &invalidErr) }},
		"a file too deep":    {files("a.md", strings.Repeat(">", 50)), func(err error) bool { return errors.As(err, &invalidErr) && strings.Contains(err.Error(), "a.md") }},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.prepare(c.in); !c.check(err) {
				t.Errorf("got %v", err)
			}
		})
	}
	many := make([]File, 0, MaxImportPages+1)
	for i := range MaxImportPages + 1 {
		many = append(many, File{Path: strings.Repeat("x", i+1) + ".md", Data: []byte("x")})
	}
	if _, _, err := (&Service{MaxBytes: DefaultMaxImportBytes}).prepare(many); !errors.As(err, &invalidErr) {
		t.Errorf("too many pages: %v", err)
	}
}

func zipped(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPathsBetweenFilesOfAnArchive(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"", "guide.md", "guide.md"},
		{"", "guide/child.md", "guide/child.md"},
		{"guide", "guide.md", "../guide.md"},
		{"guide/child", "guide/other.md", "../other.md"},
		{"a", "b/c d.md", "../b/c%20d.md"},
		{"", "x.files/résumé (1).pdf", "x.files/résumé%20%281%29.pdf"},
	} {
		if got := relPath(c.from, c.to); got != c.want {
			t.Errorf("from %q to %q is %q, want %q", c.from, c.to, got, c.want)
		}
	}
	for _, c := range []struct{ from, dest, want, fragment string }{
		{"docs/a.md", "b.md#top", "docs/b.md", "#top"},
		{"docs/a.md", "../c%20d.md?x=1", "c d.md", ""},
		{"docs/a.md", "sub/", "docs/sub", ""},
	} {
		got, fragment, ok := resolvePath(c.from, c.dest)
		if !ok || got != c.want || fragment != c.fragment {
			t.Errorf("%s from %s is %q %q %v", c.dest, c.from, got, fragment, ok)
		}
	}
	if _, _, ok := resolvePath("a.md", "../../x.md"); ok {
		t.Error("a path out of the upload resolved")
	}
}

func TestTwoFilesOfOneNameStayApart(t *testing.T) {
	taken := map[string]bool{}
	got := []string{uniqueName("a.png", taken), uniqueName("A.png", taken), uniqueName("a.png", taken), uniqueName("notes", taken)}
	if strings.Join(got, ",") != "a.png,A-2.png,a-3.png,notes" {
		t.Errorf("the names are %v", got)
	}
}
