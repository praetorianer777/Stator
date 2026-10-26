//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// A harness for apps that connect to MCP with a person's consent (#449),
// shaped like Exciter's backend/test/oauth_harness_test.go.

// consentIssuer is the issuer the suite's service names, the web origin.
const consentIssuer = "http://" + appHost

// consentAPI is the whole API with the OAuth service, its issuer the web origin.
func consentAPI(t *testing.T, h *harness) *apiServer {
	t.Helper()
	return newAPIServer(t, h, func(s *httpapi.Server) {
		s.OAuth = oauth.NewService(h.cluster, nil, consentIssuer, discard())
		s.AppBaseURL = consentIssuer
	})
}

// selfApp is an app that registered itself.
type selfApp struct {
	id, secret, redirect string
}

// registerRaw posts RFC 7591 metadata as an app does, with no cookie.
func registerRaw(t *testing.T, api *apiServer, body map[string]any) response {
	t.Helper()
	return api.anonymous().post(t, httpapi.APIPrefix+"/oauth/register", body)
}

// registerSelf registers an app and forgets it when the test ends.
func registerSelf(t *testing.T, h *harness, api *apiServer, body map[string]any) selfApp {
	t.Helper()
	got := want(t, registerRaw(t, api, body), http.StatusCreated, "register "+body["client_name"].(string))
	app := selfApp{id: got.Body["client_id"].(string)}
	app.secret, _ = got.Body["client_secret"].(string)
	app.redirect = body["redirect_uris"].([]string)[0]
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM oauth_client WHERE id = $1`, app.id) })
	return app
}

// seedApp is an app registered behind the API's back, pending as a
// registration leaves it, so the registration brake is kept for its own tests.
func seedApp(t *testing.T, h *harness, name, redirect string, confidential bool) selfApp {
	t.Helper()
	app := selfApp{redirect: redirect}
	method, digest := oauth.AuthNone, []byte(nil)
	if confidential {
		method, app.secret = oauth.AuthBasic, "stator_ocs_"+uuid.NewString()
		digest = auth.HashToken(app.secret)
	}
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO oauth_client (name, scopes, kind, status, redirect_uris, auth_method, secret_hash)
		VALUES ($1, '{read,write}', 'registered', 'pending', $2, $3, $4) RETURNING id::text`,
		name, []string{redirect}, method, digest).Scan(&app.id); err != nil {
		t.Fatalf("seed the app %s: %v", name, err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM oauth_client WHERE id = $1`, app.id) })
	return app
}

// pkce is a fresh verifier and its S256 challenge.
func pkce(t *testing.T) (string, string) {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeQuery is what an app sends the browser to the authorization
// endpoint with; extra replaces or adds parameters.
func authorizeQuery(app selfApp, challenge string, extra url.Values) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {app.id}, "redirect_uri": {app.redirect}, "state": {"st-1"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {consentIssuer + "/api/v1/mcp"},
	}
	for k, v := range extra {
		q[k] = v
	}
	return q.Encode()
}

// consentOf is what the consent page is shown for the query.
func consentOf(t *testing.T, c *client, query string) response {
	t.Helper()
	return c.get(t, httpapi.APIPrefix+"/oauth/consent?"+query)
}

// decideConsent is the person's answer on the consent page.
func decideConsent(t *testing.T, c *client, query, decision string, extra map[string]any) response {
	t.Helper()
	body := map[string]any{"query": query, "decision": decision}
	for k, v := range extra {
		body[k] = v
	}
	return c.post(t, httpapi.APIPrefix+"/oauth/consent", body)
}

// codeOf reads the code an allowed consent sends the browser back with,
// checking the state and the issuer came along (RFC 9207).
func codeOf(t *testing.T, got response, app selfApp) string {
	t.Helper()
	to, _ := got.Body["redirectTo"].(string)
	u, err := url.Parse(to)
	if got.Status != http.StatusOK || err != nil || !strings.HasPrefix(to, strings.SplitN(app.redirect, "?", 2)[0]) {
		t.Fatalf("the consent answered %d %s", got.Status, got.Raw)
	}
	q := u.Query()
	if q.Get("state") != "st-1" || q.Get("iss") != consentIssuer || !strings.HasPrefix(q.Get("code"), "stator_oac_") {
		t.Fatalf("the browser goes back to %s", to)
	}
	return q.Get("code")
}

// connect walks a person through the consent page and redeems the code.
func connect(t *testing.T, h *harness, api *apiServer, person *client, app selfApp, extra map[string]any) oauthAnswer {
	t.Helper()
	verifier, challenge := pkce(t)
	query := authorizeQuery(app, challenge, nil)
	code := codeOf(t, decideConsent(t, person, query, "allow", extra), app)
	got := redeem(t, api, app, code, verifier, nil)
	if got.Status != http.StatusOK {
		t.Fatalf("redeeming the code answered %d %v", got.Status, got.Body)
	}
	h.settle(t)
	return got
}

// redeem trades a code at the token endpoint as the app.
func redeem(t *testing.T, api *apiServer, app selfApp, code, verifier string, extra url.Values) oauthAnswer {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {app.redirect},
		"code_verifier": {verifier}, "resource": {consentIssuer + "/api/v1/mcp"}}
	for k, v := range extra {
		form[k] = v
	}
	return appPost(t, api, "/oauth/token", app, form)
}

// appPost posts a form to an OAuth endpoint as the app: by Basic with a
// secret, by client_id alone without one.
func appPost(t *testing.T, api *apiServer, path string, app selfApp, form url.Values) oauthAnswer {
	t.Helper()
	if app.secret == "" {
		form.Set("client_id", app.id)
	}
	req, _ := http.NewRequest(http.MethodPost, api.srv.URL+httpapi.APIPrefix+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if app.secret != "" {
		req.SetBasicAuth(url.QueryEscape(app.id), url.QueryEscape(app.secret))
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

// mcpCall sends one JSON-RPC request to MCP with a bearer token.
func mcpCall(t *testing.T, api *apiServer, token, method string, params any) (int, http.Header, map[string]any) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, api.srv.URL+httpapi.APIPrefix+"/mcp", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, resp.Header, out
}

// toolResult is a tools/call's result, or a failure when MCP refused the call itself.
func toolResult(t *testing.T, api *apiServer, token, tool string, args map[string]any) map[string]any {
	t.Helper()
	status, _, out := mcpCall(t, api, token, "tools/call", map[string]any{"name": tool, "arguments": args})
	result, _ := out["result"].(map[string]any)
	if status != http.StatusOK || result == nil {
		t.Fatalf("%s answered %d %v", tool, status, out)
	}
	return result
}

// noRedirects is a client that shows a redirect rather than following it.
var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
