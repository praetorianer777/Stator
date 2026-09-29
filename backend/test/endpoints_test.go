//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// Nothing behind a sign-in is reachable without one: every operation that is
// not public, called anonymously with placeholder parameters, answers 401.
// The contract counts each of those refusals.
func TestEveryEndpointRefusesAnAnonymousCaller(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	anonymous := api.anonymous()
	for _, r := range apiContract.routes {
		if r.Public {
			continue
		}
		path := r.Path
		for strings.Contains(path, "{") {
			open, end := strings.IndexByte(path, '{'), strings.IndexByte(path, '}')
			path = path[:open] + "00000000-0000-0000-0000-000000000000" + path[end+1:]
		}
		var body any
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			body = map[string]any{}
		}
		if resp := anonymous.call(t, r.Method, httpapi.APIPrefix+path, body); resp.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d to nobody, want 401", r.Method, r.Path, resp.Status)
		}
	}
}

// sendJSON is a browser request with a JSON body, answered with its status
// and decoded body.
func sendJSON(t *testing.T, b *browser, method, target string, body any) (int, map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, target, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The session's own operations and the provider settings, the way the web
// client calls them: a signed-in administrator reads and saves the provider,
// and moves the session to another organization.
func TestAnAdministratorsSessionOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	home := h.makeOrg(t, "session-home")
	away := h.makeOrg(t, "session-away")
	email := bootstrapAdmin(t, h, a, home)
	h.letIn(t, away, email, "member")

	b := newBrowser(t)
	base := a.URL + httpapi.APIPrefix
	if status, body := sendJSON(t, b, http.MethodPost, base+"/auth/login", map[string]any{"email": email, "password": adminPassword}); status != http.StatusOK {
		t.Fatalf("sign in = %d %v", status, body)
	}

	if resp, _ := b.get(t, base+"/oidc-provider"); resp.StatusCode != http.StatusOK {
		t.Fatalf("read the provider = %s", resp.Status)
	}
	provider := map[string]any{"issuer": "https://id.session.test", "clientId": "stator", "enabled": true}
	if status, body := sendJSON(t, b, http.MethodPut, base+"/oidc-provider", provider); status != http.StatusOK {
		t.Fatalf("save the provider = %d %v", status, body)
	}
	if status, _ := sendJSON(t, b, http.MethodPut, base+"/oidc-provider", map[string]any{"issuer": "", "clientId": ""}); status != http.StatusUnprocessableEntity {
		t.Errorf("saving an empty provider = %d, want 422", status)
	}

	if status, body := sendJSON(t, b, http.MethodPost, base+"/auth/switch-org", map[string]any{"slug": away.Slug}); status != http.StatusOK {
		t.Fatalf("switch to %s = %d %v", away.Slug, status, body)
	}
	if status, _ := sendJSON(t, b, http.MethodPost, base+"/auth/switch-org", map[string]any{"slug": "no-such-org"}); status != http.StatusForbidden {
		t.Errorf("switching to no organization = %d, want 403", status)
	}
	// A member of away, the provider settings are no longer theirs to read.
	if resp, _ := b.get(t, base+"/oidc-provider"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a member reading the provider = %s, want 403", resp.Status)
	}
}
