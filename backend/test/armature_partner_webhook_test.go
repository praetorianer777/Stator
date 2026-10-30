//go:build integration

package test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Connecting Armature as an administrator in one step (#429), against the
// stub's partner webhook: one consent with every scope, and Stator makes its
// own webhook with the grant it brings back.

// offerPartnerWebhooks has the harness's own Armature report both
// capabilities the one-step connect needs.
func offerPartnerWebhooks(t *testing.T, h *harness) {
	t.Helper()
	host := strings.TrimPrefix(connectedAppURL(h), "http://")
	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": append(slices.Clone(stubCapabilities), armature.CapabilityPartnerWebhooks)})
	t.Cleanup(func() { metaControl(t, http.MethodDelete, host, nil) })
	oauthControl(t, http.MethodPut, "/_stub-oauth/hosts/"+url.PathEscape(host), map[string]any{"registration": "approve"}, nil)
}

// startWebhookConnect asks Stator to connect Armature in one step as c.
func startWebhookConnect(t *testing.T, c *client) *url.URL {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.api.srv.URL+"/api/v1/armature/connection/webhook/connect", nil)
	return hop(t, noFollow(c), req)
}

func onSettings(t *testing.T, landed *url.URL, want string) {
	t.Helper()
	if landed.Path != "/admin/armature" || landed.Query().Get("armature") != want {
		t.Errorf("landed on %s, want Admin, Armature saying %s", landed, want)
	}
}

type stubHook struct {
	ClientID string   `json:"clientId"`
	URL      string   `json:"url"`
	Secret   string   `json:"secret"`
	Topics   []string `json:"topics"`
	Version  int      `json:"version"`
	Enabled  bool     `json:"enabled"`
}

// stubHook is the webhook the stub keeps for the app, nil when it has none.
func hookOf(t *testing.T, clientID string) *stubHook {
	t.Helper()
	var out struct {
		Webhooks []stubHook `json:"webhooks"`
	}
	oauthControl(t, http.MethodGet, "/_stub-oauth/webhooks", nil, &out)
	for _, h := range out.Webhooks {
		if h.ClientID == clientID {
			return &h
		}
	}
	return nil
}

func partnerOf(t *testing.T, c *client) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection", "partnerWebhook")
}

// deliverWith posts a signed issue.updated as the stub's Armature, answering
// the receiver's status.
func deliverWith(t *testing.T, api *apiServer, installation uuid.UUID, secret string) int {
	t.Helper()
	body := delivery{ID: uuid.New(), Topic: "issue.updated", OrgID: installation, Version: armature.WebhookVersion, Payload: map[string]any{"key": "CP-1"}}.body(t)
	return postWebhookWith(t, api, "", body, timestamped(body, secret, time.Now())).Status
}

