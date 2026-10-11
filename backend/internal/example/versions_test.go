package example_test

import (
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/example"
)

// Changing a page of the content without a new version would leave every
// example made before it unaware that a newer one is there.
func TestTheContentChangesOnlyWithANewVersion(t *testing.T) {
	releases, err := example.Releases()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := example.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if latest := releases[len(releases)-1]; latest.Digest != digest {
		t.Fatalf("the content changed since version %d. Add version %d to content/versions.json with the digest %s and a line in every language saying what it brings; to correct version %d instead, as long as it has not been released, give it that digest.",
			latest.Version, latest.Version+1, digest, latest.Version)
	}
}

func TestEveryVersionSaysWhatItBringsInEveryLanguage(t *testing.T) {
	releases, err := example.Releases()
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range releases {
		if r.Version != i+1 {
			t.Errorf("version %d is listed as number %d; versions count up from 1", r.Version, i+1)
		}
		if r.Digest == "" {
			t.Errorf("version %d has no digest", r.Version)
		}
		lines := len(r.New[example.English])
		if lines == 0 {
			t.Errorf("version %d says nothing of what it brings", r.Version)
		}
		for _, lang := range example.Languages {
			if len(r.New[lang]) != lines {
				t.Errorf("version %d says %d things in %s and %d in English", r.Version, len(r.New[lang]), lang, lines)
			}
			for _, line := range r.New[lang] {
				if !strings.HasSuffix(line, ".") || strings.ContainsAny(line, "–—“”„‘’") {
					t.Errorf("version %d in %s: %q is not a plain sentence", r.Version, lang, line)
				}
			}
		}
		if len(r.New) != len(example.Languages) {
			t.Errorf("version %d speaks %d languages, the content %d", r.Version, len(r.New), len(example.Languages))
		}
	}
}

func TestWhatIsNewIsWhatTheLaterVersionsBring(t *testing.T) {
	if got := example.NewSince(example.Current(), example.English); len(got) != 0 {
		t.Errorf("the current version brings itself %q", got)
	}
	releases, _ := example.Releases()
	if got, want := len(example.NewSince(0, example.German)), func() (n int) {
		for _, r := range releases {
			n += len(r.New[example.German])
		}
		return n
	}(); got != want {
		t.Errorf("an example made before versions learns of %d things, want %d", got, want)
	}
}
