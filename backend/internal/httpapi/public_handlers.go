package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/public"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Reading without signing in: an anonymous reader in every transaction,
// whatever credential rides along.

// publicPath is where the reads for anybody live; authentication leaves them
// alone, so no session can widen or refuse them.
const (
	publicPath       = APIPrefix + "/public"
	publicPathPrefix = publicPath + "/"
)

// publicMaxAge is how long a cache may keep a public answer: a page restricted
// or a space closed stops being served by a shared cache within it.
const publicMaxAge = 60

func isPublicPath(r *http.Request) bool {
	return r.URL.Path == publicPath || strings.HasPrefix(r.URL.Path, publicPathPrefix)
}

// oldPublicPaths are the reads for anybody as their addresses ran after
// /public/{orgSlug} before #380, with the operation id each answers under.
var oldPublicPaths = []struct{ path, id string }{
	{"", "publicSiteWithSlug"},
	{"/spaces/{spaceKey}", "publicSpaceWithSlug"},
	{"/pages/{pageID}", "publicPageWithSlug"},
	{"/pages/{pageID}/pdf", "publicPagePDFWithSlug"},
	{"/pages/{pageID}/docx", "publicPageWordWithSlug"},
	{"/attachments/{attachmentID}", "publicAttachmentWithSlug"},
	{"/logo", "publicLogoWithSlug"},
	{"/search", "publicSearchWithSlug"},
	{"/links/{token}", "linkedPageWithSlug"},
	{"/links/{token}/pdf", "linkedPagePDFWithSlug"},
	{"/links/{token}/docx", "linkedPageWordWithSlug"},
	{"/links/{token}/attachments/{attachmentID}", "linkedAttachmentWithSlug"},
	{"/links/{token}/logo", "linkedLogoWithSlug"},
}

// oldPublicOperations describes oldPublicPaths for the table.
func oldPublicOperations() []operation {
	out := make([]operation, 0, len(oldPublicPaths))
	for _, old := range oldPublicPaths {
		out = append(out, operation{method: "GET", path: "/public/{orgSlug}" + old.path, handler: "handleOldPublicPath", id: old.id,
			tag: "public", public: true, redirect: true, deprecated: true,
			summary:   "The address GET /public" + old.path + " had while it named the organization: a permanent redirect there when the slug is the organization's, else not found.",
			responses: map[int]any{404: errorEnvelope{}}})
	}
	return out
}

// handleOldPublicPath sends an address naming the organization on to the same
// read without it, so links already shared keep working; another slug is not found.
func (s *Server) handleOldPublicPath(w http.ResponseWriter, r *http.Request) {
	_, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.EscapedPath(), publicPathPrefix), "/")
	link := strings.HasPrefix(rest, linkSegment+"/")
	if link {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
	}
	if !s.namesTheInstance(r, chi.URLParam(r, "orgSlug")) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		if link {
			respondError(w, r, public.ErrLinkGone)
		} else {
			respondError(w, r, public.ErrNotPublic)
		}
		return
	}
	target := publicPath
	if rest != "" {
		target += "/" + rest
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusPermanentRedirect)
}

// namesTheInstance says whether slug is the site's, as an old public address
// would have spelled it.
func (s *Server) namesTheInstance(r *http.Request, slug string) bool {
	if s.Instance == nil {
		return false
	}
	site, err := s.Instance.Site(r.Context())
	return err == nil && strings.EqualFold(strings.TrimSpace(slug), site.Slug)
}

type ctxSiteKey struct{}

func siteFrom(r *http.Request) *public.Site {
	site, _ := r.Context().Value(ctxSiteKey{}).(*public.Site)
	return site
}

// anonymousReader refuses while the site lets nobody read without
// signing in, and reads as nobody from then on.
func (s *Server) anonymousReader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Public == nil || s.Instance == nil {
			respondError(w, r, public.ErrNotPublic)
			return
		}
		if _, err := s.Instance.Site(r.Context()); err != nil {
			respondError(w, r, err)
			return
		}
		site, err := s.Public.Site(r.Context())
		if err != nil {
			respondError(w, r, err)
			return
		}
		if !site.Indexable {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		ctx := public.Reading(r.Context())
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxSiteKey{}, site)))
	})
}

// cachedPublicly lets a shared cache keep an answer that is the same for
// everybody; only a successful one is marked.
func cachedPublicly(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(publicMaxAge))
}

func (s *Server) handlePublicSite(w http.ResponseWriter, r *http.Request) {
	spaces, err := s.Public.Spaces(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	cachedPublicly(w)
	respondJSON(w, r, http.StatusOK, map[string]any{"site": siteFrom(r), "spaces": spaces})
}

func (s *Server) handlePublicSpace(w http.ResponseWriter, r *http.Request) {
	sp, pages, err := s.Public.Space(r.Context(), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	cachedPublicly(w)
	respondJSON(w, r, http.StatusOK, map[string]any{"space": sp, "pages": pages})
}

func (s *Server) handlePublicPage(w http.ResponseWriter, r *http.Request) {
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
	cachedPublicly(w)
	respondJSON(w, r, http.StatusOK, map[string]any{"page": got})
}

func (s *Server) handlePublicAttachment(w http.ResponseWriter, r *http.Request) {
	if s.Attachments == nil {
		respondError(w, r, public.ErrNotPublic)
		return
	}
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Attachments.LocateAnonymous(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	serveFile(w, r, found, "public, max-age="+strconv.Itoa(publicMaxAge))
}

func (s *Server) handlePublicSearch(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := window(r, search.DefaultLimit, search.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	hits, more, err := s.Search.Anonymous(r.Context(), q.Get("q"), q.Get("space"), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	cachedPublicly(w)
	respondJSON(w, r, http.StatusOK, map[string]any{"hits": hits, "more": more, "limit": limit, "offset": offset})
}

// The switches: administrators of the organization open it, administrators
// of a space open the space.

func (s *Server) handleGetAnonymousAccess(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.Settings(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"anonymousAccess": got})
}

func (s *Server) handleSetAnonymousAccess(w http.ResponseWriter, r *http.Request) {
	var req public.Settings
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Public.SetSettings(r.Context(), actorFrom(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"anonymousAccess": got})
}

func (s *Server) handleGetSpaceAnonymousAccess(w http.ResponseWriter, r *http.Request) {
	got, err := s.Spaces.AnonymousAccess(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"anonymousAccess": got})
}

func (s *Server) handleSetSpaceAnonymousAccess(w http.ResponseWriter, r *http.Request) {
	var req space.AnonymousAccessInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Spaces.SetAnonymousAccess(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"anonymousAccess": got})
}
