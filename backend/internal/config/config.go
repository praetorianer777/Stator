// Package config loads and validates process configuration from the
// environment. Every setting is a STATOR_* variable; there is no file.
package config

import (
	"encoding/base64"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environments a deployment may declare itself as.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Defaults, named so the tests and the documentation can quote them.
const (
	DefaultHTTPAddr        = ":8080"
	DefaultMetricsAddr     = ":9090"
	DefaultAppBaseURL      = "http://localhost:5173"
	DefaultSessionCookie   = "stator_session"
	DefaultRequestTimeout  = 30 * time.Second
	DefaultMaxConns        = 20
	DefaultMinConns        = 2
	DefaultConnMaxLifetime = time.Hour
	DefaultHealthInterval  = 5 * time.Second
	DefaultMaxReplicaLag   = 2 * time.Second
	DefaultLagSamples      = 3
	DefaultReadYourWrites  = 30 * time.Second
	DefaultS3Bucket        = "stator-files"
	DefaultS3Region        = "us-east-1"
	DefaultMailFrom        = "Stator <no-reply@stator.localhost>"
	DefaultSessionTTL      = 720 * time.Hour
	// DefaultRetainAudit and MinRetainAudit are audit.DefaultRetention and
	// audit.MinRetention, which a test holds the two to.
	DefaultRetainAudit = 365 * 24 * time.Hour
	MinRetainAudit     = 24 * time.Hour
	// DefaultRetainPageViews and MinRetainPageViews are
	// pageview.DefaultRetention and pageview.MinRetention, held so by a test.
	DefaultRetainPageViews = 90 * 24 * time.Hour
	MinRetainPageViews     = 30 * 24 * time.Hour
	// DefaultVerificationCheck is page.DefaultLapseInterval, which a test
	// holds the two to.
	DefaultVerificationCheck = 10 * time.Minute
	// DefaultTaskDueCheck is task.DefaultDueInterval, which a test holds the
	// two to.
	DefaultTaskDueCheck = 10 * time.Minute
	// DefaultScheduleCheck is page.DefaultScheduleInterval, which a test
	// holds the two to.
	DefaultScheduleCheck = 30 * time.Second
	// DefaultUploadLimit is attachment.DefaultMaxSize, which a test holds
	// the two to; this package cannot import that one.
	DefaultUploadLimit int64 = 50 << 20
	// The render service's bounds, render.DefaultTimeout, DefaultConcurrency
	// and DefaultMaxSize, which a test holds these to.
	DefaultRenderTimeout           = 20 * time.Second
	DefaultRenderConcurrency       = 4
	DefaultRenderMaxSize     int64 = 50 << 20
	// OIDCCallbackPath is where an identity provider sends the browser back,
	// under the web client's origin, which proxies the API.
	OIDCCallbackPath = "/api/v1/auth/oidc/callback"
	// SecretKeyBytes is the AES-256 key length STATOR_SECRET_KEY decodes to.
	SecretKeyBytes = 32
)

// Config is the fully resolved configuration for every binary.
type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string
	// AppBaseURL is where the web client is served; the same-site check
	// accepts writes from it.
	AppBaseURL string
	// CORSOrigins may call the API with credentials from another origin. Empty
	// outside production means the development web server's own origins.
	CORSOrigins []string
	// RequestTimeout bounds every request's handler.
	RequestTimeout time.Duration

	DB        DB
	Valkey    Valkey
	Auth      Auth
	Telemetry Telemetry
	S3        S3
	// UploadLimit is the largest file a page takes, in bytes.
	UploadLimit int64
	// ConverterURL is the service that converts office documents to PDF for
	// previews; blank turns those previews off.
	ConverterURL string
	// Render is the service that prints pages as PDF; a blank URL turns
	// PDF export off.
	Render    Render
	Bootstrap Bootstrap
	// TestEndpoints serves the throwaway organizations of the browser suite.
	TestEndpoints TestEndpoints
	Mail          Mail
	Armature      Armature
	// RetainAudit is how long the worker keeps an audit entry; zero keeps
	// every one forever.
	RetainAudit time.Duration
	// RetainPageViews is how long the worker keeps who read which page on
	// which day before only the count stays; zero keeps them forever.
	RetainPageViews time.Duration
	// VerificationCheck is how often the worker looks for page verifications
	// that ran out, to tell their owners.
	VerificationCheck time.Duration
	// TaskDueCheck is how often the worker looks for tasks whose day came, to
	// remind their assignees.
	TaskDueCheck time.Duration
	// ScheduleCheck is how often the worker looks for scheduled publishes
	// whose time came; a publish goes out at most this late.
	ScheduleCheck time.Duration

	// SecretKey encrypts secrets stored in the database, such as an identity
	// provider's client secret. Nil in development when it is not set.
	SecretKey []byte
}

