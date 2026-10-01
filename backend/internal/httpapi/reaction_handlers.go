package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
)

type reactFunc func(context.Context, perm.Actor, uuid.UUID, string) ([]reaction.Reaction, db.LSN, error)

// react serves the four reaction routes: the target from the path, the emoji
// from the body when one is put on and from the query when one is taken off.
func (s *Server) react(w http.ResponseWriter, r *http.Request, param, what string, fromBody bool, do reactFunc) {
	id, apiErr := pathUUID(r, param, what)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	emoji := r.URL.Query().Get("emoji")
	if fromBody {
		var in reaction.Input
		if err := decodeJSON(w, r, &in); err != nil {
			respondError(w, r, err)
			return
		}
		emoji = in.Emoji
	}
	reactions, lsn, err := do(r.Context(), actorFrom(r), id, emoji)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"reactions": reactions})
}

func (s *Server) handleReactToPage(w http.ResponseWriter, r *http.Request) {
	s.react(w, r, "pageID", "page", true, s.Reactions.ReactToPage)
}

func (s *Server) handleUnreactPage(w http.ResponseWriter, r *http.Request) {
	s.react(w, r, "pageID", "page", false, s.Reactions.UnreactPage)
}

func (s *Server) handleReactToComment(w http.ResponseWriter, r *http.Request) {
	s.react(w, r, "commentID", "comment", true, s.Reactions.ReactToComment)
}

func (s *Server) handleUnreactComment(w http.ResponseWriter, r *http.Request) {
	s.react(w, r, "commentID", "comment", false, s.Reactions.UnreactComment)
}
