package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/theme"
)

// readinessTimeout bounds the database round trip behind /readyz, so a probe
// gets an answer before its own deadline does.
const readinessTimeout = 3 * time.Second

// APIPrefix is where every endpoint but the probes lives.
const APIPrefix = "/api/v1"

// Database is what the handlers ask of the cluster; *db.Cluster is the real one.
type Database interface {
	Ping(ctx context.Context) error
	Stats() db.Stats
}

// Server holds everything the handlers need.
type Server struct {
	DB   Database
	Auth Authenticator
	// Accounts signs people in and out; OIDC runs sign-in through a provider.
	// Both nil answer every sign-in route that it is not set up.
	Accounts *auth.Service
	OIDC     *oidc.Service
	// OIDCCallbackURL is the address to register with a provider.
	OIDCCallbackURL string
	Log             *slog.Logger
	// Telemetry counts and traces requests; nil serves without either.
	Telemetry *observability.Telemetry
	Themes    *theme.Service
	Spaces    *space.Service
	Pages     *page.Service
	Search    *search.Service
	Labels    *label.Service
	// Perms answers the permission screens and the use check in front of
	// every route; nil lets everybody who is a member through.
	Perms *perm.Service
	// Attachments keeps the files on pages; nil answers that storage is off.
	Attachments *attachment.Service
	// Fresh remembers each caller's last write between requests; nil leaves
	// reads unpinned, which is only right without replicas.
	Fresh Freshness
	// TestOrgs serves /test/orgs for the browser suite when it is set, to
	// callers with TestToken; nil leaves those paths unrouted.
	TestOrgs  TestOrgs
	TestToken string

	CookieName string
	Secure     bool
	// AppBaseURL is the web client's origin, which the same-site check trusts.
	AppBaseURL string
	// CheckOrigin also refuses a cookie-carried write from a foreign origin;
	// off in development, where the dev server proxies from an origin of its own.
	CheckOrigin bool
	// RequestTimeout bounds each handler; zero means the configured default.
	RequestTimeout time.Duration
}

