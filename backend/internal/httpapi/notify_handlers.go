package httpapi

import (
	"net/http"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/notify"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, notify.DefaultLimit, notify.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	unread := false
	if raw := r.URL.Query().Get("unread"); raw != "" {
		var err error
		if unread, err = strconv.ParseBool(raw); err != nil {
			respondError(w, r, ErrValidation(map[string]string{"unread": "Say true to list only what is unread, or false for everything."}))
			return
		}
	}
	found, total, err := s.Notifications.List(r.Context(), actorFrom(r), unread, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"notifications": found, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	n, err := s.Notifications.Unread(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"unread": n})
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	var in notify.MarkReadInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Notifications.MarkRead(r.Context(), actorFrom(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.Notifications.Preferences(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": prefs})
}

func (s *Server) handleSaveNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	var in notify.Preferences
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	prefs, lsn, err := s.Notifications.SavePreferences(r.Context(), actorFrom(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": prefs})
}
