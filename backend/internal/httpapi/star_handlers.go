package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/home"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/star"
)

func (s *Server) handleStarPage(w http.ResponseWriter, r *http.Request) {
	s.starPage(w, r, true)
}

func (s *Server) handleUnstarPage(w http.ResponseWriter, r *http.Request) {
	s.starPage(w, r, false)
}

func (s *Server) starPage(w http.ResponseWriter, r *http.Request, on bool) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	act := s.Stars.UnstarPage
	if on {
		act = s.Stars.StarPage
	}
	lsn, err := act(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleStarSpace(w http.ResponseWriter, r *http.Request) {
	s.starSpace(w, r, true)
}

func (s *Server) handleUnstarSpace(w http.ResponseWriter, r *http.Request) {
	s.starSpace(w, r, false)
}

func (s *Server) starSpace(w http.ResponseWriter, r *http.Request, on bool) {
	act := s.Stars.UnstarSpace
	if on {
		act = s.Stars.StarSpace
	}
	lsn, err := act(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// keysetWindow reads a window's limit and the cursor it starts after.
func keysetWindow(r *http.Request, defaultLimit, maxLimit int) (int, *keyset.Cursor, *APIError) {
	limit, _, apiErr := window(r, defaultLimit, maxLimit)
	if apiErr != nil {
		return 0, nil, apiErr
	}
	after, err := keyset.Decode(r.URL.Query().Get("cursor"))
	if err != nil {
		return 0, nil, ErrValidation(map[string]string{"cursor": "That cursor is not one this list gave out. Start again from the first window."})
	}
	return limit, after, nil
}

func (s *Server) handleListStars(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, star.DefaultLimit, star.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	stars, next, err := s.Stars.List(r.Context(), actorFrom(r), after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"stars": stars, "next": next})
}

func (s *Server) handleHomeUpdates(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, home.DefaultLimit, home.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	scope := home.Scope(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = home.ScopeAll
	}
	if scope != home.ScopeAll && scope != home.ScopeWatched {
		respondError(w, r, ErrValidation(map[string]string{"scope": "Ask for all updates or for watched ones."}))
		return
	}
	updates, next, err := s.Home.Updates(r.Context(), scope, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"updates": updates, "next": next})
}

func (s *Server) handleHomeEdited(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, home.DefaultLimit, home.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	pages, next, err := s.Home.Edited(r.Context(), after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"pages": pages, "next": next})
}
