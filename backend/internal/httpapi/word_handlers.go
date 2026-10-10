package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/docx"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/public"
)

// Word export (#87): the published page written as a .docx from its document,
// as its reader may read it, in the api itself; docs/decisions.md says why.

// PublicWordExportsPerMinute is the brake on one address exporting one public
// page or link: each export reads every picture on the page from storage.
const PublicWordExportsPerMinute = 6

var publicWordExports = newThrottle(PublicWordExportsPerMinute, time.Minute)

// wordScope is what the audit log names a Word export, beside PDF's and Markdown's.
const wordScope = "docx"

var (
	errExportFolder = &APIError{Status: http.StatusConflict, Code: "not_exportable",
		Message: "A folder has no words of its own to export. Open a page in it and export that one."}
	errExportUnpublished = &APIError{Status: http.StatusConflict, Code: "not_exportable",
		Message: "This page has not been published yet, and only what is published is exported. Publish it, then export it again."}
	errPublicWordExports = "This page was exported too often from here in the last minute. Wait a minute and try again."
)

// exportable refuses a page whose published words are not there to export.
func exportable(kind page.Kind, version int) *APIError {
	switch {
	case kind == page.KindFolder:
		return errExportFolder
	case version == 0:
		return errExportUnpublished
	}
	return nil
}

