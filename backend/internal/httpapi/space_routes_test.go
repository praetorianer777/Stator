package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Spaces and pages belong to an organization: nobody signed in is told to
// sign in, and somebody without an organization to choose one.
func TestSpaceAndPageRoutesNeedAnOrganization(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{"no-org": {UserID: uuid.New()}}
	h := s.Routes(nil)
	someID := uuid.NewString()
	for _, op := range operations {
		if op.tag != "spaces" && op.tag != "pages" && op.tag != "trash" && op.tag != "archive" {
			continue
		}
		path := APIPrefix + strings.NewReplacer("{spaceKey}", "DOCS", "{pageID}", someID).Replace(op.path)
		resp, body := serve(t, h, httptest.NewRequest(op.method, path, nil))
		if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
			t.Errorf("anonymous %s %s = %d %v, want 401", op.method, op.path, resp.StatusCode, body)
		}
		req := httptest.NewRequest(op.method, path, nil)
		req.Header.Set("Authorization", "Bearer no-org")
		resp, body = serve(t, h, req)
		if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "no_organization" {
			t.Errorf("%s %s with no organization = %d %v, want 400", op.method, op.path, resp.StatusCode, body)
		}
	}
}
