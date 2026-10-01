package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// maxRequestIDLength bounds a client-supplied request id, which is echoed back
// and written to every log line.
const maxRequestIDLength = 64

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxLogger
	ctxPrincipal
)

// RequestIDFrom returns the id assigned to this request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// PrincipalFrom returns the authenticated caller, if any.
func PrincipalFrom(ctx context.Context) *auth.Principal {
	p, _ := ctx.Value(ctxPrincipal).(*auth.Principal)
	return p
}

// requestID assigns every request an id and echoes it back, so one identifier
// a user can quote ties a support request to the exact log lines.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > maxRequestIDLength {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush and Unwrap keep server-sent events and http.ResponseController working
// through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logging attaches a request-scoped logger and writes one access line per request.
func logging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			log := base.With("request_id", RequestIDFrom(r.Context()))
			if traceID := observability.TraceIDFrom(r.Context()); traceID != "" {
				log = log.With("trace_id", traceID)
			}
			ctx := context.WithValue(r.Context(), ctxLogger, log)

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rec.bytes,
			}
			log.Info("request", attrs...)
		})
	}
}

// recovery turns a panic into a 500 in the error envelope rather than a
// dropped connection, and keeps the process alive.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				// http.ErrAbortHandler is a deliberate abort, not a bug.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				loggerFrom(r.Context()).Error("panic in handler", "panic", v, "path", r.URL.Path)
				respondError(w, r, ErrInternal(nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Authenticator resolves a session cookie or bearer token to its principal. It
// returns auth.ErrInvalidToken for a credential that names nobody.
type Authenticator interface {
	Authenticate(ctx context.Context, credential string) (*auth.Principal, error)
}

// authenticate puts the caller and their tenant scope on the context. It never
// rejects an anonymous request: requireAuth does, where it matters.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := credentialFrom(r, s.CookieName)
		// The test endpoints answer to their own token alone, so a session or
		// an access token riding along can neither open nor refuse them.
		if secret == "" || s.Auth == nil || isTestPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		principal, err := s.Auth.Authenticate(r.Context(), secret)
		switch {
		case err == nil:
			ctx := context.WithValue(r.Context(), ctxPrincipal, principal)
			if principal.InOrg() {
				ctx = db.WithUser(tenant.WithOrg(ctx, *principal.Org), principal.UserID)
				ctx = audit.WithIP(ctx, clientIP(r))
			}
			r = r.WithContext(ctx)
		case errors.Is(err, auth.ErrInvalidToken):
			// A dead credential makes the caller anonymous, so sign-in still
			// works with a stale cookie; clearing it stops the resending.
			if _, cookieErr := r.Cookie(s.CookieName); cookieErr == nil {
				http.SetCookie(w, &http.Cookie{Name: s.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
			}
		default:
			// Any other refusal, such as a deactivated account, is a decision
			// about this caller; degrading it to anonymous would invite retries.
			respondError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// readOnlyToken refuses a write from a token made to read, before any handler,
// so the promise holds for every route including ones added later.
func readOnlyToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP carries reads and writes alike; the rule is applied to the calls
		// it carries, which run through this chain again.
		if !safeMethod(r.Method) && r.URL.Path != mcpPath && PrincipalFrom(r.Context()).ReadOnly() {
			respondError(w, r, errReadOnlyToken)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSession refuses a token an act that has to be done by somebody at a
// keyboard, such as making another token.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := PrincipalFrom(r.Context()); p != nil && p.TokenID != nil {
			respondError(w, r, errSessionOnly)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth rejects anonymous callers.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PrincipalFrom(r.Context()) == nil {
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOrg rejects a caller who has not chosen an organization, whose queries
// would otherwise reach the database with no tenant scope.
func requireOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		if !p.InOrg() {
			respondError(w, r, tenant.ErrNoTenant)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin lets through only an owner or administrator of the organization
// the caller is acting in.
func requireAdmin(next http.Handler) http.Handler {
	return requireOrg(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !PrincipalFrom(r.Context()).CanAdminister() {
			respondError(w, r, ErrForbidden("Only an administrator of this organization can do that. Ask one of them."))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// credentialFrom prefers the Authorization header over the session cookie, so
// API clients are never affected by cookie policy.
func credentialFrom(r *http.Request, cookieName string) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if token, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(token)
		}
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}
