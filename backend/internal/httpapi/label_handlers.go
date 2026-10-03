package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/page"
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
	var req label.LabelInput
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

// listLimit reads a page list's limit, document.DefaultListedPages when absent;
// one that is no number is out of range, which the service refuses in words.
func listLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return document.DefaultListedPages
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

// Content by label (#55): the published pages carrying the labels, all or
// any, as the caller may read them.
func (s *Server) handleLabelledPages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := label.ListInput{Labels: q["label"], Match: q.Get("match"), SpaceKey: q.Get("space"), Sort: q.Get("sort"), Limit: listLimit(r)}
	if in.Match == "" {
		in.Match = document.MatchAll
	}
	if in.Sort == "" {
		in.Sort = document.SortUpdated
	}
	pages, err := s.Labels.Listed(r.Context(), actorFrom(r), in)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

// Recently updated (#55): the pages published last, in a space or anywhere,
// as the caller may read them.
func (s *Server) handleUpdatedPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.Pages.RecentlyUpdated(r.Context(), actorFrom(r), r.URL.Query().Get("space"), listLimit(r))
	if errors.Is(err, page.ErrListLimit) {
		respondError(w, r, ErrValidation(map[string]string{"limit": fmt.Sprintf("List 1 to %d pages.", document.MaxListedPages)}))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

// A properties report (#54): the properties of the pages carrying every
// label given, as the caller may read them.
func (s *Server) handlePropertiesReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	report, err := s.Labels.PropertiesReport(r.Context(), actorFrom(r), label.ReportInput{Labels: q["label"], SpaceKey: q.Get("space"), Columns: q["column"]})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, report)
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
