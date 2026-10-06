package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/public"
)

// linkPathInfix is what precedes a link's token in an address of the api,
// after the organization: /public/{org}/links/{token}.
const linkPathInfix = "/links/"

// redactPath keeps a public link's token out of the access log and the
// traces, where a path is written whole; the token is the whole of the link.
func redactPath(path string) string {
	if !strings.HasPrefix(path, publicPathPrefix) {
		return path
	}
	rest := path[len(publicPathPrefix):]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || !strings.HasPrefix(rest[slash:], linkPathInfix) {
		return path
	}
	head := publicPathPrefix + rest[:slash] + linkPathInfix
	after := rest[slash+len(linkPathInfix):]
	if i := strings.IndexByte(after, '/'); i >= 0 {
		return head + "{token}" + after[i:]
	}
	return head + "{token}"
}

// linkReader finds the organization a link's address names, while it allows
// links, and reads as an anonymous reader holding the token from then on. A
// token is a secret in the address, so no answer is kept by any cache or
// sent on as a referrer.
func (s *Server) linkReader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		token := chi.URLParam(r, "token")
		if s.Public == nil || !public.IsToken(token) {
			h.Set("X-Robots-Tag", "noindex, nofollow")
			respondError(w, r, public.ErrLinkGone)
			return
		}
		site, err := s.Public.LinkSite(r.Context(), chi.URLParam(r, "orgSlug"))
		if err != nil {
			h.Set("X-Robots-Tag", "noindex, nofollow")
			respondError(w, r, err)
			return
		}
		if !site.Indexable {
			h.Set("X-Robots-Tag", "noindex, nofollow")
		}
		ctx := public.ReadingLink(r.Context(), site, token)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxSiteKey{}, site)))
	})
}

func (s *Server) handleLinkedPage(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.LinkedPage(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"site": siteFrom(r), "page": got})
}

func (s *Server) handleLinkedAttachment(w http.ResponseWriter, r *http.Request) {
	if s.Attachments == nil {
		respondError(w, r, public.ErrLinkGone)
		return
	}
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, body, err := s.Attachments.OpenLinked(r.Context(), id)
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
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// A page's links, for whoever may edit it, and the organization's switch.

func (s *Server) handleListPageLinks(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Public.Links(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"publicLinks": got})
}

func (s *Server) handleCreatePageLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var in public.LinkInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	link, token, path, lsn, err := s.Public.CreateLink(r.Context(), actorFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The token is shown this once; an answer carrying it is kept nowhere.
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, http.StatusCreated, map[string]any{"link": link, "token": token, "path": path})
}

func (s *Server) handleRevokePageLink(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	linkID, apiErr := pathUUID(r, "linkID", "public link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Public.RevokeLink(r.Context(), actorFrom(r), id, linkID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPublicLinks(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.LinkSettings(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"publicLinks": got})
}

func (s *Server) handleSetPublicLinks(w http.ResponseWriter, r *http.Request) {
	var req public.LinkSettings
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Public.SetLinkSettings(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"publicLinks": got})
}