// handlePageWord writes the published page as the caller reads it.
func (s *Server) handlePageWord(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	actor := actorFrom(r)
	got, sp, err := s.Pages.Get(r.Context(), actor, id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if apiErr := exportable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	root, err := document.Parse(got.Body)
	if err != nil {
		respondError(w, r, err)
		return
	}
	reader := docx.Reader{
		BaseURL: s.AppBaseURL,
		Picture: func(ctx context.Context, file uuid.UUID) (io.ReadCloser, error) {
			if s.Attachments == nil {
				return nil, nil
			}
			_, body, err := s.Attachments.Open(ctx, actor, file)
			if errors.Is(err, attachment.ErrNotFound) {
				return nil, nil
			}
			return body, err
		},
		Include: func(ctx context.Context, pageID uuid.UUID, excerptID string, via []uuid.UUID) (*docx.Included, error) {
			inc, err := s.Pages.Included(ctx, actor, pageID, excerptID, via)
			if errors.Is(err, page.ErrNotFound) || errors.Is(err, page.ErrIncludeCycle) || errors.Is(err, page.ErrIncludeTooDeep) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			body, err := document.Parse(inc.Body)
			if err != nil {
				return nil, nil
			}
			return &docx.Included{Title: inc.Page.Title, URL: s.appURL(appPagePath(inc.Page.SpaceKey, inc.Page.ID, inc.Page.Title)), Body: body}, nil
		},
		FileURL: func(file uuid.UUID) string { return s.appURL("/api/v1/attachments/" + file.String()) },
	}
	p := PrincipalFrom(r.Context())
	lang := printLanguage(r, p)
	data, err := docx.Bytes(r.Context(), docx.Page{
		ID: got.ID, Title: got.Title, Space: sp.Name, Author: got.CreatedByName, Editor: got.UpdatedByName,
		Created: got.CreatedAt, Modified: got.UpdatedAt, Version: got.Version, Language: lang,
		URL: s.appURL(appPagePath(sp.Key, got.ID, got.Title)), Body: root, Brand: s.wordBrand(r, lang),
	}, reader)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteExport(w, r, audit.Entry{
		Action: audit.ActionPageExported, TargetType: "page", TargetID: &got.ID, Actor: p.UserID,
		Data: map[string]any{"title": got.Title, "space": got.SpaceKey, "scope": wordScope, "pages": 1},
	}) {
		return
	}
	sendWord(w, docx.FileName(sp.Key, got.Title, time.Now()), data)
}

// handlePublicPageWord writes a page anybody may read, as anybody reads it.
func (s *Server) handlePublicPageWord(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Public.Page(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if apiErr := exportable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if !publicWordExports.gate(w, r, clientIP(r)+" "+got.ID.String(), errPublicWordExports) {
		return
	}
	root, err := document.Parse(got.Body)
	if err != nil {
		respondError(w, r, err)
		return
	}
	org := url.PathEscape(chi.URLParam(r, "orgSlug"))
	site := "/public/" + org
	reader := docx.Reader{
		BaseURL: s.AppBaseURL,
		Picture: s.publicPicture(func(ctx context.Context, file uuid.UUID) (*attachment.Located, error) {
			return s.Attachments.LocateAnonymous(ctx, file)
		}),
		IncludeURL: func(pageID uuid.UUID) string { return s.appURL(site + "/p/" + pageID.String()) },
		FileURL: func(file uuid.UUID) string {
			return s.appURL("/api/v1/public/" + org + "/attachments/" + file.String())
		},
	}
	lang := printLanguage(r, nil)
	data, err := docx.Bytes(r.Context(), docx.Page{
		ID: got.ID, Title: got.Title, Space: got.Space.Name, Modified: got.Updated, Version: got.Version,
		Language: lang, URL: s.appURL(site + appPagePath(got.Space.Key, got.ID, got.Title)), Body: root, Brand: s.wordBrand(r, lang),
	}, reader)
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendWord(w, docx.FileName(got.Space.Key, got.Title, time.Now()), data)
}

// handleLinkedPageWord writes the page a public link opens, for whoever holds it,
// with no address carrying the token: a file travels further than a link should.
func (s *Server) handleLinkedPageWord(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.LinkedPage(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if apiErr := exportable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if !publicWordExports.gate(w, r, clientIP(r)+" "+got.ID.String(), errPublicWordExports) {
		return
	}
	root, err := document.Parse(got.Body)
	if err != nil {
		respondError(w, r, err)
		return
	}
	reader := docx.Reader{
		Picture: s.publicPicture(func(ctx context.Context, file uuid.UUID) (*attachment.Located, error) {
			return s.Attachments.LocateLinked(ctx, file)
		}),
	}
	lang := printLanguage(r, nil)
	data, err := docx.Bytes(r.Context(), docx.Page{
		ID: got.ID, Title: got.Title, Modified: got.Updated, Version: got.Version,
		Language: lang, Body: root, Brand: s.wordBrand(r, lang),
	}, reader)
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendWord(w, docx.FileName("", got.Title, time.Now()), data)
}

// publicPicture reads a picture for somebody who is not signed in through
// locate, a file they may not read being left out.
func (s *Server) publicPicture(locate func(context.Context, uuid.UUID) (*attachment.Located, error)) func(context.Context, uuid.UUID) (io.ReadCloser, error) {
	return func(ctx context.Context, file uuid.UUID) (io.ReadCloser, error) {
		if s.Attachments == nil {
			return nil, nil
		}
		found, err := locate(ctx, file)
		if errors.Is(err, attachment.ErrNotFound) || errors.Is(err, public.ErrNotPublic) || errors.Is(err, public.ErrLinkGone) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return found.Bytes(ctx)
	}
}

// appPagePath is a page's address in the web client.
func appPagePath(spaceKey string, id uuid.UUID, title string) string {
	return "/s/" + url.PathEscape(spaceKey) + "/p/" + id.String() + "/" + url.PathEscape(document.Slug(title))
}

// appURL is an address of the web client, or empty when it has none set.
func (s *Server) appURL(path string) string {
	if s.AppBaseURL == "" {
		return ""
	}
	return s.AppBaseURL + path
}

func sendWord(w http.ResponseWriter, name string, data []byte) {
	download(w, docx.ContentType, name)
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// wordBrand is the organization's brand for a document, or none where it
// cannot be read: a document is not refused for want of a logo.
func (s *Server) wordBrand(r *http.Request, lang string) *docx.Brand {
	if s.Brand == nil {
		return nil
	}
	got, err := s.Brand.Export(r.Context(), lang)
	if err != nil {
		loggerFrom(r.Context()).Warn("could not read the brand for a Word document", "error", err)
		return nil
	}
	return &docx.Brand{Name: got.Name, Footer: got.Footer, Accent: got.Accent, Logo: got.Logo}
}
