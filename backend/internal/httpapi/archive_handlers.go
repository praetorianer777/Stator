package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

func (s *Server) handleArchivePage(w http.ResponseWriter, r *http.Request) {
	s.archivePage(w, r, s.Pages.Archive)
}

func (s *Server) handleUnarchivePage(w http.ResponseWriter, r *http.Request) {
	s.archivePage(w, r, s.Pages.Unarchive)
}

func (s *Server) archivePage(w http.ResponseWriter, r *http.Request,
	act func(context.Context, perm.Actor, uuid.UUID) (*page.Page, db.LSN, error)) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p, lsn, err := act(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"page": p})
}

func (s *Server) handleArchiveSpace(w http.ResponseWriter, r *http.Request) {
	s.archiveSpace(w, r, true)
}

func (s *Server) handleUnarchiveSpace(w http.ResponseWriter, r *http.Request) {
	s.archiveSpace(w, r, false)
}

func (s *Server) archiveSpace(w http.ResponseWriter, r *http.Request, archived bool) {
	sp, lsn, err := s.Spaces.Archive(r.Context(), actorFrom(r), spaceKey(r), archived)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"space": sp})
}

func (s *Server) handleListArchivedPages(w http.ResponseWriter, r *http.Request) {
	items, err := s.Pages.ListArchive(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"items": items})
}
