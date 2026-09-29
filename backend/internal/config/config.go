// Package config loads and validates process configuration from the
// environment. Every setting is a STATOR_* variable; there is no file.
package config

import (
	"fmt"
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
}

// DB describes the Postgres cluster: one writable primary and optional read
// replicas, so a developer can run against a single instance.
type DB struct {
	PrimaryURL  string
	ReplicaURLs []string
	// AdminURL connects as stator_admin, exempt from row level security by
	// policy, for signup, login and cross-tenant jobs. Defaults to PrimaryURL.
	AdminURL string

	MaxConns        int32
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

// Auth holds the session cookie's settings.
type Auth struct {
	SessionCookie string
	SecureCookies bool
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
			MaxConns:        int32(l.integer("STATOR_DB_MAX_CONNS", DefaultMaxConns)),
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
	}

	if c.DB.PrimaryURL == "" {
		l.problem("Set STATOR_DB_PRIMARY_URL to the connection URL of the primary database.")
	}
	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		l.problem(fmt.Sprintf("STATOR_ENV is %q; set it to development, staging or production.", c.Env))
	}
	if c.DB.MaxConns < 1 {
		l.problem("STATOR_DB_MAX_CONNS must be at least 1.")
	}
	if c.DB.MinConns > c.DB.MaxConns {
		l.problem(fmt.Sprintf("STATOR_DB_MIN_CONNS (%d) exceeds STATOR_DB_MAX_CONNS (%d); lower the first or raise the second.", c.DB.MinConns, c.DB.MaxConns))
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
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		l.problem("STATOR_OTEL_SAMPLE_RATIO must be between 0 and 1.")
	}
	if c.S3.Endpoint != "" && (c.S3.AccessKey == "" || c.S3.SecretKey == "") {
		l.problem("STATOR_S3_ENDPOINT is set, so set STATOR_S3_ACCESS_KEY and STATOR_S3_SECRET_KEY as well.")
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
