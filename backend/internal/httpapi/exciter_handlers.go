package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/exciter"
)

// Connecting Exciter (#440): the installation for the administrators, each
// member's own token, and requirements by key as the caller may see them.
// It follows Armature's routes; see docs/integration.md.

var errExciterOff = &APIError{Status: http.StatusServiceUnavailable, Code: "exciter_unavailable",
	Message: "This server is not set up to reach Exciter. Ask the operator to check its configuration."}

func (s *Server) handleGetExciterConnection(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	connection, err := s.Exciter.Connection(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleSaveExciterConnection(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	var req exciter.ConnectionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Exciter.SaveConnection(r.Context(), req, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleCheckExciterConnection(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	var req exciter.ConnectionCheckInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	check, err := s.Exciter.CheckConnection(r.Context(), req)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"check": check})
}

func (s *Server) handleRemoveExciterConnection(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	lsn, err := s.Exciter.RemoveConnection(r.Context(), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetExciterAccount(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	account, err := s.Exciter.Account(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleConnectExciterAccount(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	var req exciter.TokenInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	account, lsn, err := s.Exciter.Connect(r.Context(), req.Token)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleCheckExciterAccount(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	account, lsn, err := s.Exciter.Check(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleDisconnectExciterAccount(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	lsn, err := s.Exciter.Disconnect(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// Chips: requirements by key as the caller may see them in Exciter. A read
// that cannot ask Exciter answers 200 with the status saying why.

func (s *Server) handleLookupExciterRequirements(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	keys, err := requirementKeys(r.URL.Query()["key"])
	if err != nil {
		respondError(w, r, err)
		return
	}
	status, found, err := s.Exciter.Lookup(r.Context(), keys)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if found == nil {
		found = []exciter.RequirementResult{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "requirements": found})
}

const requirementKeyExample = "Write the project key, -R and the number, such as CHK-R42."

// requirementKeys is the distinct keys asked for, upper case, in their order.
func requirementKeys(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("Name 1 to %d requirement keys, such as CHK-R42.", exciter.MaxLookupKeys)})
	}
	keys := make([]string, 0, len(raw))
	for _, k := range raw {
		key, ok := exciter.NormalizeKey(k)
		if !ok {
			return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("%q is not an Exciter requirement key. %s", k, requirementKeyExample)})
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) > exciter.MaxLookupKeys {
		return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("Ask for at most %d requirement keys at once, and for the rest in another request.", exciter.MaxLookupKeys)})
	}
	return keys, nil
}

func (s *Server) handleGetExciterRequirement(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	key, ok := exciter.NormalizeKey(chi.URLParam(r, "reqKey"))
	if !ok {
		respondError(w, r, ErrValidation(map[string]string{"reqKey": "That is not an Exciter requirement key. " + requirementKeyExample}))
		return
	}
	status, requirement, err := s.Exciter.Requirement(r.Context(), key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "requirement": requirement})
}

// The picker: requirements by the start of their key or title, or by a key
// they had, as the caller may see them.
func (s *Server) handleSearchExciterRequirements(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	status, found, err := s.Exciter.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if found == nil {
		found = []exciter.Hit{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "requirements": found})
}

func (s *Server) handleListExciterProjects(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	status, projects, err := s.Exciter.Projects(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if projects == nil {
		projects = []exciter.Project{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "projects": projects})
}

// The list block: one window of what a query finds in a project, as the
// caller may see it in Exciter.
func (s *Server) handleListExciterRequirements(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	limit, offset, bad := window(r, exciter.DefaultListLimit, exciter.MaxListLimit)
	if bad != nil {
		respondError(w, r, bad)
		return
	}
	status, found, err := s.Exciter.List(r.Context(), chi.URLParam(r, "projectKey"), r.URL.Query().Get("query"), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if found == nil {
		found = &exciter.ListResult{Requirements: []exciter.Row{}, Limit: limit, Offset: offset, Found: true}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"status": status, "requirements": found.Requirements, "total": found.Total, "limit": found.Limit, "offset": found.Offset, "found": found.Found,
	})
}

// Completing a list block's query at the caret.
func (s *Server) handleSuggestExciterQuery(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	q := r.URL.Query()
	at := -1
	if raw := q.Get("at"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			respondError(w, r, ErrValidation(map[string]string{"at": "Put the caret inside the query, counted in characters from 0."}))
			return
		}
		at = n
	}
	status, suggestions, err := s.Exciter.Suggest(r.Context(), chi.URLParam(r, "projectKey"), q.Get("q"), at)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "suggestions": suggestions})
}

// Webhooks: Exciter signs each delivery and its time with the site's secret.
// A site without a secret and a wrong signature get one answer, so the
// address never tells which exist.

var errExciterBadSignature = &APIError{Status: http.StatusUnauthorized, Code: "bad_signature",
	Message: "The signature does not match. Check that the webhook secret in Exciter is the one saved in Stator under Settings, Exciter."}

var errExciterWebhookStale = &APIError{Status: http.StatusUnauthorized, Code: "stale_delivery",
	Message: fmt.Sprintf("The delivery was signed more than %d minutes away from Stator's clock. Check that both servers keep the right time; Exciter sends it again by itself.", int(armature.WebhookTimestampTolerance.Minutes()))}

var errExciterWebhookVersion = &APIError{Status: http.StatusUnprocessableEntity, Code: "unsupported_version",
	Message: fmt.Sprintf("Stator reads webhook envelope version %d. Register the webhook in Exciter for version %d.", exciter.WebhookVersion, exciter.WebhookVersion)}

var errExciterWebhookTooLarge = &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
	Message: "The delivery is larger than Stator reads. Subscribe the webhook to the requirement topics only."}

func (s *Server) handleExciterWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Exciter == nil {
		respondError(w, r, errExciterOff)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, armature.WebhookMaxBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		respondError(w, r, errExciterWebhookTooLarge)
		return
	}
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery could not be read. Exciter sends it again by itself."))
		return
	}
	err = s.Exciter.Webhook(r.Context(), body, exciter.WebhookHeaders{
		Signature:            r.Header.Get(exciter.SignatureHeader),
		Timestamp:            r.Header.Get(exciter.TimestampHeader),
		TimestampedSignature: r.Header.Get(exciter.TimestampedSignatureHeader),
		Version:              r.Header.Get(exciter.WebhookVersionHeader),
	})
	switch {
	case errors.Is(err, exciter.ErrBadSignature):
		respondError(w, r, errExciterBadSignature)
	case errors.Is(err, exciter.ErrWebhookStale):
		respondError(w, r, errExciterWebhookStale)
	case errors.Is(err, exciter.ErrWebhookVersion):
		respondError(w, r, errExciterWebhookVersion)
	case err != nil:
		respondError(w, r, err)
	default:
		respondNoContent(w)
	}
}
