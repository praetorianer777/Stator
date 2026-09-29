package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Every theme route needs somebody signed in, and all but the examples an
// organization too; an anonymous caller is told so in the envelope.
func TestThemeRoutesRefuseAnonymousCallers(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{"no-org": {UserID: uuid.New()}}
	h := s.Routes(nil)
	someID := uuid.NewString()
	for _, op := range operations {
		if op.tag != "themes" {
			continue
		}
		path := strings.NewReplacer("{themeID}", someID, "{assetID}", someID).Replace(op.path)
		resp, body := serve(t, h, httptest.NewRequest(op.method, APIPrefix+path, nil))
		if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
			t.Errorf("anonymous %s %s = %d %v, want 401", op.method, op.path, resp.StatusCode, body)
		}
		if op.path == "/themes/examples" {
			continue
		}
		req := httptest.NewRequest(op.method, APIPrefix+path, nil)
		req.Header.Set("Authorization", "Bearer no-org")
		resp, body = serve(t, h, req)
		if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "no_organization" {
			t.Errorf("%s %s with no organization = %d %v, want 400", op.method, op.path, resp.StatusCode, body)
		}
	}

	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/themes/examples", nil)
	req.Header.Set("Authorization", "Bearer no-org")
	resp, body := serve(t, h, req)
	examples, _ := body["examples"].([]any)
	if resp.StatusCode != http.StatusOK || len(examples) != 2 {
		t.Fatalf("the examples for somebody signed in = %d %v", resp.StatusCode, body)
	}
}

func TestServiceErrorsReadAsSentences(t *testing.T) {
	for in, want := range map[string]string{
		"a theme needs a name":   "A theme needs a name.",
		"already one.":           "Already one.",
		"":                       "",
		"is this a question?":    "Is this a question?",
		"Already capitalised it": "Already capitalised it.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}
