package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// The organization's people and the groups at its provider that decide their
// roles, for its administrators.

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	members, err := s.Accounts.Members(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		respondError(w, r, errSignInOff)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Accounts.RemoveMember(r.Context(), userID, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

type setGroupRoleRequest struct {
	// Group is a value of the provider's groups claim.
	Group string `json:"group"`
	// Role is what the group grants: member, or admin.
	Role auth.OrgRole `json:"role"`
}

func (s *Server) handleListGroupRoles(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	mapping, err := s.OIDC.GroupRoles(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groupRoles": mapping})
}

func (s *Server) handleSetGroupRole(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	var req setGroupRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.OIDC.SetGroupRole(r.Context(), req.Group, req.Role, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groupRole": saved})
}

func (s *Server) handleRemoveGroupRole(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, errSignInOff)
		return
	}
	id, apiErr := pathUUID(r, "groupRoleID", "group role's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.OIDC.RemoveGroupRole(r.Context(), id, userFrom(r), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
