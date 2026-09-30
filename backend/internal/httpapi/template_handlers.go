package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/template"
)

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": template.BuiltIns()})
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	found, err := template.ByKey(chi.URLParam(r, "templateKey"))
	if errors.Is(err, template.ErrUnknown) {
		respondError(w, r, ErrNotFound("There is no such template. Pick one from the list of templates."))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"template": found})
}
