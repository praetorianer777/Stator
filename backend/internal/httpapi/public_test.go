package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The reads for anybody are reads, need no session and are nobody's to
// refuse: every row under /public/ is a public GET, and none is a tool.
func TestThePublicReadsAreReadsForAnybody(t *testing.T) {
	found := 0
	for _, op := range operations {
		if !strings.HasPrefix(op.path, "/public/") {
			continue
		}
		found++
		if op.method != http.MethodGet || !op.public || op.orgWide || op.tool != "" {
			t.Errorf("%s %s: method %s, public %v, orgWide %v, tool %q; want a public GET that is no tool", op.method, op.path, op.method, op.public, op.orgWide, op.tool)
		}
	}
	if found == 0 {
		t.Fatal("the table has no public reads")
	}
}

// A credential riding along on a public read is not looked at: a session that
// would be refused, as a deactivated account is, neither refuses the read
// nor makes it the person's.
func TestAPublicReadIgnoresWhateverCredentialRidesAlong(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{}
	h := s.Routes(nil)
	for _, credential := range []string{"blocked", "nobody-knows-this"} {
		req := httptest.NewRequest(http.MethodGet, APIPrefix+"/public/acme", nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		resp, body := serve(t, h, req)
		errBody, _ := body["error"].(map[string]any)
		if resp.StatusCode != http.StatusNotFound || errBody["code"] != "not_public" {
			t.Errorf("with %q a public read = %d %v, want 404 not_public", credential, resp.StatusCode, body)
		}
	}
	if resp, _ := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/pages/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", nil)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a member's read without a session = %d, want 401", resp.StatusCode)
	}
}
