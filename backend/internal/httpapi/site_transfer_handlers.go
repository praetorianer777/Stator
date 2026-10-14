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

func (s *Server) handleCreateSiteExport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	job, lsn, err := s.SpaceTransfers.QueueSiteExport(r.Context(), actorFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"export": job})
}

func (s *Server) handleListSiteExports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.SiteExports(r.Context(), actorFrom(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"exports": jobs})
}

func (s *Server) handleDownloadSiteExport(w http.ResponseWriter, r *http.Request) {
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
	job, body, err := s.SpaceTransfers.DownloadSiteExport(ctx, actorFrom(r), id)
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

func (s *Server) handleCreateSiteImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	limit := s.SpaceTransfers.MaxSiteImportBytes
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
			err = &spaceio.SiteTooLargeError{Limit: limit}
		}
		respondError(w, r, err)
		return
	}
	job, lsn, err := s.SpaceTransfers.QueueSiteImport(r.Context(), actorFrom(r), f, size)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"import": job})
}

func (s *Server) handleListSiteImports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.SiteImports(r.Context(), actorFrom(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"imports": jobs})
}

func (s *Server) handleGetSiteImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	job, err := s.SpaceTransfers.SiteImport(r.Context(), actorFrom(r), id)
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
