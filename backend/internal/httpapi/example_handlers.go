package httpapi

import (
	"errors"
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/example"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// The example space (#288): one per organization, made by its administrators.
// The worker makes it (#306), and the page follows the job until it is done.

type exampleSpaceRequest struct {
	// Language is en or de; empty takes the caller's own, else English.
	Language string `json:"language,omitempty"`
}

type exampleSpaceResponse struct {
	// Space is the example once it is made; null while there is none, and
	// while it is being made.
	Space *space.Space `json:"space"`
	// Job is the latest making of it, followed while queued or running;
	// null when nobody asked for one, or the example exists already.
	Job *example.Job `json:"job"`
}

func (s *Server) handleGetExampleSpace(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	job, err := s.ExampleJobs.Latest(r.Context(), actor)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := exampleSpaceResponse{Job: job}
	if job == nil || !job.State.Open() {
		if out.Space, err = s.Spaces.Example(r.Context(), actor); err != nil {
			respondError(w, r, err)
			return
		}
	}
	// What the worker wrote is read next, from wherever the caller reads.
	if job != nil && job.State == example.StateDone {
		noteWrite(r.Context(), job.Written)
	}
	respondJSON(w, r, http.StatusOK, out)
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
	actor := actorFrom(r)
	job, lsn, err := s.ExampleJobs.Queue(r.Context(), actor, lang)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if job != nil {
		respondJSON(w, r, http.StatusAccepted, exampleSpaceResponse{Job: job})
		return
	}
	found, err := s.Spaces.Example(r.Context(), actor)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, exampleSpaceResponse{Space: found})
}
