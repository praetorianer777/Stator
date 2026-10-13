package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/tenant"
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
		if op.method != http.MethodGet || !op.public || op.wholeInstance || op.tool != "" {
			t.Errorf("%s %s: method %s, public %v, wholeInstance %v, tool %q; want a public GET that is no tool", op.method, op.path, op.method, op.public, op.wholeInstance, op.tool)
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
		req := httptest.NewRequest(http.MethodGet, APIPrefix+"/public", nil)
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

// A link's token is the whole of the link, so the access log and the traces
// write every address that carries one with the token blanked.
func TestALinksTokenIsBlankedInEveryPathWritten(t *testing.T) {
	token := strings.Repeat("s", 43)
	for path, want := range map[string]string{
		APIPrefix + "/public/links/" + token:                                  APIPrefix + "/public/links/{token}",
		APIPrefix + "/public/links/" + token + "/attachments/x":               APIPrefix + "/public/links/{token}/attachments/x",
		APIPrefix + "/public/acme/links/" + token:                             APIPrefix + "/public/acme/links/{token}",
		APIPrefix + "/public/acme/links/" + token + "/pdf":                    APIPrefix + "/public/acme/links/{token}/pdf",
		APIPrefix + "/public/pages/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01":      APIPrefix + "/public/pages/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01",
		APIPrefix + "/public/acme/pages/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01": APIPrefix + "/public/acme/pages/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01",
		APIPrefix + "/public":                                                 APIPrefix + "/public",
		APIPrefix + "/pages/links/" + token:                                   APIPrefix + "/pages/links/" + token,
	} {
		if got := redactPath(path); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// Whatever a link opens or does not, the answer is kept by no cache and the
// address is sent on to no other site.
func TestALinksAnswerIsNeverCachedNorSentOn(t *testing.T) {
	h := newServer(t).Routes(nil)
	for _, token := range []string{"not-a-token", strings.Repeat("a", 43)} {
		resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/public/links/"+token, nil))
		errBody, _ := body["error"].(map[string]any)
		if resp.StatusCode != http.StatusNotFound || errBody["code"] != "link_gone" {
			t.Errorf("%s = %d %v, want 404 link_gone", token, resp.StatusCode, body)
		}
		if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("X-Robots-Tag") == "" {
			t.Errorf("%s is served with caching %q, referrer %q and robots %q", token,
				resp.Header.Get("Cache-Control"), resp.Header.Get("Referrer-Policy"), resp.Header.Get("X-Robots-Tag"))
		}
	}
}

// fixedInstance is an instance whose organization is always the one given.
type fixedInstance tenant.Org

func (f fixedInstance) Org(context.Context) (tenant.Org, error) { return tenant.Org(f), nil }

// concrete fills a template's parameters the way a real address would.
func concrete(template string) string {
	return strings.NewReplacer(
		"{spaceKey}", "OPEN",
		"{pageID}", "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01",
		"{attachmentID}", "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02",
		"{token}", strings.Repeat("t", 43),
	).Replace(template)
}

// An address from before the organization left the public reads is sent on
// for good when its slug is the instance's, whatever its case, and is not
// found with any other; the new addresses are never taken for old ones.
func TestAnOldPublicAddressIsSentOnOnlyWithTheInstancesSlug(t *testing.T) {
	s := newServer(t)
	s.Instance = fixedInstance{ID: uuid.New(), Slug: "acme"}
	h := s.Routes(nil)
	for _, old := range oldPublicPaths {
		rest := concrete(old.path)
		for _, slug := range []string{"acme", "ACME"} {
			resp, _ := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/public/"+slug+rest+"?q=1", nil))
			if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != APIPrefix+"/public"+rest+"?q=1" {
				t.Errorf("%s%s = %d to %q, want 308 to the new address", slug, rest, resp.StatusCode, resp.Header.Get("Location"))
			}
		}
		resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/public/globex"+rest, nil))
		want := "not_public"
		if strings.HasPrefix(old.path, "/links/") {
			want = "link_gone"
		}
		if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Location") != "" || errorOf(t, body)["code"] != want {
			t.Errorf("globex%s = %d %v, want 404 %s", rest, resp.StatusCode, body, want)
		}
	}
	for _, route := range []string{"/public", "/public/logo", "/public/search", "/public/spaces/OPEN", "/public/links/" + strings.Repeat("t", 43)} {
		if resp, _ := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+route, nil)); resp.StatusCode == http.StatusPermanentRedirect {
			t.Errorf("%s is taken for an old address", route)
		}
	}
}
