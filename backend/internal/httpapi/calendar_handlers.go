package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/calendar"
)

// Team calendars (#60): a space's calendars and their events.

func (s *Server) handleListCalendars(w http.ResponseWriter, r *http.Request) {
	list, err := s.Calendars.List(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"calendars": list})
}

func (s *Server) handleCreateCalendar(w http.ResponseWriter, r *http.Request) {
	var req calendar.CalendarInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Calendars.Create(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"calendar": made})
}

func (s *Server) handleRenameCalendar(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req calendar.CalendarInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	renamed, lsn, err := s.Calendars.Rename(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": renamed})
}

func (s *Server) handleDeleteCalendar(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Calendars.Delete(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListCalendarEvents(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	from, to, err := calendar.Range(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	events, err := s.Calendars.Events(r.Context(), actorFrom(r), id, from, to)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, events)
}

func (s *Server) handleCreateCalendarEvent(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req calendar.CalendarEventInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Calendars.CreateEvent(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"event": made})
}

func (s *Server) handleUpdateCalendarEvent(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	eventID, apiErr := pathUUID(r, "eventID", "event")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req calendar.CalendarEventInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	changed, lsn, err := s.Calendars.UpdateEvent(r.Context(), actorFrom(r), id, eventID, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"event": changed})
}

func (s *Server) handleDeleteCalendarEvent(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	eventID, apiErr := pathUUID(r, "eventID", "event")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Calendars.DeleteEvent(r.Context(), actorFrom(r), id, eventID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
