package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Access: roles and what they hold, groups, and who holds what where (#585),
// in the shapes Armature answers them in.

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.Perms.Roles(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"permissions": perm.Permissions()})
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req perm.RoleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	role, lsn, err := s.Perms.CreateRole(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"role": role})
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req perm.RoleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	role, lsn, err := s.Perms.UpdateRole(r.Context(), actorFrom(r), chi.URLParam(r, "roleKey"), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"role": role})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Perms.DeleteRole(r.Context(), actorFrom(r), chi.URLParam(r, "roleKey"))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req perm.GroupInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	group, lsn, err := s.Perms.CreateGroup(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"group": group})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "groupID", "group")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	group, err := s.Perms.Group(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": group})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "groupID", "group")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req perm.GroupInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	group, lsn, err := s.Perms.UpdateGroup(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": group})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "groupID", "group")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Perms.DeleteGroup(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "groupID", "group")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req perm.GroupMemberInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	group, lsn, err := s.Perms.AddGroupMember(r.Context(), actorFrom(r), id, req.UserID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": group})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "groupID", "group")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	userID, apiErr := pathUUID(r, "userID", "person's")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	group, lsn, err := s.Perms.RemoveGroupMember(r.Context(), actorFrom(r), id, userID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": group})
}

func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, perm.DefaultAssignmentLimit, perm.MaxAssignmentLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	scope := q.Get("scope")
	if scope != "" && scope != "installation" {
		respondError(w, r, ErrValidation(map[string]string{"scope": "Leave scope out, or ask for installation to see only the grants across the whole installation."}))
		return
	}
	page, err := s.Perms.Assignments(r.Context(), actorFrom(r), perm.AssignmentFilter{
		Q: q.Get("q"), Space: q.Get("space"), Installation: scope == "installation", Role: q.Get("role"), Limit: limit, Offset: offset,
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, page)
}

func (s *Server) handleGrantRole(w http.ResponseWriter, r *http.Request) {
	var req perm.GrantInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	assignment, lsn, err := s.Perms.Grant(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"assignment": assignment})
}

func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "assignmentID", "grant")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Perms.Revoke(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