// Render names the render service, when there is one, and bounds its prints.
type Render struct {
	URL string
	// Timeout covers waiting for a free browser and printing.
	Timeout time.Duration
	// Concurrency is how many prints one api process runs at once.
	Concurrency int
	// MaxSize is the largest PDF handed back, in bytes.
	MaxSize int64
}

// DB describes the Postgres cluster: one writable primary and optional read
// replicas, so a developer can run against a single instance.
type DB struct {
	PrimaryURL  string
	ReplicaURLs []string
	// AdminURL connects as stator_admin, exempt from row level security by
	// policy, for signup, login and cross-tenant jobs. Defaults to PrimaryURL.
	AdminURL string

	// PrimaryMaxConns bounds the write pool, and the admin pool beside it;
	// ReplicaMaxConns bounds each read pool.
	PrimaryMaxConns int32
	ReplicaMaxConns int32
	MinConns        int32
	ConnMaxLifetime time.Duration
	HealthInterval  time.Duration
	MaxReplicaLag   time.Duration
	// ReplicaLagSamples is how many connections of a replica's pool each
	// health pass asks, so behind a load balanced service it sees several.
	ReplicaLagSamples int
	// ReadYourWritesTTL is how long a reader's last write position keeps
	// their reads off any replica that has not replayed it.
	ReadYourWritesTTL time.Duration
}

// Valkey is the shared store behind read-your-writes. A blank URL keeps the
// positions in the process, which is only right for a single api process.
type Valkey struct {
	URL string
}

// Auth holds the session cookie's settings and how sign-in reaches identity
// providers.
type Auth struct {
	SessionCookie string
	SecureCookies bool
	SessionTTL    time.Duration
	// OIDCRedirectURL is the callback address providers are told to send the
	// browser back to.
	OIDCRedirectURL string
	// OIDCBackchannel maps a provider's public origin to the one this process
	// reaches it at, for a stack where the two differ.
	OIDCBackchannel map[string]string
}

// Armature is how this process reaches the Armature instances organizations
// connect, and whatever else outside it an address names.
type Armature struct {
	// OutboundAllow lists the host names and CIDRs inside the network that
	// the SSRF guard lets through, as STATOR_OUTBOUND_ALLOW names them.
	OutboundAllow string
	// Backchannel maps an Armature's public origin to the one this process
	// reaches it at, for a stack where the two differ.
	Backchannel map[string]string
}

// Bootstrap is what cmd/seed sets up in the demo organization: a first local
// administrator, and an identity provider when it has none yet.
type Bootstrap struct {
	AdminEmail    string
	AdminPassword string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string

	// Members are let into the demo organization ahead of their first sign-in,
	// so a development stack needs nobody to approve them.
	Members []BootstrapMember
}

// BootstrapMember is one address and the standing it is given.
type BootstrapMember struct {
	Email string
	Role  string
}

// TestEndpoints make and remove throwaway organizations under /api/v1/test.
// Off, they answer 404 like any path that does not exist.
type TestEndpoints struct {
	Enabled bool
	// Token is the shared secret every call carries, so an endpoint switched
	// on by accident is still closed.
	Token string
}

