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
		"STATOR_DB_MIN_CONNS", "STATOR_DB_CONN_MAX_LIFETIME", "STATOR_DB_HEALTH_INTERVAL",
		"STATOR_DB_MAX_REPLICA_LAG", "STATOR_SESSION_COOKIE",
		"STATOR_SECURE_COOKIES", "STATOR_OTEL_ENDPOINT", "STATOR_OTEL_SAMPLE_RATIO",
		"STATOR_S3_ENDPOINT", "STATOR_S3_BUCKET", "STATOR_S3_ACCESS_KEY", "STATOR_S3_SECRET_KEY",
		"STATOR_S3_REGION", "STATOR_S3_USE_SSL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("STATOR_DB_PRIMARY_URL", "postgres://app@db/stator")
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
	if c.DB.MaxConns != DefaultMaxConns || c.DB.MinConns != DefaultMinConns || c.DB.HealthInterval != DefaultHealthInterval {
		t.Errorf("pool defaults = %+v", c.DB)
	}
	if c.DB.AdminURL != c.DB.PrimaryURL {
		t.Errorf("the admin URL should fall back to the primary, got %q", c.DB.AdminURL)
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
}

func TestSecureCookiesOutsideDevelopment(t *testing.T) {
	for _, env := range []string{EnvStaging, EnvProduction} {
		t.Run(env, func(t *testing.T) {
			clean(t)
			t.Setenv("STATOR_ENV", env)
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
