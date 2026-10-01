package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/stale"
)

func (s *Server) handleListStalePages(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, stale.DefaultLimit, stale.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	filter, problems := stale.ParseFilter(r.URL.Query())
	if len(problems) > 0 {
		respondError(w, r, ErrValidation(problems))
		return
	}
	pages, next, err := s.Stale.List(r.Context(), actorFrom(r), filter, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages, "next": next})
}
