package httpapi

import (
	"net/http"
)

// A link's card: what the page it leads to says about itself.

func (s *Server) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	got, err := s.Unfurl.Preview(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	// What a page says changes slowly, and the server keeps it an hour anyway.
	w.Header().Set("Cache-Control", "private, max-age=300")
	respondJSON(w, r, http.StatusOK, map[string]any{"preview": got})
}
