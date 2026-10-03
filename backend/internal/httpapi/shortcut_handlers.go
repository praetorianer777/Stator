package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/shortcut"
)

func (s *Server) handleListShortcuts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Shortcuts.List(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"shortcuts": list})
}

func (s *Server) handleCreateShortcut(w http.ResponseWriter, r *http.Request) {
	var req shortcut.ShortcutInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Shortcuts.Create(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"shortcut": made})
}

func (s *Server) handleMoveShortcut(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "shortcutID", "shortcut")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req shortcut.ShortcutMove
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	list, lsn, err := s.Shortcuts.Move(r.Context(), actorFrom(r), spaceKey(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"shortcuts": list})
}

func (s *Server) handleDeleteShortcut(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "shortcutID", "shortcut")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Shortcuts.Delete(r.Context(), actorFrom(r), spaceKey(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
