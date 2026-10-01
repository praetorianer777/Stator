package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/share"
)

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var in share.Input
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	shared, lsn, err := s.Shares.Share(r.Context(), actorFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"share": shared})
}

func (s *Server) handleShareRecipients(w http.ResponseWriter, r *http.Request) {
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
	people, groups, err := s.Shares.Recipients(r.Context(), actorFrom(r), id, r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": people, "groups": groups})
}

func (s *Server) handleListViewers(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, offset, apiErr := window(r, share.DefaultViewerLimit, share.MaxViewerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	viewers, err := s.Shares.Viewers(r.Context(), actorFrom(r), id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"viewers": viewers.People, "total": viewers.Total, "everyone": viewers.Everyone, "limit": limit, "offset": offset,
	})
}
