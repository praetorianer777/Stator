//go:build integration

package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/seed"
	"github.com/praetorianer777/stator/backend/internal/version"
	"github.com/praetorianer777/stator/backend/migrations"
)

// Where the compose stack answers, from inside its network; set by make
// test-integration.
const (
	envWebURL      = "STATOR_TEST_WEB_URL"
	envKeycloakURL = "STATOR_TEST_KEYCLOAK_URL"
	// What the stack's images were stamped with, from scripts/build-stamp.sh.
	envBuildVersion = "STATOR_TEST_BUILD_VERSION"
	envBuildCommit  = "STATOR_TEST_BUILD_COMMIT"
)

// The suite's realm and its development-only credentials, as
// deploy/keycloak/realm.json defines them and the README documents them.
const (
	realm          = "dynamo"
	statorClient   = "stator"
	statorSecret   = "stator-dev-secret"
	armatureClient = "armature"
	armatureSecret = "armature-dev-secret"
	exciterClient  = "exciter"
	exciterSecret  = "exciter-dev-secret"
)

// replicaCatchUp bounds the wait for the api's first replica health pass, which
// runs on an interval and may not have happened when the stack reports healthy.
const replicaCatchUp = 30 * time.Second

var httpClient = &http.Client{Timeout: 10 * time.Second}

func stackURL(t *testing.T, env string) string {
	t.Helper()
	u := os.Getenv(env)
	if u == "" {
		t.Skipf("%s is not set; run the suite with make test-integration against a running stack", env)
	}
	return strings.TrimSuffix(u, "/")
}

func get(t *testing.T, u string) (*http.Response, []byte) {
	t.Helper()
	resp, err := httpClient.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", u, err)
	}
	return resp, body
}

// The browser only ever talks to the web container; the api, its database and
// the replica behind it all have to answer through nginx.
func TestTheWebContainerServesTheAppAndProxiesTheAPI(t *testing.T) {
	web := stackURL(t, envWebURL)

	t.Run("readiness reaches the api and a healthy replica", func(t *testing.T) {
		var ready struct {
			Status  string `json:"status"`
			Routing struct {
				Replicas []struct {
					Name    string `json:"name"`
					Healthy bool   `json:"healthy"`
				} `json:"replicas"`
			} `json:"routing"`
		}
		deadline := time.Now().Add(replicaCatchUp)
		for {
			resp, body := get(t, web+"/readyz")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /readyz = %s:\n%s", resp.Status, body)
			}
			if err := json.Unmarshal(body, &ready); err != nil {
				t.Fatalf("GET /readyz is not JSON: %v\n%s", err, body)
			}
			if ready.Status == "ok" && len(ready.Routing.Replicas) == 1 && ready.Routing.Replicas[0].Healthy {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the api is not reading from a healthy replica after %s:\n%s", replicaCatchUp, body)
			}
			time.Sleep(time.Second)
		}
	})

	t.Run("the api is served under /api", func(t *testing.T) {
		resp, body := get(t, web+"/api/v1/openapi.json")
		var doc struct {
			OpenAPI string `json:"openapi"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &doc) != nil || doc.OpenAPI == "" {
			t.Fatalf("GET /api/v1/openapi.json = %s, want the OpenAPI document:\n%.200s", resp.Status, body)
		}
		requireSecurityHeaders(t, "an api answer", resp)
	})

	t.Run("the api reports the build this checkout stamped, to anybody", func(t *testing.T) {
		resp, body := get(t, web+httpapi.APIPrefix+"/meta")
		var meta struct {
			Product      string       `json:"product"`
			Build        version.Info `json:"build"`
			APIVersion   string       `json:"apiVersion"`
			Capabilities []string     `json:"capabilities"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &meta) != nil {
			t.Fatalf("GET /api/v1/meta = %s, want the build:\n%s", resp.Status, body)
		}
		if meta.Product != httpapi.ProductName || meta.APIVersion != httpapi.Spec().Info.Version || meta.Capabilities == nil || meta.Build.BuiltAt == "" {
			t.Errorf("GET /api/v1/meta = %s", body)
		}
		// make test-integration passes what the stack's images were built with.
		for _, stamp := range []struct{ env, got string }{
			{envBuildVersion, meta.Build.Version},
			{envBuildCommit, meta.Build.Commit},
		} {
			if want := os.Getenv(stamp.env); want != "" && stamp.got != want {
				t.Errorf("the api says %q where the build was stamped %q (%s); rebuild the stack with make stack-up", stamp.got, want, stamp.env)
			}
		}
	})

	t.Run("an app route falls back to the single page app", func(t *testing.T) {
		resp, body := get(t, web+"/spaces/somewhere/deep")
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="root"`) {
			t.Fatalf("GET an app route = %s, want index.html:\n%.200s", resp.Status, body)
		}
		requireSecurityHeaders(t, "the page", resp)

		_, rest, found := strings.Cut(string(body), `src="/assets/`)
		if !found {
			t.Fatalf("index.html loads no script from /assets/:\n%s", body)
		}
		asset, _, _ := strings.Cut(rest, `"`)
		resp, _ = get(t, web+"/assets/"+asset)
		if cc := resp.Header.Values("Cache-Control"); resp.StatusCode != http.StatusOK || len(cc) != 1 || !strings.Contains(cc[0], "immutable") {
			t.Fatalf("GET /assets/%s = %s with Cache-Control %q, want 200 and one immutable", asset, resp.Status, cc)
		}
		requireSecurityHeaders(t, "a build asset", resp)
	})
}

