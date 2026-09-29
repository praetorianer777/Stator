package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Spaces and their pages. Whether the caller may do something is perm's to
// say, inside the service; the handlers only read the request and answer.

// pageResponse is a page with the space it is in, which the reader shows
// around it and asks what it may do there.
type pageResponse struct {
	Page  page.Page   `json:"page"`
	Space space.Space `json:"space"`
}

// pageBodyBytes lets a request carry the largest document the allowlist
// accepts, with room for the fields around it.
const pageBodyBytes = document.MaxBytes + uploadSlack

func actorFrom(r *http.Request) perm.Actor { return perm.ActorOf(PrincipalFrom(r.Context())) }

func spaceKey(r *http.Request) string { return chi.URLParam(r, "spaceKey") }

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	spaces, err := s.Spaces.List(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"spaces": spaces})
}

func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	var req space.CreateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Spaces.Create(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"space": made})
}

func (s *Server) handleGetSpace(w http.ResponseWriter, r *http.Request) {
	found, err := s.Spaces.Get(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"space": found})
}

func (s *Server) handleUpdateSpace(w http.ResponseWriter, r *http.Request) {
	var req space.UpdateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Spaces.Update(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"space": updated})
}

func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Spaces.Delete(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, sp, err := s.Pages.Get(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, pageResponse{Page: *found, Space: *sp})
}

func (s *Server) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.UpdateInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Pages.Update(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"page": updated})
}

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	var parent *uuid.UUID
	if raw := r.URL.Query().Get("parent"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("That is not a valid page id for parent. Check the address."))
			return
		}
		parent = &id
	}
	pages, err := s.Pages.Children(r.Context(), actorFrom(r), spaceKey(r), parent)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) handleSpaceOutline(w http.ResponseWriter, r *http.Request) {
	pages, err := s.Pages.Outline(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var req page.CreateInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Pages.Create(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"page": made})
}

func (s *Server) handleMovePage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.MoveInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	moved, lsn, err := s.Pages.Move(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"page": moved})
}

func (s *Server) handleCopyPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.CopyInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Pages.Copy(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"page": made})
}