// MinTestTokenLength keeps the test endpoints' token from being guessable.
const MinTestTokenLength = 16

// Mail is the relay the worker sends notifications through. A blank address
// leaves mail off, and only the rows in the app are written.
type Mail struct {
	SMTPAddr string
	From     string
}

// S3 is the bucket uploaded files live in: theme assets now, attachments
// later. Any service that speaks the S3 protocol will do; a blank endpoint
// leaves uploads off, and they are refused with the setting to fix.
type S3 struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Telemetry is the metrics listener and the trace collector. Both are off when
// their address is blank, and a blank set on purpose is honoured.
type Telemetry struct {
	// MetricsAddr serves /metrics on its own port, never through the API.
	MetricsAddr string
	// OTLPEndpoint is the collector's traces URL; empty means no traces leave.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept, 0 to 1.
	SampleRatio float64
}

// Error lists every problem Load found, so an operator fixes them in one go.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "Stator cannot start until its configuration is fixed:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Load reads configuration from the environment, applies defaults and validates
// the result. It returns every problem it finds at once rather than the first.
func Load() (Config, error) {
	l := &loader{}
	// STATOR_DB_MAX_CONNS sizes both pools unless one is sized on its own.
	maxConns := l.integer("STATOR_DB_MAX_CONNS", DefaultMaxConns)
	c := Config{
		Env:            l.str("STATOR_ENV", EnvDevelopment),
		HTTPAddr:       l.str("STATOR_HTTP_ADDR", DefaultHTTPAddr),
		LogLevel:       l.str("STATOR_LOG_LEVEL", "info"),
		AppBaseURL:     strings.TrimSuffix(l.str("STATOR_APP_URL", DefaultAppBaseURL), "/"),
		CORSOrigins:    splitList(l.str("STATOR_CORS_ORIGINS", "")),
		RequestTimeout: l.duration("STATOR_REQUEST_TIMEOUT", DefaultRequestTimeout),
		DB: DB{
			PrimaryURL:      l.str("STATOR_DB_PRIMARY_URL", ""),
			ReplicaURLs:     splitList(l.str("STATOR_DB_REPLICA_URLS", "")),
			AdminURL:        l.str("STATOR_DB_ADMIN_URL", ""),
			PrimaryMaxConns: int32(l.integer("STATOR_DB_PRIMARY_MAX_CONNS", maxConns)),
			ReplicaMaxConns: int32(l.integer("STATOR_DB_REPLICA_MAX_CONNS", maxConns)),
			MinConns:        int32(l.integer("STATOR_DB_MIN_CONNS", DefaultMinConns)),
			ConnMaxLifetime: l.duration("STATOR_DB_CONN_MAX_LIFETIME", DefaultConnMaxLifetime),
			HealthInterval:  l.duration("STATOR_DB_HEALTH_INTERVAL", DefaultHealthInterval),
			MaxReplicaLag:   l.duration("STATOR_DB_MAX_REPLICA_LAG", DefaultMaxReplicaLag),

			ReplicaLagSamples: l.integer("STATOR_DB_REPLICA_LAG_SAMPLES", DefaultLagSamples),
			ReadYourWritesTTL: l.duration("STATOR_READ_YOUR_WRITES_TTL", DefaultReadYourWrites),
		},
		Valkey: Valkey{
			URL: l.str("STATOR_VALKEY_URL", ""),
		},
		Auth: Auth{
			SessionCookie: l.str("STATOR_SESSION_COOKIE", DefaultSessionCookie),
			SecureCookies: l.boolean("STATOR_SECURE_COOKIES", false),
			SessionTTL:    l.duration("STATOR_SESSION_TTL", DefaultSessionTTL),
		},
		Telemetry: Telemetry{
			MetricsAddr:  l.strOrBlank("STATOR_METRICS_ADDR", DefaultMetricsAddr),
			OTLPEndpoint: l.str("STATOR_OTEL_ENDPOINT", ""),
			SampleRatio:  l.float("STATOR_OTEL_SAMPLE_RATIO", 1),
		},
		S3: S3{
			Endpoint:  l.str("STATOR_S3_ENDPOINT", ""),
			Bucket:    l.str("STATOR_S3_BUCKET", DefaultS3Bucket),
			AccessKey: l.str("STATOR_S3_ACCESS_KEY", ""),
			SecretKey: l.str("STATOR_S3_SECRET_KEY", ""),
			Region:    l.str("STATOR_S3_REGION", DefaultS3Region),
			UseSSL:    l.boolean("STATOR_S3_USE_SSL", false),
		},
		UploadLimit:  l.size("STATOR_UPLOAD_LIMIT", DefaultUploadLimit),
		ConverterURL: strings.TrimSuffix(l.str("STATOR_CONVERTER_URL", ""), "/"),
		Render: Render{
			URL:         strings.TrimSuffix(l.str("STATOR_RENDER_URL", ""), "/"),
			Timeout:     l.duration("STATOR_RENDER_TIMEOUT", DefaultRenderTimeout),
			Concurrency: l.integer("STATOR_RENDER_CONCURRENCY", DefaultRenderConcurrency),
			MaxSize:     l.size("STATOR_RENDER_MAX_SIZE", DefaultRenderMaxSize),
		},
		Bootstrap: Bootstrap{
			AdminEmail: l.str("STATOR_BOOTSTRAP_ADMIN_EMAIL", ""),
			// Not trimmed: a password is exactly what was typed.
			AdminPassword:    os.Getenv("STATOR_BOOTSTRAP_ADMIN_PASSWORD"),
			OIDCIssuer:       l.str("STATOR_BOOTSTRAP_OIDC_ISSUER", ""),
			OIDCClientID:     l.str("STATOR_BOOTSTRAP_OIDC_CLIENT_ID", ""),
			OIDCClientSecret: l.str("STATOR_BOOTSTRAP_OIDC_CLIENT_SECRET", ""),
			Members:          l.members("STATOR_BOOTSTRAP_MEMBERS"),
		},
		Mail: Mail{
			SMTPAddr: l.str("STATOR_SMTP_ADDR", ""),
			From:     l.str("STATOR_MAIL_FROM", DefaultMailFrom),
		},
		TestEndpoints: TestEndpoints{
			Enabled: l.boolean("STATOR_TEST_ENDPOINTS", false),
			Token:   l.str("STATOR_TEST_ENDPOINTS_TOKEN", ""),
		},
		RetainAudit:       l.duration("STATOR_RETAIN_AUDIT", DefaultRetainAudit),
		RetainPageViews:   l.duration("STATOR_RETAIN_PAGE_VIEWS", DefaultRetainPageViews),
		VerificationCheck: l.duration("STATOR_VERIFICATION_CHECK_INTERVAL", DefaultVerificationCheck),
		TaskDueCheck:      l.duration("STATOR_TASK_DUE_CHECK_INTERVAL", DefaultTaskDueCheck),
		ScheduleCheck:     l.duration("STATOR_SCHEDULE_CHECK_INTERVAL", DefaultScheduleCheck),
	}
	c.Auth.OIDCRedirectURL = l.str("STATOR_OIDC_REDIRECT_URL", c.AppBaseURL+OIDCCallbackPath)
	c.Auth.OIDCBackchannel = l.rewrites("STATOR_OIDC_BACKCHANNEL")
	c.Armature.OutboundAllow = l.str("STATOR_OUTBOUND_ALLOW", "")
	c.Armature.Backchannel = l.rewrites("STATOR_ARMATURE_BACKCHANNEL")
	c.SecretKey = l.secretKey("STATOR_SECRET_KEY", c.Env)

	if c.DB.PrimaryURL == "" {
		l.problem("Set STATOR_DB_PRIMARY_URL to the connection URL of the primary database.")
	}
	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		l.problem(fmt.Sprintf("STATOR_ENV is %q; set it to development, staging or production.", c.Env))
	}
	for _, pool := range []struct {
		key string
		max int32
	}{{"STATOR_DB_PRIMARY_MAX_CONNS", c.DB.PrimaryMaxConns}, {"STATOR_DB_REPLICA_MAX_CONNS", c.DB.ReplicaMaxConns}} {
		if pool.max < 1 {
			l.problem(pool.key + " must be at least 1.")
		} else if c.DB.MinConns > pool.max {
			l.problem(fmt.Sprintf("STATOR_DB_MIN_CONNS (%d) exceeds %s (%d); lower the first or raise the second.", c.DB.MinConns, pool.key, pool.max))
		}
	}
	if c.DB.ReplicaLagSamples < 1 {
		l.problem("STATOR_DB_REPLICA_LAG_SAMPLES must be at least 1.")
	}
	if c.DB.ReadYourWritesTTL <= 0 {
		l.problem("STATOR_READ_YOUR_WRITES_TTL must be longer than zero, such as 30s.")
	}
	if c.RequestTimeout <= 0 {
		l.problem("STATOR_REQUEST_TIMEOUT must be longer than zero, such as 30s.")
	}
	// Staging is a deployment people reach over a network, so it is held to
	// what production is held to; development is the one that runs locally.
	if c.Env != EnvDevelopment && !c.Auth.SecureCookies {
		l.problem("Set STATOR_SECURE_COOKIES to true outside development, so the session cookie never travels in clear.")
	}
	if c.Auth.SessionTTL <= 0 {
		l.problem("STATOR_SESSION_TTL must be longer than zero, such as 720h.")
	}
	if u, err := url.Parse(c.Auth.OIDCRedirectURL); err != nil || u.Scheme == "" || u.Host == "" {
		l.problem(fmt.Sprintf("STATOR_OIDC_REDIRECT_URL is %q; set it to an absolute URL ending in %s.", c.Auth.OIDCRedirectURL, OIDCCallbackPath))
	}
	if (c.Bootstrap.AdminEmail == "") != (c.Bootstrap.AdminPassword == "") {
		l.problem("Set both STATOR_BOOTSTRAP_ADMIN_EMAIL and STATOR_BOOTSTRAP_ADMIN_PASSWORD, or neither.")
	}
	if (c.Bootstrap.OIDCIssuer == "") != (c.Bootstrap.OIDCClientID == "") {
		l.problem("Set both STATOR_BOOTSTRAP_OIDC_ISSUER and STATOR_BOOTSTRAP_OIDC_CLIENT_ID, or neither.")
	}
	if c.Bootstrap.OIDCClientSecret != "" && c.SecretKey == nil {
		l.problem("STATOR_BOOTSTRAP_OIDC_CLIENT_SECRET is set but STATOR_SECRET_KEY is not; set the key so the secret can be stored sealed.")
	}
	if c.Bootstrap.AdminEmail != "" && !strings.Contains(c.Bootstrap.AdminEmail, "@") {
		l.problem(fmt.Sprintf("STATOR_BOOTSTRAP_ADMIN_EMAIL is %q; set it to an email address.", c.Bootstrap.AdminEmail))
	}
	if c.TestEndpoints.Enabled {
		if c.Env == EnvProduction {
			l.problem("STATOR_TEST_ENDPOINTS is on in production, where it would let anybody with its token make and delete organizations. Turn it off.")
		}
		if len(c.TestEndpoints.Token) < MinTestTokenLength {
			l.problem(fmt.Sprintf("STATOR_TEST_ENDPOINTS is on, so set STATOR_TEST_ENDPOINTS_TOKEN to a secret of at least %d characters.", MinTestTokenLength))
		}
		if c.Bootstrap.OIDCIssuer == "" {
			l.problem("STATOR_TEST_ENDPOINTS is on, so set STATOR_BOOTSTRAP_OIDC_ISSUER and its client: every throwaway organization signs in through that provider.")
		}
	}
	if c.RetainAudit != 0 && c.RetainAudit < MinRetainAudit {
		l.problem(fmt.Sprintf("STATOR_RETAIN_AUDIT is %s, but the audit log keeps every entry for at least %.0fh; set it to that or longer, such as 8760h, or to 0 to keep the log forever.", c.RetainAudit, MinRetainAudit.Hours()))
	}
	if c.RetainPageViews != 0 && c.RetainPageViews < MinRetainPageViews {
		l.problem(fmt.Sprintf("STATOR_RETAIN_PAGE_VIEWS is %s, but page views are kept for at least %.0fh so the recent counts have their whole period; set it to that or longer, such as 2160h, or to 0 to keep them forever.", c.RetainPageViews, MinRetainPageViews.Hours()))
	}
	if c.VerificationCheck < time.Second {
		l.problem(fmt.Sprintf("STATOR_VERIFICATION_CHECK_INTERVAL is %s; set it to a second or more, such as 10m.", c.VerificationCheck))
	}
	if c.TaskDueCheck < time.Second {
		l.problem(fmt.Sprintf("STATOR_TASK_DUE_CHECK_INTERVAL is %s; set it to a second or more, such as 10m.", c.TaskDueCheck))
	}
	if c.ScheduleCheck < time.Second {
		l.problem(fmt.Sprintf("STATOR_SCHEDULE_CHECK_INTERVAL is %s; set it to a second or more, such as 30s.", c.ScheduleCheck))
	}
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		l.problem("STATOR_OTEL_SAMPLE_RATIO must be between 0 and 1.")
	}
	if c.S3.Endpoint != "" && (c.S3.AccessKey == "" || c.S3.SecretKey == "") {
		l.problem("STATOR_S3_ENDPOINT is set, so set STATOR_S3_ACCESS_KEY and STATOR_S3_SECRET_KEY as well.")
	}
	if c.ConverterURL != "" {
		if u, err := url.Parse(c.ConverterURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			l.problem(fmt.Sprintf("STATOR_CONVERTER_URL is %q; set it to the conversion service's address, such as http://converter:3000, or leave it blank to turn office previews off.", c.ConverterURL))
		}
	}
	if c.Render.URL != "" {
		if u, err := url.Parse(c.Render.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			l.problem(fmt.Sprintf("STATOR_RENDER_URL is %q; set it to the render service's address, such as http://render:8090, or leave it blank to turn PDF export off.", c.Render.URL))
		}
	}
	if c.Render.Timeout <= 0 || c.Render.Timeout >= c.RequestTimeout {
		l.problem(fmt.Sprintf("STATOR_RENDER_TIMEOUT is %s; set it above zero and below STATOR_REQUEST_TIMEOUT (%s), so a slow print is refused in words rather than cut off.", c.Render.Timeout, c.RequestTimeout))
	}
	if c.Render.Concurrency < 1 {
		l.problem(fmt.Sprintf("STATOR_RENDER_CONCURRENCY is %d; set it to 1 or more.", c.Render.Concurrency))
	}
	if c.Render.MaxSize < 1 {
		l.problem("STATOR_RENDER_MAX_SIZE must be above zero, such as 50MB.")
	}
	if c.Mail.SMTPAddr != "" {
		if _, err := mail.ParseAddress(c.Mail.From); err != nil {
			l.problem(fmt.Sprintf("STATOR_MAIL_FROM is %q; set it to an address such as Stator <no-reply@example.com>.", c.Mail.From))
		}
	}
	if c.DB.AdminURL == "" {
		c.DB.AdminURL = c.DB.PrimaryURL
	}
	if len(l.problems) > 0 {
		return Config{}, &Error{Problems: l.problems}
	}
	return c, nil
}

