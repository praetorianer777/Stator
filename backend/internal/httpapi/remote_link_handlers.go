package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/remotelink"
)

func (s *Server) handleListPageRemoteLinks(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	links, err := s.RemoteLinks.OnPage(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"remoteLinks": links})
}

func (s *Server) handlePutPageRemoteLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req remotelink.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	link, created, lsn, err := s.RemoteLinks.Put(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondJSON(w, r, status, map[string]any{"remoteLink": link})
}

func (s *Server) handleRemovePageRemoteLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	linkID, apiErr := pathUUID(r, "linkID", "remote link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.RemoteLinks.Remove(r.Context(), actorFrom(r), id, linkID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
