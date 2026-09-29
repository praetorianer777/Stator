//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// envKeycloakAdminPassword overrides the stack's Keycloak administrator
// password, which the compose file defaults to defaultKeycloakAdminPassword.
const (
	envKeycloakAdminPassword     = "STATOR_TEST_KEYCLOAK_ADMIN_PASSWORD"
	defaultKeycloakAdminPassword = "admin"
)

// The development realm's people, as deploy/keycloak/realm.json has them.
const (
	alicePassword = "alice password"
	bobPassword   = "bob password"
	// appHost is where the API sends the browser after a sign-in; the
	// browser stops there, since no web client is running in the suite.
	appHost = "app.test"
)

const browserTimeout = 30 * time.Second

// cheapPasswords keeps argon2 a real argon2id hash but fast enough for a suite.
func cheapPasswords() auth.PasswordParams {
	return auth.PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

// api is the HTTP surface wired to the real database and the real services.
type api struct {
	URL      string
	accounts *auth.Service
	sso      *oidc.Service
	box      *secret.Box
}

func (h *harness) startAPI(t *testing.T, sessionTTL time.Duration) *api {
	t.Helper()
	key := make([]byte, secret.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	// Unstarted, so the callback address can name the port before any
	// request arrives.
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	a := &api{
		URL:      base,
		accounts: auth.NewService(h.cluster, cheapPasswords(), sessionTTL),
		sso:      oidc.NewService(h.cluster, box, base+httpapi.APIPrefix+"/auth/oidc/callback"),
		box:      box,
	}
	server.Config.Handler = (&httpapi.Server{
		DB:              h.cluster,
		Auth:            a.accounts,
		Accounts:        a.accounts,
		OIDC:            a.sso,
		OIDCCallbackURL: base + httpapi.APIPrefix + "/auth/oidc/callback",
		Log:             discard(),
		CookieName:      h.cfg.Auth.SessionCookie,
		AppBaseURL:      "http://" + appHost,
	}).Routes(nil)
	server.Start()
	t.Cleanup(server.Close)
	return a
}

// makeOrg is an organization with nobody in it, made behind the policies' back.
func (h *harness) makeOrg(t *testing.T, slug string) tenant.Org {
	t.Helper()
	full := fmt.Sprintf("%s-%s", slug, uuid.NewString()[:8])
	org := tenant.Org{Slug: full}
	if err := h.super.QueryRow(context.Background(), `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, full, slug).Scan(&org.ID); err != nil {
		t.Fatalf("create org %s: %v", full, err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM org WHERE id = $1`, org.ID) })
	return org
}

// forgetPerson removes an account a test makes, by address, before and after,
// so a run against a kept stack starts from nothing.
func (h *harness) forgetPerson(t *testing.T, email string) {
	t.Helper()
	clean := func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE email = $1`, email) }
	clean()
	t.Cleanup(clean)
}

// browser is a cookie-keeping client that follows redirects until the API
// hands it to the web client, and remembers every callback it was sent to.
type browser struct {
	*http.Client
	callbacks []string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &browser{}
	b.Client = &http.Client{Jar: jar, Timeout: browserTimeout, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if strings.HasSuffix(req.URL.Path, "/auth/oidc/callback") {
			b.callbacks = append(b.callbacks, req.URL.String())
		}
		if req.URL.Host == appHost {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return b
}

func (b *browser) get(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	resp, err := b.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (b *browser) send(t *testing.T, method, target string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := b.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

var loginFormAction = regexp.MustCompile(`id="kc-form-login"[^>]*action="([^"]+)"`)

// signIn goes the whole way a person does, through Keycloak's login form, and
// returns where the app was told to land.
func (b *browser) signIn(t *testing.T, a *api, orgSlug, username, password, next string) string {
	t.Helper()
	start := a.URL + httpapi.APIPrefix + "/auth/oidc/" + orgSlug + "/start"
	if next != "" {
		start += "?next=" + url.QueryEscape(next)
	}
	resp, page := b.get(t, start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the start did not reach a login form: %s at %s\n%.500s", resp.Status, resp.Request.URL, page)
	}
	if q := resp.Request.URL.Query(); q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Errorf("the provider was asked without state, nonce or PKCE: %s", resp.Request.URL)
	}
	match := loginFormAction.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("no login form on %s:\n%.1000s", resp.Request.URL, page)
	}
	form := url.Values{"username": {username}, "password": {password}, "credentialId": {""}}
	resp, err := b.PostForm(html.UnescapeString(match[1]), form)
	if err != nil {
		t.Fatalf("submit the login form: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the login form answered %s at %s, want a redirect into the app:\n%.1000s", resp.Status, resp.Request.URL, body)
	}
	return resp.Header.Get("Location")
}

