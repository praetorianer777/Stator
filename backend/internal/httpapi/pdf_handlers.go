package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/render"
)

// PDF export (#86), adapted from Armature's pdf_handlers.go: the render service
// opens the page's print view in a browser and prints it. A reader who signed
// in is printed with a token made for that one print; anybody else's print is
// of what anybody may read, and needs no credential.

// PublicPDFsPerMinute is the brake on one address printing one public page or
// link; a print is a whole browser's work, and nobody signed in to be asked.
const PublicPDFsPerMinute = 6

var publicPDFs = newThrottle(PublicPDFsPerMinute, time.Minute)

// pdfScope is what the audit log names a PDF export, beside Markdown's scopes.
const pdfScope = "pdf"

// errPrintFolder and errPrintUnpublished refuse what has no published words to print.
var (
	errPrintFolder = &APIError{Status: http.StatusConflict, Code: "not_printable",
		Message: "A folder has no words of its own to print. Open a page in it and export that one."}
	errPrintUnpublished = &APIError{Status: http.StatusConflict, Code: "not_printable",
		Message: "This page has not been published yet, and only what is published is exported. Publish it, then export it again."}
	errPublicPDFs = "This page was printed too often from here in the last minute. Wait a minute and try again."
)

func (s *Server) renderer() render.Renderer {
	if s.Renderer == nil {
		return render.Unavailable{}
	}
	return s.Renderer
}

// printable refuses a page whose published words are not there to print.
func printable(kind page.Kind, version int) *APIError {
	switch {
	case kind == page.KindFolder:
		return errPrintFolder
	case version == 0:
		return errPrintUnpublished
	}
	return nil
}

// handlePagePDF prints the published page as the caller reads it.
func (s *Server) handlePagePDF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, sp, err := s.Pages.Get(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if apiErr := printable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if s.Accounts == nil {
		respondError(w, r, render.ErrUnavailable)
		return
	}
	if _, unavailable := s.renderer().(render.Unavailable); unavailable {
		respondError(w, r, render.ErrUnavailable)
		return
	}
	p := PrincipalFrom(r.Context())
	secret, tokenID, lsn, err := s.Accounts.MintRenderToken(r.Context(), p)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// Spent or not, the token goes once the print is back, even when the
	// reader has stopped waiting for it.
	defer func() {
		if err := s.Accounts.RevokeRenderToken(context.WithoutCancel(r.Context()), tokenID); err != nil {
			loggerFrom(r.Context()).Error("a print's token outlives its print until it expires", "token_id", tokenID, "error", err)
		}
	}()
	carryWrites(r.Context(), tokenFreshnessKey(tokenID), lsn)

	pdf, err := s.renderer().PDF(r.Context(), render.Request{Path: printPath("/print/p/"+got.ID.String(), r, p), Token: secret})
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteExport(w, r, audit.Entry{
		Action: audit.ActionPageExported, TargetType: "page", TargetID: &got.ID, Actor: p.UserID,
		Data: map[string]any{"title": got.Title, "space": got.SpaceKey, "scope": pdfScope, "pages": 1},
	}) {
		return
	}
	sendPDF(w, pdfName(sp.Key, got.Title), pdf)
}

// handlePublicPagePDF prints a page anybody may read, as anybody reads it.
func (s *Server) handlePublicPagePDF(w http.ResponseWriter, r *http.Request) {
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
	if apiErr := printable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if !publicPDFs.gate(w, r, clientIP(r)+" "+got.ID.String(), errPublicPDFs) {
		return
	}
	org := chi.URLParam(r, "orgSlug")
	pdf, err := s.renderer().PDF(r.Context(), render.Request{Path: printPath("/print/public/"+url.PathEscape(org)+"/p/"+got.ID.String(), r, nil)})
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendPDF(w, pdfName(got.Space.Key, got.Title), pdf)
}

// handleLinkedPagePDF prints the page a public link opens, for whoever holds it.
func (s *Server) handleLinkedPagePDF(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.LinkedPage(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if apiErr := printable(got.Kind, got.Version); apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if !publicPDFs.gate(w, r, clientIP(r)+" "+got.ID.String(), errPublicPDFs) {
		return
	}
	org, token := chi.URLParam(r, "orgSlug"), chi.URLParam(r, "token")
	pdf, err := s.renderer().PDF(r.Context(), render.Request{Path: printPath("/print/public/"+url.PathEscape(org)+"/link/"+url.PathEscape(token), r, nil)})
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendPDF(w, pdfName("", got.Title), pdf)
}

// printPath is the print view of path in the reader's language: the one they
// chose, else the first of their browser's the interface speaks.
func printPath(path string, r *http.Request, p *auth.Principal) string {
	return path + "?lang=" + printLanguage(r, p)
}

func printLanguage(r *http.Request, p *auth.Principal) string {
	if p != nil && p.Locale != auth.LocaleBrowser {
		return string(p.Locale)
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		primary, _, _ := strings.Cut(strings.ToLower(tag), "-")
		for _, l := range auth.Locales {
			if l != auth.LocaleBrowser && string(l) == primary {
				return primary
			}
		}
	}
	return string(auth.LocaleEnglish)
}

// pdfName names the file after the space, the page and the day, as Armature
// names a printed dashboard.
func pdfName(spaceKey, title string) string {
	name := document.Slug(title) + "-" + time.Now().Format("2006-01-02") + ".pdf"
	if spaceKey == "" {
		return name
	}
	return spaceKey + "-" + name
}

func sendPDF(w http.ResponseWriter, name string, pdf []byte) {
	download(w, "application/pdf", name)
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// tokenFreshnessKey is where a token's last write position is kept.
func tokenFreshnessKey(id uuid.UUID) string { return "t:" + id.String() }
