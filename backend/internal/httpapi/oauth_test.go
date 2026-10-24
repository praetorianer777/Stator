package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// mcpServer knows a consented app's token, which acts at MCP alone, and a
// partner's, which acts anywhere, both for the same person.
func mcpServer(t *testing.T) *Server {
	t.Helper()
	s := newServer(t)
	s.OAuth = oauth.NewService(nil, nil, "https://wiki.example", nil)
	user, consented, partner := uuid.New(), uuid.New(), uuid.New()
	s.Auth = fakeAuth{
		"consented": {UserID: user, Role: auth.RoleMember, TokenID: &consented, MCPOnly: true},
		"partner":   {UserID: user, Role: auth.RoleMember, TokenID: &partner},
	}
	return s
}

func TestAConsentedTokenActsAtMCPAlone(t *testing.T) {
	router := mcpServer(t).Routes(nil)
	resp, body := serve(t, router, withBearer(http.MethodGet, APIPrefix+"/tokens", "consented", ""))
	if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "mcp_only_token" ||
		resp.Header.Get("WWW-Authenticate") != `Bearer error="invalid_token"` {
		t.Errorf("a consented token at /tokens = %d %v %q", resp.StatusCode, body, resp.Header.Get("WWW-Authenticate"))
	}
	_, decoded := serve(t, router, withBearer(http.MethodPost, mcpPath, "consented", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if result, _ := decoded["result"].(map[string]any); result == nil {
		t.Errorf("a consented token at MCP = %v", decoded)
	}
	// MCP's tool calls run through the router again, marked as its own.
	_, decoded = serve(t, router, withBearer(http.MethodPost, mcpPath, "consented",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`))
	if strings.Contains(fmt.Sprint(decoded), "mcp_only_token") {
		t.Errorf("MCP's own call was refused the consented token: %v", decoded)
	}
	if resp, body := serve(t, router, withBearer(http.MethodGet, APIPrefix+"/tokens", "partner", "")); resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("a partner's token was refused outside MCP: %v", body)
	}
}

func TestA401AtMCPSaysWhereToConnect(t *testing.T) {
	router := mcpServer(t).Routes(nil)
	want := `Bearer resource_metadata="https://wiki.example/.well-known/oauth-protected-resource/api/v1/mcp", scope="read write"`
	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{}`))
	resp, body := serve(t, router, req)
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != want {
		t.Errorf("no credential at MCP = %d %q %v", resp.StatusCode, resp.Header.Get("WWW-Authenticate"), body)
	}
	resp, _ = serve(t, router, withBearer(http.MethodPost, mcpPath, "stator_pat_gone", `{}`))
	if resp.Header.Get("WWW-Authenticate") != want+`, error="invalid_token"` {
		t.Errorf("a refused token at MCP is challenged with %q", resp.Header.Get("WWW-Authenticate"))
	}
	// Anywhere else a 401 stays the plain one.
	resp, _ = serve(t, router, httptest.NewRequest(http.MethodGet, APIPrefix+"/tokens", nil))
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != "" {
		t.Errorf("/tokens unsigned = %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}

func TestTheAuthorizationEndpointSendsTheBrowserToTheConsentPage(t *testing.T) {
	s := mcpServer(t)
	session := uuid.New()
	s.Auth = fakeAuth{"session": {UserID: uuid.New(), Role: auth.RoleMember, SessionID: &session}}
	router := s.Routes(nil)
	query := "client_id=abc&state=s%201&code_challenge_method=S256"
	redirect := func(req *http.Request) *http.Response {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Result()
	}
	resp := redirect(httptest.NewRequest(http.MethodGet, APIPrefix+"/oauth/authorize?"+query, nil))
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://app.test/login?next="+url.QueryEscape("/oauth/authorize?"+query) {
		t.Errorf("signed out, authorize = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/oauth/authorize?"+query, nil)
	req.AddCookie(&http.Cookie{Name: s.CookieName, Value: "session"})
	resp = redirect(req)
	if resp.Header.Get("Location") != "http://app.test/oauth/authorize?"+query {
		t.Errorf("signed in, authorize = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestRegistrationsFromOneAddressAreBraked(t *testing.T) {
	s := newServer(t)
	router := s.Routes(nil)
	brake := registrations
	registrations = newThrottle(RegistrationsPerWindow, RegistrationWindow)
	t.Cleanup(func() { registrations = brake })
	for range RegistrationsPerWindow {
		// A server without the service still counts the attempt.
		if resp, _ := serve(t, router, httptest.NewRequest(http.MethodPost, APIPrefix+"/oauth/register", strings.NewReader(`{}`))); resp.StatusCode == http.StatusTooManyRequests {
			t.Fatal("braked before the limit")
		}
	}
	resp, body := serve(t, router, httptest.NewRequest(http.MethodPost, APIPrefix+"/oauth/register", strings.NewReader(`{}`)))
	if resp.StatusCode != http.StatusTooManyRequests || errorOf(t, body)["code"] != "too_many_requests" {
		t.Errorf("one registration too many = %d %v", resp.StatusCode, body)
	}
}
