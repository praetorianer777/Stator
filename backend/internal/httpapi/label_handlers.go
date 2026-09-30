package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/label"
)

// labelName reads a label out of the route. No label holds a percent sign, so
// unescaping what the router left escaped cannot change one.
func labelName(r *http.Request) string {
	raw := chi.URLParam(r, "labelName")
	if name, err := url.PathUnescape(raw); err == nil {
		return name
	}
	return raw
}

func (s *Server) handleListPageLabels(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	names, err := s.Labels.OnPage(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": names})
}

func (s *Server) handleAddPageLabel(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req label.AddInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	names, lsn, err := s.Labels.Add(r.Context(), actorFrom(r), id, req.Name)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": names})
}

func (s *Server) handleRemovePageLabel(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Labels.Remove(r.Context(), actorFrom(r), id, labelName(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleSuggestLabels(w http.ResponseWriter, r *http.Request) {
	limit, _, apiErr := window(r, label.DefaultSuggestions, label.MaxSuggestions)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	typed := r.URL.Query().Get("q")
	if utf8.RuneCountInString(typed) > label.MaxNameLength {
		respondError(w, r, ErrValidation(map[string]string{"q": "A label has at most " + strconv.Itoa(label.MaxNameLength) + " characters. Type fewer."}))
		return
	}
	found, err := s.Labels.Suggest(r.Context(), actorFrom(r), typed, r.URL.Query().Get("space"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": found})
}

func (s *Server) handleListLabelPages(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, label.DefaultLimit, label.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	pages, total, err := s.Labels.Pages(r.Context(), actorFrom(r), labelName(r), r.URL.Query().Get("space"), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages, "total": total, "limit": limit, "offset": offset})
}
