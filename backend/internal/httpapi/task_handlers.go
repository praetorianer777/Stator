package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/task"
)

func (s *Server) handleListMyTasks(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, task.DefaultLimit, task.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	state := task.State(r.URL.Query().Get("state"))
	if state == "" {
		state = task.StateOpen
	}
	if state != task.StateOpen && state != task.StateDone {
		respondError(w, r, ErrValidation(map[string]string{"state": "Ask for open tasks or for done ones."}))
		return
	}
	tasks, next, err := s.Tasks.Mine(r.Context(), actorFrom(r), state, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tasks": tasks, "next": next})
}

func (s *Server) handleSetTaskDone(w http.ResponseWriter, r *http.Request) {
	pageID, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	taskID, apiErr := pathUUID(r, "taskID", "task")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req task.SetDoneInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	t, lsn, err := s.Pages.SetTaskDone(r.Context(), actorFrom(r), pageID, taskID, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"task": t})
}
