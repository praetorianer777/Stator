//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Connecting one's account with one click (#428), against the stub's
// authorization server. The test is the browser: it follows Stator's
// redirect to the consent page, fills it in, and brings the code back.

// stubCapabilities are what the stub reports, with connected apps.
var stubCapabilities = []string{"calendar@1", "issue-embed@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks@1", armature.CapabilityPartnerTokens}

// connectedAppURL is the stub under a host of the harness's own, which
// reports oauth.partner-tokens@1 and whose registration a test may close.
func connectedAppURL(h *harness) string {
	if h.connectedApp == "" {
		h.connectedApp = "http://" + uuid.NewString()[:8] + ".connected-app.test"
	}
	return h.connectedApp
}

func offerConnectedApps(t *testing.T, h *harness, registration string) {
	t.Helper()
	host := strings.TrimPrefix(connectedAppURL(h), "http://")
	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": stubCapabilities})
	t.Cleanup(func() { metaControl(t, http.MethodDelete, host, nil) })
	oauthControl(t, http.MethodPut, "/_stub-oauth/hosts/"+url.PathEscape(host), map[string]any{"registration": registration}, nil)
}

func oauthControl(t *testing.T, method, path string, body, out any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, os.Getenv(envArmatureStub)+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("the stub answered %s %s with %d", method, path, resp.StatusCode)
	}
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
}

