package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/testorg"
)

const unitTestToken = "a token for the unit tests"

func testRequests() []*http.Request {
	return []*http.Request{
		httptest.NewRequest(http.MethodPost, APIPrefix+"/test/orgs", strings.NewReader(`{"label":"x"}`)),
		httptest.NewRequest(http.MethodDelete, APIPrefix+"/test/orgs/x-1234", nil),
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
	s.TestOrgs = &testorg.Service{}
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
	s.TestOrgs = &testorg.Service{}
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
