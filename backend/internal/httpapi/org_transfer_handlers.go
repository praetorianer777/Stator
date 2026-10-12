package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/spaceio"
)

// Organization export and import (#383), run by the worker as space
// transfers are; the route group holds them to administrators.

func (s *Server) handleCreateOrgExport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	job, lsn, err := s.SpaceTransfers.QueueOrgExport(r.Context(), actorFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"export": job})
}

func (s *Server) handleListOrgExports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.OrgExports(r.Context(), actorFrom(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"exports": jobs})
}

func (s *Server) handleDownloadOrgExport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "exportID", "export")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	// As for a space's export: the file may outlast the request's limit.
	ctx := context.WithoutCancel(r.Context())
	job, body, err := s.SpaceTransfers.DownloadOrgExport(ctx, actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	download(w, "application/zip", *job.FileName)
	w.Header().Set("Content-Length", strconv.FormatInt(*job.Size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		loggerFrom(r.Context()).Warn("an organization export's download stopped part way", "export", id, "error", err)
	}
}

func (s *Server) handleCreateOrgImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	limit := s.SpaceTransfers.MaxOrgImportBytes
	f, size, err := readUploadedArchive(w, r, limit)
	if f != nil {
		defer func() {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}()
	}
	if err != nil {
		var over *spaceio.TooLargeError
		if errors.As(err, &over) {
			err = &spaceio.OrgTooLargeError{Limit: limit}
		}
		respondError(w, r, err)
		return
	}
	job, lsn, err := s.SpaceTransfers.QueueOrgImport(r.Context(), actorFrom(r), f, size)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"import": job})
}

func (s *Server) handleListOrgImports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.OrgImports(r.Context(), actorFrom(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"imports": jobs})
}

func (s *Server) handleGetOrgImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	job, err := s.SpaceTransfers.OrgImport(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The organization the worker filled is read next, from wherever the caller reads.
	if job.State == spaceio.StateDone {
		noteWrite(r.Context(), job.Written)
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": job})
}
