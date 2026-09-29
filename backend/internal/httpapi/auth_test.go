package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

func cookieFrom(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", name, resp.Header.Values("Set-Cookie"))
	return nil
}

// The session cookie is out of reach of scripts, off cross-site posts, and
// never sent in clear once the deployment says it is served over TLS.
func TestTheSessionCookieFlags(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure=%v", secure), func(t *testing.T) {
			s := newServer(t)
			s.Secure = secure
			rec := httptest.NewRecorder()
			expires := time.Now().Add(time.Hour)
			s.setSessionCookie(rec, "the-secret", expires)
			c := cookieFrom(t, rec.Result(), "stator_session")
			if c.Value != "the-secret" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure != secure {
				t.Errorf("cookie = %+v", c)
			}
			if c.MaxAge < 3500 || c.MaxAge > 3600 {
				t.Errorf("max age = %d, want about an hour", c.MaxAge)
			}

			rec = httptest.NewRecorder()
			s.clearSessionCookie(rec)
			c = cookieFrom(t, rec.Result(), "stator_session")
			if c.Value != "" || c.MaxAge >= 0 || !c.HttpOnly || c.Secure != secure {
				t.Errorf("cleared cookie = %+v", c)
			}
		})
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{"token-user": {UserID: uuid.New()}}
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer token-user")
	resp, _ := serve(t, s.Routes(nil), req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	if c := cookieFrom(t, resp, "stator_session"); c.MaxAge >= 0 {
		t.Errorf("logout left the cookie: %+v", c)
	}

	resp, body := serve(t, s.Routes(nil), httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/logout", nil))
	if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
		t.Errorf("anonymous logout = %d %v", resp.StatusCode, body)
	}
}

func TestSafeRedirectStaysInTheApp(t *testing.T) {
	for next, want := range map[string]string{
		"/spaces/abc?x=1":      "/spaces/abc?x=1",
		"/":                    "/",
		"":                     "",
		"https://evil.test/":   "",
		"//evil.test/":         "",
		"/\\evil.test":         "",
		"spaces":               "",
		"/ok\r\nSet-Cookie: x": "",
	} {
		if got := safeRedirect(next); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestSignInFailuresAreNamedForThePage(t *testing.T) {
	for err, want := range map[error]string{
		oidc.ErrNotAMember: "not_a_member",
		fmt.Errorf("%w: x", oidc.ErrUnreachable): "unreachable",
		oidc.ErrNoEmail:                           "no_email",
		oidc.ErrEmailUnverified:                   "unverified_email",
		fmt.Errorf("x: %w", oidc.ErrUnknownLogin): "expired",
		oidc.ErrNotConfigured:                     "not_configured",
		auth.ErrUserInactive:                      "inactive",
		errors.New("the provider is down"):        "failed",
	} {
		if got := signInFailure(err); got != want {
			t.Errorf("signInFailure(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestTheProviderSettingsAreForAdministratorsOnly(t *testing.T) {
	s := newServer(t)
	org := &tenant.Org{ID: uuid.New(), Slug: "acme"}
	s.Auth = fakeAuth{
		"admin":  {UserID: uuid.New(), Org: org, Role: auth.RoleAdmin},
		"member": {UserID: uuid.New(), Org: org, Role: auth.RoleMember},
		"no-org": {UserID: uuid.New(), Role: auth.RoleOwner},
	}
	get := func(credential string) (*http.Response, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, APIPrefix+"/oidc-provider", nil)
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		return serve(t, s.Routes(nil), req)
	}

	for credential, want := range map[string]struct {
		status int
		code   string
	}{
		"":       {http.StatusUnauthorized, "unauthorized"},
		"member": {http.StatusForbidden, "forbidden"},
		"no-org": {http.StatusBadRequest, "no_organization"},
		// Past the guard, a server without the services says so.
		"admin": {http.StatusServiceUnavailable, "sign_in_unavailable"},
	} {
		resp, body := get(credential)
		if resp.StatusCode != want.status || errorOf(t, body)["code"] != want.code {
			t.Errorf("%q = %d %v, want %d %s", credential, resp.StatusCode, body, want.status, want.code)
		}
	}

	// Letting people in is administration too.
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, APIPrefix+"/users/requests", nil),
		httptest.NewRequest(http.MethodPost, APIPrefix+"/users/requests/"+uuid.NewString()+"/admit", strings.NewReader(`{"role":"member"}`)),
		httptest.NewRequest(http.MethodDelete, APIPrefix+"/users/requests/"+uuid.NewString(), nil),
	} {
		req.Header.Set("Authorization", "Bearer member")
		resp, body := serve(t, s.Routes(nil), req)
		if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "forbidden" {
			t.Errorf("a member at %s %s = %d %v", req.Method, req.URL.Path, resp.StatusCode, body)
		}
	}
}

func TestLoginAsksForBothFields(t *testing.T) {
	s := newServer(t)
	s.Accounts = &auth.Service{}
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/login", strings.NewReader(`{"email":" ","password":""}`))
	req.Header.Set("Content-Type", "application/json")
	resp, body := serve(t, s.Routes(nil), req)
	e := errorOf(t, body)
	fields, _ := e["fields"].(map[string]any)
	if resp.StatusCode != http.StatusUnprocessableEntity || fields["email"] == nil || fields["password"] == nil {
		t.Fatalf("empty login = %d %v", resp.StatusCode, body)
	}

	req = httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/login", strings.NewReader(`{"email":"a@b.test","password":"x","extra":1}`))
	resp, body = serve(t, s.Routes(nil), req)
	if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "bad_request" {
		t.Fatalf("an unknown field = %d %v", resp.StatusCode, body)
	}
}

func TestDomainErrorsAreSentences(t *testing.T) {
	for _, err := range []error{
		auth.ErrInvalidCredentials, auth.ErrUserInactive, auth.ErrNotAMember, auth.ErrSessionStaysHome,
		oidc.ErrNotConfigured, &oidc.ValidationError{Field: "issuer", Message: "Enter it."},
		auth.ErrNoSuchRequest, auth.ErrBadJoinRole,
	} {
		apiErr := toAPIError(err)
		if apiErr.Status >= 500 {
			t.Errorf("%v became a server error", err)
		}
		if !strings.HasSuffix(apiErr.Message, ".") || strings.ToUpper(apiErr.Message[:1]) != apiErr.Message[:1] {
			t.Errorf("%v reads %q, not a sentence", err, apiErr.Message)
		}
	}
}
