package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Connecting Armature (#27): the organization's instance for its
// administrators, and each member's own token. See docs/api-contract-m3.md.

var errArmatureOff = &APIError{Status: http.StatusServiceUnavailable, Code: "armature_unavailable",
	Message: "This server is not set up to reach Armature. Ask the operator to check its configuration."}

func (s *Server) handleGetArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	connection, err := s.Armature.Connection(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleSaveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.ConnectionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Armature.SaveConnection(r.Context(), req, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleRemoveArmatureConnection(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	lsn, err := s.Armature.RemoveConnection(r.Context(), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	account, err := s.Armature.Account(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleConnectArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.TokenInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	account, lsn, err := s.Armature.Connect(r.Context(), req.Token)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleCheckArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	account, lsn, err := s.Armature.Check(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"account": account})
}

func (s *Server) handleDisconnectArmatureAccount(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	lsn, err := s.Armature.Disconnect(r.Context())
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
