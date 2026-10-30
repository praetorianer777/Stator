//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// Stator handing a partner tool a person's token by token exchange (#461),
// against the stack's Keycloak with nothing stood in for: Exciter's client
// in the realm dynamo signs alice in, and the access token it holds for her,
// naming Stator in its audience, is traded for a Stator grant.

// partnerToken is the access token the realm gives the client for a person,
// as the partner's own sign-in would hold it.
func partnerToken(t *testing.T, kc *keycloak, client, secret, username, password string) string {
	t.Helper()
	resp, err := kc.client.PostForm(kc.issuer()+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {client}, "client_secret": {secret},
		"username": {username}, "password": {password}, "scope": {"openid profile email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("no access token for %s from %s (%s): %v", username, client, resp.Status, err)
	}
	return out.AccessToken
}

type oauthAnswer struct {
	Status int
	Header http.Header
	Body   map[string]any
}

func (o oauthAnswer) str(key string) string { s, _ := o.Body[key].(string); return s }

// oauthPost posts a form to an OAuth endpoint as the app, by HTTP Basic.
func oauthPost(t *testing.T, a *api, path, id, secret string, form url.Values) oauthAnswer {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.URL+httpapi.APIPrefix+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if id != "" {
		req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := oauthAnswer{Status: resp.StatusCode, Header: resp.Header, Body: map[string]any{}}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

// httpPatch sends a JSON PATCH from the browser.
func httpPatch(t *testing.T, b *browser, target string, body any) (*http.Response, map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPatch, target, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// partnerAccess is an access token of a fresh grant for subject's person.
func partnerAccess(t *testing.T, a *api, id, secret, subject string) string {
	t.Helper()
	got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(subject))
	if got.Status != http.StatusOK {
		t.Fatalf("the exchange answered %d %v", got.Status, got.Body)
	}
	return got.str("access_token")
}

func exchangeForm(subject string) url.Values {
	return url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {oauth.TokenTypeAccess}}
}

// bearer calls the API with a token and nothing else.
func bearer(t *testing.T, a *api, method, path, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, a.URL+httpapi.APIPrefix+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// registerApp registers a partner app through the administrators' route.
func registerApp(t *testing.T, admin *browser, a *api, body map[string]any) (string, string) {
	t.Helper()
	resp, out := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/oauth/clients", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %v = %d %v", body, resp.StatusCode, out)
	}
	client, _ := out["client"].(map[string]any)
	id, _ := client["id"].(string)
	secret, _ := client["secret"].(string)
	if id == "" || !strings.HasPrefix(secret, "stator_ocs_") {
		t.Fatalf("the app registered is %v", client)
	}
	return id, secret
}

func TestAPartnerTradesAPersonsSignInForAStatorToken(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	kc.audience(t, exciterClient, statorClient)
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) {
		s.OAuth = oauth.NewService(h.cluster, s.OIDC, "http://"+appHost, discard())
	})
	h.emptySite(t, "exchange-server")
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, "alice@suite.test", "member")
	admin, _ := adminOf(t, h, a)

	t.Run("the metadata names every address under the issuer", func(t *testing.T) {
		resp, body := newBrowser(t).get(t, a.URL+oauth.MetadataPath)
		var meta oauth.Metadata
		_ = json.Unmarshal([]byte(body), &meta)
		if resp.StatusCode != http.StatusOK || meta.Issuer != "http://"+appHost || meta.TokenEndpoint != "http://"+appHost+"/api/v1/oauth/token" {
			t.Fatalf("the metadata answered %d %s", resp.StatusCode, body)
		}
		if status, meta := bearer(t, a, http.MethodGet, "/meta", ""); status != http.StatusOK || !strings.Contains(fmt.Sprint(meta["capabilities"]), oauth.CapabilityTokenExchange) {
			t.Errorf("/meta answered %d %v", status, meta)
		}
	})

	id, secret := registerApp(t, admin, a, map[string]any{"name": "Exciter", "scopes": []string{"read", "write"}, "keycloakClientId": exciterClient})
	if resp, out := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/oauth/clients", map[string]any{"name": "Again", "scopes": []string{"read"}, "keycloakClientId": exciterClient}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("a second app for one Keycloak client answered %d %v", resp.StatusCode, out)
	}
	subject := partnerToken(t, kc, exciterClient, exciterSecret, "alice", alicePassword)

	var access, refresh string
	t.Run("alice's token from Exciter's sign-in is traded for a grant of hers", func(t *testing.T) {
		got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(subject))
		if got.Status != http.StatusOK || !strings.HasPrefix(got.str("access_token"), "stator_oat_") || !strings.HasPrefix(got.str("refresh_token"), "stator_ort_") ||
			got.str("scope") != "read write" || got.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("the exchange answered %d %v", got.Status, got.Body)
		}
		access, refresh = got.str("access_token"), got.str("refresh_token")
		// The partner's next call has no session to wait for the write with.
		h.settle(t)
		status, me := bearer(t, a, http.MethodGet, "/auth/me", access)
		if user, _ := me["user"].(map[string]any); status != http.StatusOK || user["email"] != "alice@suite.test" {
			t.Fatalf("the grant acts as %d %v", status, me)
		}
		status, list := bearer(t, a, http.MethodGet, "/tokens", access)
		tokens, _ := list["tokens"].([]any)
		if status != http.StatusOK || len(tokens) != 1 {
			t.Fatalf("alice's tokens are %d %v", status, list)
		}
		if token := tokens[0].(map[string]any); token["name"] != "Exciter (connected app)" || token["clientId"] != id || token["expiresAt"] == nil {
			t.Errorf("the grant in alice's list is %v", token)
		}
	})

	t.Run("a refresh rotates both tokens, and a rotated one presented again ends the grant", func(t *testing.T) {
		got := oauthPost(t, a, "/oauth/token", id, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
		if got.Status != http.StatusOK || got.str("refresh_token") == refresh || got.str("access_token") == access {
			t.Fatalf("the refresh answered %d %v", got.Status, got.Body)
		}
		if status, _ := bearer(t, a, http.MethodGet, "/auth/me", access); status != http.StatusUnauthorized {
			t.Errorf("the old access token still answers %d", status)
		}
		renewed := got.str("access_token")
		if again := oauthPost(t, a, "/oauth/token", id, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}); again.Status != http.StatusBadRequest || again.str("error") != "invalid_grant" {
			t.Errorf("a rotated refresh token answered %d %v", again.Status, again.Body)
		}
		if status, _ := bearer(t, a, http.MethodGet, "/auth/me", renewed); status != http.StatusUnauthorized {
			t.Errorf("the grant outlived its replayed refresh token: %d", status)
		}
	})

	t.Run("revoking the grant in alice's token list ends its refresh token", func(t *testing.T) {
		got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(subject))
		if got.Status != http.StatusOK {
			t.Fatalf("the exchange answered %d %v", got.Status, got.Body)
		}
		h.settle(t)
		_, list := bearer(t, a, http.MethodGet, "/tokens", got.str("access_token"))
		tokens, _ := list["tokens"].([]any)
		if len(tokens) != 1 {
			t.Fatalf("alice's tokens after the second exchange are %v", list)
		}
		token := tokens[0].(map[string]any)
		if status, _ := bearer(t, a, http.MethodDelete, "/tokens/"+token["id"].(string), got.str("access_token")); status != http.StatusNoContent {
			t.Fatalf("revoking the grant answered %d", status)
		}
		if after := oauthPost(t, a, "/oauth/token", id, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {got.str("refresh_token")}}); after.Status != http.StatusBadRequest || after.str("error") != "invalid_grant" {
			t.Errorf("a revoked grant refreshes with %d %v", after.Status, after.Body)
		}
	})

	t.Run("the partner's own revocation ends the grant, and asks for the app's secret", func(t *testing.T) {
		got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(subject))
		if got.Status != http.StatusOK {
			t.Fatalf("the exchange answered %d %v", got.Status, got.Body)
		}
		if refused := oauthPost(t, a, "/oauth/revoke", id, "stator_ocs_wrong", url.Values{"token": {got.str("refresh_token")}}); refused.Status != http.StatusUnauthorized {
			t.Errorf("a revocation with a wrong secret answered %d", refused.Status)
		}
		if revoked := oauthPost(t, a, "/oauth/revoke", id, secret, url.Values{"token": {got.str("refresh_token")}}); revoked.Status != http.StatusOK {
			t.Fatalf("the revocation answered %d %v", revoked.Status, revoked.Body)
		}
		if status, _ := bearer(t, a, http.MethodGet, "/auth/me", got.str("access_token")); status != http.StatusUnauthorized {
			t.Errorf("the access token outlived its grant's revocation: %d", status)
		}
	})

	t.Run("administrators list the apps, and members may not", func(t *testing.T) {
		resp, body := admin.get(t, a.URL+httpapi.APIPrefix+"/oauth/clients")
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"keycloakClientId":"exciter"`) || strings.Contains(body, "stator_ocs_") {
			t.Fatalf("the apps answered %d %s", resp.StatusCode, body)
		}
		status, _ := bearer(t, a, http.MethodGet, "/oauth/clients", partnerAccess(t, a, id, secret, subject))
		if status != http.StatusForbidden {
			t.Errorf("a member's token lists the apps: %d", status)
		}
	})

	t.Run("refusals are OAuth's", func(t *testing.T) {
		if got := oauthPost(t, a, "/oauth/token", id, "stator_ocs_wrong", exchangeForm(subject)); got.Status != http.StatusUnauthorized || got.str("error") != "invalid_client" ||
			!strings.HasPrefix(got.Header.Get("WWW-Authenticate"), "Basic") {
			t.Errorf("a wrong secret answered %d %v", got.Status, got.Body)
		}
		ownToken := partnerToken(t, kc, statorClient, statorSecret, "alice", alicePassword)
		if got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(ownToken)); got.Status != http.StatusBadRequest || got.str("error") != "invalid_grant" {
			t.Errorf("a token issued to another client answered %d %v", got.Status, got.Body)
		}
		asked := exchangeForm(subject)
		asked.Set("scope", "read admin")
		if got := oauthPost(t, a, "/oauth/token", id, secret, asked); got.Status != http.StatusBadRequest || got.str("error") != "invalid_scope" {
			t.Errorf("a scope beyond the app's answered %d %v", got.Status, got.Body)
		}
		if got := oauthPost(t, a, "/oauth/token", id, secret, url.Values{"grant_type": {"password"}}); got.Status != http.StatusBadRequest || got.str("error") != "unsupported_grant_type" {
			t.Errorf("the password grant answered %d %v", got.Status, got.Body)
		}
	})

	t.Run("carol is let in only by the group mapping, once it lets people in", func(t *testing.T) {
		carol := partnerToken(t, kc, exciterClient, exciterSecret, "carol", carolPassword)
		got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(carol))
		if got.Status != http.StatusBadRequest || got.str("error") != "invalid_grant" || !strings.Contains(got.str("error_description"), "has no Stator account yet") {
			t.Fatalf("carol, whom nobody let in, answered %d %v", got.Status, got.Body)
		}
		if n := h.countRows(t, `SELECT count(*) FROM app_user WHERE email = 'carol@suite.test'`); n != 0 {
			t.Errorf("a refused exchange kept carol's account")
		}
		mapGroup(t, admin, a, "quality", "member")
		admitByGroup(t, admin, a, true)
		got = oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(carol))
		if got.Status != http.StatusOK {
			t.Fatalf("carol, in a mapped group, answered %d %v", got.Status, got.Body)
		}
		if role := h.standing(t, "carol@suite.test"); role != "member" {
			t.Errorf("carol's membership is %s", role)
		}
	})

	t.Run("an app that may only read hands out a token that only reads", func(t *testing.T) {
		resp, out := httpPatch(t, admin, a.URL+httpapi.APIPrefix+"/oauth/clients/"+id, map[string]any{"scopes": []string{"read"}})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("narrowing the app answered %d %v", resp.StatusCode, out)
		}
		got := oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(subject))
		if got.Status != http.StatusOK || got.str("scope") != "read" {
			t.Fatalf("the narrowed app's exchange answered %d %v", got.Status, got.Body)
		}
		if status, _ := bearer(t, a, http.MethodPost, "/spaces", got.str("access_token")); status != http.StatusForbidden {
			t.Errorf("a reading grant was let write: %d", status)
		}
	})

	t.Run("removing the app ends its grants", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, a.URL+httpapi.APIPrefix+"/oauth/clients/"+id, nil)
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("removing the app answered %d", resp.StatusCode)
		}
		if n := h.countRows(t, `SELECT count(*) FROM api_token WHERE client_id IS NOT NULL`); n != 0 {
			t.Errorf("%d grants outlived their app", n)
		}
	})

	t.Run("each act on the apps is recorded once, with its actor and target", func(t *testing.T) {
		for action, want := range map[string]int{
			"oauth.client_created": 1, "oauth.client_updated": 1, "oauth.client_removed": 1,
		} {
			if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2 AND actor_user_id IS NOT NULL`, action, id); n != want {
				t.Errorf("%s is recorded %d times, want %d", action, n, want)
			}
		}
		// One per exchange that answered: alice four times, carol and the narrowed app.
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'oauth.grant_created' AND actor_user_id IS NOT NULL AND target_id IS NOT NULL`); n != 6 {
			t.Errorf("oauth.grant_created is recorded %d times, want one per exchange", n)
		}
	})
}

func TestGrantsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) {
		s.OAuth = oauth.NewService(h.cluster, s.OIDC, "http://"+appHost, discard())
	})
	h.emptySite(t, "exchange-wall")
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, "alice@suite.test", "member")
	admin, _ := adminOf(t, h, a)
	id, _ := registerApp(t, admin, a, map[string]any{"name": "Exciter", "scopes": []string{"read"}, "keycloakClientId": exciterClient})
	alice := h.userID(t, "alice@suite.test")

	conn := appConn(t)
	actAs(t, conn, alice)
	var n int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM oauth_client`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a member reads %d partner apps: %v", n, err)
	}
	denied(t, conn, "forging a grant", `INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at)
		VALUES ($1, 'Mine (connected app)', '\x0000000000000000000000000000000000000000000000000000000000000000', '{}', now() + interval '1 day',
		       $2, '\x1100000000000000000000000000000000000000000000000000000000000000', now() + interval '1 hour')`, alice, id)
	denied(t, conn, "registering an app", `INSERT INTO oauth_client (name, scopes, keycloak_client_id, secret_hash)
		VALUES ('Mine', '{read}', 'mine', '\x0000000000000000000000000000000000000000000000000000000000000000')`)
}