func TestAnAdministratorConnectsArmatureInOneStep(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-one-step")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	offerPartnerWebhooks(t, h)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect Armature")
	if got := partnerOf(t, owner); got["offered"] != true || got["connected"] != false || got["manageable"] != false || got["url"] != "http://stator.test"+armature.WebhookPath {
		t.Errorf("before the one-step connect the webhook is %v", got)
	}
	want(t, bob.get(t, "/api/v1/armature/connection/webhook/connect"), http.StatusForbidden, "a member connecting Armature for the organization")

	authorize := startWebhookConnect(t, owner)
	if got := authorize.Query().Get("scope"); got != "read issue.write webhooks.partner" {
		t.Fatalf("the administrator is asked for %q, want every scope at once", got)
	}
	onSettings(t, finish(t, owner, consentAs(t, owner, authorize, slug, "admin", "allow")), "connected")
	h.settle(t)
	conn := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection")
	app := obj(t, response{Body: conn}, "app")
	partner := obj(t, response{Body: conn}, "partnerWebhook")
	if conn["webhookSecretSet"] != true || partner["connected"] != true || partner["manageable"] != true || partner["refusal"] != nil || app["status"] != "active" {
		t.Fatalf("after the one-step connect the connection is %v", conn)
	}
	clientID := app["clientId"].(string)
	hook := hookOf(t, clientID)
	if hook == nil || hook.URL != "http://stator.test"+armature.WebhookPath || hook.Version != armature.WebhookVersion || !hook.Enabled ||
		strings.Join(hook.Topics, ",") != strings.Join(slices.Sorted(slices.Values(armature.WebhookTopics)), ",") {
		t.Fatalf("Armature keeps the webhook %+v", hook)
	}
	if strings.Contains(string(want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection").Raw), hook.Secret) {
		t.Error("the webhook secret is answered")
	}
	if account := obj(t, want(t, owner.get(t, "/api/v1/armature/account"), http.StatusOK, "the owner's account"), "account"); account["via"] != "app" ||
		!slices.Contains(account["app"].(map[string]any)["scopes"].([]any), any(armature.ScopeWebhooks)) {
		t.Errorf("the owner's own account after the one-step connect is %v", account)
	}
	installation := uuid.MustParse(conn["installationId"].(string))
	if status := deliverWith(t, api, installation, hook.Secret); status != http.StatusNoContent {
		t.Errorf("a delivery signed with Armature's secret answered %d", status)
	}

	// Armature's consent page refuses webhooks.partner to somebody who does
	// not administer it, once the app is allowed, so nothing comes back.
	refused := consentAs(t, owner, startWebhookConnect(t, owner), slug, "alice", "allow")
	said, _ := io.ReadAll(refused.Body)
	_ = refused.Body.Close()
	if refused.StatusCode != http.StatusOK || !strings.Contains(string(said), "needs an Armature administrator") {
		t.Errorf("a member allowing webhooks.partner was answered %d: %s", refused.StatusCode, said)
	}

	// Connecting again finds the webhook Armature has and gives it a new secret.
	onSettings(t, finish(t, owner, consentAs(t, owner, startWebhookConnect(t, owner), slug, "admin", "allow")), "connected")
	again := hookOf(t, clientID)
	if again == nil || again.Secret == hook.Secret {
		t.Fatalf("connecting again left the webhook %+v", again)
	}
	if deliverWith(t, api, installation, hook.Secret) == http.StatusNoContent || deliverWith(t, api, installation, again.Secret) != http.StatusNoContent {
		t.Error("the receiver does not hold to the secret Armature answered last")
	}

	// The owner connecting their own account later asks for no less.
	if got := startConnect(t, owner, "").Query().Get("scope"); got != armature.AdminScopes {
		t.Errorf("an administrator's own connect asks for %q, which would drop webhooks.partner", got)
	}

	rotated := obj(t, want(t, owner.post(t, "/api/v1/armature/connection/webhook/rotate-secret", nil), http.StatusOK, "rotate the secret"), "connection")
	if third := hookOf(t, clientID); third == nil || third.Secret == again.Secret || rotated["webhookSecretSet"] != true {
		t.Errorf("rotating left %+v", third)
	}
	test := obj(t, want(t, owner.post(t, "/api/v1/armature/connection/webhook/test", nil), http.StatusOK, "test the webhook"), "test")
	if test["status"] != nil || test["error"] == nil {
		t.Errorf("a ping to an address the stub cannot reach answered %v", test)
	}

	// An Armature administrator removed it there: rotating makes it again.
	oauthControl(t, http.MethodDelete, "/_stub-oauth/webhooks/"+clientID, nil, nil)
	want(t, owner.post(t, "/api/v1/armature/connection/webhook/rotate-secret", nil), http.StatusOK, "rotate a webhook Armature lost")
	if hookOf(t, clientID) == nil {
		t.Error("a webhook Armature lost was not made again")
	}

	// A new address removes it from the Armature being left.
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "move to another Armature")
	if hookOf(t, clientID) != nil {
		t.Error("the webhook stayed in the Armature Stator left")
	}
	if got := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the moved connection"), "connection"); got["webhookSecretSet"] != false ||
		got["partnerWebhook"].(map[string]any)["connected"] != false {
		t.Errorf("after the move the connection keeps %v", got)
	}
}

