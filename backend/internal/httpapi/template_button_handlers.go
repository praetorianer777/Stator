package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// A template button (#62): what it would make and where, as the caller sees it.
func (s *Server) handleTemplateButton(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := page.TemplateTarget{Template: q.Get("template"), SpaceKey: q.Get("spaceKey")}
	if raw := q.Get("parentId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"parentId": "That is not a page's id. Pick the page the new one goes under again."}))
			return
		}
		in.Parent = &id
	}
	button, err := s.Pages.TemplateButton(r.Context(), actorFrom(r), in)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, button)
}

// A page made from a template, for a template button or an assistant.
func (s *Server) handleCreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req page.FromTemplateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Pages.CreateFromTemplate(r.Context(), actorFrom(r), chi.URLParam(r, "templateKey"), req, time.Now())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"page": made})
}

// A contributors block: who published the page, or it and the pages below it.
func (s *Server) handlePageContributors(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	in := page.ContributorsQuery{Scope: q.Get("scope"), Limit: document.DefaultContributors}
	if in.Scope == "" {
		in.Scope = document.ContributorsPage
	}
	// A limit that is no number is out of range, which the service refuses in words.
	if raw := q.Get("limit"); raw != "" {
		in.Limit, _ = strconv.Atoi(raw)
	}
	out, err := s.Pages.Contributors(r.Context(), actorFrom(r), id, in)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, out)
}
