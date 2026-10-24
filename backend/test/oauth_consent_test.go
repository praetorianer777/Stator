//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// consentWorld is a site with an administrator and a member, alice, served
// the whole API with the OAuth service.
type consentWorld struct {
	h                *harness
	api              *apiServer
	admin, alice     *client
	ownerID, aliceID uuid.UUID
}

func newConsentWorld(t *testing.T, slug string) *consentWorld {
	t.Helper()
	h := newHarness(t)
	api := consentAPI(t, h)
	home := h.makeMember(t, slug)
	aliceID := h.addPerson(t, "member")
	return &consentWorld{h: h, api: api, admin: api.as(t, home.user), alice: api.as(t, aliceID), ownerID: home.user, aliceID: aliceID}
}

// allowed is an app the administrator allowed by connecting it.
func (w *consentWorld) allowed(t *testing.T, name, redirect string, confidential bool) selfApp {
	t.Helper()
	app := seedApp(t, w.h, name, redirect, confidential)
	connect(t, w.h, w.api, w.admin, app, nil)
	return app
}

// An app learns from the metadata and a 401 how to connect (#449), and
// registers itself under the rules.
func TestAnAppLearnsHowToConnectToMCP(t *testing.T) {
	w := newConsentWorld(t, "consent-meta")
	api := w.api

	t.Run("both metadata documents name the endpoints and the resource", func(t *testing.T) {
		resp, err := http.Get(api.srv.URL + oauth.MetadataPath)
		if err != nil {
			t.Fatal(err)
		}
		var server oauth.Metadata
		_ = json.NewDecoder(resp.Body).Decode(&server)
		resp.Body.Close()
		if server.AuthorizationEndpoint != consentIssuer+"/api/v1/oauth/authorize" || server.RegistrationEndpoint != consentIssuer+"/api/v1/oauth/register" ||
			!slices.Contains(server.GrantTypesSupported, "authorization_code") || !slices.Contains(server.GrantTypesSupported, oauth.GrantTokenExchange) ||
			!slices.Equal(server.CodeChallengeMethodsSupported, []string{"S256"}) || !slices.Contains(server.TokenEndpointAuthMethods, "none") {
			t.Errorf("the server metadata is %+v", server)
		}
		for _, path := range []string{oauth.ResourceMetadataPath + "/api/v1/mcp", oauth.ResourceMetadataPath} {
			resp, err := http.Get(api.srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			var resource oauth.ResourceMetadata
			_ = json.NewDecoder(resp.Body).Decode(&resource)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resource.Resource != consentIssuer+"/api/v1/mcp" ||
				!slices.Equal(resource.AuthorizationServers, []string{consentIssuer}) || !slices.Equal(resource.ScopesSupported, []string{"read", "write"}) {
				t.Errorf("%s answered %d %+v", path, resp.StatusCode, resource)
			}
		}
	})

	t.Run("a 401 at MCP points a client to the metadata", func(t *testing.T) {
		want := `Bearer resource_metadata="` + consentIssuer + `/.well-known/oauth-protected-resource/api/v1/mcp", scope="read write"`
		if status, header, _ := mcpCall(t, api, "", "tools/list", nil); status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != want {
			t.Errorf("no token at MCP = %d %q", status, header.Get("WWW-Authenticate"))
		}
		if status, header, _ := mcpCall(t, api, "stator_oat_gone", "tools/list", nil); status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != want+`, error="invalid_token"` {
			t.Errorf("a dead token at MCP = %d %q", status, header.Get("WWW-Authenticate"))
		}
	})

	t.Run("registration holds an app to the rules", func(t *testing.T) {
		got := registerRaw(t, api, map[string]any{"client_name": "Plain", "redirect_uris": []string{"http://rotor.example/cb"}})
		if got.Status != http.StatusBadRequest || got.Body["error"] != "invalid_redirect_uri" {
			t.Errorf("a plain http return address answered %d %s", got.Status, got.Raw)
		}
		got = registerRaw(t, api, map[string]any{"client_name": "Exchanger", "redirect_uris": []string{"https://rotor.example/cb"},
			"grant_types": []string{oauth.GrantTokenExchange}})
		if got.Status != http.StatusBadRequest || got.Body["error"] != "invalid_client_metadata" {
			t.Errorf("the exchange without authorization_code answered %d %s", got.Status, got.Raw)
		}
	})

	t.Run("the authorization endpoint sends the browser to the consent page, or the sign-in", func(t *testing.T) {
		app := registerSelf(t, w.h, api, map[string]any{"client_name": "Local assistant", "redirect_uris": []string{"http://127.0.0.1:8000/callback"},
			"token_endpoint_auth_method": "none"})
		if app.secret != "" {
			t.Fatalf("a public app was given a secret")
		}
		_, challenge := pkce(t)
		query := authorizeQuery(app, challenge, nil)
		resp, err := noRedirects.Get(api.srv.URL + httpapi.APIPrefix + "/oauth/authorize?" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != consentIssuer+"/login?next="+url.QueryEscape("/oauth/authorize?"+query) {
			t.Errorf("signed out, authorize = %d %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})
}

// A member cannot connect an app nobody allowed; an administrator's own
// consent allows it, for every member after.
func TestAnAppWaitsForAnAdministrator(t *testing.T) {
	w := newConsentWorld(t, "consent-pending")
	h, api, admin, alice := w.h, w.api, w.admin, w.alice
	// Rotor registers as a confidential app and asks for the exchange the
	// metadata lists, beside the code.
	rotor := registerSelf(t, h, api, map[string]any{"client_name": "Rotor", "redirect_uris": []string{"https://rotor.example/oauth/callback"},
		"token_endpoint_auth_method": "client_secret_basic", "scope": "read write",
		"grant_types": []string{"authorization_code", "refresh_token", oauth.GrantTokenExchange}})
	if !strings.HasPrefix(rotor.secret, "stator_ocs_") {
		t.Fatalf("Rotor's secret is %q", rotor.secret)
	}
	t.Run("a pending app is refused for a member, then allowed by an administrator's consent", func(t *testing.T) {
		_, challenge := pkce(t)
		query := authorizeQuery(rotor, challenge, nil)
		shown := want(t, consentOf(t, alice, query), http.StatusOK, "alice's consent page")
		page := obj(t, shown, "consent")
		if page["pending"] != true || page["mayAllow"] != false || !strings.Contains(page["refusal"].(string), "administrator must allow Rotor") {
			t.Errorf("alice is shown %v", page)
		}
		if got := decideConsent(t, alice, query, "allow", nil); got.Status != http.StatusForbidden {
			t.Errorf("alice allowing a pending app answered %d %s", got.Status, got.Raw)
		}
		if got := appPost(t, api, "/oauth/token", rotor, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"stator_ort_x"}}); got.Status != http.StatusUnauthorized || got.str("error") != "invalid_client" {
			t.Errorf("a pending app at the token endpoint answered %d %v", got.Status, got.Body)
		}
		page = obj(t, want(t, consentOf(t, admin, query), http.StatusOK, "the administrator's consent page"), "consent")
		if page["mayAllow"] != true || page["allowsApp"] != true {
			t.Errorf("the administrator is shown %v", page)
		}
		connect(t, h, api, admin, rotor, nil)
		if n := h.countRows(t, `SELECT count(*) FROM oauth_client WHERE id = $1 AND status = 'allowed'`, rotor.id); n != 1 {
			t.Errorf("Rotor is not allowed after the administrator's consent")
		}
		page = obj(t, want(t, consentOf(t, alice, query), http.StatusOK, "alice's consent page again"), "consent")
		if page["mayAllow"] != true || page["pending"] != false {
			t.Errorf("alice is shown the allowed app as %v", page)
		}
	})

	t.Run("each act is recorded", func(t *testing.T) {
		for action, want := range map[string]int{"oauth.client_registered": 1, "oauth.client_allowed": 1, "oauth.grant_consented": 1} {
			if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, rotor.id); n != want {
				t.Errorf("%s is recorded %d times for Rotor, want %d", action, n, want)
			}
		}
	})
}

// A consented grant acts as its person at MCP and nowhere else; its refresh
// token rotates, and the app may revoke it.
func TestAConsentedGrantActsAtMCPAlone(t *testing.T) {
	w := newConsentWorld(t, "consent-grant")
	h, api, alice := w.h, w.api, w.alice
	rotor := w.allowed(t, "Rotor", "https://rotor.example/oauth/callback", true)

	var access, refresh string
	t.Run("alice connects Rotor and its token acts as her at MCP alone", func(t *testing.T) {
		got := connect(t, h, api, alice, rotor, nil)
		access, refresh = got.str("access_token"), got.str("refresh_token")
		if !strings.HasPrefix(access, "stator_oat_") || got.str("scope") != "read write" || got.Header.Get("Cache-Control") != "no-store" ||
			got.Body["expires_in"] != float64(oauth.AccessTokenTTL.Seconds()) {
			t.Fatalf("the token answer is %v", got.Body)
		}
		me := toolResult(t, api, access, "whoami", map[string]any{})
		if me["isError"] == true || !strings.Contains(fmt.Sprint(me["structuredContent"]), "member-") {
			t.Errorf("whoami through MCP answered %v", me)
		}
		resp := api.withToken(access).get(t, httpapi.APIPrefix+"/tokens")
		if resp.Status != http.StatusUnauthorized || !strings.Contains(string(resp.Raw), "mcp_only_token") {
			t.Errorf("the token at /tokens answered %d %s", resp.Status, resp.Raw)
		}
		tokens := list(t, want(t, alice.get(t, httpapi.APIPrefix+"/tokens"), http.StatusOK, "alice's tokens"), "tokens")
		if len(tokens) != 1 || tokens[0].(map[string]any)["name"] != "Rotor (connected app)" {
			t.Errorf("alice's token list is %v", tokens)
		}
	})

	t.Run("a refresh rotates both tokens, and a rotated one presented again ends the grant", func(t *testing.T) {
		got := appPost(t, api, "/oauth/token", rotor, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh},
			"resource": {consentIssuer + "/api/v1/mcp/"}})
		if got.Status != http.StatusOK || got.str("refresh_token") == refresh {
			t.Fatalf("the refresh answered %d %v", got.Status, got.Body)
		}
		h.settle(t)
		renewed := got.str("access_token")
		if status, _, _ := mcpCall(t, api, access, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the old access token still answers %d", status)
		}
		if status, _, _ := mcpCall(t, api, renewed, "tools/list", nil); status != http.StatusOK {
			t.Errorf("the renewed access token answers %d", status)
		}
		elsewhere := appPost(t, api, "/oauth/token", rotor, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {got.str("refresh_token")},
			"resource": {consentIssuer + "/api/v1"}})
		if elsewhere.Status != http.StatusBadRequest || elsewhere.str("error") != "invalid_target" {
			t.Errorf("a refresh for another resource answered %d %v", elsewhere.Status, elsewhere.Body)
		}
		again := appPost(t, api, "/oauth/token", rotor, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
		if again.Status != http.StatusBadRequest || again.str("error") != "invalid_grant" {
			t.Errorf("a rotated refresh token answered %d %v", again.Status, again.Body)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, renewed, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the grant outlived its replayed refresh token: %d", status)
		}
	})

	t.Run("revocation ends a grant", func(t *testing.T) {
		got := connect(t, h, api, alice, rotor, nil)
		if revoked := appPost(t, api, "/oauth/revoke", rotor, url.Values{"token": {got.str("refresh_token")}}); revoked.Status != http.StatusOK {
			t.Fatalf("the revocation answered %d %v", revoked.Status, revoked.Body)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, got.str("access_token"), "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the access token outlived its grant's revocation: %d", status)
		}
		if refused := appPost(t, api, "/oauth/revoke", selfApp{id: rotor.id, secret: "stator_ocs_guess"}, url.Values{"token": {"x"}}); refused.Status != http.StatusUnauthorized {
			t.Errorf("a wrong secret answered %d", refused.Status)
		}
	})
}