func requireSecurityHeaders(t *testing.T, what string, resp *http.Response) {
	t.Helper()
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if n := len(resp.Header.Values(h)); n != 1 {
			t.Errorf("%s is served with %d %s headers, want exactly one", what, n, h)
		}
	}
}

// The migrate container ran every embedded migration before the api started.
func TestEveryMigrationIsApplied(t *testing.T) {
	h := newStackHarness(t)

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var latest int64
	for _, f := range files {
		prefix, _, _ := strings.Cut(f, "_")
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("migration %s does not start with a version number", f)
		}
		latest = max(latest, v)
	}
	if latest == 0 {
		t.Fatal("no migrations are embedded")
	}

	var applied int64
	if err := h.super.QueryRow(context.Background(),
		`SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&applied); err != nil {
		t.Fatalf("read the schema version: %v", err)
	}
	if applied != latest {
		t.Fatalf("the stack's schema is at version %d, want %d, the newest embedded migration", applied, latest)
	}
}

// The seed container ran once on the way up; running it again finds what it
// made instead of making it twice.
func TestTheSeedIsIdempotent(t *testing.T) {
	h := newStackHarness(t)
	ctx := context.Background()

	demoOrgs := func() int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM site WHERE slug = $1`, seed.DemoSiteSlug).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := demoOrgs(); n != 1 {
		t.Fatalf("the stack came up with %d demo organizations, want the seed's one", n)
	}
	for run := range 2 {
		res, err := seed.Run(ctx, h.cluster)
		if err != nil {
			t.Fatalf("seed run %d: %v", run+1, err)
		}
		if res.SiteCreated {
			t.Errorf("seed run %d made the demo organization again", run+1)
		}
	}
	if n := demoOrgs(); n != 1 {
		t.Fatalf("after seeding again there are %d demo organizations, want 1", n)
	}
}

