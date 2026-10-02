package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/template"
)

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Templates.List(r.Context(), actorFrom(r), r.URL.Query().Get("space"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": list})
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	found, err := s.Templates.Get(r.Context(), actorFrom(r), chi.URLParam(r, "templateKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"template": found})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req template.CreateInput
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Templates.Create(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"template": made})
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	var req template.Input
	if err := decodeJSONWithin(w, r, &req, pageBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.Templates.Update(r.Context(), actorFrom(r), chi.URLParam(r, "templateKey"), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"template": saved})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Templates.Delete(r.Context(), actorFrom(r), chi.URLParam(r, "templateKey"))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
