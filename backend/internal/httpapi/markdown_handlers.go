package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/mdio"
)

// Markdown import and export. The conversion runs here rather than in the
// browser, so a script with a token gets the same pages the editor would.

func (s *Server) markdown() *mdio.Service {
	if s.Markdown != nil {
		return s.Markdown
	}
	return mdio.NewService(s.Pages, s.Attachments)
}

func (s *Server) handleExportPage(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	subtree, apiErr := flagParam(r, "subtree")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	export, err := s.markdown().Export(r.Context(), actorFrom(r), id, subtree)
	if err != nil {
		respondError(w, r, err)
		return
	}
	scope := mdio.ScopePage
	if subtree {
		scope = mdio.ScopeSubtree
	}
	if !s.noteExport(w, r, export.Audit(scope)) {
		return
	}
	download(w, "application/zip", export.Name()+".zip")
	w.WriteHeader(http.StatusOK)
	// The status is sent, so a failure part way leaves an archive without
	// its directory, which no tool opens as whole.
	if err := export.Write(r.Context(), w); err != nil {
		loggerFrom(r.Context()).Error("markdown export stopped part way", "page", id, "error", err)
	}
}

func (s *Server) handleGetPageMarkdown(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	export, err := s.markdown().Export(r.Context(), actorFrom(r), id, false)
	if err != nil {
		respondError(w, r, err)
		return
	}
	md, err := export.Single()
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteExport(w, r, export.Audit(mdio.ScopeMarkdown)) {
		return
	}
	download(w, "text/markdown; charset=utf-8", export.Name()+".md")
	w.Header().Set("Content-Length", strconv.Itoa(len(md)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, md)
}

func (s *Server) handleReplacePageMarkdown(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil || version < 0 {
		respondError(w, r, ErrValidation(map[string]string{"version": "Send the page's version the Markdown replaces, as a whole number."}))
		return
	}
	svc := s.markdown()
	files, err := readUploadedFiles(w, r, svc.MaxBytes)
	if err != nil {
		respondError(w, r, err)
		return
	}
	replaced, lsn, err := svc.Replace(r.Context(), actorFrom(r), id, version, files)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, replaced)
}

func (s *Server) handleImportMarkdown(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	svc := s.markdown()
	files, err := readUploadedFiles(w, r, svc.MaxBytes)
	if err != nil {
		respondError(w, r, err)
		return
	}
	imported, lsn, err := svc.Import(r.Context(), actorFrom(r), id, files)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, imported)
}

// readUploadedFiles reads every part named file with the path it was sent
// with, which the part's own FileName cuts to its last name.
func readUploadedFiles(w http.ResponseWriter, r *http.Request, limit int64) ([]mdio.File, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, ErrBadRequest("Send the Markdown files as multipart form data, each in a part named file.")
	}
	var files []mdio.File
	var total int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if tooBig(err) {
				return nil, &mdio.TooLargeError{Limit: limit}
			}
			return nil, ErrBadRequest("The upload could not be read. Try sending the files again.")
		}
		if part.FormName() != "file" {
			continue
		}
		name := ""
		if _, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition")); err == nil {
			name = params["filename"]
		}
		if name == "" {
			return nil, ErrBadRequest("Every part named file needs a file name, with the folders it sits in.")
		}
		if len(files) >= mdio.MaxImportFiles {
			return nil, &mdio.InvalidError{Message: "the upload holds more than " + strconv.Itoa(mdio.MaxImportFiles) + " files; import its folders one at a time"}
		}
		data, err := io.ReadAll(io.LimitReader(part, limit-total+1))
		if err != nil {
			if tooBig(err) {
				return nil, &mdio.TooLargeError{Limit: limit}
			}
			return nil, ErrBadRequest("The upload could not be read. Try sending the files again.")
		}
		total += int64(len(data))
		if total > limit {
			return nil, &mdio.TooLargeError{Limit: limit}
		}
		files = append(files, mdio.File{Path: name, Data: data})
	}
	if len(files) == 0 {
		return nil, ErrBadRequest("The upload has no part named file. Send each Markdown file in a part named file.")
	}
	return files, nil
}

// flagParam reads a yes or no query parameter, no when it is absent.
func flagParam(r *http.Request, name string) (bool, *APIError) {
	switch r.URL.Query().Get(name) {
	case "", "false", "0":
		return false, nil
	case "true", "1":
		return true, nil
	}
	return false, ErrValidation(map[string]string{name: "Say true or false."})
}

// download names what is written as a file to save, never one to show.
func download(w http.ResponseWriter, contentType, name string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
}
