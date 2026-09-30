//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Connecting Armature (#27) against the armature-stub, the one stand-in the
// suite has, which a contract test holds to Armature's own document.

const (
	// envArmature is the stub as a browser opens it, which is the base URL an
	// administrator enters; envArmatureStub is where this process reaches it.
	envArmature     = "STATOR_TEST_ARMATURE_URL"
	envArmatureStub = "STATOR_TEST_ARMATURE_STUB_URL"
	// armatureStubHost is the one host inside the network the guard lets through.
	armatureStubHost = "armature-stub"
	webhookSecret    = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	armatureAppURL   = "http://stator.test"
)

var testSecretKey = bytes.Repeat([]byte{7}, secret.KeySize)

func armatureURL(t *testing.T) string {
	t.Helper()
	if os.Getenv(envArmature) == "" || os.Getenv(envArmatureStub) == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envArmature)
	}
	return os.Getenv(envArmature)
}

func (h *harness) valkey(t *testing.T) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// armature is the service as cmd/api builds it, over the stack's stub.
func (h *harness) armature(t *testing.T) *armature.Service {
	t.Helper()
	box, err := secret.New(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(armatureStubHost)
	client := armature.NewClient(allow, map[string]string{armatureURL(t): os.Getenv(envArmatureStub)})
	return armature.NewService(h.cluster, box, client, armature.NewCache(h.valkey(t), discard()),
		armature.Options{AppURL: armatureAppURL, Allow: allow, Development: true, Log: discard()})
}

func patFor(tenant, person string) string { return armature.TokenPrefix + tenant + "_" + person }

func fieldError(t *testing.T, r response, field string) string {
	t.Helper()
	if code := errorCode(t, r); code != "validation_failed" {
		t.Fatalf("code %s, want validation_failed: %s", code, r.Raw)
	}
	msg, _ := obj(t, r, "error", "fields")[field].(string)
	if msg == "" {
		t.Fatalf("no sentence on %s: %s", field, r.Raw)
	}
	return msg
}

func TestAnAdministratorConnectsArmatureAndMembersTheirTokens(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-connect")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	base := armatureURL(t)
	bobToken := patFor(slug, "bob")

	if got := want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "no connection yet"); got.Body["connection"] != nil {
		t.Errorf("a new organization has a connection: %s", got.Raw)
	}
	errorCode(t, want(t, bob.get(t, "/api/v1/armature/connection"), http.StatusForbidden, "a member reads the connection"))
	account := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "an account before any connection"), "account")
	if account["configured"] != false || account["status"] != "not_configured" || account["baseUrl"] != nil {
		t.Errorf("before a connection the account is %v", account)
	}
	if code := errorCode(t, want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusConflict, "a token before a connection")); code != "armature_not_configured" {
		t.Errorf("code %s", code)
	}

	for _, tc := range []struct {
		name, baseURL, slug, secret, field, says string
	}{
		{"an address that is no origin", base + "/api/v1", slug, "", "baseUrl", "without a path"},
		{"a host the guard does not let through", "http://valkey:6379", slug, "", "baseUrl", "STATOR_OUTBOUND_ALLOW"},
		{"an address inside the network", "http://10.11.12.13", slug, "", "baseUrl", "inside the server's own network"},
		{"a slug Armature never makes", base, "Not A Slug", "", "orgSlug", "slug"},
		{"a secret Armature never shows", base, slug, "hunter2", "webhookSecret", armature.WebhookSecretPrefix},
	} {
		body := map[string]any{"baseUrl": tc.baseURL, "orgSlug": tc.slug}
		if tc.secret != "" {
			body["webhookSecret"] = tc.secret
		}
		if msg := fieldError(t, want(t, owner.put(t, "/api/v1/armature/connection", body), http.StatusUnprocessableEntity, tc.name), tc.field); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	errorCode(t, want(t, bob.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "orgSlug": slug}), http.StatusForbidden, "a member connects Armature"))

	saved := want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base + "/", "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	conn := obj(t, saved, "connection")
	if conn["baseUrl"] != base || conn["orgSlug"] != slug || conn["webhookSecretSet"] != true || conn["armatureOrgId"] != nil {
		t.Errorf("the connection is %v", conn)
	}
	if conn["webhookUrl"] != armatureAppURL+"/api/v1/armature/webhook/"+slug || len(conn["webhookTopics"].([]any)) != len(armature.WebhookTopics) {
		t.Errorf("the webhook settings are %v %v", conn["webhookUrl"], conn["webhookTopics"])
	}
	if bytes.Contains(saved.Raw, []byte(webhookSecret)) {
		t.Error("the webhook secret was answered")
	}
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT webhook_secret FROM armature_connection WHERE org_id = $1`, home.org).Scan(&sealed); err != nil || bytes.Contains(sealed, []byte(webhookSecret)) || len(sealed) == 0 {
		t.Errorf("the webhook secret is not sealed at rest: %v", err)
	}

	for _, tc := range []struct{ name, token, says string }{
		{"not an Armature token", "stator_pat_something", armature.TokenPrefix},
		{"a token Armature refuses", "armature_pat_revoked", "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here."},
		{"a token of another Armature organization", patFor("globex-corp", "bob"), "another Armature organization"},
	} {
		if msg := fieldError(t, want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": tc.token}), http.StatusUnprocessableEntity, tc.name), "token"); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	stored := want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects")
	account = obj(t, stored, "account")
	if account["configured"] != true || account["connected"] != true || account["status"] != "ok" || account["baseUrl"] != base || account["checkedAt"] == nil {
		t.Errorf("after connecting the account is %v", account)
	}
	if name := obj(t, stored, "account", "user")["name"]; name != "Bob" {
		t.Errorf("the token acts as %v, want Bob", name)
	}
	if bytes.Contains(stored.Raw, []byte(bobToken)) {
		t.Error("the token was answered")
	}
	var sealedToken []byte
	var tokenID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id, token FROM armature_token WHERE org_id = $1 AND user_id = $2`, home.org, bobID).Scan(&tokenID, &sealedToken); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedToken, []byte(bobToken)) || bytes.Contains(sealedToken, []byte("_bob")) {
		t.Error("the token is readable at rest")
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_connection WHERE org_id = $1 AND armature_org_id IS NOT NULL`, home.org); n != 1 {
		t.Error("the Armature organization's id was not learned from the first token")
	}

	got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob reads his account"), "account")
	if got["status"] != "ok" || got["connected"] != true {
		t.Errorf("bob reads %v", got)
	}
	checked := obj(t, want(t, bob.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "bob checks his token"), "account")
	if checked["status"] != "ok" {
		t.Errorf("a check answered %v", checked)
	}
	if n := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the owner reads"), "connection")["connected"]; n != float64(1) {
		t.Errorf("connected = %v, want 1", n)
	}

	// A new token is a new row, so nothing cached for the old one is read for it.
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects again")
	var again uuid.UUID
	_ = h.super.QueryRow(context.Background(), `SELECT id FROM armature_token WHERE org_id = $1 AND user_id = $2`, home.org, bobID).Scan(&again)
	if again == tokenID {
		t.Error("storing a token again kept the row id that keys the cache")
	}

	// A read-only Stator token reads the account and changes nothing.
	_, readOnly := makeToken(t, bob, map[string]any{"name": "armature-reader", "scopes": []string{"read"}})
	want(t, api.withToken(readOnly).get(t, "/api/v1/armature/account"), http.StatusOK, "a read-only token reads")
	errorCode(t, want(t, api.withToken(readOnly).post(t, "/api/v1/armature/account/check", nil), http.StatusForbidden, "a read-only token checks"))
	errorCode(t, want(t, api.withToken(readOnly).delete(t, "/api/v1/armature/account/token"), http.StatusForbidden, "a read-only token disconnects"))

	// Only the secret changes: the tokens stay.
	kept := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "orgSlug": slug, "webhookSecret": ""}), http.StatusOK, "remove the secret"), "connection")
	if kept["webhookSecretSet"] != false || kept["connected"] != float64(1) || kept["armatureOrgId"] == nil {
		t.Errorf("removing the secret changed more: %v", kept)
	}
	// Another address, even of the same Armature, forgets every token.
	moved := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": os.Getenv(envArmatureStub), "orgSlug": slug}), http.StatusOK, "move the connection"), "connection")
	if moved["connected"] != float64(0) || moved["armatureOrgId"] != nil {
		t.Errorf("a new address kept the tokens: %v", moved)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after the move"), "account"); got["connected"] != false || got["status"] != "not_connected" {
		t.Errorf("bob is still connected after the move: %v", got)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2 AND data->>'tokensForgotten' = '1'`, home.org, armature.ActionConnectionSaved); n != 1 {
		t.Errorf("the move is audited %d times with the forgotten token", n)
	}

	want(t, bob.delete(t, "/api/v1/armature/account/token"), http.StatusNoContent, "disconnect with no token")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects to the new address")
	want(t, bob.delete(t, "/api/v1/armature/account/token"), http.StatusNoContent, "bob disconnects")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
		t.Errorf("%d tokens are left after disconnecting", n)
	}

	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects before the removal")
	errorCode(t, want(t, bob.delete(t, "/api/v1/armature/connection"), http.StatusForbidden, "a member removes the connection"))
	want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "remove the connection")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
		t.Errorf("%d tokens outlived the connection", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2`, home.org, armature.ActionConnectionRemoved); n != 1 {
		t.Errorf("the removal is audited %d times", n)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after the removal"), "account"); got["status"] != "not_configured" {
		t.Errorf("bob after the removal: %v", got)
	}
	errorCode(t, want(t, api.anonymous().get(t, "/api/v1/armature/account"), http.StatusUnauthorized, "nobody reads an account"))
}

// Armature not answering is 502, never a 401 that would read as a sign-in to
// Stator, and a check records it without dropping the token.
func TestAnArmatureThatDoesNotAnswer(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-silent")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "connect a token")

	// Behind the service's back: the same host on a port where nothing listens.
	if _, err := h.super.Exec(context.Background(), `
		ALTER TABLE armature_connection DISABLE TRIGGER armature_connection_moved;
		UPDATE armature_connection SET base_url = 'http://armature-stub:9' WHERE org_id = '`+home.org.String()+`';
		ALTER TABLE armature_connection ENABLE TRIGGER armature_connection_moved;`); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if code := errorCode(t, want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusBadGateway, "a token while Armature is down")); code != "armature_unreachable" {
		t.Errorf("code %s", code)
	}
	checked := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "a check while Armature is down"), "account")
	if checked["status"] != "unreachable" || checked["connected"] != true {
		t.Errorf("the check is %v", checked)
	}
}

// Revoked in Armature, a stored token is marked rejected by a check and not
// used again until its owner stores a new one or a check finds it working.
func TestACheckFindsARevokedToken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-revoked")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "connect a token")
	service := h.armature(t)
	h.settle(t)
	if v, status, err := service.Viewer(owner.ctx); err != nil || status != armature.StatusOK || v.Caller.BaseURL() != armatureURL(t) {
		t.Fatalf("the viewer is %v %s %v", v, status, err)
	}

	box, _ := secret.New(testSecretKey)
	bound := append(append([]byte("armature.token:"), home.org[:]...), home.user[:]...)
	revoked, _ := box.Seal([]byte("armature_pat_revoked"), bound)
	if _, err := h.super.Exec(owner.ctx, `UPDATE armature_token SET token = $2 WHERE user_id = $1`, home.user, revoked); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if got := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "check a revoked token"), "account"); got["status"] != "rejected" {
		t.Errorf("a revoked token checks as %v", got["status"])
	}
	h.settle(t)
	if _, status, _ := service.Viewer(owner.ctx); status != armature.StatusRejected {
		t.Errorf("a rejected token is handed out: %s", status)
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "store a new token")
	h.settle(t)
	if _, status, _ := service.Viewer(owner.ctx); status != armature.StatusOK {
		t.Errorf("a new token is not used: %s", status)
	}
}

// Straight through SQL as stator_app: a member reads and changes only their own
// token, only administrators the connection, and nobody another organization's.
func TestArmatureRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-wall")
	other := h.makeMember(t, "arm-wall-other")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "alice connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
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

	t.Run("a member reads their own token and where Armature is, nothing more", func(t *testing.T) {
		actAs(t, conn, home.org, bobID)
		if n := count(`SELECT count(*) FROM armature_token`); n != 1 {
			t.Errorf("bob reads %d tokens, want his own", n)
		}
		if n := count(`SELECT count(*) FROM armature_connection`); n != 0 {
			t.Errorf("bob reads the connection with its sealed secret")
		}
		if n := count(`SELECT count(*) FROM armature_endpoint()`); n != 1 {
			t.Errorf("bob cannot learn where Armature is")
		}
		var connected *int
		_ = conn.QueryRow(ctx, `SELECT armature_connected_count()`).Scan(&connected)
		if connected != nil {
			t.Errorf("bob counts the connected members: %d", *connected)
		}
		untouched(t, conn, "pointing the connection elsewhere", `UPDATE armature_connection SET base_url = 'https://evil.example'`)
		untouched(t, conn, "removing the connection", `DELETE FROM armature_connection`)
		untouched(t, conn, "marking alice's token", `UPDATE armature_token SET status = 'rejected' WHERE user_id = $1`, home.user)
		untouched(t, conn, "removing alice's token", `DELETE FROM armature_token WHERE user_id = $1`, home.user)
		denied(t, conn, "storing a token for alice", `INSERT INTO armature_token (org_id, user_id, token, status) VALUES ($1, $2, '\x00', 'ok')`, home.org, home.user)
		denied(t, conn, "rewriting his token in place", `UPDATE armature_token SET token = '\x00' WHERE user_id = $1`, bobID)
		denied(t, conn, "choosing his row id", `INSERT INTO armature_token (org_id, user_id, id, token, status) VALUES ($1, $2, $3, '\x00', 'ok')`, home.org, bobID, uuid.New())
	})

	t.Run("an administrator keeps the connection and still reads only their own token", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		if n := count(`SELECT count(*) FROM armature_token`); n != 1 {
			t.Errorf("the owner reads %d tokens, want their own", n)
		}
		if n := count(`SELECT armature_connected_count()`); n != 2 {
			t.Errorf("the owner counts %d connected members, want 2", n)
		}
		denied(t, conn, "pinning the Armature organization", `UPDATE armature_connection SET armature_org_id = $1`, uuid.New())
		if _, err := conn.Exec(ctx, `UPDATE armature_connection SET base_url = 'https://elsewhere.example'`); err != nil {
			t.Fatalf("the owner moves the connection: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
			t.Errorf("raw SQL moved the connection and kept %d tokens", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_connection WHERE org_id = $1 AND armature_org_id IS NULL`, home.org); n != 1 {
			t.Error("the learned organization id survived the move")
		}
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM armature_connection WHERE org_id = $1`, home.org); n != 0 {
			t.Error("another organization reads the connection")
		}
		if n := count(`SELECT count(*) FROM armature_endpoint()`); n != 0 {
			t.Error("another organization learns where Armature is")
		}
		untouched(t, conn, "removing another organization's connection", `DELETE FROM armature_connection WHERE org_id = $1`, home.org)
		denied(t, conn, "connecting another organization", `INSERT INTO armature_connection (org_id, base_url, org_slug) VALUES ($1, 'https://evil.example', 'evil')`, home.org)
		denied(t, conn, "storing a token in another organization", `INSERT INTO armature_token (org_id, user_id, token, status) VALUES ($1, $2, '\x00', 'ok')`, home.org, other.user)
	})
}

// The cache keeps each answer per token row in Valkey, judges an entry's age
// itself, and keeps nothing at all without Valkey.
func TestTheCacheKeepsAnswersPerTokenRow(t *testing.T) {
	h := newHarness(t)
	store := h.valkey(t)
	cache := armature.NewCache(store, discard())
	ctx := context.Background()
	org, mine, theirs := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		keys, _ := store.Keys(ctx, armature.CachePrefix+org.String()+":*").Result()
		if len(keys) > 0 {
			store.Del(ctx, keys...)
		}
	})

	type issue struct{ Summary string }
	var got *issue
	if cache.Issue(ctx, org, mine, "CP-1", &got) {
		t.Fatal("a miss was a hit")
	}
	cache.PutIssue(ctx, org, mine, "cp-1", &issue{Summary: "Mine"})
	cache.PutIssue(ctx, org, theirs, "CP-1", nil)
	if !cache.Issue(ctx, org, mine, "CP-1", &got) || got == nil || got.Summary != "Mine" {
		t.Errorf("my answer is %v", got)
	}
	got = &issue{}
	if !cache.Issue(ctx, org, theirs, "CP-1", &got) || got != nil {
		t.Errorf("an issue somebody may not see is not kept as null: %v", got)
	}
	if ttl := store.TTL(ctx, armature.IssueKey(org, "CP-1")).Val(); ttl <= 0 || ttl > armature.IssueCacheTTL {
		t.Errorf("the hash lives %v", ttl)
	}
	stale, _ := json.Marshal(map[string]any{"at": time.Now().Add(-2 * armature.IssueCacheTTL), "value": map[string]any{"Summary": "Old"}})
	store.HSet(ctx, armature.IssueKey(org, "CP-2"), mine.String(), stale)
	if cache.Issue(ctx, org, mine, "CP-2", &got) {
		t.Error("a field older than the TTL was served")
	}
	cache.ForgetIssues(ctx, org, "CP-1")
	if cache.Issue(ctx, org, mine, "CP-1", &got) {
		t.Error("a forgotten issue was served")
	}

	var rows []string
	cache.PutSearch(ctx, org, mine, "project = CP", 20, 0, []string{"CP-1"})
	if !cache.Search(ctx, org, mine, "project = CP", 20, 0, &rows) || len(rows) != 1 {
		t.Errorf("the search answer is %v", rows)
	}
	if cache.Search(ctx, org, theirs, "project = CP", 20, 0, &rows) || cache.Search(ctx, org, mine, "project = CP", 20, 20, &rows) {
		t.Error("a search answer was read for another person or page")
	}
	cache.ForgetSearches(ctx, org)
	if cache.Search(ctx, org, mine, "project = CP", 20, 0, &rows) {
		t.Error("a forgotten search was served")
	}

	var meta map[string]any
	cache.PutMeta(ctx, org, mine, map[string]any{"projects": []string{"CP"}})
	if !cache.Meta(ctx, org, mine, &meta) || cache.Meta(ctx, org, theirs, &meta) {
		t.Error("meta is not kept per token row")
	}
	var followed *string
	cache.PutTheme(ctx, org, mine, nil)
	if !cache.Theme(ctx, org, mine, &followed) || followed != nil {
		t.Error("Armature's built-in theme is not kept as null")
	}

	event := uuid.New()
	if !cache.FirstDelivery(ctx, org, event) || cache.FirstDelivery(ctx, org, event) {
		t.Error("a webhook event is not remembered once")
	}

	none := armature.NewCache(nil, discard())
	none.PutIssue(ctx, org, mine, "CP-1", &issue{})
	if none.Issue(ctx, org, mine, "CP-1", &got) || !none.FirstDelivery(ctx, org, event) {
		t.Error("without Valkey something was kept")
	}
}
