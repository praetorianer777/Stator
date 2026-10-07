package httpapi

import (
	"errors"
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/example"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// The example space (#288): one per organization, made by its administrators.

type exampleSpaceRequest struct {
	// Language is en or de; empty takes the caller's own, else English.
	Language string `json:"language,omitempty"`
}

type exampleSpaceResponse struct {
	Space space.Space `json:"space"`
	// Created says this call made it; false is the one made before.
	Created bool `json:"created"`
}

func (s *Server) exampleMaker() *example.Maker {
	return &example.Maker{
		Spaces: s.Spaces, Pages: s.Pages, Labels: s.Labels, Calendars: s.Calendars,
		Comments: s.Comments, Reactions: s.Reactions, Attachments: s.Attachments, Armature: s.Armature,
	}
}

func (s *Server) handleGetExampleSpace(w http.ResponseWriter, r *http.Request) {
	found, err := s.Spaces.Example(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"space": found})
}

func (s *Server) handleCreateExampleSpace(w http.ResponseWriter, r *http.Request) {
	var req exampleSpaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	lang, err := example.Language(req.Language, string(p.Locale))
	if errors.Is(err, example.ErrNoLanguage) {
		respondError(w, r, ErrValidation(map[string]string{"language": "The example space is written in English (en) and German (de). Choose one of them."}))
		return
	}
	made, created, lsn, err := s.exampleMaker().Make(r.Context(), actorFrom(r), example.Person{ID: p.UserID, Name: p.Name}, lang)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondJSON(w, r, status, exampleSpaceResponse{Space: *made, Created: created})
}
