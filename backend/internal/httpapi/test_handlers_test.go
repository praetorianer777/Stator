package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/testinstance"
)

const unitTestToken = "a token for the unit tests"

// fakeTestInstance answers without a database; err, when set, is what every
// call returns.
type fakeTestInstance struct {
	made, deleted, reset, aged int
	asked                      testinstance.NewPerson
	err                        error
}

func (f *fakeTestInstance) CreatePerson(_ context.Context, in testinstance.NewPerson) (*testinstance.Session, error) {
	f.made++
	f.asked = in
	if f.err != nil {
		return nil, f.err
	}
	return &testinstance.Session{
		Person:    testinstance.Person{ID: uuid.New(), Email: in.Label + "-0123abcd@" + testinstance.PeopleDomain, Name: in.Name, Role: in.Role},
		SessionID: uuid.New(), Secret: "a session secret", ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeTestInstance) DeletePerson(context.Context, uuid.UUID) error {
	f.deleted++
	return f.err
}

func (f *fakeTestInstance) Reset(context.Context) error {
	f.reset++
	return f.err
}

func (f *fakeTestInstance) AgeExample(context.Context, string, string) error {
	f.aged++
	return f.err
}

// Sessions and access tokens neither open the test endpoints nor refuse them:
// a read-only token, a deactivated account or a session on a foreign origin
// still comes down to the test token alone.
func TestTestEndpointsIgnoreSessionsAndAccessTokens(t *testing.T) {
	s := newServer(t)
	tokenID, sessionID := uuid.New(), uuid.New()
	s.Auth = fakeAuth{
		"read-only": {UserID: uuid.New(), TokenID: &tokenID, Scopes: []string{auth.ScopeRead}},
		"session":   {UserID: uuid.New(), SessionID: &sessionID, Role: auth.RoleMember, Administers: true},
	}
	instance := &fakeTestInstance{}
	s.TestInstance = instance
	s.TestToken = unitTestToken
	h := s.Routes(nil)

	withCredential := func(req *http.Request, credential string) *http.Request {
		switch credential {
		case "session":
			req.AddCookie(&http.Cookie{Name: s.CookieName, Value: credential})
			req.Header.Set("Origin", "http://elsewhere.test")
		case "":
		default:
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		return req
	}
	for _, credential := range []string{"read-only", "blocked", "session", "no such token"} {
		for _, req := range testRequests() {
			resp, body := serve(t, h, withCredential(req, credential))
			if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
				t.Errorf("%s %s with %q and no test token = %d %v, want 401", req.Method, req.URL.Path, credential, resp.StatusCode, body)
			}
		}
		for i, req := range testRequests() {
			req.Header.Set(TestTokenHeader, unitTestToken)
			req.Header.Set("Content-Type", "application/json")
			want := testRequestStatuses[i]
			if resp, body := serve(t, h, withCredential(req, credential)); resp.StatusCode != want {
				t.Errorf("%s %s with %q and the test token = %d %v, want %d", req.Method, req.URL.Path, credential, resp.StatusCode, body, want)
			}
		}
	}
	if instance.made != 4 || instance.deleted != 4 || instance.reset != 4 || instance.aged != 4 {
		t.Fatalf("reached the instance %d, %d, %d and %d times, want 4 each", instance.made, instance.deleted, instance.reset, instance.aged)
	}
}

// testRequestStatuses is what each of testRequests answers with the token.
var testRequestStatuses = []int{http.StatusCreated, http.StatusNoContent, http.StatusNoContent, http.StatusNoContent}

func testRequests() []*http.Request {
	return []*http.Request{
		httptest.NewRequest(http.MethodPost, APIPrefix+"/test/people", strings.NewReader(`{"label":"x","role":"member"}`)),
		httptest.NewRequest(http.MethodDelete, APIPrefix+"/test/people/"+uuid.NewString(), nil),
		httptest.NewRequest(http.MethodPost, APIPrefix+"/test/reset", nil),
		httptest.NewRequest(http.MethodPost, APIPrefix+"/test/spaces/EX/example-space/age", strings.NewReader(`{"drop":"x"}`)),
	}
}

// A test person comes back with a session cookie as a sign-in sets it, so the
// suite holds a real session without walking through a provider.
func TestATestPersonComesSignedIn(t *testing.T) {
	s := newServer(t)
	instance := &fakeTestInstance{}
	s.TestInstance = instance
	s.TestToken = unitTestToken
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/test/people", strings.NewReader(`{"label":"bob-themes","name":"Bob themes","role":"member"}`))
	req.Header.Set(TestTokenHeader, unitTestToken)
	req.Header.Set("Content-Type", "application/json")
	resp, body := serve(t, s.Routes(nil), req)
	if resp.StatusCode != http.StatusCreated || body["name"] != "Bob themes" || body["role"] != "member" {
		t.Fatalf("POST /test/people = %d %v", resp.StatusCode, body)
	}
	if instance.asked.Label != "bob-themes" || instance.asked.Role != auth.RoleMember {
		t.Errorf("the service was asked for %+v", instance.asked)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == s.CookieName {
			session = c
		}
	}
	if session == nil || session.Value != "a session secret" || !session.HttpOnly || session.Path != "/" {
		t.Fatalf("the session cookie is %+v", session)
	}
}

// Each refusal of the service reaches the caller as a sentence naming what to
// change, with the status a client would expect.
func TestTheTestInstanceEndpointsSayWhatWentWrong(t *testing.T) {
	for _, tc := range []struct {
		err    error
		req    *http.Request
		status int
		code   string
	}{
		{testinstance.ErrBadRole, httptest.NewRequest(http.MethodPost, APIPrefix+"/test/people", strings.NewReader(`{"role":"owner"}`)), http.StatusUnprocessableEntity, "validation_failed"},
		{testinstance.ErrGuestSpace, httptest.NewRequest(http.MethodPost, APIPrefix+"/test/people", strings.NewReader(`{"role":"guest"}`)), http.StatusUnprocessableEntity, "validation_failed"},
		{testinstance.ErrNotFound, httptest.NewRequest(http.MethodDelete, APIPrefix+"/test/people/"+uuid.NewString(), nil), http.StatusNotFound, "not_found"},
		{nil, httptest.NewRequest(http.MethodDelete, APIPrefix+"/test/people/not-an-id", nil), http.StatusBadRequest, "bad_request"},
		{testinstance.ErrNoExample, httptest.NewRequest(http.MethodPost, APIPrefix+"/test/spaces/EX/example-space/age", strings.NewReader(`{"drop":"x"}`)), http.StatusNotFound, "not_found"},
	} {
		s := newServer(t)
		s.TestInstance = &fakeTestInstance{err: tc.err}
		s.TestToken = unitTestToken
		tc.req.Header.Set(TestTokenHeader, unitTestToken)
		tc.req.Header.Set("Content-Type", "application/json")
		resp, body := serve(t, s.Routes(nil), tc.req)
		if resp.StatusCode != tc.status || errorOf(t, body)["code"] != tc.code {
			t.Errorf("%s %s refused with %v = %d %v, want %d %s", tc.req.Method, tc.req.URL.Path, tc.err, resp.StatusCode, body, tc.status, tc.code)
		}
	}
}

// Off, the test endpoints are paths like any other that does not exist, even
// to a caller who knows the token.
func TestTestEndpointsAreNotFoundWhenOff(t *testing.T) {
	s := newServer(t)
	s.TestToken = unitTestToken
	h := s.Routes(nil)
	for _, req := range testRequests() {
		req.Header.Set(TestTokenHeader, unitTestToken)
		resp, body := serve(t, h, req)
		if resp.StatusCode != http.StatusNotFound || errorOf(t, body)["code"] != "not_found" {
			t.Errorf("%s %s with the endpoints off = %d %v, want 404", req.Method, req.URL.Path, resp.StatusCode, body)
		}
	}
}

func TestTestEndpointsRefuseAWrongOrMissingToken(t *testing.T) {
	s := newServer(t)
	s.TestInstance = &fakeTestInstance{}
	s.TestToken = unitTestToken
	h := s.Routes(nil)
	for _, token := range []string{"", "wrong", unitTestToken + "x"} {
		for _, req := range testRequests() {
			if token != "" {
				req.Header.Set(TestTokenHeader, token)
			}
			resp, body := serve(t, h, req)
			if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
				t.Errorf("%s %s with token %q = %d %v, want 401", req.Method, req.URL.Path, token, resp.StatusCode, body)
			}
		}
	}

	s.TestToken = ""
	h = s.Routes(nil)
	req := testRequests()[0]
	req.Header.Set(TestTokenHeader, "")
	if resp, _ := serve(t, h, req); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("with no token configured an empty one got in: %d", resp.StatusCode)
	}
}

// On, the router serves exactly the public table and the test one, and the
// published document still names only the public table.
func TestTestRoutesAreServedWhenOnAndNeverDocumented(t *testing.T) {
	s := newServer(t)
	s.TestInstance = &fakeTestInstance{}
	served := routedBy(t, s)
	want := map[string]bool{}
	for _, op := range append(append([]operation{}, operations...), testOperations...) {
		want[op.method+" "+op.path] = true
	}
	for key := range served {
		if !want[key] {
			t.Errorf("%s is served but in neither table", key)
		}
	}
	for key := range want {
		if !served[key] {
			t.Errorf("%s is in a table but not served", key)
		}
	}
	for path := range Spec().Paths {
		if strings.HasPrefix(path, "/test/") {
			t.Errorf("the OpenAPI document names the test path %s", path)
		}
	}
}
