package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/watch"
)

func (s *Server) handleWatchPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var in watch.Input
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	watching, lsn, err := s.Watches.WatchPage(r.Context(), actorFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watching": watching})
}

func (s *Server) handleUnwatchPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Watches.UnwatchPage(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListWatchers(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, offset, apiErr := window(r, watch.DefaultLimit, watch.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	watchers, total, err := s.Watches.Watchers(r.Context(), actorFrom(r), id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watchers": watchers, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleWatchSpace(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.WatchSpace(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleUnwatchSpace(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.UnwatchSpace(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListWatches(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, watch.DefaultLimit, watch.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	watches, total, err := s.Watches.List(r.Context(), actorFrom(r), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watches": watches, "total": total, "limit": limit, "offset": offset})
}