// me asks the API who the browser is signed in as.
func (b *browser) me(t *testing.T, a *api) (int, map[string]any) {
	t.Helper()
	resp, body := b.get(t, a.URL+httpapi.APIPrefix+"/auth/me")
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return resp.StatusCode, out
}

// sessionCookie is the session cookie the browser holds for the API.
func (b *browser) sessionCookie(t *testing.T, a *api, name string) string {
	t.Helper()
	u, _ := url.Parse(a.URL)
	for _, c := range b.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// keycloak administers the test realm through Keycloak's admin API.
type keycloak struct {
	base, realm, password string
	client                *http.Client
}

func newKeycloak(t *testing.T) *keycloak {
	t.Helper()
	password := os.Getenv(envKeycloakAdminPassword)
	if password == "" {
		password = defaultKeycloakAdminPassword
	}
	return &keycloak{base: stackURL(t, envKeycloakURL), realm: realm, password: password, client: &http.Client{Timeout: browserTimeout}}
}

func (k *keycloak) issuer() string { return k.base + "/realms/" + k.realm }

func (k *keycloak) token(t *testing.T) string {
	t.Helper()
	resp, err := k.client.PostForm(k.base+"/realms/master/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {k.password},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("no admin token from Keycloak (%s): %v", resp.Status, err)
	}
	return out.AccessToken
}

func (k *keycloak) call(t *testing.T, method, path string, into any) {
	t.Helper()
	k.send(t, method, path, nil, into)
}

func (k *keycloak) send(t *testing.T, method, path string, body, into any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, k.base+"/admin/realms/"+k.realm+path, payload)
	req.Header.Set("Authorization", "Bearer "+k.token(t))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s = %s: %s", method, path, resp.Status, body)
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
}

func (k *keycloak) idOf(t *testing.T, path string) string {
	t.Helper()
	var found []struct {
		ID string `json:"id"`
	}
	k.call(t, http.MethodGet, path, &found)
	if len(found) != 1 {
		t.Fatalf("%s found %d, want exactly one", path, len(found))
	}
	return found[0].ID
}

// setMembership adds or removes a realm user from a realm group.
func (k *keycloak) setMembership(t *testing.T, username, group string, member bool) {
	t.Helper()
	user := k.idOf(t, "/users?exact=true&username="+url.QueryEscape(username))
	grp := k.idOf(t, "/groups?exact=true&search="+url.QueryEscape(group))
	method := http.MethodDelete
	if member {
		method = http.MethodPut
	}
	k.call(t, method, "/users/"+user+"/groups/"+grp, nil)
}

// newPerson adds somebody to the realm whom Stator has never seen, removed
// again, here and in Keycloak, when the test ends. It returns their address.
func (k *keycloak) newPerson(t *testing.T, h *harness, username, password string) string {
	t.Helper()
	email := username + "@stator.test"
	k.send(t, http.MethodPost, "/users", map[string]any{
		"username": username, "email": email, "firstName": "New", "lastName": "Person",
		"enabled": true, "emailVerified": true,
		"credentials": []map[string]any{{"type": "password", "value": password, "temporary": false}},
	}, nil)
	h.forgetPerson(t, email)
	t.Cleanup(func() {
		k.call(t, http.MethodDelete, "/users/"+k.idOf(t, "/users?exact=true&username="+url.QueryEscape(username)), nil)
	})
	return email
}

// letIn makes the address a member of org ahead of its first sign-in, the
// way the seed does, making the account if need be.
func (h *harness) letIn(t *testing.T, org tenant.Org, email, role string) {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id`, email, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, org.ID, id, role); err != nil {
		t.Fatal(err)
	}
}

// reachable reports whether the test container can open a connection to
// Keycloak, so a missing provider fails with a sentence rather than a timeout.
func (k *keycloak) reachable(t *testing.T) {
	t.Helper()
	u, _ := url.Parse(k.base)
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("Keycloak at %s is not reachable; run make idp-up: %v", k.base, err)
	}
	conn.Close()
}
