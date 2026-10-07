package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// clean starts every test from an environment with only the required setting,
// so a variable exported on the developer's machine cannot change the outcome.
func clean(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"STATOR_ENV", "STATOR_HTTP_ADDR", "STATOR_LOG_LEVEL", "STATOR_APP_URL", "STATOR_CORS_ORIGINS",
		"STATOR_REQUEST_TIMEOUT", "STATOR_DB_REPLICA_URLS", "STATOR_DB_ADMIN_URL", "STATOR_DB_MAX_CONNS",
		"STATOR_DB_PRIMARY_MAX_CONNS", "STATOR_DB_REPLICA_MAX_CONNS",
		"STATOR_DB_MIN_CONNS", "STATOR_DB_CONN_MAX_LIFETIME", "STATOR_DB_HEALTH_INTERVAL",
		"STATOR_DB_MAX_REPLICA_LAG", "STATOR_DB_REPLICA_LAG_SAMPLES", "STATOR_READ_YOUR_WRITES_TTL",
		"STATOR_VALKEY_URL", "STATOR_SESSION_COOKIE",
		"STATOR_SECURE_COOKIES", "STATOR_OTEL_ENDPOINT", "STATOR_OTEL_SAMPLE_RATIO",
		"STATOR_S3_ENDPOINT", "STATOR_S3_BUCKET", "STATOR_S3_ACCESS_KEY", "STATOR_S3_SECRET_KEY",
		"STATOR_S3_REGION", "STATOR_S3_USE_SSL", "STATOR_UPLOAD_LIMIT", "STATOR_CONVERTER_URL",
		"STATOR_RENDER_URL", "STATOR_RENDER_TIMEOUT", "STATOR_RENDER_CONCURRENCY", "STATOR_RENDER_MAX_SIZE",
		"STATOR_SESSION_TTL", "STATOR_OIDC_REDIRECT_URL", "STATOR_OIDC_BACKCHANNEL", "STATOR_SECRET_KEY",
		"STATOR_BOOTSTRAP_ADMIN_EMAIL", "STATOR_BOOTSTRAP_ADMIN_PASSWORD",
		"STATOR_BOOTSTRAP_OIDC_ISSUER", "STATOR_BOOTSTRAP_OIDC_CLIENT_ID", "STATOR_BOOTSTRAP_OIDC_CLIENT_SECRET",
		"STATOR_BOOTSTRAP_MEMBERS", "STATOR_TEST_ENDPOINTS", "STATOR_TEST_ENDPOINTS_TOKEN",
		"STATOR_SMTP_ADDR", "STATOR_MAIL_FROM", "STATOR_OUTBOUND_ALLOW", "STATOR_ARMATURE_BACKCHANNEL",
		"STATOR_RETAIN_AUDIT", "STATOR_RETAIN_PAGE_VIEWS",
		"STATOR_VERIFICATION_CHECK_INTERVAL", "STATOR_TASK_DUE_CHECK_INTERVAL", "STATOR_SCHEDULE_CHECK_INTERVAL",
		"STATOR_EXAMPLE_CHECK_INTERVAL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("STATOR_DB_PRIMARY_URL", "postgres://app@db/stator")
}

// The conversion service is optional, and an address that is no web
// address is refused by name rather than failing at the first preview.
func TestTheConverterIsAnHTTPAddressOrNothing(t *testing.T) {
	clean(t)
	if cfg, err := Load(); err != nil || cfg.ConverterURL != "" {
		t.Fatalf("no converter reads as %q, %v", cfg.ConverterURL, err)
	}
	t.Setenv("STATOR_CONVERTER_URL", "http://converter:3000/")
	if cfg, err := Load(); err != nil || cfg.ConverterURL != "http://converter:3000" {
		t.Errorf("the converter reads as %q, %v", cfg.ConverterURL, err)
	}
	for _, bad := range []string{"converter:3000", "ftp://converter", "http://"} {
		t.Setenv("STATOR_CONVERTER_URL", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_CONVERTER_URL") {
			t.Errorf("%q should be refused by name, got %v", bad, err)
		}
	}
}