func TestArmaturesRefusalsOfTheOneStepConnectAreSaid(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-one-step-refused")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	other := api.as(t, h.addPerson(t, "admin"))
	host := strings.TrimPrefix(connectedAppURL(h), "http://")

	// An Armature with connected apps but no partner webhooks keeps the pasted secret.
	offerConnectedApps(t, h, "approve")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect Armature")
	if got := partnerOf(t, owner); got["offered"] != false {
		t.Errorf("an Armature without webhooks.partner@1 offers the one-step connect: %v", got)
	}
	onSettings(t, startWebhookConnect(t, owner), "unavailable")

	// The settings page asks Armature afresh, which is how an upgrade shows.
	offerPartnerWebhooks(t, h)
	if got := partnerOf(t, owner); got["offered"] != true {
		t.Fatalf("an upgraded Armature does not offer the one-step connect: %v", got)
	}
	oauthControl(t, http.MethodPut, "/_stub-oauth/hosts/"+url.PathEscape(host)+"/outbound", map[string]any{"refuse": "outbound_refused"}, nil)
	onSettings(t, finish(t, owner, consentAs(t, owner, startWebhookConnect(t, owner), slug, "admin", "allow")), "webhook_refused")
	h.settle(t)
	partner := partnerOf(t, owner)
	if refusal, _ := partner["refusal"].(string); !strings.Contains(refusal, "Add stator.test to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)") || partner["connected"] != false {
		t.Errorf("the outbound guard's refusal reads %v", partner)
	}
	rotate := want(t, owner.post(t, "/api/v1/armature/connection/webhook/rotate-secret", nil), http.StatusUnprocessableEntity, "rotate while Armature cannot reach Stator")
	if code := errorCode(t, rotate); code != "armature_webhook_refused" || !strings.Contains(string(rotate.Raw), "Armature cannot reach Stator") {
		t.Errorf("a rotate the guard refuses answers %s", rotate.Raw)
	}
	oauthControl(t, http.MethodPut, "/_stub-oauth/hosts/"+url.PathEscape(host)+"/outbound", map[string]any{"refuse": ""}, nil)
	want(t, owner.post(t, "/api/v1/armature/connection/webhook/rotate-secret", nil), http.StatusOK, "rotate once Armature may reach Stator")
	if got := partnerOf(t, owner); got["refusal"] != nil || got["connected"] != true {
		t.Errorf("once made the webhook reads %v", got)
	}

	// Another administrator, whose own grant does not hold webhooks.partner.
	if code := errorCode(t, want(t, other.post(t, "/api/v1/armature/connection/webhook/test", nil), http.StatusConflict, "test without the scope")); code != "armature_webhook_admin" {
		t.Errorf("a test without webhooks.partner answers %s", code)
	}
	if got := partnerOf(t, other); got["manageable"] != false || got["connected"] != true {
		t.Errorf("another administrator reads %v", got)
	}

	// Disconnecting removes the webhook from Armature.
	clientID := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the connection"), "connection", "app")["clientId"].(string)
	want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "disconnect Armature")
	if hookOf(t, clientID) != nil {
		t.Error("the webhook outlived the disconnect")
	}
}

// Straight through SQL as stator_app: only an administrator starts the trip
// that writes the webhook secret, and a webhook made by Stator has one.
func TestTheOneStepConnectIsWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-one-step-wall")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	offerPartnerWebhooks(t, h)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": connectedAppURL(h)}), http.StatusOK, "connect Armature")
	h.settle(t)

	conn := appConn(t)
	actAs(t, conn, bobID)
	denied(t, conn, "a member starting the administrator's trip", `INSERT INTO armature_oauth_attempt (state_digest, user_id, session_id, code_verifier, expires_at, purpose)
		VALUES ('\x02', $1, $2, 'v', now() + interval '1 hour', 'webhook')`, bobID, uuid.New())
	if _, err := conn.Exec(context.Background(), `INSERT INTO armature_oauth_attempt (state_digest, user_id, session_id, code_verifier, expires_at)
		VALUES ('\x03', $1, $2, 'v', now() + interval '1 hour')`, bobID, uuid.New()); err != nil {
		t.Errorf("a member could not start their own trip: %v", err)
	}
	actAs(t, conn, home.user)
	denied(t, conn, "marking a webhook as Stator's without its secret", `UPDATE armature_connection SET webhook_secret = NULL, webhook_partner = true`)
	denied(t, conn, "a trip for something Stator does not know", `INSERT INTO armature_oauth_attempt (state_digest, user_id, session_id, code_verifier, expires_at, purpose)
		VALUES ('\x04', $1, $2, 'v', now() + interval '1 hour', 'elsewhere')`, home.user, uuid.New())
}
