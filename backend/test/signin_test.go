//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oidc"
)

const testSessionTTL = time.Hour

// configureProvider points an organization at the test realm as the operator
// does, since the test's organization may have no administrator to do it.
func configureProvider(t *testing.T, a *api, issuer string, createGroups bool) {
	t.Helper()
	err := a.sso.Configure(context.Background(), oidc.Provider{
		Issuer: issuer, ClientID: statorClient, ClientSecret: statorSecret,
		CreateGroups: createGroups, Enabled: true,
	})
	if err != nil {
		t.Fatalf("configure the provider: %v", err)
	}
}

// providerGroups lists the provider groups a person is in within one organization.
func (h *harness) providerGroups(t *testing.T, email string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT g.external_ref FROM group_member m
		JOIN groups g ON g.id = m.group_id
		JOIN app_user u ON u.id = m.user_id
		WHERE u.email = $1 AND g.source = 'oidc'
		ORDER BY 1`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatal(err)
		}
		out = append(out, ref)
	}
	return out
}

// Alice signs in through Keycloak's own login form, with nothing stood in for,
// and her groups follow her until she signs out.
func TestSignInThroughKeycloak(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	site := h.emptySite(t, "wonderland")
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, "alice@suite.test", "member")
	ctx := context.Background()

	t.Run("the client secret is sealed at rest", func(t *testing.T) {
		var stored []byte
		if err := h.super.QueryRow(ctx, `SELECT client_secret FROM oidc_provider`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if len(stored) == 0 || bytes.Contains(stored, []byte(statorSecret)) {
			t.Fatalf("the stored secret is %q", stored)
		}
	})

	b := newBrowser(t)
	landing := b.signIn(t, a, "alice", alicePassword, "/spaces/handbook")
	if landing != "http://"+appHost+"/spaces/handbook" {
		t.Fatalf("landed on %q, want the page asked for", landing)
	}

	var userID, subject string
	t.Run("the account is made from the token and remembered by subject", func(t *testing.T) {
		status, me := b.me(t, a)
		if status != http.StatusOK {
			t.Fatalf("/auth/me = %d %v", status, me)
		}
		user, _ := me["user"].(map[string]any)
		current, _ := me["organization"].(map[string]any)
		if user["email"] != "alice@suite.test" || user["name"] != "Alice Admin" {
			t.Errorf("user = %v", user)
		}
		if current["name"] == nil || current["role"] != string(auth.RoleMember) {
			t.Errorf("organization = %v, want %s as a member", current, site.Slug)
		}
		userID, _ = user["id"].(string)
		if err := h.super.QueryRow(ctx, `SELECT subject FROM user_identity WHERE issuer = $1 AND user_id = $2`, kc.issuer(), userID).Scan(&subject); err != nil || subject == "" {
			t.Fatalf("no identity for alice under %s: %v", kc.issuer(), err)
		}
	})

	t.Run("her groups are created and joined", func(t *testing.T) {
		if got := h.providerGroups(t, "alice@suite.test"); !slices.Equal(got, aliceGroups) {
			t.Fatalf("groups = %q", got)
		}
	})

	t.Run("only the digest of the session token is stored", func(t *testing.T) {
		secret := b.sessionCookie(t, a, h.cfg.Auth.SessionCookie)
		if secret == "" {
			t.Fatal("the browser holds no session cookie")
		}
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE token_hash = $1 AND proof = 'oidc'`, auth.HashToken(secret)).Scan(&n); err != nil || n != 1 {
			t.Fatalf("sessions under the digest = %d (%v)", n, err)
		}
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE position(convert_to($1, 'UTF8') in token_hash) > 0`, secret).Scan(&n); err != nil || n != 0 {
			t.Fatalf("the raw token is stored: %d (%v)", n, err)
		}
	})

	t.Run("a replayed callback opens nothing", func(t *testing.T) {
		if len(b.callbacks) == 0 {
			t.Fatal("the browser never passed the callback")
		}
		replay := newBrowser(t)
		resp, _ := replay.get(t, b.callbacks[len(b.callbacks)-1])
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/login?sso=expired" {
			t.Fatalf("a replay = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		if replay.sessionCookie(t, a, h.cfg.Auth.SessionCookie) != "" {
			t.Fatal("a replayed callback set a session cookie")
		}
	})

	t.Run("a group removed at the provider is left on the next sign-in", func(t *testing.T) {
		kc.setMembership(t, "alice", "engineering", false)
		t.Cleanup(func() { kc.setMembership(t, "alice", "engineering", true) })

		again := newBrowser(t)
		again.signIn(t, a, "alice", alicePassword, "")
		if got := h.providerGroups(t, "alice@suite.test"); !slices.Equal(got, []string{"armature-administrators", "exciter-administrators", "stator-administrators"}) {
			t.Fatalf("groups after leaving engineering = %q", got)
		}
		var accounts int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = 'alice@suite.test'`).Scan(&accounts); err != nil || accounts != 1 {
			t.Fatalf("alice has %d accounts after signing in twice (%v)", accounts, err)
		}

		kc.setMembership(t, "alice", "engineering", true)
		again = newBrowser(t)
		again.signIn(t, a, "alice", alicePassword, "")
		if got := h.providerGroups(t, "alice@suite.test"); !slices.Equal(got, aliceGroups) {
			t.Fatalf("groups after rejoining engineering = %q", got)
		}
	})

	t.Run("signing out ends the session", func(t *testing.T) {
		secret := b.sessionCookie(t, a, h.cfg.Auth.SessionCookie)
		resp, _ := b.send(t, http.MethodPost, a.URL+httpapi.APIPrefix+"/auth/logout")
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout = %s", resp.Status)
		}
		if status, _ := b.me(t, a); status != http.StatusUnauthorized {
			t.Fatalf("/auth/me after logout = %d", status)
		}
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE token_hash = $1`, auth.HashToken(secret)).Scan(&n); err != nil || n != 0 {
			t.Fatalf("the session outlived the logout: %d (%v)", n, err)
		}
	})
}

// Bob is in another group, and a provider that does not create groups only
// maps the ones an administrator already made.
func TestOnlyExistingGroupsAreJoinedWhenCreationIsOff(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "builders")
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, "bob@suite.test", "member")

	newBrowser(t).signIn(t, a, "bob", bobPassword, "")
	if got := h.providerGroups(t, "bob@suite.test"); len(got) != 0 {
		t.Fatalf("groups = %q, want none while no group is mapped", got)
	}

	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO groups (name, source, external_ref) VALUES ('Developers', 'oidc', 'developers')`); err != nil {
		t.Fatal(err)
	}
	newBrowser(t).signIn(t, a, "bob", bobPassword, "")
	if got := h.providerGroups(t, "bob@suite.test"); !slices.Equal(got, []string{"developers"}) {
		t.Fatalf("groups = %q, want the mapped one", got)
	}
}

