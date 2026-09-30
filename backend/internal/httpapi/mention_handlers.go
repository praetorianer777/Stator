package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

func (s *Server) handleListMentionable(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, _, apiErr := window(r, perm.DefaultPickerLimit, perm.MaxPickerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	people, err := s.Pages.Mentionable(r.Context(), actorFrom(r), id, r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": people})
}
