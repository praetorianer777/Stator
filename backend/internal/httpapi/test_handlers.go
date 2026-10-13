package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/testinstance"
)

// What the browser suite arranges: people of a spec's own and the
// organization's settings put back.
// They are served only when STATOR_TEST_ENDPOINTS is on and are left out of
// the OpenAPI document: they are not part of the API a client may rely on.
// See docs/decisions.md.

// TestTokenHeader carries the shared secret of the test endpoints. A header of
// its own, so it never meets the session and bearer token handling.
const TestTokenHeader = "X-Stator-Test-Token"

// testOperations describes the test routes as operations does the public
// ones; a test holds the router to it, and Spec never reads it.
var testOperations = []operation{
	{method: "POST", path: "/test/people", handler: "handleCreateTestPerson", tag: "test", summary: "Make a person in the organization and sign them in, with no identity provider.",
		request: createTestPersonRequest{}, responses: created(testinstance.Person{})},
	{method: "DELETE", path: "/test/people/{userID}", handler: "handleDeleteTestPerson", tag: "test", summary: "Remove a test person with the spaces they made.",
		responses: none()},
	{method: "POST", path: "/test/reset", handler: "handleResetTestInstance", tag: "test", summary: "Put the organization's settings back as the bootstrap left them.",
		responses: none()},
	{method: "POST", path: "/test/spaces/{spaceKey}/example-space/age", handler: "handleAgeTestExampleSpace", tag: "test", summary: "Make an example space one made before its content had versions, without the page of the file named.",
		request: ageTestExampleRequest{}, responses: none()},
}

// TestInstance arranges the instance's one organization;
// *testinstance.Service is the real one.
type TestInstance interface {
	CreatePerson(ctx context.Context, in testinstance.NewPerson) (*testinstance.Session, error)
	DeletePerson(ctx context.Context, id uuid.UUID) error
	Reset(ctx context.Context) error
	AgeExample(ctx context.Context, spaceKey, drop string) error
}

// testPathPrefix is where every test endpoint lives.
const testPathPrefix = APIPrefix + "/test/"

func isTestPath(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, testPathPrefix)
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

type ageTestExampleRequest struct {
	// Drop names the file of the content whose page the older example lacks.
	Drop string `json:"drop"`
}

type createTestPersonRequest struct {
	// Label starts the address, so a leftover person names the spec that made them.
	Label string `json:"label,omitempty"`
	// Name is what the interface shows; the label when empty.
	Name string       `json:"name,omitempty"`
	Role auth.Role `json:"role"`
	// SpaceKey is the one space a guest is let into, as a viewer.
	SpaceKey string `json:"spaceKey,omitempty"`
}

func (s *Server) handleCreateTestPerson(w http.ResponseWriter, r *http.Request) {
	var req createTestPersonRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, err := s.TestInstance.CreatePerson(r.Context(), testinstance.NewPerson{Label: req.Label, Name: req.Name, Role: req.Role, SpaceKey: req.SpaceKey})
	switch {
	case errors.Is(err, testinstance.ErrBadRole):
		respondError(w, r, ErrValidation(map[string]string{"role": "Choose admin, member or guest."}))
		return
	case errors.Is(err, testinstance.ErrGuestSpace):
		respondError(w, r, ErrValidation(map[string]string{"spaceKey": "Name an existing space for a guest, and none for anybody else."}))
		return
	case err != nil:
		respondError(w, r, err)
		return
	}
	s.noteSession(r.Context(), made.SessionID, made.LSN)
	s.setSessionCookie(w, made.Secret, made.ExpiresAt)
	respondJSON(w, r, http.StatusCreated, made.Person)
}

func (s *Server) handleDeleteTestPerson(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "userID", "person")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	err := s.TestInstance.DeletePerson(r.Context(), id)
	if errors.Is(err, testinstance.ErrNotFound) {
		respondError(w, r, ErrNotFound("No test person has that id. They may be gone already, or were not made by POST "+APIPrefix+"/test/people."))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleResetTestInstance(w http.ResponseWriter, r *http.Request) {
	if err := s.TestInstance.Reset(r.Context()); err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleAgeTestExampleSpace(w http.ResponseWriter, r *http.Request) {
	var req ageTestExampleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	err := s.TestInstance.AgeExample(r.Context(), chi.URLParam(r, "spaceKey"), req.Drop)
	if errors.Is(err, testinstance.ErrNoExample) {
		respondError(w, r, ErrNotFound("No example space has that key and a page of that name. Make the example space first."))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