// PDF export is optional too, and its bounds keep a print inside a request.
func TestTheRenderServiceIsAnHTTPAddressWithBoundsInsideARequest(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil || cfg.Render.URL != "" || cfg.Render.Timeout != DefaultRenderTimeout || cfg.Render.Concurrency != DefaultRenderConcurrency || cfg.Render.MaxSize != DefaultRenderMaxSize {
		t.Fatalf("no render service reads as %+v, %v", cfg.Render, err)
	}
	t.Setenv("STATOR_RENDER_URL", "http://render:8090/")
	t.Setenv("STATOR_RENDER_MAX_SIZE", "10MB")
	t.Setenv("STATOR_RENDER_CONCURRENCY", "2")
	if cfg, err := Load(); err != nil || cfg.Render.URL != "http://render:8090" || cfg.Render.MaxSize != 10<<20 || cfg.Render.Concurrency != 2 {
		t.Errorf("the render service reads as %+v, %v", cfg.Render, err)
	}
	for key, bad := range map[string]string{
		"STATOR_RENDER_URL":         "render:8090",
		"STATOR_RENDER_TIMEOUT":     "30s",
		"STATOR_RENDER_CONCURRENCY": "0",
	} {
		clean(t)
		t.Setenv(key, bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q should be refused by name, got %v", key, bad, err)
		}
	}
}

func TestUploadLimitReadsSizes(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{{"50MB", 50 << 20}, {"2g", 2 << 30}, {"512 kb", 512 << 10}, {"1048576", 1 << 20}, {"", DefaultUploadLimit}} {
		clean(t)
		t.Setenv("STATOR_UPLOAD_LIMIT", tc.text)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", tc.text, err)
		}
		if cfg.UploadLimit != tc.want {
			t.Errorf("%q read as %d, want %d", tc.text, cfg.UploadLimit, tc.want)
		}
	}
	for _, bad := range []string{"lots", "-5MB", "0"} {
		clean(t)
		t.Setenv("STATOR_UPLOAD_LIMIT", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_UPLOAD_LIMIT") {
			t.Errorf("%q should be refused by name, got %v", bad, err)
		}
	}
}

func TestDefaults(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_METRICS_ADDR", DefaultMetricsAddr)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvDevelopment || c.HTTPAddr != DefaultHTTPAddr || c.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("process defaults = %+v", c)
	}
	if c.DB.PrimaryMaxConns != DefaultMaxConns || c.DB.ReplicaMaxConns != DefaultMaxConns || c.DB.MinConns != DefaultMinConns || c.DB.HealthInterval != DefaultHealthInterval {
		t.Errorf("pool defaults = %+v", c.DB)
	}
	if c.DB.AdminURL != c.DB.PrimaryURL {
		t.Errorf("the admin URL should fall back to the primary, got %q", c.DB.AdminURL)
	}
	if c.DB.ReplicaLagSamples != DefaultLagSamples || c.DB.ReadYourWritesTTL != DefaultReadYourWrites || c.Valkey.URL != "" {
		t.Errorf("replica and freshness defaults = %+v, %+v", c.DB, c.Valkey)
	}
	if c.Auth.SessionCookie != DefaultSessionCookie || c.Auth.SecureCookies {
		t.Errorf("auth defaults = %+v", c.Auth)
	}
	if c.Telemetry.MetricsAddr != DefaultMetricsAddr || c.Telemetry.SampleRatio != 1 {
		t.Errorf("telemetry defaults = %+v", c.Telemetry)
	}
}

