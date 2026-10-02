package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// tokenServer knows a session, a token that may do anything and one that may
// only read, all for the same person in the same organization.
func tokenServer(t *testing.T) *Server {
	t.Helper()
	s := newServer(t)
	org := &tenant.Org{ID: uuid.New(), Slug: "acme"}
	user := uuid.New()
	session, full, reader, limited := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	s.Auth = fakeAuth{
		"session": {UserID: user, Org: org, Role: auth.RoleMember, SessionID: &session},
		"full":    {UserID: user, Org: org, Role: auth.RoleMember, TokenID: &full},
		"reader":  {UserID: user, Org: org, Role: auth.RoleMember, TokenID: &reader, Scopes: []string{auth.ScopeRead}},
		// An owner's, so a refusal is the token's and never the person's.
		"spaces": {UserID: user, Org: org, Role: auth.RoleOwner, TokenID: &limited, SpacesOnly: true, TokenSpaces: []uuid.UUID{uuid.New()}},
	}
	return s
}

// The table's orgWide marks and the router agree both ways: a token limited to
// spaces is refused every marked operation and no other one.
func TestATokenLimitedToSpacesIsRefusedTheWholeOrganization(t *testing.T) {
	router := tokenServer(t).Routes(nil)
	marked := 0
	for _, route := range Catalog() {
		if APIPrefix+route.Path == mcpPath || route.Public {
			continue
		}
		path := APIPrefix + pathParamPattern.ReplaceAllString(route.Path, uuid.NewString())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, withBearer(route.Method, path, "spaces", `{}`))
		refused := rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), `"spaces_token"`)
		if route.OrgWide {
			marked++
		}
		if refused != route.OrgWide {
			t.Errorf("a token limited to spaces at %s %s = %d %s, orgWide %v", route.Method, route.Path, rec.Code, rec.Body.String(), route.OrgWide)
		}
	}
	if marked == 0 {
		t.Fatal("no operation is marked orgWide")
	}
	// The person behind it, signed in, gets past the same guard.
	resp, body := serve(t, router, withBearer(http.MethodGet, APIPrefix+"/audit", "session", ""))
	if resp.StatusCode == http.StatusForbidden && errorOf(t, body)["code"] == "spaces_token" {
		t.Errorf("a session was refused as a limited token: %v", body)
	}
}

func TestATokenLimitedToSpacesIsOfferedNoOrganizationTool(t *testing.T) {
	all, limited := listTools(false, false), listTools(false, true)
	if len(limited) == 0 || len(limited) >= len(all) {
		t.Fatalf("%d tools for a limited token of %d", len(limited), len(all))
	}
	for _, tool := range limited {
		if t2, _ := toolByName(tool["name"].(string)); t2.OrgWide {
			t.Errorf("a limited token is offered %s", tool["name"])
		}
	}
	router := tokenServer(t).Routes(nil)
	_, decoded := serve(t, router, withBearer(http.MethodPost, mcpPath, "spaces",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_audit_log","arguments":{}}}`))
	result, _ := decoded["result"].(map[string]any)
	if result == nil || result["isError"] != true || !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), "limited to some spaces") {
		t.Errorf("a limited token's call of an organization tool: %v", decoded)
	}
}

func withBearer(method, path, credential, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAReadOnlyTokenIsRefusedEveryWrite(t *testing.T) {
	s := tokenServer(t)
	router := s.Routes(nil)
	for _, route := range Catalog() {
		// MCP carries reads too; its writing tools are refused in mcp_test.go.
		if route.Method == http.MethodGet || APIPrefix+route.Path == mcpPath {
			continue
		}
		path := APIPrefix + pathParamPattern.ReplaceAllString(route.Path, uuid.NewString())
		resp, body := serve(t, router, withBearer(route.Method, path, "reader", `{}`))
		if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "read_only_token" {
			t.Errorf("a read-only token at %s %s = %d %v, want 403 read_only_token", route.Method, route.Path, resp.StatusCode, body)
		}
	}
	// Reads pass the guard; whatever answers them is past it.
	resp, body := serve(t, router, withBearer(http.MethodGet, APIPrefix+"/tokens", "reader", ""))
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("a read-only token was refused a read: %v", body)
	}
}

func TestATokenCannotMakeAnotherToken(t *testing.T) {
	router := tokenServer(t).Routes(nil)
	resp, body := serve(t, router, withBearer(http.MethodPost, APIPrefix+"/tokens", "full", `{"name":"child"}`))
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "session_only" {
		t.Fatalf("a token making a token = %d %v, want 403 session_only", resp.StatusCode, body)
	}
	// A session gets past the guard to a server without the services behind it.
	resp, body = serve(t, router, withBearer(http.MethodPost, APIPrefix+"/tokens", "session", `{"name":"mine"}`))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a session making a token = %d %v, want past the guard", resp.StatusCode, body)
	}
}

func TestATokenKeysItsOwnWrites(t *testing.T) {
	s, f := freshServer(t)
	token := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/themes", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, &auth.Principal{TokenID: &token}))
	rec := httptest.NewRecorder()
	s.readYourWrites(writing(11)).ServeHTTP(rec, req)
	if f.lsns["t:"+token.String()] != 11 || len(f.lsns) != 1 {
		t.Fatalf("the write should be the token's alone: %v", f.lsns)
	}
	if clientCookie(rec.Result()) != nil {
		t.Error("a caller with a token was handed a client cookie")
	}
}

func TestTokenErrorsAreSentences(t *testing.T) {
	for err, field := range map[error]string{auth.ErrTokenName: "name", auth.ErrTokenScope: "scopes", auth.ErrTokenExpiry: "expiresAt"} {
		apiErr := toAPIError(err)
		msg := apiErr.Fields[field]
		if apiErr.Status != http.StatusUnprocessableEntity || !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
			t.Errorf("%v = %d %q, want a 422 sentence on %s", err, apiErr.Status, msg, field)
		}
	}
	for err, field := range map[error]string{auth.ErrTokenSpaces: "spaces", auth.ErrNoSuchSpace: "spaces"} {
		if msg := toAPIError(err).Fields[field]; !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
			t.Errorf("%v = %q, want a sentence on %s", err, msg, field)
		}
	}
	for _, apiErr := range []*APIError{toAPIError(auth.ErrNoSuchToken), errReadOnlyToken, errSessionOnly, errSpacesToken} {
		if !strings.HasSuffix(apiErr.Message, ".") || apiErr.Status >= 500 {
			t.Errorf("%q is not a sentence the caller can act on", apiErr.Message)
		}
	}
}