func TestSignInRefusals(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	start := func() *http.Response {
		b := newBrowser(t)
		b.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/start")
		return resp
	}

	notConfigured := "http://" + appHost + "/login?sso=not_configured"
	// One organization, its provider set again by each case in turn.
	h.emptySite(t, "refusals")
	t.Run("an organization without a provider says so", func(t *testing.T) {
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != notConfigured {
			t.Errorf("start = %s to %q, want the sign-in page saying so", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("a provider that is turned off is not used", func(t *testing.T) {
		configureProvider(t, a, kc.issuer(), false)
		if err := a.sso.Configure(context.Background(), oidc.Provider{Issuer: kc.issuer(), ClientID: statorClient, Enabled: false}); err != nil {
			t.Fatal(err)
		}
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != notConfigured {
			t.Errorf("start with the provider off = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("a provider that cannot be reached is named as the reason", func(t *testing.T) {
		configureProvider(t, a, "http://nowhere.invalid/realms/none", false)
		if resp := start(); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://"+appHost+"/login?sso=unreachable" {
			t.Errorf("start with an unreachable provider = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
	})

	t.Run("an expired sign-in is refused at the callback", func(t *testing.T) {
		configureProvider(t, a, kc.issuer(), false)
		if resp := start(); resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), kc.issuer()) {
			t.Fatalf("start = %s to %q", resp.Status, resp.Header.Get("Location"))
		}
		var state string
		if err := h.super.QueryRow(context.Background(), `
			UPDATE oidc_login SET expires_at = now() - interval '1 second' RETURNING state`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		b := newBrowser(t)
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/callback?state="+state+"&code=whatever")
		if resp.Header.Get("Location") != "http://"+appHost+"/login?sso=expired" {
			t.Fatalf("an expired callback went to %q", resp.Header.Get("Location"))
		}
	})

	t.Run("a refusal by the provider is shown, not swallowed", func(t *testing.T) {
		b := newBrowser(t)
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/oidc/callback?error=access_denied&state=x")
		if resp.Header.Get("Location") != "http://"+appHost+"/login?sso=denied" {
			t.Fatalf("a refused sign-in went to %q", resp.Header.Get("Location"))
		}
	})
}

// The API reaches a provider at an address of the network's while asking for
// it by its public name, and Keycloak then describes itself under that name.
func TestTheBackchannelReachesKeycloakUnderItsPublicName(t *testing.T) {
	kc := newKeycloak(t)
	kc.reachable(t)
	const public = "http://idp.public.test"
	client := oidc.Backchannel(map[string]string{public: kc.base})
	provider, err := coreoidc.NewProvider(coreoidc.ClientContext(context.Background(), client), public+"/realms/"+kc.realm)
	if err != nil {
		t.Fatalf("discovery through the rewrite: %v", err)
	}
	if got := provider.Endpoint().TokenURL; !strings.HasPrefix(got, public+"/") {
		t.Fatalf("the token endpoint is %q, want it under the public name", got)
	}
}
