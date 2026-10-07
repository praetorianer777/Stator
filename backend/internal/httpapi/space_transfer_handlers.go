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

// Space export and import (#88). Both run in the worker: a request queues a
// job and answers it, and the page follows the job until it is done.

// spaceTransferList is how many exports or imports a list answers.
const spaceTransferList = 10

type exportRequest struct {
	// Format is archive, to import again, or html, to read offline.
	Format spaceio.ExportFormat `json:"format"`
}

func (s *Server) spaceTransfers(w http.ResponseWriter, r *http.Request) bool {
	if s.SpaceTransfers == nil {
		respondError(w, r, &APIError{Status: http.StatusServiceUnavailable, Code: "storage_unavailable",
			Message: "Spaces cannot be exported or imported on this server yet. Ask an administrator to set up file storage."})
		return false
	}
	return true
}

func (s *Server) handleCreateSpaceExport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	var req exportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Format != spaceio.FormatArchive && req.Format != spaceio.FormatHTML {
		respondError(w, r, ErrValidation(map[string]string{"format": "Choose archive, to import the space again, or html, to read it offline."}))
		return
	}
	job, lsn, err := s.SpaceTransfers.QueueExport(r.Context(), actorFrom(r), spaceKey(r), req.Format)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"export": job})
}

func (s *Server) handleListSpaceExports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.Exports(r.Context(), actorFrom(r), spaceKey(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"exports": jobs})
}

func (s *Server) handleDownloadSpaceExport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "exportID", "export")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	// The file may outlast the request's limit on a slow line; the server's
	// write timeout still bounds it.
	ctx := context.WithoutCancel(r.Context())
	job, body, err := s.SpaceTransfers.Download(ctx, actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	download(w, "application/zip", *job.FileName)
	w.Header().Set("Content-Length", strconv.FormatInt(*job.Size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		loggerFrom(r.Context()).Warn("a space export's download stopped part way", "export", id, "error", err)
	}
}

func (s *Server) handleCreateSpaceImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	limit := s.SpaceTransfers.MaxImportBytes
	f, size, err := readUploadedArchive(w, r, limit)
	if f != nil {
		defer func() {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}()
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	in := spaceio.ImportInput{Key: r.URL.Query().Get("key"), Name: r.URL.Query().Get("name")}
	job, lsn, err := s.SpaceTransfers.QueueImport(r.Context(), actorFrom(r), in, f, size)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"import": job})
}

// readUploadedArchive writes the part named file to a file of its own, as
// an archive is read from its end and may be larger than memory should hold.
func readUploadedArchive(w http.ResponseWriter, r *http.Request, limit int64) (*os.File, int64, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, 0, ErrBadRequest("Send the archive as multipart form data in a part named file.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, 0, ErrBadRequest("The upload has no part named file. Send the archive in a part named file.")
		}
		if err != nil {
			if tooBig(err) {
				return nil, 0, &spaceio.TooLargeError{Limit: limit}
			}
			return nil, 0, ErrBadRequest("The upload could not be read. Try sending the archive again.")
		}
		if part.FormName() != "file" {
			continue
		}
		f, err := os.CreateTemp("", "stator-import-*.zip")
		if err != nil {
			return nil, 0, err
		}
		size, err := io.Copy(f, io.LimitReader(part, limit+1))
		switch {
		case tooBig(err), err == nil && size > limit:
			return f, 0, &spaceio.TooLargeError{Limit: limit}
		case err != nil:
			return f, 0, ErrBadRequest("The upload could not be read. Try sending the archive again.")
		case size == 0:
			return f, 0, ErrBadRequest("The archive is empty. Choose the file an export of a space made.")
		}
		return f, size, nil
	}
}

func (s *Server) handleListSpaceImports(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	jobs, err := s.SpaceTransfers.Imports(r.Context(), actorFrom(r), spaceTransferList)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"imports": jobs})
}

func (s *Server) handleGetSpaceImport(w http.ResponseWriter, r *http.Request) {
	if !s.spaceTransfers(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	job, err := s.SpaceTransfers.Import(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The space the worker made is read next, from wherever the caller reads.
	if job.State == spaceio.StateDone {
		noteWrite(r.Context(), job.Written)
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": job})
}
