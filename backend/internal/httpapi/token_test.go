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
	session, full, reader := uuid.New(), uuid.New(), uuid.New()
	s.Auth = fakeAuth{
		"session": {UserID: user, Org: org, Role: auth.RoleMember, SessionID: &session},
		"full":    {UserID: user, Org: org, Role: auth.RoleMember, TokenID: &full},
		"reader":  {UserID: user, Org: org, Role: auth.RoleMember, TokenID: &reader, Scopes: []string{auth.ScopeRead}},
	}
	return s
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
		if route.Method == http.MethodGet {
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
	for _, apiErr := range []*APIError{toAPIError(auth.ErrNoSuchToken), errReadOnlyToken, errSessionOnly} {
		if !strings.HasSuffix(apiErr.Message, ".") || apiErr.Status >= 500 {
			t.Errorf("%q is not a sentence the caller can act on", apiErr.Message)
		}
	}
}
