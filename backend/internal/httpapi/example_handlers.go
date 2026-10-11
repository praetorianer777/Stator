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
	// Job is the latest making or renewal of it, followed while queued or
	// running; null when nobody asked for one, or the example exists already.
	Job *example.Job `json:"job"`
	// Edition says which version of the content the example has and which
	// is current; null while there is no example.
	Edition *exampleEdition `json:"edition"`
}

type exampleEdition struct {
	// Version is the version of the content the example was made or last
	// renewed from, 0 when it was made before versions were kept.
	Version int `json:"version"`
	// Current is the version a renewal brings it to; newer than Version
	// when there is something to renew.
	Current int `json:"current"`
	// Language is the one the example's pages are written in.
	Language string `json:"language"`
	// New is what the versions after Version bring, a sentence each, in
	// the language asked for; empty when the example is current.
	New []string `json:"new"`
}

func (s *Server) exampleEdition(r *http.Request, lang string) (*exampleEdition, error) {
	e, err := s.ExampleJobs.Edition(r.Context(), actorFrom(r))
	if err != nil || e == nil {
		return nil, err
	}
	return &exampleEdition{Version: e.Version, Current: example.Current(), Language: e.Language, New: example.NewSince(e.Version, lang)}, nil
}

// readerLanguage is the language asked for in the query, else the caller's own.
func readerLanguage(r *http.Request) string {
	lang, err := example.Language(r.URL.Query().Get("language"), string(PrincipalFrom(r.Context()).Locale))
	if err != nil {
		return example.English
	}
	return lang
}

func (s *Server) handleGetExampleSpace(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	job, err := s.ExampleJobs.Latest(r.Context(), actor)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := exampleSpaceResponse{Job: job}
	if job == nil || !job.State.Open() || job.Kind == example.KindRenew {
		if out.Space, err = s.Spaces.Example(r.Context(), actor); err != nil {
			respondError(w, r, err)
			return
		}
	}
	if out.Space != nil {
		if out.Edition, err = s.exampleEdition(r, readerLanguage(r)); err != nil {
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
	out := exampleSpaceResponse{Space: found}
	if found != nil {
		if out.Edition, err = s.exampleEdition(r, lang); err != nil {
			respondError(w, r, err)
			return
		}
	}
	respondJSON(w, r, http.StatusOK, out)
}

type renewExampleSpaceRequest struct {
	// Language is en or de; empty keeps the language the example's pages
	// are written in.
	Language string `json:"language,omitempty"`
}

func (s *Server) handleRenewExampleSpace(w http.ResponseWriter, r *http.Request) {
	var req renewExampleSpaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Language != "" {
		if _, err := example.Language(req.Language, ""); err != nil {
			respondError(w, r, ErrValidation(map[string]string{"language": "The example space is written in English (en) and German (de). Choose one of them."}))
			return
		}
	}
	actor := actorFrom(r)
	job, lsn, err := s.ExampleJobs.Renew(r.Context(), actor, req.Language)
	noteWrite(r.Context(), lsn)
	if errors.Is(err, example.ErrNoExample) {
		respondError(w, r, ErrConflict("The organization has no example space to renew. Create the example space instead."))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, exampleSpaceResponse{Job: job})
}
