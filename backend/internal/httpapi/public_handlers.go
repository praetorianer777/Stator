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

// Reading without signing in: the organization in the address, an anonymous
// reader in every transaction, whatever credential rides along.

// publicPathPrefix is where the reads for anybody live; authentication
// leaves them alone, so no session can widen or refuse them.
const publicPathPrefix = APIPrefix + "/public/"

// publicMaxAge is how long a cache may keep a public answer: a page restricted
// or a space closed stops being served by a shared cache within it.
const publicMaxAge = 60

func isPublicPath(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, publicPathPrefix) }

type ctxSiteKey struct{}

func siteFrom(r *http.Request) *public.Site {
	site, _ := r.Context().Value(ctxSiteKey{}).(*public.Site)
	return site
}

// anonymousReader finds the organization the address names, refuses one that
// lets nobody read without signing in, and reads as nobody from then on.
func (s *Server) anonymousReader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Public == nil {
			respondError(w, r, public.ErrNotPublic)
			return
		}
		site, err := s.Public.Site(r.Context(), chi.URLParam(r, "orgSlug"))
		if err != nil {
			respondError(w, r, err)
			return
		}
		if !site.Indexable {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		ctx := public.Reading(r.Context(), site)
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