func TestListsAreTrimmed(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_DB_REPLICA_URLS", " postgres://a , ,postgres://b ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DB.ReplicaURLs) != 2 || c.DB.ReplicaURLs[0] != "postgres://a" || c.DB.ReplicaURLs[1] != "postgres://b" {
		t.Fatalf("replicas = %q", c.DB.ReplicaURLs)
	}
}

// Blank on purpose is off, unlike every other setting where blank is unset.
func TestMetricsAddressBlankMeansOff(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_METRICS_ADDR", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Telemetry.MetricsAddr != "" {
		t.Fatalf("metrics address = %q, want it off", c.Telemetry.MetricsAddr)
	}
}

func TestEveryProblemIsReportedAtOnceAsSentences(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_DB_PRIMARY_URL", "")
	t.Setenv("STATOR_ENV", "moon")
	t.Setenv("STATOR_DB_MAX_CONNS", "many")
	t.Setenv("STATOR_REQUEST_TIMEOUT", "soon")
	t.Setenv("STATOR_SECURE_COOKIES", "perhaps")
	t.Setenv("STATOR_OTEL_SAMPLE_RATIO", "7")

	_, err := Load()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v, want a *config.Error", err)
	}
	for _, want := range []string{
		"STATOR_DB_PRIMARY_URL", "STATOR_ENV", "STATOR_DB_MAX_CONNS", "STATOR_REQUEST_TIMEOUT",
		"STATOR_SECURE_COOKIES", "STATOR_OTEL_SAMPLE_RATIO",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %s:\n%v", want, err)
		}
	}
	for _, p := range cfgErr.Problems {
		if p == "" || p[0] < 'A' || p[0] > 'Z' || !strings.HasSuffix(p, ".") {
			t.Errorf("%q is not a sentence", p)
		}
	}
}

func TestPoolBoundsMustAgree(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_DB_MAX_CONNS", "2")
	t.Setenv("STATOR_DB_MIN_CONNS", "5")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_DB_MIN_CONNS") {
		t.Fatalf("a minimum above the maximum should be refused: %v", err)
	}
	t.Setenv("STATOR_DB_MAX_CONNS", "")
	t.Setenv("STATOR_DB_MIN_CONNS", "")
	t.Setenv("STATOR_DB_REPLICA_MAX_CONNS", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_DB_REPLICA_MAX_CONNS must be at least 1.") {
		t.Fatalf("an empty read pool should be refused: %v", err)
	}
}

func TestThePoolsAreSizedApart(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_DB_MAX_CONNS", "12")
	t.Setenv("STATOR_DB_REPLICA_MAX_CONNS", "40")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DB.PrimaryMaxConns != 12 || c.DB.ReplicaMaxConns != 40 {
		t.Fatalf("pools = %d write, %d read; want 12 from STATOR_DB_MAX_CONNS and 40 of its own", c.DB.PrimaryMaxConns, c.DB.ReplicaMaxConns)
	}
}

func TestSecureCookiesOutsideDevelopment(t *testing.T) {
	for _, env := range []string{EnvStaging, EnvProduction} {
		t.Run(env, func(t *testing.T) {
			clean(t)
			t.Setenv("STATOR_ENV", env)
			t.Setenv("STATOR_SECRET_KEY", testKey)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_SECURE_COOKIES") {
				t.Fatalf("insecure cookies in %s should be refused: %v", env, err)
			}
			t.Setenv("STATOR_SECURE_COOKIES", "true")
			c, err := Load()
			if err != nil {
				t.Fatalf("with secure cookies %s should load: %v", env, err)
			}
			if c.IsProduction() != (env == EnvProduction) {
				t.Errorf("IsProduction = %v in %s", c.IsProduction(), env)
			}
		})
	}
}

func TestDurationsAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_REQUEST_TIMEOUT", "45s")
	t.Setenv("STATOR_DB_MAX_REPLICA_LAG", "250ms")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.RequestTimeout != 45*time.Second || c.DB.MaxReplicaLag != 250*time.Millisecond {
		t.Fatalf("durations = %s, %s", c.RequestTimeout, c.DB.MaxReplicaLag)
	}
}

