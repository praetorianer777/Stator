package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/guest"
)

// The guests of a space, whom the organization's administrators invite and
// take out again.

func (s *Server) handleListGuests(w http.ResponseWriter, r *http.Request) {
	guests, err := s.Guests.List(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"guests": guests})
}

func (s *Server) handleInviteGuest(w http.ResponseWriter, r *http.Request) {
	var req guest.InviteInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Guests.Invite(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"guest": made})
}

func (s *Server) handleRemoveGuest(w http.ResponseWriter, r *http.Request) {
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Guests.Remove(r.Context(), actorFrom(r), spaceKey(r), userID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
