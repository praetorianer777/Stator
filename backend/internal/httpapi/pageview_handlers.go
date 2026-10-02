package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/pageview"
)

func (s *Server) handlePageViews(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	counts, err := s.PageViews.Counts(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, counts)
}

func (s *Server) handlePageReaders(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, after, apiErr := keysetWindow(r, pageview.DefaultLimit, pageview.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	readers, err := s.PageViews.List(r.Context(), actorFrom(r), id, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	readers.RetentionDays = int(s.PageViewRetention.Hours() / 24)
	respondJSON(w, r, http.StatusOK, readers)
}