// Routes builds the HTTP surface. The order matters: an id first so every later
// line can quote it, recovery inside the logger so a panic still logs a line.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	timeout := s.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}

	r := chi.NewRouter()
	r.Use(requestID)
	if s.Telemetry != nil {
		r.Use(observe(s.Telemetry.Metrics))
	}
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(middleware.Timeout(timeout))
	r.Use(s.authenticate)
	r.Use(readOnlyToken)
	r.Use(s.readYourWrites)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)

	r.Route(APIPrefix, func(r chi.Router) {
		r.Get("/openapi.json", s.handleOpenAPI)

		r.Post("/auth/login", s.handleLogin)
		r.Get("/auth/oidc/{orgSlug}/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/switch-org", s.handleSwitchOrg)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/oidc-provider", s.handleGetOIDCProvider)
			r.Put("/oidc-provider", s.handleSaveOIDCProvider)
			r.Get("/oidc-provider/group-roles", s.handleListGroupRoles)
			r.Post("/oidc-provider/group-roles", s.handleSetGroupRole)
			r.Delete("/oidc-provider/group-roles/{groupRoleID}", s.handleRemoveGroupRole)
			r.Get("/users", s.handleListMembers)
			r.Delete("/users/{userID}", s.handleRemoveMember)
			r.Get("/users/requests", s.handleListJoinRequests)
			r.Post("/users/requests/{userID}/admit", s.handleAdmitJoinRequest)
			r.Delete("/users/requests/{userID}", s.handleDeclineJoinRequest)
			r.Get("/org/tokens", s.handleListOrgAPITokens)
			r.Delete("/org/tokens/{tokenID}", s.handleRevokeOrgAPIToken)
			r.Get("/org/permissions", s.handleListGlobalPermissions)
			r.Put("/org/permissions/{permission}", s.handleSetGlobalPermission)
		})

		// What the caller may do is theirs to read even without use, so the
		// client can say why everything else is refused.
		r.With(requireOrg).Get("/access/me", s.handleMyAccess)

		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/tokens", s.handleListAPITokens)
			// Making one is for a session only, so a leaked token cannot mint a
			// longer lived one and outlive its own revocation.
			r.With(requireSession).Post("/tokens", s.handleCreateAPIToken)
			r.Delete("/tokens/{tokenID}", s.handleRevokeAPIToken)
		})

		// Themes are a person's in an organization; the examples are anybody's
		// signed in. The fixed paths come first so "active" is never an id.
		r.With(requireAuth).Get("/themes/examples", s.handleThemeExamples)
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/themes", s.handleListThemes)
			r.Post("/themes", s.handleCreateTheme)
			r.Get("/themes/active", s.handleActiveTheme)
			r.Put("/themes/active", s.handleChooseTheme)
			r.Put("/themes/default", s.handleSetDefaultTheme)
			r.Post("/themes/import", s.handleImportTheme)
			r.Get("/themes/{themeID}/export", s.handleExportTheme)
			r.Get("/themes/{themeID}", s.handleGetTheme)
			r.Patch("/themes/{themeID}", s.handleUpdateTheme)
			r.Delete("/themes/{themeID}", s.handleDeleteTheme)
			r.Post("/themes/{themeID}/assets", s.handleUploadThemeAsset)
			r.Get("/themes/{themeID}/assets/{assetID}", s.handleThemeAsset)
			r.Delete("/themes/{themeID}/assets/{assetID}", s.handleDeleteThemeAsset)
		})

		if s.TestOrgs != nil {
			r.Group(func(r chi.Router) {
				r.Use(s.requireTestToken)
				r.Post("/test/orgs", s.handleCreateTestOrg)
				r.Delete("/test/orgs/{orgSlug}", s.handleDeleteTestOrg)
			})
		}

		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/people", s.handleListPeople)
			r.Get("/groups", s.handleListGroups)
			r.Get("/spaces", s.handleListSpaces)
			r.Post("/spaces", s.handleCreateSpace)
			r.Get("/spaces/{spaceKey}", s.handleGetSpace)
			r.Patch("/spaces/{spaceKey}", s.handleUpdateSpace)
			r.Delete("/spaces/{spaceKey}", s.handleDeleteSpace)
			r.Get("/spaces/{spaceKey}/pages", s.handleListPages)
			r.Get("/spaces/{spaceKey}/outline", s.handleSpaceOutline)
			r.Get("/spaces/{spaceKey}/permissions", s.handleListSpacePermissions)
			r.Put("/spaces/{spaceKey}/permissions", s.handleSetSpacePermissions)
			r.Get("/spaces/{spaceKey}/trash", s.handleListTrash)
			r.Delete("/spaces/{spaceKey}/trash", s.handleEmptyTrash)
			r.Post("/spaces/{spaceKey}/trash/{pageID}/restore", s.handleRestorePage)
			r.Delete("/spaces/{spaceKey}/trash/{pageID}", s.handlePurgePage)
			r.Post("/pages", s.handleCreatePage)
			r.Get("/pages/{pageID}", s.handleGetPage)
			r.Patch("/pages/{pageID}", s.handleUpdatePage)
			r.Delete("/pages/{pageID}", s.handleTrashPage)
			r.Post("/pages/{pageID}/move", s.handleMovePage)
			r.Post("/pages/{pageID}/copy", s.handleCopyPage)
			r.Get("/templates", s.handleListTemplates)
			r.Get("/templates/{templateKey}", s.handleGetTemplate)
			r.Get("/pages/{pageID}/labels", s.handleListPageLabels)
			r.Post("/pages/{pageID}/labels", s.handleAddPageLabel)
			r.Delete("/pages/{pageID}/labels/{labelName}", s.handleRemovePageLabel)
			r.Get("/labels", s.handleSuggestLabels)
			r.Get("/labels/{labelName}/pages", s.handleListLabelPages)
			r.Get("/pages/{pageID}/attachments", s.handleListAttachments)
			r.Post("/pages/{pageID}/attachments", s.handleUploadAttachment)
			r.Get("/attachments/{attachmentID}", s.handleDownloadAttachment)
			r.Delete("/attachments/{attachmentID}", s.handleDeleteAttachment)
			r.Get("/pages/{pageID}/draft", s.handleGetDraft)
			r.Put("/pages/{pageID}/draft", s.handleSaveDraft)
			r.Delete("/pages/{pageID}/draft", s.handleDiscardDraft)
			r.Post("/pages/{pageID}/publish", s.handlePublishPage)
			r.Get("/pages/{pageID}/versions", s.handleListVersions)
			r.Get("/pages/{pageID}/versions/{versionNumber}", s.handleGetVersion)
			r.Post("/pages/{pageID}/versions/{versionNumber}/restore", s.handleRestoreVersion)
			r.Get("/pages/{pageID}/compare", s.handleCompareVersions)
			r.Get("/pages/{pageID}/restrictions", s.handleGetPageRestrictions)
			r.Put("/pages/{pageID}/restrictions", s.handleSetPageRestrictions)
			r.Post("/pages/{pageID}/visit", s.handleVisitPage)
			r.Get("/search", s.handleSearch)
			r.Get("/search/quick", s.handleQuickSearch)
			r.Get("/recent-pages", s.handleRecentPages)
			mountPending(r)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, ErrNotFound("No such endpoint. The API lives under "+APIPrefix+"."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "That method is not allowed here.",
		})
	})
	return r
}

// errNotImplemented answers an operation the API describes but does not serve yet.
var errNotImplemented = &APIError{Status: http.StatusNotImplemented, Code: "not_implemented",
	Message: "This part of the API is not built yet. Update Stator to a release that has it, or leave it out for now."}

// mountPending routes every pending operation to a 501, behind the same
// guards as the pages, so clients can be written against it before it exists.
func mountPending(r chi.Router) {
	for _, op := range operations {
		if op.pending {
			r.Method(op.method, op.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respondError(w, r, errNotImplemented)
			}))
		}
	}
}

// statusResponse is what the probes answer.
type statusResponse struct {
	Status string `json:"status"`
}

// readinessResponse adds how reads are being routed, for an operator.
type readinessResponse struct {
	Status  string   `json:"status"`
	Routing db.Stats `json:"routing"`
}

// handleLiveness touches nothing external on purpose: a database outage must
// not get the container killed and restarted, which would only make it worse.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

// handleReadiness answers whether the process can serve traffic. Nobody has
// signed in to read it, so a failure says whether, not why.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	if s.DB == nil {
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	if err := s.DB.Ping(ctx); err != nil {
		loggerFrom(r.Context()).Error("not ready", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	respondJSON(w, r, http.StatusOK, readinessResponse{Status: "ok", Routing: s.DB.Stats()})
}