// noFollow follows no redirect, so each hop is looked at.
func noFollow(c *client) *http.Client {
	return &http.Client{Jar: c.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func hop(t *testing.T, b *http.Client, req *http.Request) *url.URL {
	t.Helper()
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("%s %s answered %d, want a redirect", req.Method, req.URL, resp.StatusCode)
	}
	next, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// startConnect asks Stator to connect c, answering where it sends the browser.
func startConnect(t *testing.T, c *client, back string) *url.URL {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.api.srv.URL+"/api/v1/armature/oauth/start?return="+url.QueryEscape(back), nil)
	return hop(t, noFollow(c), req)
}

// atStub sends a request for the public address to the stub, keeping the
// public host, as the backchannel does for the api.
func atStub(t *testing.T, method string, public *url.URL, form url.Values) *http.Request {
	t.Helper()
	stub, _ := url.Parse(os.Getenv(envArmatureStub))
	target := *public
	target.Scheme, target.Host = stub.Scheme, stub.Host
	var req *http.Request
	if form != nil {
		req, _ = http.NewRequest(method, target.String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req, _ = http.NewRequest(method, target.String(), nil)
	}
	req.Host = public.Host
	return req
}

// consentAs fills in the consent page Stator sent the browser to.
func consentAs(t *testing.T, c *client, authorize *url.URL, tenant, person, decision string) *http.Response {
	t.Helper()
	b := noFollow(c)
	shown, err := b.Do(atStub(t, http.MethodGet, authorize, nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = shown.Body.Close()
	if shown.StatusCode != http.StatusOK {
		t.Fatalf("the consent page answered %d", shown.StatusCode)
	}
	page := *authorize
	page.RawQuery = ""
	resp, err := b.Do(atStub(t, http.MethodPost, &page, url.Values{"query": {authorize.RawQuery}, "tenant": {tenant}, "person": {person}, "decision": {decision}}))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// finish brings Armature's answer back to Stator, answering where it lands.
func finish(t *testing.T, c *client, answer *http.Response) *url.URL {
	t.Helper()
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusFound {
		t.Fatalf("the consent page answered %d, want a redirect back", answer.StatusCode)
	}
	back, _ := url.Parse(answer.Header.Get("Location"))
	if back.Host != "stator.test" || back.Path != armature.OAuthCallbackPath {
		t.Fatalf("Armature sent the browser to %s", back)
	}
	req, _ := http.NewRequest(http.MethodGet, c.api.srv.URL+back.Path+"?"+back.RawQuery, nil)
	return hop(t, noFollow(c), req)
}

func outcome(t *testing.T, landed *url.URL, want, back string) {
	t.Helper()
	if landed.Path != "/settings/profile" || landed.Query().Get("armature") != want || landed.Query().Get("return") != back || landed.Fragment != "armature" {
		t.Errorf("landed on %s, want the profile saying %s and going back to %q", landed, want, back)
	}
}

type stubGrant struct {
	Person    string `json:"person"`
	ClientID  string `json:"clientId"`
	Scope     string `json:"scope"`
	Refreshes int    `json:"refreshes"`
	Ended     bool   `json:"ended"`
}

func stubGrants(t *testing.T, tenant, person string) []stubGrant {
	t.Helper()
	var out struct {
		Grants []stubGrant `json:"grants"`
	}
	oauthControl(t, http.MethodGet, "/_stub-oauth/"+tenant+"/grants", nil, &out)
	var mine []stubGrant
	for _, g := range out.Grants {
		if g.Person == person {
			mine = append(mine, g)
		}
	}
	return mine
}

func lookupStatus(t *testing.T, c *client, key string) string {
	t.Helper()
	got := want(t, c.get(t, "/api/v1/armature/issues?key="+key), http.StatusOK, "look up "+key)
	status, _ := got.Body["status"].(string)
	return status
}

func TestAMemberConnectsArmatureWithOneClick(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-app")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	offerConnectedApps(t, h, "approve")

	// Without connected apps the token form stays, and so does the start.
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect the stub without apps")
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "an account without apps"), "account", "app"); got["offered"] != false {
		t.Errorf("an Armature without oauth.partner-tokens@1 offers the app: %v", got)
	}
	outcome(t, startConnect(t, bob, "/s/ENG"), "unavailable", "/s/ENG")

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect an Armature with apps")
	app := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection", "app")
	if app["offered"] != true || app["status"] != "none" || app["redirectUri"] != "http://stator.test"+armature.OAuthCallbackPath {
		t.Errorf("before anybody connects the app is %v", app)
	}

	// Bob is first: Stator registers itself, and Armature holds it pending.
	authorize := startConnect(t, bob, "/s/ENG/p/1?x=1")
	q := authorize.Query()
	if authorize.String()[:len(connectedAppURL(h))] != connectedAppURL(h) || authorize.Path != "/oauth/authorize" || q.Get("scope") != "read issue.write" ||
		q.Get("code_challenge_method") != "S256" || len(q.Get("state")) < 22 || q.Get("redirect_uri") != "http://stator.test"+armature.OAuthCallbackPath {
		t.Fatalf("the consent page asked is %s", authorize)
	}
	if strings.Contains(authorize.RawQuery, "webhooks.partner") {
		t.Error("the connect asks for the partner webhook, which #429 asks for")
	}
	h.settle(t)
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob while the app is pending"), "account", "app"); got["pending"] != true || got["offered"] != true {
		t.Errorf("while Armature holds the app the account says %v", got)
	}
	pending := consentAs(t, bob, authorize, slug, "bob", "allow")
	_ = pending.Body.Close()
	if pending.StatusCode != http.StatusOK {
		t.Errorf("a member allowed a pending app: %d", pending.StatusCode)
	}

	// Another session of bob's cannot finish his trip.
	stolen := api.as(t, bobID)
	ownerTrip := startConnect(t, owner, "")
	answer := consentAs(t, owner, ownerTrip, slug, "admin", "allow")
	outcome(t, finish(t, stolen, answer), "expired", "")

	// The administrator allows the app on the way.
	ownerTrip = startConnect(t, owner, "")
	outcome(t, finish(t, owner, consentAs(t, owner, ownerTrip, slug, "admin", "allow")), "connected", "")
	h.settle(t)
	account := obj(t, want(t, owner.get(t, "/api/v1/armature/account"), http.StatusOK, "the owner's account"), "account")
	if account["via"] != "app" || account["status"] != "ok" || obj(t, response{Body: account}, "user")["name"] != "Admin" {
		t.Errorf("the owner's account is %v", account)
	}
	if app := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection", "app"); app["status"] != "active" || app["registeredBy"] != "stator" {
		t.Errorf("after the first grant the app is %v", app)
	}

	// Now bob, who cancels once and then allows.
	outcome(t, finish(t, bob, consentAs(t, bob, startConnect(t, bob, "/s/ENG"), slug, "bob", "deny")), "denied", "/s/ENG")
	trip := startConnect(t, bob, "/s/ENG/p/1?x=1")
	answer = consentAs(t, bob, trip, slug, "bob", "allow")
	defer answer.Body.Close()
	back, _ := url.Parse(answer.Header.Get("Location"))
	if back.Query().Get("iss") != connectedAppURL(h) {
		t.Fatalf("Armature named the issuer %q", back.Query().Get("iss"))
	}
	mixedUp := url.Values{"code": {back.Query().Get("code")}, "state": {back.Query().Get("state")}, "iss": {"https://elsewhere.example"}}
	req, _ := http.NewRequest(http.MethodGet, bob.api.srv.URL+armature.OAuthCallbackPath+"?"+mixedUp.Encode(), nil)
	outcome(t, hop(t, noFollow(bob), req), "failed", "/s/ENG/p/1?x=1")

	trip = startConnect(t, bob, "/s/ENG/p/1?x=1")
	landed := finish(t, bob, consentAs(t, bob, trip, slug, "bob", "allow"))
	outcome(t, landed, "connected", "/s/ENG/p/1?x=1")
	h.settle(t)
	if status := lookupStatus(t, bob, "CP-1"); status != "ok" {
		t.Fatalf("bob's lookup through the grant is %s", status)
	}
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT access_token FROM armature_grant WHERE user_id = $1`, bobID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("armature_oat_")) {
		t.Error("the access token is readable at rest")
	}

	// Armature ends access tokens within the hour; two calls at once renew
	// the grant once, since a refresh token presented twice would end it.
	oauthControl(t, http.MethodPost, "/_stub-oauth/"+slug+"/people/bob/expire", nil, nil)
	var wg sync.WaitGroup
	statuses := make([]string, 2)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := bob.get(t, "/api/v1/armature/issues?key=CP-"+[]string{"2", "3"}[i])
			statuses[i], _ = got.Body["status"].(string)
		}()
	}
	wg.Wait()
	grants := stubGrants(t, slug, "bob")
	if len(grants) != 1 || grants[0].Ended || grants[0].Refreshes != 1 || statuses[0] != "ok" || statuses[1] != "ok" {
		t.Errorf("after the access token ended the lookups are %v and the grant %+v; want one refresh and the grant alive", statuses, grants)
	}

	// Disconnecting in Armature ends it here too, said as rejected.
	oauthControl(t, http.MethodPost, "/_stub-oauth/"+slug+"/people/bob/end", nil, nil)
	if status := lookupStatus(t, bob, "CP-4"); status != "rejected" {
		t.Errorf("a grant ended in Armature looks up as %s", status)
	}
	h.settle(t)
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after Armature ended it"), "account"); got["status"] != "rejected" || got["via"] != "app" {
		t.Errorf("bob's account after Armature ended the grant is %v", got)
	}

	// Disconnecting in Stator revokes the grant at Armature.
	if live := stubGrants(t, slug, "admin"); len(live) != 1 || live[0].Ended {
		t.Fatalf("the owner's grant is %+v", live)
	}
	want(t, owner.delete(t, "/api/v1/armature/account/token"), http.StatusNoContent, "the owner disconnects")
	if ended := stubGrants(t, slug, "admin"); len(ended) != 1 || !ended[0].Ended {
		t.Errorf("disconnecting left the grant alive in Armature: %+v", ended)
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_grant WHERE user_id = $1`, home.user); n != 0 {
		t.Error("the owner's grant outlived the disconnect")
	}

	// A token pasted later replaces a grant, and a grant a token.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner pastes a token")
	outcome(t, finish(t, owner, consentAs(t, owner, startConnect(t, owner, ""), slug, "admin", "allow")), "connected", "")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, home.user); n != 0 {
		t.Error("the token stayed beside the grant")
	}

	// A new address forgets the app and every grant with the tokens.
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "move the connection")
	if n := h.countRows(t, `SELECT (SELECT count(*) FROM armature_app) + (SELECT count(*) FROM armature_grant)`); n != 0 {
		t.Errorf("%d app and grant rows outlived the move", n)
	}
}

