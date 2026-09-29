package httpapi

import (
	"net/http"
	"time"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// Personal access tokens: a person's own, and the organization's for its
// administrators, as in Armature.

type createTokenRequest struct {
	Name string `json:"name"`
	// Scopes is empty for a token that may do whatever its owner may, or
	// ["read"] for one that may only read.
	Scopes []string `json:"scopes,omitempty"`
	// ExpiresAt is when the token stops working; left out, it lasts until revoked.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req createTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	token, lsn, err := s.Accounts.CreateAPIToken(r.Context(), p.Org.ID, p.UserID,
		auth.NewAPIToken{Name: req.Name, Scopes: req.Scopes, ExpiresAt: req.ExpiresAt}, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"token": token})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	tokens, err := s.Accounts.ListAPITokens(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	id, apiErr := pathUUID(r, "tokenID", "token")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Accounts.RevokeAPIToken(r.Context(), p.Org.ID, p.UserID, id, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListOrgAPITokens(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	tokens, err := s.Accounts.ListOrgAPITokens(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeOrgAPIToken(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	id, apiErr := pathUUID(r, "tokenID", "token")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Accounts.RevokeOrgAPIToken(r.Context(), p.Org.ID, p.UserID, id, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