// The watch on verifications runs every ten minutes unless told otherwise,
// and an interval too short to be meant is refused with what to set.
func TestTheVerificationCheckIsAnInterval(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.VerificationCheck != DefaultVerificationCheck {
		t.Errorf("the verification check runs every %s, want %s", c.VerificationCheck, DefaultVerificationCheck)
	}
	t.Setenv("STATOR_VERIFICATION_CHECK_INTERVAL", "30s")
	if c, err = Load(); err != nil || c.VerificationCheck != 30*time.Second {
		t.Errorf("30s read as %s, %v", c.VerificationCheck, err)
	}
	for _, bad := range []string{"0s", "500ms", "-1m"} {
		t.Setenv("STATOR_VERIFICATION_CHECK_INTERVAL", bad)
		_, err := Load()
		var cfgErr *Error
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "STATOR_VERIFICATION_CHECK_INTERVAL") {
			t.Errorf("%s was not refused by name: %v", bad, err)
		}
	}
}

// The task reminder runs every ten minutes unless told otherwise, and an
// interval too short to be meant is refused with what to set.
func TestTheTaskDueCheckIsAnInterval(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TaskDueCheck != DefaultTaskDueCheck {
		t.Errorf("the task reminder runs every %s, want %s", c.TaskDueCheck, DefaultTaskDueCheck)
	}
	t.Setenv("STATOR_TASK_DUE_CHECK_INTERVAL", "1m")
	if c, err = Load(); err != nil || c.TaskDueCheck != time.Minute {
		t.Errorf("1m read as %s, %v", c.TaskDueCheck, err)
	}
	for _, bad := range []string{"0s", "10ms", "-5m"} {
		t.Setenv("STATOR_TASK_DUE_CHECK_INTERVAL", bad)
		_, err := Load()
		var cfgErr *Error
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "STATOR_TASK_DUE_CHECK_INTERVAL") {
			t.Errorf("%s was not refused by name: %v", bad, err)
		}
	}
}

// A bucket with no credentials would be refused on the first upload, long
// after the operator has stopped watching the start-up.
func TestAnEndpointWithoutCredentialsIsRefused(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.S3.Endpoint != "" || c.S3.Bucket != DefaultS3Bucket || c.S3.Region != DefaultS3Region {
		t.Errorf("storage defaults = %+v", c.S3)
	}
	t.Setenv("STATOR_S3_ENDPOINT", "seaweedfs:8333")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_S3_ACCESS_KEY") {
		t.Fatalf("an endpoint without keys = %v, want the keys asked for", err)
	}
	t.Setenv("STATOR_S3_ACCESS_KEY", "stator")
	t.Setenv("STATOR_S3_SECRET_KEY", "secret")
	if _, err := Load(); err != nil {
		t.Fatalf("a complete bucket was refused: %v", err)
	}
}

func TestReplicaSamplesAndFreshnessMustBePositive(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_DB_REPLICA_LAG_SAMPLES", "0")
	t.Setenv("STATOR_READ_YOUR_WRITES_TTL", "0s")
	_, err := Load()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || len(cfgErr.Problems) != 2 {
		t.Fatalf("want both settings refused, got %v", err)
	}
}

// testKey is 32 zero bytes, which is a well formed key and nothing more.
const testKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestSignInDefaults(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.SessionTTL != DefaultSessionTTL {
		t.Errorf("session ttl = %s", c.Auth.SessionTTL)
	}
	if c.Auth.OIDCRedirectURL != DefaultAppBaseURL+OIDCCallbackPath {
		t.Errorf("the callback defaults to %q, want it under the web client", c.Auth.OIDCRedirectURL)
	}
	if c.SecretKey != nil || len(c.Auth.OIDCBackchannel) != 0 {
		t.Errorf("development without a key or rewrites = %v, %v", c.SecretKey, c.Auth.OIDCBackchannel)
	}
}

