package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/hub"
)

// The organization's hub: everybody reads it, administrators choose it.

func (s *Server) handleGetHub(w http.ResponseWriter, r *http.Request) {
	got, err := s.Hub.Get(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"hub": got})
}

func (s *Server) handleSetHub(w http.ResponseWriter, r *http.Request) {
	var req hub.HubInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Hub.Set(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"hub": got})
}
