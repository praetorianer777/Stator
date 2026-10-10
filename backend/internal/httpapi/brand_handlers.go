package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/praetorianer777/stator/backend/internal/brand"
)

// The brand of the organization's exports: every member reads it,
// administrators change it.

func (s *Server) handleGetBrand(w http.ResponseWriter, r *http.Request) {
	got, err := s.Brand.Get(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"brand": got})
}

func (s *Server) handleSetBrandFooter(w http.ResponseWriter, r *http.Request) {
	var req brand.FooterInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Brand.SetFooter(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"brand": got})
}

func (s *Server) handleSetBrandLogo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, brand.MaxLogoBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the logo as multipart form data in a part named file."))
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			respondError(w, r, ErrBadRequest("The upload has no part named file."))
			return
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				respondError(w, r, brand.ErrLogoTooLarge)
				return
			}
			respondError(w, r, ErrBadRequest("The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		got, lsn, err := s.Brand.SetLogo(r.Context(), actorFrom(r), part)
		noteWrite(r.Context(), lsn)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = brand.ErrLogoTooLarge
			}
			respondError(w, r, err)
			return
		}
		respondJSON(w, r, http.StatusOK, map[string]any{"brand": got})
		return
	}
}

func (s *Server) handleDeleteBrandLogo(w http.ResponseWriter, r *http.Request) {
	got, lsn, err := s.Brand.DeleteLogo(r.Context(), actorFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"brand": got})
}

// handleBrandLogo serves the picture; its version is in the address the
// client was given, so the browser may keep it.
func (s *Server) handleBrandLogo(w http.ResponseWriter, r *http.Request) {
	body, logo, err := s.Brand.OpenLogo(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", logo.ContentType)
	h.Set("Content-Length", strconv.Itoa(logo.Size))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// The logo of an open site, and of the site a public link names, are the same
// picture as a member reads; the site is already in the context.
func (s *Server) handlePublicLogo(w http.ResponseWriter, r *http.Request) { s.handleBrandLogo(w, r) }

func (s *Server) handleLinkedLogo(w http.ResponseWriter, r *http.Request) { s.handleBrandLogo(w, r) }
