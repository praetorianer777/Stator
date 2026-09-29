package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/theme"
)

// Themes: a person's own redefinition of the interface, shared with the
// organization when they say so. Every route reads as the caller; changing
// one is the owner's, or an administrator's once it is shared. Adapted from
// Armature, so its theme files and its clients work here unchanged.

type chooseThemeRequest struct {
	ThemeID *uuid.UUID `json:"themeId"`
	// BuiltIn with no theme keeps the built-in theme over the organization's default.
	BuiltIn bool `json:"builtIn,omitempty"`
}

type defaultThemeRequest struct {
	ThemeID *uuid.UUID `json:"themeId"`
}

// administers asks the database rather than the session, since there are no
// permissions on the principal yet; a failed lookup administers nothing.
func (s *Server) administers(r *http.Request) bool {
	ok, err := s.Themes.Administers(r.Context(), userFrom(r))
	if err != nil {
		loggerFrom(r.Context()).Warn("could not tell whether the caller administers", "error", err)
		return false
	}
	return ok
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	themes, err := s.Themes.List(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"themes": themes})
}

func (s *Server) handleThemeExamples(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"examples": theme.Examples()})
}

func (s *Server) handleCreateTheme(w http.ResponseWriter, r *http.Request) {
	var req theme.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, _, err := s.Themes.Create(r.Context(), userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"theme": made})
}

func (s *Server) handleActiveTheme(w http.ResponseWriter, r *http.Request) {
	active, source, err := s.Themes.Active(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": active, "source": string(source)})
}

func (s *Server) handleSetDefaultTheme(w http.ResponseWriter, r *http.Request) {
	var req defaultThemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.administers(r) {
		respondError(w, r, ErrForbidden("Only an owner or an administrator of the organization can name its default theme."))
		return
	}
	chosen, _, err := s.Themes.SetDefault(r.Context(), userFrom(r), req.ThemeID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": chosen})
}

func (s *Server) handleChooseTheme(w http.ResponseWriter, r *http.Request) {
	var req chooseThemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	chosen, _, err := s.Themes.Choose(r.Context(), userFrom(r), req.ThemeID, req.BuiltIn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": chosen})
}

func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Themes.Get(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": found})
}

func (s *Server) handleUpdateTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req theme.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, _, err := s.Themes.Update(r.Context(), id, userFrom(r), s.administers(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": updated})
}

func (s *Server) handleDeleteTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	_, err := s.Themes.Delete(r.Context(), id, userFrom(r), s.administers(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleUploadThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, theme.MaxAssetBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the file as multipart form data in a part named file."))
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
				respondError(w, r, theme.ErrAssetTooLarge)
				return
			}
			respondError(w, r, ErrBadRequest("The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		asset, _, err := s.Themes.UploadAsset(r.Context(), id, userFrom(r), s.administers(r), part.FileName(), part)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = theme.ErrAssetTooLarge
			}
			respondError(w, r, err)
			return
		}
		respondJSON(w, r, http.StatusCreated, map[string]any{"asset": asset})
		return
	}
}

// handleExportTheme hands the theme over as one file, so it can be kept or
// given to another organization.
func (s *Server) handleExportTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	pkg, err := s.Themes.Export(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	body, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		respondError(w, r, err)
		return
	}
	safe := strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			return c
		}
		return '-'
	}, pkg.Name)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Content-Disposition", `attachment; filename="`+safe+`.armature-theme.json"`)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleImportTheme reads an exported theme, as a multipart part named file,
// and makes it the caller's own.
func (s *Server) handleImportTheme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, theme.MaxPackageBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the theme file as multipart form data in a part named file."))
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			respondError(w, r, ErrBadRequest("The upload has no part named file."))
			return
		}
		if err != nil {
			respondError(w, r, ErrBadRequest("The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		var pkg theme.Package
		if err := json.NewDecoder(part).Decode(&pkg); err != nil {
			respondError(w, r, asValidationError(theme.ErrNotAThemeFile))
			return
		}
		made, _, err := s.Themes.Import(r.Context(), userFrom(r), &pkg)
		if err != nil {
			respondError(w, r, asValidationError(err))
			return
		}
		respondJSON(w, r, http.StatusCreated, map[string]any{"theme": made})
		return
	}
}

// handleThemeAsset streams a file as a download, so an SVG never renders as a
// page of ours; the id never changes, so the browser may keep it.
func (s *Server) handleThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	assetID, apiErr := pathUUID(r, "assetID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	body, asset, err := s.Themes.OpenAsset(r.Context(), id, assetID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", asset.ContentType)
	h.Set("Content-Length", strconv.FormatInt(asset.Size, 10))
	h.Set("Content-Disposition", `attachment; filename="`+asset.Name+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (s *Server) handleDeleteThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	assetID, apiErr := pathUUID(r, "assetID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	_, err := s.Themes.DeleteAsset(r.Context(), id, assetID, userFrom(r), s.administers(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
