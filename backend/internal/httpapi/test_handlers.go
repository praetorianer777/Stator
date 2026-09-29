package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/testorg"
)

// Throwaway organizations for the browser suite. They are served only when
// STATOR_TEST_ENDPOINTS is on and are left out of the OpenAPI document: they
// are not part of the API a client may rely on. See docs/decisions.md.

// TestTokenHeader carries the shared secret of the test endpoints. A header of
// its own, so it never meets the session and bearer token handling.
const TestTokenHeader = "X-Stator-Test-Token"

// testOperations describes the test routes as operations does the public
// ones; a test holds the router to it, and Spec never reads it.
var testOperations = []operation{
	{method: "POST", path: "/test/orgs", handler: "handleCreateTestOrg", tag: "test", summary: "Make a throwaway organization with the seed's people and provider in it.",
		request: createTestOrgRequest{}, responses: created(testorg.Org{})},
	{method: "DELETE", path: "/test/orgs/{orgSlug}", handler: "handleDeleteTestOrg", tag: "test", summary: "Remove a throwaway organization with all of its rows and files.",
		responses: none()},
}

type createTestOrgRequest struct {
	// Label starts the slug, so a leftover organization names the spec that made it.
	Label string `json:"label,omitempty"`
}

// requireTestToken refuses a caller without the configured token, and every
// caller when none is configured.
func (s *Server) requireTestToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get(TestTokenHeader)
		if s.TestToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.TestToken)) != 1 {
			respondError(w, r, ErrUnauthorized("Send the test endpoints' token in the "+TestTokenHeader+" header, as STATOR_TEST_ENDPOINTS_TOKEN sets it."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleCreateTestOrg(w http.ResponseWriter, r *http.Request) {
	var req createTestOrgRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			respondError(w, r, err)
			return
		}
	}
	org, err := s.TestOrgs.Create(r.Context(), req.Label)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, org)
}

func (s *Server) handleDeleteTestOrg(w http.ResponseWriter, r *http.Request) {
	err := s.TestOrgs.Delete(r.Context(), chi.URLParam(r, "orgSlug"))
	if errors.Is(err, testorg.ErrNotFound) {
		respondError(w, r, ErrNotFound("No throwaway organization has that slug. It may be gone already, or it was not made by POST "+APIPrefix+"/test/orgs."))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
