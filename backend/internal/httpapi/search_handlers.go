package httpapi

import (
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/praetorianer777/stator/backend/internal/search"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, search.DefaultLimit, search.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q, err := search.ParseQuery(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	q.Limit, q.Offset = limit, offset
	hits, total, err := s.Search.Search(r.Context(), actorFrom(r), q)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"hits": hits, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleQuickSearch(w http.ResponseWriter, r *http.Request) {
	limit, _, apiErr := window(r, search.DefaultQuickLimit, search.MaxQuickLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	typed := r.URL.Query().Get("q")
	if utf8.RuneCountInString(typed) > search.MaxQueryLength {
		respondError(w, r, ErrValidation(map[string]string{"q": "Keep the search to " + strconv.Itoa(search.MaxQueryLength) + " characters."}))
		return
	}
	pages, err := s.Search.Quick(r.Context(), actorFrom(r), typed, r.URL.Query().Get("space"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) handleRecentPages(w http.ResponseWriter, r *http.Request) {
	limit, _, apiErr := window(r, search.DefaultRecentLimit, search.MaxRecentLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	pages, err := s.Search.Recent(r.Context(), actorFrom(r), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) handleVisitPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Search.Visit(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
