package httpapi

import (
	"net/http"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// Blog posts (#72): pages of a space's blog, outside its tree, by date.

// yearMonth reads the year and month a list of posts keeps, 0 for absent.
func yearMonth(r *http.Request) (int, int, *APIError) {
	read := func(name, message string) (int, *APIError) {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, ErrValidation(map[string]string{name: message})
		}
		return n, nil
	}
	year, apiErr := read("year", "Give the year as a number, such as 2026.")
	if apiErr != nil {
		return 0, 0, apiErr
	}
	month, apiErr := read("month", "Give the month as a number from 1 to 12.")
	return year, month, apiErr
}

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	limit, after, apiErr := keysetWindow(r, page.DefaultPostLimit, document.MaxListedPages)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	year, month, apiErr := yearMonth(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	posts, next, err := s.Pages.Posts(r.Context(), actorFrom(r), page.PostsInput{
		SpaceKey: r.URL.Query().Get("space"), Year: year, Month: month, After: after, Limit: limit,
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"posts": posts, "next": next})
}

func (s *Server) handleGetBlog(w http.ResponseWriter, r *http.Request) {
	blog, err := s.Pages.Blog(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"blog": blog})
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var req page.PostInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Pages.CreatePost(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"page": made})
}

func (s *Server) handleWatchBlog(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.WatchBlog(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleUnwatchBlog(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.UnwatchBlog(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
