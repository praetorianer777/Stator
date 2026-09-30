package httpapi

import (
	"net/http"
	"slices"

	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// commentBodyBytes leaves room around the largest comment for the JSON that
// carries it.
const commentBodyBytes = comment.MaxBodyBytes + uploadSlack

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	kind := comment.Kind(r.URL.Query().Get("kind"))
	if kind != "" && !slices.Contains(comment.Kinds, kind) {
		respondError(w, r, ErrValidation(map[string]string{"kind": "Ask for page or inline threads, or leave kind out for both."}))
		return
	}
	threads, err := s.Comments.List(r.Context(), actorFrom(r), id, kind)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"threads": threads})
}

func (s *Server) handleStartThread(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.ThreadInput
	if err := decodeJSONWithin(w, r, &req, commentBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	thread, lsn, err := s.Comments.Start(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"thread": thread})
}

func (s *Server) handleStartInlineThread(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.InlineThreadInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes+commentBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	thread, lsn, err := s.Comments.StartInline(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The page is read past the write, so the answer draws the new highlight.
	found, _, err := s.Pages.Get(db.PinLSN(r.Context(), lsn), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"thread": thread, "page": found})
}

func (s *Server) handleResolveThread(w http.ResponseWriter, r *http.Request) {
	s.setResolved(w, r, true)
}

func (s *Server) handleReopenThread(w http.ResponseWriter, r *http.Request) {
	s.setResolved(w, r, false)
}

func (s *Server) setResolved(w http.ResponseWriter, r *http.Request, resolved bool) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	thread, lsn, err := s.Comments.SetResolved(r.Context(), actorFrom(r), id, resolved)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	thread, err := s.Comments.Get(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.BodyInput
	if err := decodeJSONWithin(w, r, &req, commentBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	made, thread, lsn, err := s.Comments.Reply(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"comment": made, "thread": thread})
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.BodyInput
	if err := decodeJSONWithin(w, r, &req, commentBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	changed, lsn, err := s.Comments.Edit(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comment": changed})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Comments.Delete(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