// IsProduction reports whether this is the production deployment.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// loader reads variables and collects what it could not read, instead of
// quietly falling back to a default the operator did not ask for.
type loader struct {
	problems []string
}

func (l *loader) problem(p string) { l.problems = append(l.problems, p) }

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// strOrBlank is str for a setting where blank means off: set to nothing, it
// stays nothing rather than falling back to the default.
func (l *loader) strOrBlank(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func (l *loader) integer(key string, def int) int {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a whole number such as %d.", key, v, def))
		return def
	}
	return n
}

func (l *loader) float(key string, def float64) float64 {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a number such as %g.", key, v, def))
		return def
	}
	return f
}

func (l *loader) boolean(key string, def bool) bool {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to true or false.", key, v))
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a duration such as %s.", key, v, def))
		return def
	}
	return d
}

func (l *loader) size(key string, def int64) int64 {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	n, err := ParseSize(v)
	if err != nil {
		l.problem(fmt.Sprintf("%s is %q; set it to a size such as 50MB.", key, v))
		return def
	}
	return n
}

// ParseSize reads a byte count with an optional unit, as Armature does: B,
// K, KB, M, MB, G or GB, in either case, with or without a space.
func ParseSize(text string) (int64, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	units := []struct {
		suffix string
		scale  int64
	}{{"GB", 1 << 30}, {"G", 1 << 30}, {"MB", 1 << 20}, {"M", 1 << 20}, {"KB", 1 << 10}, {"K", 1 << 10}, {"B", 1}}
	scale := int64(1)
	for _, u := range units {
		if strings.HasSuffix(text, u.suffix) {
			text = strings.TrimSpace(strings.TrimSuffix(text, u.suffix))
			scale = u.scale
			break
		}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a size such as 50MB", text)
	}
	return n * scale, nil
}