func TestTheSecretKeyIsThirtyTwoBytesAndRequiredOutsideDevelopment(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_SECRET_KEY", testKey)
	c, err := Load()
	if err != nil || len(c.SecretKey) != SecretKeyBytes {
		t.Fatalf("a good key = %d bytes, %v", len(c.SecretKey), err)
	}

	for _, bad := range []string{"short", "AAAA", "not base64 at all!"} {
		t.Setenv("STATOR_SECRET_KEY", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_SECRET_KEY") {
			t.Errorf("key %q should be refused: %v", bad, err)
		}
	}

	t.Setenv("STATOR_SECRET_KEY", "")
	t.Setenv("STATOR_ENV", EnvProduction)
	t.Setenv("STATOR_SECURE_COOKIES", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_SECRET_KEY") {
		t.Fatalf("production without a key should be refused: %v", err)
	}
}

func TestBackchannelRewritesAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_OIDC_BACKCHANNEL", "http://localhost:8180/=http://keycloak:8080, https://a.test=https://b.test/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.OIDCBackchannel["http://localhost:8180"] != "http://keycloak:8080" || c.Auth.OIDCBackchannel["https://a.test"] != "https://b.test" || len(c.Auth.OIDCBackchannel) != 2 {
		t.Fatalf("rewrites = %v", c.Auth.OIDCBackchannel)
	}
	t.Setenv("STATOR_OIDC_BACKCHANNEL", "keycloak")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_OIDC_BACKCHANNEL") {
		t.Fatalf("a malformed rewrite should be refused: %v", err)
	}
}

func TestTheBootstrapAdminNeedsBothHalves(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("an email without a password should be refused: %v", err)
	}
	t.Setenv("STATOR_BOOTSTRAP_ADMIN_PASSWORD", " a password with spaces ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Bootstrap.AdminPassword != " a password with spaces " {
		t.Errorf("the password was altered: %q", c.Bootstrap.AdminPassword)
	}
}

func TestBootstrapMembersAreParsed(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_BOOTSTRAP_MEMBERS", "alice@stator.test=admin, bob@stator.test = member")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []BootstrapMember{{"alice@stator.test", "admin"}, {"bob@stator.test", "member"}}
	if len(c.Bootstrap.Members) != 2 || c.Bootstrap.Members[0] != want[0] || c.Bootstrap.Members[1] != want[1] {
		t.Fatalf("members = %+v", c.Bootstrap.Members)
	}
	for _, bad := range []string{"alice@stator.test", "alice=admin", "alice@stator.test=king"} {
		t.Setenv("STATOR_BOOTSTRAP_MEMBERS", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_BOOTSTRAP_MEMBERS") {
			t.Errorf("%q should be refused: %v", bad, err)
		}
	}
}

func TestTheBootstrapProviderNeedsAClientAndAKeyForItsSecret(t *testing.T) {
	clean(t)
	t.Setenv("STATOR_BOOTSTRAP_OIDC_ISSUER", "http://localhost:8180/realms/stator-dev")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_BOOTSTRAP_OIDC_CLIENT_ID") {
		t.Fatalf("an issuer without a client = %v", err)
	}
	t.Setenv("STATOR_BOOTSTRAP_OIDC_CLIENT_ID", "stator")
	t.Setenv("STATOR_BOOTSTRAP_OIDC_CLIENT_SECRET", "stator-dev-secret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_SECRET_KEY") {
		t.Fatalf("a secret with no key to seal it = %v", err)
	}
	t.Setenv("STATOR_SECRET_KEY", testKey)
	c, err := Load()
	if err != nil || c.Bootstrap.OIDCClientID != "stator" || c.Bootstrap.OIDCClientSecret != "stator-dev-secret" {
		t.Fatalf("bootstrap provider = %+v, %v", c.Bootstrap, err)
	}
}

