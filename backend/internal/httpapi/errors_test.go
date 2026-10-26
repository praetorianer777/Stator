package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// The settings name the screen where roles change by account.sso in the web's
// English strings; the answers about fixed roles send people there by that name.
func TestFixedRoleAnswersNameTheScreenAsTheSettingsDo(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "i18n", "en.ts"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)\n  account: \{.*?\n    sso: "([^"]+)",`).FindSubmatch(src)
	if m == nil {
		t.Fatal("web/src/i18n/en.ts has no account.sso")
	}
	screen := "under " + string(m[1])
	for name, msg := range map[string]string{
		"the answer":    toAPIError(perm.ErrFixed).Message,
		"perm.ErrFixed": perm.ErrFixed.Error(),
	} {
		if !strings.Contains(msg, screen) {
			t.Errorf("%s %q does not say %q", name, msg, screen)
		}
	}
}
