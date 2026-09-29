package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/oidc"
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
	// Fresh remembers each caller's last write between requests; nil leaves
	// reads unpinned, which is only right without replicas.
	Fresh Freshness

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
			r.Get("/users/requests", s.handleListJoinRequests)
			r.Post("/users/requests/{userID}/admit", s.handleAdmitJoinRequest)
			r.Delete("/users/requests/{userID}", s.handleDeclineJoinRequest)
			r.Get("/org/tokens", s.handleListOrgAPITokens)
			r.Delete("/org/tokens/{tokenID}", s.handleRevokeOrgAPIToken)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireOrg)
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
			r.Use(requireOrg)
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
