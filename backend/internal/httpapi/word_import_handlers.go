package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/wordio"
)

// Word import (#89): one document becomes a page in the request; several,
// or an archive of them in folders, are made pages by the worker while the
// page that queued them follows.

type wordImportResponse struct {
	Job *wordio.Job `json:"job"`
}

// wordUploads names what an upload of Word documents holds, in refusals.
const wordUploads = "Word documents"

func (s *Server) handleImportWord(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if s.Word == nil {
		respondError(w, r, objectstore.ErrUnavailable)
		return
	}
	files, err := readUploadedFiles(w, r, wordio.MaxFileBytes, wordUploads)
	if err != nil {
		respondError(w, r, err)
		return
	}
	imported, lsn, err := s.Word.Import(r.Context(), actorFrom(r), id, files)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, imported)
}

func (s *Server) handleQueueWordImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if s.Word == nil {
		respondError(w, r, objectstore.ErrUnavailable)
		return
	}
	files, err := readUploadedFiles(w, r, wordio.MaxUploadBytes, wordUploads)
	if err != nil {
		respondError(w, r, err)
		return
	}
	job, lsn, err := s.Word.Queue(r.Context(), actorFrom(r), id, files)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, wordImportResponse{Job: job})
}

func (s *Server) handleGetWordImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if s.Word == nil {
		respondError(w, r, wordio.ErrJobNotFound)
		return
	}
	job, err := s.Word.Job(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The pages the worker made are read next, from wherever the caller reads.
	if !job.State.Open() {
		noteWrite(r.Context(), job.Written)
	}
	respondJSON(w, r, http.StatusOK, wordImportResponse{Job: job})
}
