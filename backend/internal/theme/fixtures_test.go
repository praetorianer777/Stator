package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// vendoredTheme is a theme file another product wrote, pinned by its SHA-256:
// a fixture edited here would test nothing of theirs.
type vendoredTheme struct {
	sum, source string
}

// vendoredThemes are every theme file under testdata, each as its product
// wrote it (#443).
var vendoredThemes = map[string]vendoredTheme{
	"minecraft.armature-theme.json": {"f5e087b4e23e9ade216f61ec877d97f4bf5526e1e07d30f3fdb6b8bdd115ea82", "Armature 7380763, docs/design/themes"},
	"exciter.armature-theme.json":   {"3278a30d8e7811ee69947126a3f6fa83a2a9be3b6a411af2a5c5d5070203d339", "Exciter 7303ef0, backend/internal/theme/testdata"},
}

func TestTheVendoredThemeFilesAreTheirsByteForByte(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		want, ok := vendoredThemes[name]
		if !ok {
			t.Errorf("%s is not pinned; add its SHA-256 and where it came from to vendoredThemes", name)
		} else if hex.EncodeToString(sum[:]) != want.sum {
			t.Errorf("%s is not the file as it came from %s; vendor it again rather than edit it", name, want.source)
		}
	}
	if len(files) != len(vendoredThemes) {
		t.Errorf("testdata holds %d theme files, want %d", len(files), len(vendoredThemes))
	}
}