func TestTheTestEndpointsNeedATokenAProviderAndNoProduction(t *testing.T) {
	clean(t)
	if c, err := Load(); err != nil || c.TestEndpoints.Enabled {
		t.Fatalf("the test endpoints are on by default: %+v, %v", c.TestEndpoints, err)
	}
	t.Setenv("STATOR_TEST_ENDPOINTS", "1")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "STATOR_TEST_ENDPOINTS_TOKEN") || !strings.Contains(err.Error(), "STATOR_BOOTSTRAP_OIDC_ISSUER") {
		t.Fatalf("on without a token or a provider = %v", err)
	}
	t.Setenv("STATOR_TEST_ENDPOINTS_TOKEN", "short")
	t.Setenv("STATOR_BOOTSTRAP_OIDC_ISSUER", "http://localhost:8180/realms/stator-dev")
	t.Setenv("STATOR_BOOTSTRAP_OIDC_CLIENT_ID", "stator")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_TEST_ENDPOINTS_TOKEN") {
		t.Fatalf("a short token = %v", err)
	}
	t.Setenv("STATOR_TEST_ENDPOINTS_TOKEN", "a token long enough to use")
	c, err := Load()
	if err != nil || !c.TestEndpoints.Enabled || c.TestEndpoints.Token != "a token long enough to use" {
		t.Fatalf("on with a token = %+v, %v", c.TestEndpoints, err)
	}
	t.Setenv("STATOR_ENV", EnvProduction)
	t.Setenv("STATOR_SECURE_COOKIES", "true")
	t.Setenv("STATOR_SECRET_KEY", testKey)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_TEST_ENDPOINTS is on in production") {
		t.Fatalf("on in production = %v", err)
	}
}

// Mail is off until a relay is named, and a sender that is no address is
// refused by name only once mail would go out.
func TestMailNeedsARelayAndASender(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.SMTPAddr != "" || cfg.Mail.From != DefaultMailFrom {
		t.Errorf("mail defaults to %+v", cfg.Mail)
	}
	t.Setenv("STATOR_MAIL_FROM", "nobody")
	if _, err := Load(); err != nil {
		t.Errorf("a sender is judged while mail is off: %v", err)
	}
	t.Setenv("STATOR_SMTP_ADDR", "mailpit:1025")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_MAIL_FROM") {
		t.Errorf("a sender that is no address is let through: %v", err)
	}
	t.Setenv("STATOR_MAIL_FROM", "Stator <wiki@example.com>")
	if cfg, err = Load(); err != nil || cfg.Mail.SMTPAddr != "mailpit:1025" {
		t.Errorf("mail through mailpit is %+v, %v", cfg.Mail, err)
	}
}

func TestTheAuditLogIsKeptAYearAndNeverLessThanADay(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetainAudit != DefaultRetainAudit {
		t.Errorf("the audit log is kept %s by default, want %s", cfg.RetainAudit, DefaultRetainAudit)
	}
	for value, want := range map[string]time.Duration{"0": 0, "24h": 24 * time.Hour, "2160h": 2160 * time.Hour} {
		t.Setenv("STATOR_RETAIN_AUDIT", value)
		if cfg, err := Load(); err != nil || cfg.RetainAudit != want {
			t.Errorf("STATOR_RETAIN_AUDIT=%s reads as %s, %v", value, cfg.RetainAudit, err)
		}
	}
	for _, value := range []string{"23h", "-1h", "a while"} {
		t.Setenv("STATOR_RETAIN_AUDIT", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_RETAIN_AUDIT") {
			t.Errorf("STATOR_RETAIN_AUDIT=%s is let through: %v", value, err)
		}
	}
}