// A production install runs the bootstrap on every install and upgrade: it
// makes the organization and its administrator once, leaves them alone after,
// and a changed password is the one thing it applies again.
func TestTheBootstrapMakesTheOrganizationAndItsAdministratorOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	email := "boot-" + strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 36)) + "@bootstrap.test"
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM app_user WHERE email = $1`, email) })

	for run := range 2 {
		res, err := seed.Ensure(ctx, h.cluster, "Bootstrapped")
		if err != nil {
			t.Fatalf("run %d: %v", run+1, err)
		}
		if res.SiteCreated != (run == 0) {
			t.Errorf("run %d: organization created = %v", run+1, res.SiteCreated)
		}
		made, err := accounts.EnsureAdmin(ctx, email, "Administrator", "first password 1")
		if err != nil {
			t.Fatalf("run %d: %v", run+1, err)
		}
		if made != (run == 0) {
			t.Errorf("run %d: administrator created = %v", run+1, made)
		}
	}

	var org, owners int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM site WHERE name = 'Bootstrapped'`).Scan(&org); err != nil || org != 1 {
		t.Fatalf("organizations: %d (%v), want the one the bootstrap made", org, err)
	}
	if err := h.super.QueryRow(ctx, `
		SELECT count(*) FROM member m JOIN app_user u ON u.id = m.user_id
		WHERE u.email = $1 AND m.role = 'owner'`, email).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners named %s: %d (%v), want 1", email, owners, err)
	}
	if _, err := accounts.EnsureAdmin(ctx, email, "Administrator", "second password 2"); err != nil {
		t.Fatalf("a changed password: %v", err)
	}

	res, err := seed.Ensure(ctx, h.cluster, "Another")
	if err != nil || res.SiteCreated {
		t.Fatalf("bootstrapping under another name = %+v, %v; want the organization there left alone", res, err)
	}
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM site`).Scan(&org); err != nil || org != 1 {
		t.Fatalf("organizations after bootstrapping again: %d (%v), want 1", org, err)
	}
}

// Where the role that ran the migrations is an ordinary owner, as under
// CloudNativePG, row level security applies to the trigger that lets everyone
// use a new organization; the stack's owner is a superuser, which it does not.
// So the trigger is run as an owner row level security binds, and an
// organization made by the admin role must still get its grant.
func TestANewOrganizationGetsItsGrantWhereTheOwnerIsBoundByRowLevelSecurity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, q := range []string{
		`CREATE ROLE rls_bound_owner NOLOGIN NOSUPERUSER NOBYPASSRLS`,
		`GRANT USAGE ON SCHEMA public TO rls_bound_owner`,
		`GRANT INSERT, SELECT ON global_grant TO rls_bound_owner`,
		`ALTER FUNCTION site_default_grants() OWNER TO rls_bound_owner`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	slug := "rls-" + strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 36))
	if _, err := tx.Exec(ctx, `INSERT INTO site (slug, name) VALUES ($1, 'Bound')`, slug); err != nil {
		t.Fatalf("making an organization where the owner is bound by row level security: %v", err)
	}
	var grants int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM global_grant g
		WHERE g.permission = 'use' AND g.subject_type = 'everyone'`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("grants of the new organization: %d (%v), want 1", grants, err)
	}
}

// The realm the stack imports is the one sign-in is built against: every
// tool's client signs the test users in, and their tokens carry the groups claim.
func TestTheRealmSignsInTheTestUsersWithTheirGroups(t *testing.T) {
	kc := stackURL(t, envKeycloakURL)

	for _, tc := range []struct {
		client, secret, user, password string
		groups                         []string
	}{
		{statorClient, statorSecret, "alice", alicePassword, aliceGroups},
		{statorClient, statorSecret, "bob", bobPassword, []string{"developers", "engineering"}},
		{statorClient, statorSecret, "carol", carolPassword, []string{"portal-owners", "quality", "scrum-masters"}},
		{statorClient, statorSecret, "dave", davePassword, []string{"marketing", "readers"}},
		{armatureClient, armatureSecret, "alice", alicePassword, aliceGroups},
		{exciterClient, exciterSecret, "alice", alicePassword, aliceGroups},
	} {
		t.Run(tc.client+" signs in "+tc.user, func(t *testing.T) {
			resp, err := httpClient.PostForm(kc+"/realms/"+realm+"/protocol/openid-connect/token", url.Values{
				"grant_type":    {"password"},
				"client_id":     {tc.client},
				"client_secret": {tc.secret},
				"username":      {tc.user},
				"password":      {tc.password},
				"scope":         {"openid"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("token request = %s:\n%s", resp.Status, body)
			}
			var tokens struct {
				IDToken string `json:"id_token"`
			}
			if err := json.Unmarshal(body, &tokens); err != nil || tokens.IDToken == "" {
				t.Fatalf("no ID token in the answer (%v):\n%s", err, body)
			}
			claims := jwtClaims(t, tokens.IDToken)
			if claims.Username != tc.user {
				t.Errorf("the token names %q, want %q", claims.Username, tc.user)
			}
			got := slices.Sorted(slices.Values(claims.Groups))
			if !slices.Equal(got, tc.groups) {
				t.Errorf("groups claim = %v, want %v", got, tc.groups)
			}
		})
	}
}

type idClaims struct {
	Username string   `json:"preferred_username"`
	Groups   []string `json:"groups"`
}

// jwtClaims reads a token's payload without checking its signature; the test
// asks what the realm puts in a token, not whether to trust one.
func jwtClaims(t *testing.T, token string) idClaims {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode the token payload: %v", err)
	}
	var c idClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		t.Fatalf("parse the token payload: %v", err)
	}
	return c
}
