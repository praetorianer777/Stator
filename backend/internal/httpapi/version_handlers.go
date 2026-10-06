package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/page"
)

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	draft, err := s.Pages.GetDraft(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"draft": draft})
}

func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.DraftInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	draft, lsn, err := s.Pages.SaveDraft(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"draft": draft})
}

func (s *Server) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Pages.DiscardDraft(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleSchedulePublish(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.ScheduleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	schedule, lsn, err := s.Pages.SchedulePublish(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"schedule": schedule})
}

func (s *Server) handleCancelSchedule(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Pages.CancelSchedule(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handlePublishPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.PublishInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	published, version, lsn, err := s.Pages.Publish(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"page": published, "version": version})
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, offset, apiErr := window(r, page.DefaultVersionLimit, page.MaxVersionLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	versions, total, err := s.Pages.Versions(r.Context(), actorFrom(r), id, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"versions": versions, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	number, apiErr := versionNumber(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	version, err := s.Pages.GetVersion(r.Context(), actorFrom(r), id, number)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"version": version})
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	number, apiErr := versionNumber(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.RestoreInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	restored, version, lsn, err := s.Pages.RestoreVersion(r.Context(), actorFrom(r), id, number, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"page": restored, "version": version})
}

func (s *Server) handleCompareVersions(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	sides := map[string]*page.SideRef{}
	for _, name := range []string{"from", "to"} {
		ref, err := page.ParseSide(r.URL.Query().Get(name))
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{name: sentence(err.Error())}))
			return
		}
		sides[name] = ref
	}
	comparison, err := s.Pages.Compare(r.Context(), actorFrom(r), id, sides["from"], sides["to"])
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comparison": comparison})
}

// versionNumber reads the version out of the route; a number the page never
// reached is the service's to answer as not found.
func versionNumber(r *http.Request) (int, *APIError) {
	n, err := strconv.Atoi(chi.URLParam(r, "versionNumber"))
	if err != nil || n < 1 {
		return 0, ErrBadRequest("That is not a version number. Versions count from 1; check the address.")
	}
	return n, nil
}

// window reads limit and offset for a list that pages, refusing values out
// of range rather than quietly clamping them.
func window(r *http.Request, defaultLimit, maxLimit int) (int, int, *APIError) {
	limit, offset := defaultLimit, 0
	q := r.URL.Query()
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			return 0, 0, ErrValidation(map[string]string{"limit": "Ask for 1 to " + strconv.Itoa(maxLimit) + " at a time."})
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, ErrValidation(map[string]string{"offset": "Start from 0 or later."})
		}
		offset = n
	}
	return limit, offset, nil
}

func (s *Server) handleSaveLive(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.LiveInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	saved, replaced, lsn, err := s.Pages.SaveLive(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	s.resetRoom(id, replaced)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, saved)
}

func (s *Server) handleSetPageMode(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req page.ModeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	changed, replaced, lsn, err := s.Pages.SetMode(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	s.resetRoom(id, replaced)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, changed)
}

// resetRoom sends the editors of a shared draft the service threw away to
// load the one that takes its place.
func (s *Server) resetRoom(pageID uuid.UUID, room *uuid.UUID) {
	if room != nil && s.Collab != nil {
		s.Collab.Reset(pageID, *room)
	}
}