func TestPageViewsAreKeptASeasonAndNeverLessThanTheirRecentPeriod(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetainPageViews != DefaultRetainPageViews {
		t.Errorf("page views are kept %s by default, want %s", cfg.RetainPageViews, DefaultRetainPageViews)
	}
	for value, want := range map[string]time.Duration{"0": 0, "720h": 720 * time.Hour, "8760h": 8760 * time.Hour} {
		t.Setenv("STATOR_RETAIN_PAGE_VIEWS", value)
		if cfg, err := Load(); err != nil || cfg.RetainPageViews != want {
			t.Errorf("STATOR_RETAIN_PAGE_VIEWS=%s reads as %s, %v", value, cfg.RetainPageViews, err)
		}
	}
	for _, value := range []string{"719h", "24h", "-1h", "a while"} {
		t.Setenv("STATOR_RETAIN_PAGE_VIEWS", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_RETAIN_PAGE_VIEWS") {
			t.Errorf("STATOR_RETAIN_PAGE_VIEWS=%s is let through: %v", value, err)
		}
	}
}

func TestArmatureIsReachedAsTheOperatorSays(t *testing.T) {
	clean(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Armature.OutboundAllow != "" || len(cfg.Armature.Backchannel) != 0 {
		t.Errorf("nothing inside the network is reached by default, got %+v", cfg.Armature)
	}

	clean(t)
	t.Setenv("STATOR_OUTBOUND_ALLOW", "armature-stub")
	t.Setenv("STATOR_ARMATURE_BACKCHANNEL", "http://localhost:20008/=http://armature-stub:8080")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Armature.OutboundAllow != "armature-stub" || cfg.Armature.Backchannel["http://localhost:20008"] != "http://armature-stub:8080" {
		t.Errorf("settings read as %+v", cfg.Armature)
	}

	clean(t)
	t.Setenv("STATOR_ARMATURE_BACKCHANNEL", "armature-stub")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STATOR_ARMATURE_BACKCHANNEL") {
		t.Errorf("an unreadable pair was not refused by name: %v", err)
	}
}

// Scheduled publishes are looked for every half minute unless told
// otherwise, and an interval too short to be meant is refused with what to set.
func TestTheScheduleCheckIsAnInterval(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ScheduleCheck != DefaultScheduleCheck {
		t.Errorf("scheduled publishes are looked for every %s, want %s", c.ScheduleCheck, DefaultScheduleCheck)
	}
	t.Setenv("STATOR_SCHEDULE_CHECK_INTERVAL", "5s")
	if c, err = Load(); err != nil || c.ScheduleCheck != 5*time.Second {
		t.Errorf("5s read as %s, %v", c.ScheduleCheck, err)
	}
	for _, bad := range []string{"0s", "100ms", "-30s"} {
		t.Setenv("STATOR_SCHEDULE_CHECK_INTERVAL", bad)
		_, err := Load()
		var cfgErr *Error
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "STATOR_SCHEDULE_CHECK_INTERVAL") {
			t.Errorf("%s was not refused by name: %v", bad, err)
		}
	}
}

// The example space is looked for every two seconds unless told otherwise,
// and an interval too short to be meant is refused with what to set.
func TestTheExampleCheckIsAnInterval(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ExampleCheck != DefaultExampleCheck {
		t.Errorf("example spaces are looked for every %s, want %s", c.ExampleCheck, DefaultExampleCheck)
	}
	t.Setenv("STATOR_EXAMPLE_CHECK_INTERVAL", "5s")
	if c, err = Load(); err != nil || c.ExampleCheck != 5*time.Second {
		t.Errorf("5s read as %s, %v", c.ExampleCheck, err)
	}
	for _, bad := range []string{"0s", "100ms", "-2s"} {
		t.Setenv("STATOR_EXAMPLE_CHECK_INTERVAL", bad)
		_, err := Load()
		var cfgErr *Error
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "STATOR_EXAMPLE_CHECK_INTERVAL") {
			t.Errorf("%s was not refused by name: %v", bad, err)
		}
	}
}
