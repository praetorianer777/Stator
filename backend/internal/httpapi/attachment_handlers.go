package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// Attachments: files on a page, uploaded as multipart form data and streamed
// back through the API, as Armature does, so that the session and not a
// bucket policy decides who may read them.

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	if !s.attachmentsOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	current := false
	if raw := r.URL.Query().Get("current"); raw != "" {
		var err error
		if current, err = strconv.ParseBool(raw); err != nil {
			respondError(w, r, ErrValidation(map[string]string{"current": "Say true to list only the latest version of each file, or false for every version."}))
			return
		}
	}
	found, err := s.Attachments.List(r.Context(), actorFrom(r), id, current)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attachments": found})
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.attachmentsOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	in, err := readUploadedFile(w, r, s.Attachments.MaxSize)
	if err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Attachments.Upload(r.Context(), actorFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, uploadError(err, s.Attachments.MaxSize))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"attachment": created})
}

// readUploadedFile finds the part named file and hands its stream on. The
// body is capped first: refusing at the connection is cheaper than reading.
func readUploadedFile(w http.ResponseWriter, r *http.Request, limit int64) (attachment.UploadInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		return attachment.UploadInput{}, ErrBadRequest("Send the file as multipart form data in a part named file.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return attachment.UploadInput{}, ErrBadRequest("The upload has no part named file. Send the file in a part named file.")
		}
		if err != nil {
			if tooBig(err) {
				return attachment.UploadInput{}, &attachment.TooLargeError{Limit: limit}
			}
			return attachment.UploadInput{}, ErrBadRequest("The upload could not be read. Try sending the file again.")
		}
		if part.FormName() != "file" {
			continue
		}
		return attachment.UploadInput{
			FileName:    part.FileName(),
			ContentType: part.Header.Get("Content-Type"),
			Body:        part,
		}, nil
	}
}

func tooBig(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// uploadError turns the body cap tripping mid-stream into the size refusal.
func uploadError(err error, limit int64) error {
	if tooBig(err) {
		return &attachment.TooLargeError{Limit: limit}
	}
	return err
}

// handleDownloadAttachment streams the bytes as a download with the stored
// type; only types that cannot run script show in place, and only when asked.
func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.attachmentsOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, body, err := s.Attachments.Open(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && isSafeInline(found.ContentType) {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", found.ContentType)
	h.Set("Content-Length", strconv.FormatInt(found.Size, 10))
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": found.FileName}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// handlePreviewAttachment answers with a file as a PDF shown in place. The
// first reader of an office document waits for its conversion, so it is a write.
func (s *Server) handlePreviewAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.attachmentsOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	preview, lsn, err := s.Attachments.Preview(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer preview.Body.Close()
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Length", strconv.FormatInt(preview.Size, 10))
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": preview.Name}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, preview.Body)
}

// isSafeInline says which types a browser may show in place: images, PDFs
// and plain text, which cannot run script against this origin. SVG can.
func isSafeInline(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain":
		return true
	}
	return false
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.attachmentsOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Attachments.Delete(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// attachmentsOn answers for a server built without the file service, such as
// a test's, as for one without storage.
func (s *Server) attachmentsOn(w http.ResponseWriter, r *http.Request) bool {
	if s.Attachments == nil {
		respondError(w, r, objectstore.ErrUnavailable)
		return false
	}
	return true
}

// sweepFiles removes the objects of files whose pages were just deleted for
// good. It is best effort: what it misses, the worker's reaper removes.
func (s *Server) sweepFiles(ctx context.Context) {
	if s.Attachments == nil {
		return
	}
	if _, err := s.Attachments.Sweep(ctx); err != nil {
		loggerFrom(ctx).Warn("files of deleted pages left for the reaper", "error", err)
	}
}