// A code is good once, for the app, the return address, the verifier and the
// resource it was given for; a request the app got wrong is answered at the app.
func TestACodeIsRedeemedOnce(t *testing.T) {
	w := newConsentWorld(t, "consent-code")
	h, api, alice := w.h, w.api, w.alice
	// A tool on the person's machine keeps no secret and listens on a loopback port.
	local := w.allowed(t, "Local assistant", "http://127.0.0.1:8000/callback", false)

	t.Run("a code is redeemed once with its verifier, return address and resource", func(t *testing.T) {
		verifier, challenge := pkce(t)
		query := authorizeQuery(local, challenge, url.Values{"redirect_uri": {"http://127.0.0.1:51234/callback"}})
		local.redirect = "http://127.0.0.1:51234/callback"
		code := codeOf(t, decideConsent(t, alice, query, "allow", nil), local)
		for why, extra := range map[string]url.Values{
			"a wrong verifier":          {"code_verifier": {strings.Repeat("x", 43)}},
			"another return address":    {"redirect_uri": {"http://127.0.0.1:51235/callback"}},
			"another resource":          {"resource": {consentIssuer + "/api/v1"}},
			"a secret it does not have": {"client_secret": {"stator_ocs_guess"}},
		} {
			got := redeem(t, api, local, code, verifier, extra)
			if got.Status == http.StatusOK {
				t.Errorf("%s was given a token", why)
			}
		}
		// None of those spent the code; the right verifier still redeems it.
		first := redeem(t, api, local, code, verifier, nil)
		if first.Status != http.StatusOK || first.str("scope") != "read write" {
			t.Fatalf("the right verifier answered %d %v", first.Status, first.Body)
		}
		h.settle(t)
		again := redeem(t, api, local, code, verifier, nil)
		if again.Status != http.StatusBadRequest || again.str("error") != "invalid_grant" {
			t.Errorf("a code used twice answered %d %v", again.Status, again.Body)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, first.str("access_token"), "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the grant outlived its code used twice: %d", status)
		}
	})

	t.Run("a request the app got wrong is answered at the app, an unknown address never", func(t *testing.T) {
		_, challenge := pkce(t)
		got := consentOf(t, alice, authorizeQuery(local, challenge, url.Values{"resource": {"https://elsewhere.example/api/v1/mcp"}}))
		if to, _ := got.Body["redirectTo"].(string); !strings.Contains(to, "error=invalid_target") || !strings.HasPrefix(to, local.redirect) {
			t.Errorf("a wrong resource answered %d %s", got.Status, got.Raw)
		}
		got = consentOf(t, alice, authorizeQuery(local, challenge, url.Values{"code_challenge_method": {"plain"}}))
		if to, _ := got.Body["redirectTo"].(string); !strings.Contains(to, "error=invalid_request") {
			t.Errorf("a plain challenge answered %d %s", got.Status, got.Raw)
		}
		got = consentOf(t, alice, authorizeQuery(local, challenge, url.Values{"redirect_uri": {"https://evil.example/cb"}}))
		if got.Status != http.StatusBadRequest || got.Body["redirectTo"] != nil {
			t.Errorf("an unregistered return address answered %d %s", got.Status, got.Raw)
		}
		denied := decideConsent(t, alice, authorizeQuery(local, challenge, nil), "deny", nil)
		if to, _ := denied.Body["redirectTo"].(string); !strings.Contains(to, "error=access_denied") || !strings.Contains(to, "state=st-1") {
			t.Errorf("cancelling answered %d %s", denied.Status, denied.Raw)
		}
	})
}