func TestAnAdministratorPastesTheAppWhereArmatureRegistersNone(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-app-closed")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	offerConnectedApps(t, h, "closed")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect Armature")

	outcome(t, startConnect(t, bob, "/s/ENG"), "refused", "/s/ENG")
	h.settle(t)
	account := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after the refusal"), "account", "app")
	if account["offered"] != false || !strings.Contains(account["refusal"].(string), "Settings, Connected apps") {
		t.Errorf("after Armature refused to register Stator, bob reads %v", account)
	}
	app := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection", "app")
	if app["status"] != "refused" || app["clientId"] != nil {
		t.Errorf("the refused app is %v", app)
	}

	for _, tc := range []struct{ name, id, secret, field string }{
		{"no client id", " ", "", "clientId"},
		{"a secret Armature never shows", "abc", "hunter2", "clientSecret"},
	} {
		fieldError(t, want(t, owner.put(t, "/api/v1/armature/connection/app", map[string]any{"clientId": tc.id, "clientSecret": tc.secret}), http.StatusUnprocessableEntity, tc.name), tc.field)
	}
	errorCode(t, want(t, bob.put(t, "/api/v1/armature/connection/app", map[string]any{"clientId": "x"}), http.StatusForbidden, "a member pastes the app"))

	// What an Armature administrator registered by hand: the stub's own
	// registration, at an address that registers.
	var reg struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	oauthControl(t, http.MethodPost, "/api/v1/oauth/register", armature.Registration{ClientName: armature.AppName,
		RedirectURIs: []string{"http://stator.test" + armature.OAuthCallbackPath}, Scope: armature.AppScopes, TokenEndpointAuthMethod: "client_secret_basic"}, &reg)
	saved := obj(t, want(t, owner.put(t, "/api/v1/armature/connection/app", map[string]any{"clientId": reg.ClientID, "clientSecret": reg.ClientSecret}), http.StatusOK, "paste the app"), "connection", "app")
	if saved["status"] != "active" || saved["registeredBy"] != "admin" || saved["clientId"] != reg.ClientID {
		t.Errorf("the pasted app is %v", saved)
	}
	if strings.Contains(string(want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection").Raw), reg.ClientSecret) {
		t.Error("the client secret is answered")
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1`, "armature.app_saved"); n != 1 {
		t.Errorf("pasting the app is audited %d times", n)
	}
	outcome(t, finish(t, owner, consentAs(t, owner, startConnect(t, owner, ""), slug, "admin", "allow")), "connected", "")
	h.settle(t)
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob once the app is pasted"), "account", "app"); got["offered"] != true || got["refusal"] != nil {
		t.Errorf("once the app is pasted bob reads %v", got)
	}

	want(t, owner.delete(t, "/api/v1/armature/connection/app"), http.StatusNoContent, "forget the app")
	if n := h.countRows(t, `SELECT count(*) FROM armature_grant`); n != 0 {
		t.Errorf("%d grants outlived their app", n)
	}
}

// Straight through SQL as stator_app: a person reads and changes only their
// own grant and trip, and only administrators see the app.
func TestConnectedAppRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-app-wall")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	offerConnectedApps(t, h, "approve")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect Armature")
	outcome(t, finish(t, owner, consentAs(t, owner, startConnect(t, owner, ""), slug, "admin", "allow")), "connected", "")
	startConnect(t, bob, "")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	actAs(t, conn, bobID)
	if n := count(`SELECT count(*) FROM armature_app`); n != 0 {
		t.Error("a member reads the app with its sealed secret")
	}
	if n := count(`SELECT count(*) FROM armature_grant`); n != 0 {
		t.Error("bob reads the owner's grant")
	}
	if n := count(`SELECT count(*) FROM armature_oauth_attempt`); n != 1 {
		t.Errorf("bob reads %d trips, want his own", n)
	}
	untouched(t, conn, "removing the owner's grant", `DELETE FROM armature_grant WHERE user_id = $1`, home.user)
	untouched(t, conn, "marking the owner's grant", `UPDATE armature_grant SET status = 'rejected' WHERE user_id = $1`, home.user)
	untouched(t, conn, "removing the app", `DELETE FROM armature_app`)
	denied(t, conn, "storing a grant for the owner", `INSERT INTO armature_grant (user_id, access_token, refresh_token, access_expires_at, refresh_expires_at, scopes, status)
		VALUES ($1, '\x00', '\x00', now(), now(), '{}', 'ok')`, home.user)
	denied(t, conn, "starting a trip for the owner", `INSERT INTO armature_oauth_attempt (state_digest, user_id, session_id, code_verifier, expires_at)
		VALUES ('\x01', $1, $2, 'v', now() + interval '1 hour')`, home.user, uuid.New())
	denied(t, conn, "replacing the app", `UPDATE armature_app SET client_id = 'mine'`)
	denied(t, conn, "choosing his grant's id", `INSERT INTO armature_grant (user_id, id, access_token, refresh_token, access_expires_at, refresh_expires_at, scopes, status)
		VALUES ($1, $2, '\x00', '\x00', now(), now(), '{}', 'ok')`, bobID, uuid.New())

	actAs(t, conn, home.user)
	if n := count(`SELECT count(*) FROM armature_app`); n != 1 {
		t.Error("the owner does not see the app")
	}
	if n := count(`SELECT count(*) FROM armature_oauth_attempt`); n != 0 {
		t.Error("the owner reads bob's trip")
	}
	if n := count(`SELECT armature_connected_count()`); n != 1 {
		t.Errorf("the owner counts %d connected members, want the one grant", n)
	}
	denied(t, conn, "giving the app a status Stator does not know", `UPDATE armature_app SET status = 'unknown'`)
}
