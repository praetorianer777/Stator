package template

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

var keyPattern = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

func hasHint(n document.Node) bool {
	for _, m := range n.Marks {
		if m.Type == "hint" {
			return true
		}
	}
	for _, c := range n.Content {
		if hasHint(c) {
			return true
		}
	}
	return false
}

func TestEveryBuiltInIsADocumentThePageWouldTake(t *testing.T) {
	want := []string{"meeting-notes", "how-to", "troubleshooting", "retrospective", "decision-record", "product-requirements", "project-plan"}
	list := BuiltIns()
	if len(list) != len(want) {
		t.Fatalf("%d built-in templates, want %d", len(list), len(want))
	}
	seen := map[string]bool{}
	for i, tpl := range list {
		if tpl.Key != want[i] {
			t.Errorf("template %d is %q, want %q", i, tpl.Key, want[i])
		}
		if !keyPattern.MatchString(tpl.Key) || seen[tpl.Key] {
			t.Errorf("key %q is malformed or taken twice", tpl.Key)
		}
		seen[tpl.Key] = true
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Description) == "" || !tpl.BuiltIn {
			t.Errorf("%s: %+v", tpl.Key, tpl)
		}
		if rest := strings.ReplaceAll(tpl.Title, DateToken, ""); strings.ContainsAny(rest, "{}") {
			t.Errorf("%s: the title %q names a token nobody fills in", tpl.Key, tpl.Title)
		}
		if err := document.Validate(tpl.Body); err != nil {
			t.Errorf("%s: the allowlist refuses the body: %v", tpl.Key, err)
			continue
		}
		root, _ := document.Parse(tpl.Body)
		if !hasHint(root) {
			t.Errorf("%s has no hint to guide its author", tpl.Key)
		}
		for _, h := range document.Headings(root) {
			if h.Anchor == "" {
				t.Errorf("%s: heading %q has no anchor", tpl.Key, h.Text)
			}
		}
	}
}

func TestByKey(t *testing.T) {
	got, err := ByKey("decision-record")
	if err != nil || got.Name != "Decision record" || !json.Valid(got.Body) {
		t.Fatalf("ByKey(decision-record) = %+v, %v", got, err)
	}
	if _, err := ByKey("nope"); err != ErrUnknown {
		t.Fatalf("an unknown key gives %v", err)
	}
}
