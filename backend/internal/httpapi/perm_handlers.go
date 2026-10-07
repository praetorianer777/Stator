package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Permissions: the caller's own, the organization's, a space's and a page's,
// and the pickers that name whom to grant them.

var errNoAccess = &APIError{Status: http.StatusForbidden, Code: "no_access",
	Message: "You are a member of this organization but may not use Stator in it. Ask one of its administrators for access."}

// requireUse refuses a member without the use permission every route but
// the ones that say who they are, so a client can tell them why.
func (s *Server) requireUse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if s.Perms != nil && p.InOrg() && !p.CanAdminister() {
			can, err := s.Perms.Mine(r.Context(), actorFrom(r))
			if err != nil {
				respondError(w, r, err)
				return
			}
			if !can.Use {
				respondError(w, r, errNoAccess)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleMyAccess(w http.ResponseWriter, r *http.Request) {
	can, err := s.Perms.Mine(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"can": can})
}

func (s *Server) handleListGlobalPermissions(w http.ResponseWriter, r *http.Request) {
	grants, err := s.Perms.GlobalGrants(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"permissions": grants})
}

func (s *Server) handleSetGlobalPermission(w http.ResponseWriter, r *http.Request) {
	var req perm.GlobalGrantInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	grant, lsn, err := s.Perms.SetGlobalGrant(r.Context(), actorFrom(r), perm.GlobalPermission(chi.URLParam(r, "permission")), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"permission": grant})
}

func (s *Server) handleListSpacePermissions(w http.ResponseWriter, r *http.Request) {
	grants, err := s.Spaces.Permissions(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleSetSpacePermissions(w http.ResponseWriter, r *http.Request) {
	var req perm.SpaceGrantsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	grants, lsn, err := s.Spaces.SetPermissions(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handlePreviewPermissionCopy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	preview, err := s.Spaces.PreviewCopy(r.Context(), actorFrom(r), spaceKey(r), space.CopyInput{From: q.Get("from"), Mode: perm.CopyMode(q.Get("mode"))})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preview": preview})
}

func (s *Server) handleCopyPermissions(w http.ResponseWriter, r *http.Request) {
	var req space.CopyInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	grants, applied, lsn, err := s.Spaces.CopyPermissions(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants, "copy": applied})
}

func (s *Server) handleGetPageRestrictions(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	restrictions, err := s.Pages.GetRestrictions(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"restrictions": restrictions})
}

func (s *Server) handleSetPageRestrictions(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.RestrictionsInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	restrictions, lsn, err := s.Pages.SetRestrictions(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"restrictions": restrictions})
}

func (s *Server) handleInspectPageAccess(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	person, apiErr := pathUUID(r, "userID", "person")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	report, err := s.Pages.InspectAccess(r.Context(), actorFrom(r), id, person)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"access": report})
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	limit, _, apiErr := window(r, perm.DefaultPickerLimit, perm.MaxPickerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	people, err := s.Perms.People(r.Context(), actorFrom(r), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": people})
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	limit, _, apiErr := window(r, perm.DefaultPickerLimit, perm.MaxPickerLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	groups, err := s.Perms.Groups(r.Context(), actorFrom(r), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}