// secretKey reads the encryption key. Development may run without one, and then
// cannot store secrets; a deployment reached over a network must set it.
func (l *loader) secretKey(key, env string) []byte {
	v := l.str(key, "")
	if v == "" {
		if env != EnvDevelopment {
			l.problem(fmt.Sprintf("Set %s to %d random bytes in base64, such as the output of openssl rand -base64 %d; it encrypts stored secrets.", key, SecretKeyBytes, SecretKeyBytes))
		}
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(v, "="))
	}
	if err != nil || len(decoded) != SecretKeyBytes {
		l.problem(fmt.Sprintf("%s must be %d random bytes in base64, such as the output of openssl rand -base64 %d.", key, SecretKeyBytes, SecretKeyBytes))
		return nil
	}
	return decoded
}

// members parses "email=role,email=role", each role owner, admin or member.
func (l *loader) members(key string) []BootstrapMember {
	var out []BootstrapMember
	for _, pair := range splitList(l.str(key, "")) {
		email, role, _ := strings.Cut(pair, "=")
		email, role = strings.TrimSpace(email), strings.TrimSpace(role)
		if !strings.Contains(email, "@") || (role != "owner" && role != "admin" && role != "member") {
			l.problem(fmt.Sprintf("%s has %q; write each entry as email=role, with the role owner, admin or member.", key, pair))
			continue
		}
		out = append(out, BootstrapMember{Email: email, Role: role})
	}
	return out
}

// rewrites parses "public=reachable,public=reachable", refusing a pair it
// cannot read rather than quietly dropping it.
func (l *loader) rewrites(key string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitList(l.str(key, "")) {
		from, to, ok := strings.Cut(pair, "=")
		from, to = strings.TrimSuffix(strings.TrimSpace(from), "/"), strings.TrimSuffix(strings.TrimSpace(to), "/")
		fromURL, errFrom := url.Parse(from)
		toURL, errTo := url.Parse(to)
		if !ok || errFrom != nil || errTo != nil || fromURL.Host == "" || toURL.Host == "" {
			l.problem(fmt.Sprintf("%s has %q; write each entry as public-origin=reachable-origin, such as http://localhost:8180=http://keycloak:8080.", key, pair))
			continue
		}
		out[from] = to
	}
	return out
}

// splitList parses a comma separated list, trimming blanks.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
