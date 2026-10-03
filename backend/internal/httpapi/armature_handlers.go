package httpapi

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Connecting Armature (#27): the organization's instance for its
// administrators, and each member's own token. See docs/api-contract-m3.md.

var errArmatureOff = &APIError{Status: http.StatusServiceUnavailable, Code: "armature_unavailable",
	Message: "This server is not set up to reach Armature. Ask the operator to check its configuration."}

func (s *Server) handleGetArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	connection, err := s.Armature.Connection(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleSaveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.ConnectionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Armature.SaveConnection(r.Context(), req, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleRemoveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	lsn, err := s.Armature.RemoveConnection(r.Context(), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	account, err := s.Armature.Account(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleConnectArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.TokenInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	account, lsn, err := s.Armature.Connect(r.Context(), req.Token)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleCheckArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	account, lsn, err := s.Armature.Check(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleDisconnectArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	lsn, err := s.Armature.Disconnect(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// Smart links (#28): issues by key as the caller may see them in Armature. A
// read that cannot ask Armature answers 200 with the status saying why.

func (s *Server) handleLookupArmatureIssues(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	keys, err := lookupKeys(r.URL.Query()["key"])
	if err != nil {
		respondError(w, r, err)
		return
	}
	status, issues, err := s.Armature.Lookup(r.Context(), keys)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if issues == nil {
		issues = []armature.IssueResult{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issues": issues})
}

// lookupKeys is the distinct keys asked for, upper case, in their order.
func lookupKeys(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("Name 1 to %d issue keys, such as CP-12.", armature.MaxLookupKeys)})
	}
	keys := make([]string, 0, len(raw))
	for _, k := range raw {
		key, ok := armature.NormalizeKey(k)
		if !ok {
			return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("%q is not an Armature issue key. Write the project key, a hyphen and the number, such as CP-12.", k)})
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) > armature.MaxLookupKeys {
		return nil, ErrValidation(map[string]string{"key": fmt.Sprintf("Ask for at most %d issue keys at once, and for the rest in another request.", armature.MaxLookupKeys)})
	}
	return keys, nil
}

func (s *Server) handleGetArmatureIssue(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	key, ok := armature.NormalizeKey(chi.URLParam(r, "issueKey"))
	if !ok {
		respondError(w, r, ErrValidation(map[string]string{"issueKey": "That is not an Armature issue key. Write the project key, a hyphen and the number, such as CP-12."}))
		return
	}
	status, issue, err := s.Armature.Issue(r.Context(), key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issue": issue})
}

func (s *Server) handleListArmatureProjects(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	status, projects, err := s.Armature.Projects(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if projects == nil {
		projects = []armature.Project{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "projects": projects})
}

// The list block (#30): one page of the issues an NQL query matches, as the
// caller may see them in Armature.
func (s *Server) handleSearchArmatureIssues(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	limit, offset, bad := window(r, armature.DefaultListLimit, armature.MaxListLimit)
	if bad != nil {
		respondError(w, r, bad)
		return
	}
	status, found, err := s.Armature.Search(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if found == nil {
		found = &armature.SearchResult{Issues: []armature.Issue{}, Limit: limit, Offset: offset}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"status": status, "issues": found.Issues, "total": found.Total, "limit": found.Limit, "offset": found.Offset, "url": found.URL,
	})
}

// The chart block (#51): a count of the issues an NQL query matches, as the
// caller may see them, drawn as a pie or as created against resolved.
func (s *Server) handleArmatureChart(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	q := r.URL.Query()
	in := armature.ChartInput{Project: q.Get("project"), Query: q.Get("q"), Kind: armature.ChartKind(q.Get("kind")), GroupBy: q.Get("groupBy"), Days: armature.DefaultChartDays}
	if raw := q.Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"days": fmt.Sprintf("Count %d to %d days back.", armature.MinChartDays, armature.MaxChartDays)}))
			return
		}
		in.Days = n
	}
	status, chart, err := s.Armature.Chart(r.Context(), in)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "chart": chart})
}

// The roadmap block (#52): the issues an NQL query matches on a timeline of
// their start and due days, under their epics or their teams.
func (s *Server) handleArmatureRoadmap(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	q := r.URL.Query()
	status, roadmap, err := s.Armature.Roadmap(r.Context(), armature.RoadmapInput{Project: q.Get("project"), Query: q.Get("q"), GroupBy: armature.RoadmapGrouping(q.Get("groupBy"))})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "roadmap": roadmap})
}

func (s *Server) handleListArmatureIssueTypes(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	status, types, err := s.Armature.IssueTypes(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if types == nil {
		types = []armature.IssueType{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issueTypes": types})
}

// Creating issues (#31): one Armature issue per item of a selection on a page
// the caller may edit, filed as the caller, stopping at the first refusal.

func (s *Server) handleCreateArmatureIssues(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.CreateIssuesInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	found, sp, err := s.Pages.Get(r.Context(), actorFrom(r), req.PageID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !found.Can.Edit {
		respondError(w, r, found.Refusal(perm.EditPages))
		return
	}
	if err := armature.CheckCreate(&req); err != nil {
		respondError(w, r, err)
		return
	}
	made, at, err := s.Armature.CreateIssues(r.Context(), req, armature.PageLink{ID: found.ID, SpaceKey: sp.Key, Title: found.Title})
	var failed *armature.CreateFailure
	if err != nil {
		if at == 0 {
			respondError(w, r, err)
			return
		}
		refusal := toAPIError(err)
		failed = &armature.CreateFailure{Index: at, Code: refusal.Code, Message: refusalSentence(refusal)}
		loggerFrom(r.Context()).Info("Armature refused an issue of a selection", "index", at, "code", refusal.Code, "error", err)
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"issues": made, "failed": failed})
}

// refusalSentence is what to tell the author about one item: Armature's
// sentences on its fields say more than its general "Some fields need attention".
func refusalSentence(refusal *APIError) string {
	if len(refusal.Fields) == 0 {
		return refusal.Message
	}
	sentences := make([]string, 0, len(refusal.Fields))
	for _, field := range slices.Sorted(maps.Keys(refusal.Fields)) {
		sentences = append(sentences, sentence(refusal.Fields[field]))
	}
	return strings.Join(sentences, " ")
}

// Pages in Armature (#32): which issues the page's published version names,
// and whether each carries the page's remote link yet.
func (s *Server) handleListArmatureLinks(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, _, err := s.Pages.Get(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	links := []armature.Link{}
	if !found.Unpublished {
		if links, err = s.Armature.PageLinks(r.Context(), found.ID, found.Body); err != nil {
			respondError(w, r, err)
			return
		}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links})
}

// Webhooks (#33): Armature signs each delivery with the organization's
// secret. An unknown organization, one without a secret and a wrong
// signature get one answer, so the address never tells which exist.

var errBadSignature = &APIError{Status: http.StatusUnauthorized, Code: "bad_signature",
	Message: "The signature does not match. Check that the webhook secret in Armature is the one saved in Stator under Settings, Armature."}

var errWebhookTooLarge = &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
	Message: "The delivery is larger than Stator reads. Subscribe the endpoint to the issue topics only."}

func (s *Server) handleArmatureWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, armature.WebhookMaxBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		respondError(w, r, errWebhookTooLarge)
		return
	}
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery could not be read. Armature sends it again by itself."))
		return
	}
	err = s.Armature.Webhook(r.Context(), chi.URLParam(r, "orgSlug"), body, r.Header.Get(armature.SignatureHeader))
	if errors.Is(err, armature.ErrBadSignature) {
		respondError(w, r, errBadSignature)
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
