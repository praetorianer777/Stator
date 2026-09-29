//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// makeToken asks for a token over the API and returns its id and secret.
func makeToken(t *testing.T, c *client, body map[string]any) (string, string) {
	t.Helper()
	made := obj(t, want(t, c.post(t, "/api/v1/tokens", body), http.StatusCreated, "make a token"), "token")
	secret, _ := made["secret"].(string)
	if !strings.HasPrefix(secret, auth.APITokenPrefix) {
		t.Fatalf("the new token's secret %q lacks the prefix", secret)
	}
	return made["id"].(string), secret
}

// lastUsed reads the column straight from the primary, where the api wrote it.
func (h *harness) lastUsed(t *testing.T, tokenID string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := h.super.QueryRow(context.Background(), `SELECT last_used_at FROM api_token WHERE id = $1`, tokenID).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestATokenActsAsItsOwnerUntilRevoked(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	m := h.makeMember(t, "token-owner")
	other := h.makeMember(t, "token-elsewhere")
	if _, err := h.super.Exec(context.Background(), `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, other.org, m.user); err != nil {
		t.Fatal(err)
	}
	browser := api.as(t, m.user, m.org, h.slugOf(t, m.org))

	id, secret := makeToken(t, browser, map[string]any{"name": "deploy"})

	var stored int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM api_token WHERE token_hash = sha256($1::bytea) AND id = $2`, []byte(secret), id).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("the row does not hold the SHA-256 of the whole token: %d, %v", stored, err)
	}
	var leaks int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM api_token t WHERE position($1 in t::text) > 0`, strings.TrimPrefix(secret, auth.APITokenPrefix)).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("the secret is stored in the clear: %d, %v", leaks, err)
	}

	listed := list(t, want(t, browser.get(t, "/api/v1/tokens"), http.StatusOK, "list tokens"), "tokens")
	if len(listed) != 1 || listed[0].(map[string]any)["secret"] != nil || listed[0].(map[string]any)["lastUsedAt"] != nil {
		t.Fatalf("the list shows the secret, or a use nobody made: %v", listed)
	}
	if h.lastUsed(t, id) != nil {
		t.Fatal("last_used_at is set before the token was used")
	}

	script := api.withToken(secret)
	me := want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "who the token is")
	if obj(t, me, "user")["id"] != m.user.String() || obj(t, me, "organization")["id"] != m.org.String() {
		t.Fatalf("the token is not its owner in its organization: %s", me.Raw)
	}
	if orgs := list(t, me, "organizations"); len(orgs) != 1 {
		t.Errorf("a token reaches only the organization it was made in, got %v", orgs)
	}
	if h.lastUsed(t, id) == nil {
		t.Error("using the token did not record last_used_at")
	}

	want(t, script.post(t, "/api/v1/themes", map[string]any{"name": "Made by a script"}), http.StatusCreated, "a token without scopes writes")
	if code := errorCode(t, want(t, script.post(t, "/api/v1/tokens", map[string]any{"name": "child"}), http.StatusForbidden, "a token making a token")); code != "session_only" {
		t.Errorf("a token making a token = %s", code)
	}
	if code := errorCode(t, want(t, script.post(t, "/api/v1/auth/switch-org", map[string]any{"slug": h.slugOf(t, other.org)}), http.StatusBadRequest, "a token switching")); code != "bad_request" {
		t.Errorf("a token switching organizations = %s", code)
	}

	want(t, browser.delete(t, "/api/v1/tokens/"+id), http.StatusNoContent, "revoke")
	if code := errorCode(t, want(t, script.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "a revoked token")); code != "unauthorized" {
		t.Errorf("a revoked token = %s", code)
	}
	want(t, browser.delete(t, "/api/v1/tokens/"+id), http.StatusNotFound, "revoke twice")
	want(t, browser.delete(t, "/api/v1/tokens/not-an-id"), http.StatusBadRequest, "revoke nonsense")

	var audited int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE org_id = $1 AND target_id = $2 AND action IN ('token.created', 'token.revoked')`, m.org, id).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("making and revoking the token left %d audit entries (%v), want 2", audited, err)
	}
}

func TestANewTokenIsRefusedWhatItCannotBe(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	m := h.makeMember(t, "token-refusals")
	browser := api.as(t, m.user, m.org, h.slugOf(t, m.org))

	for field, body := range map[string]map[string]any{
		"name":      {"name": "  "},
		"scopes":    {"name": "deploy", "scopes": []string{"write"}},
		"expiresAt": {"name": "deploy", "expiresAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
	} {
		got := want(t, browser.post(t, "/api/v1/tokens", body), http.StatusUnprocessableEntity, "a token with a bad "+field)
		errorCode(t, got)
		if fields, _ := obj(t, got, "error")["fields"].(map[string]any); fields[field] == nil {
			t.Errorf("the refusal does not name %s: %s", field, got.Raw)
		}
	}
	want(t, api.anonymous().post(t, "/api/v1/tokens", map[string]any{"name": "deploy"}), http.StatusUnauthorized, "nobody making a token")
	want(t, api.anonymous().get(t, "/api/v1/tokens"), http.StatusUnauthorized, "nobody listing tokens")
	want(t, api.anonymous().delete(t, "/api/v1/tokens/"+uuid.NewString()), http.StatusUnauthorized, "nobody revoking")
	want(t, api.withToken(auth.APITokenPrefix+"not-a-token").get(t, "/api/v1/tokens"), http.StatusUnauthorized, "a malformed token")
}

func TestAReadTokenReadsAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	m := h.makeMember(t, "token-reader")
	browser := api.as(t, m.user, m.org, h.slugOf(t, m.org))
	id, secret := makeToken(t, browser, map[string]any{"name": "reporting", "scopes": []string{"read"}})
	script := api.withToken(secret)

	want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "a read token reads who it is")
	want(t, script.get(t, "/api/v1/themes"), http.StatusOK, "a read token lists themes")
	scopes := list(t, want(t, script.get(t, "/api/v1/tokens"), http.StatusOK, "a read token lists tokens"), "tokens")[0].(map[string]any)["scopes"]
	if s, _ := scopes.([]any); len(s) != 1 || s[0] != "read" {
		t.Errorf("the token's scopes are %v, want [read]", scopes)
	}

	for _, attempt := range []response{
		script.post(t, "/api/v1/themes", map[string]any{"name": "Should not exist"}),
		script.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil, "builtIn": true}),
		script.delete(t, "/api/v1/tokens/"+id),
		script.post(t, "/api/v1/auth/logout", nil),
	} {
		if attempt.Status != http.StatusForbidden || errorCode(t, attempt) != "read_only_token" {
			t.Errorf("a read token wrote: %d %s", attempt.Status, attempt.Raw)
		}
	}
	if n := h.count(t, m.ctx, `SELECT count(*) FROM theme`); n != 0 {
		t.Errorf("a read token made %d themes", n)
	}
	want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "the token survives its own attempt to revoke itself")
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	m := h.makeMember(t, "token-expiry")
	browser := api.as(t, m.user, m.org, h.slugOf(t, m.org))
	id, secret := makeToken(t, browser, map[string]any{"name": "short lived", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	script := api.withToken(secret)
	want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "before it expires")

	if _, err := h.super.Exec(context.Background(), `UPDATE api_token SET expires_at = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	want(t, script.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "after it expired")
	expired := list(t, want(t, browser.get(t, "/api/v1/tokens"), http.StatusOK, "list with an expired token"), "tokens")
	if len(expired) != 1 || expired[0].(map[string]any)["expiresAt"] == nil {
		t.Errorf("an expired token is still listed, with its expiry, until revoked: %v", expired)
	}
}

func TestAdministratorsSeeAndRevokeEveryToken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := h.makeMember(t, "token-admin")
	slug := h.slugOf(t, owner.org)
	memberID := h.addPerson(t, owner.org, "member")
	admin := api.as(t, owner.user, owner.org, slug)
	member := api.as(t, memberID, owner.org, slug)

	id, secret := makeToken(t, member, map[string]any{"name": "member's script"})
	all := list(t, want(t, admin.get(t, "/api/v1/org/tokens"), http.StatusOK, "the organization's tokens"), "tokens")
	if len(all) != 1 || all[0].(map[string]any)["owner"].(map[string]any)["id"] != memberID.String() || all[0].(map[string]any)["secret"] != nil {
		t.Fatalf("the administrator does not see whose token it is, or sees its secret: %v", all)
	}
	errorCode(t, want(t, member.get(t, "/api/v1/org/tokens"), http.StatusForbidden, "a member listing everybody's tokens"))
	errorCode(t, want(t, member.delete(t, "/api/v1/org/tokens/"+id), http.StatusForbidden, "a member revoking as an administrator"))
	errorCode(t, want(t, admin.delete(t, "/api/v1/tokens/"+id), http.StatusNotFound, "an administrator revoking somebody's token as their own"))

	want(t, admin.delete(t, "/api/v1/org/tokens/"+id), http.StatusNoContent, "the administrator revokes it")
	want(t, api.withToken(secret).get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "the revoked token")
	want(t, admin.delete(t, "/api/v1/org/tokens/"+id), http.StatusNotFound, "revoke twice")

	// Leaving the organization takes a person's tokens there with them.
	_, kept := makeToken(t, member, map[string]any{"name": "outlives nothing"})
	if _, err := h.super.Exec(context.Background(), `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, owner.org, memberID); err != nil {
		t.Fatal(err)
	}
	want(t, api.withToken(kept).get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "a token of somebody who left")
	var left int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM api_token WHERE user_id = $1`, memberID).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d tokens outlived their owner's membership (%v)", left, err)
	}
}

// The service refusing is not proof: straight through SQL as stator_app, a
// tenant can neither see, change nor plant another tenant's tokens.
func TestTokenRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "token-wall-a")
	b := h.makeMember(t, "token-wall-b")
	idA, _ := makeToken(t, api.as(t, a.user, a.org, h.slugOf(t, a.org)), map[string]any{"name": "a's"})
	makeToken(t, api.as(t, b.user, b.org, h.slugOf(t, b.org)), map[string]any{"name": "b's"})

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	if got := count(`SELECT count(*) FROM api_token`); got != 0 {
		t.Errorf("an unscoped connection sees %d tokens", got)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, b.org.String()); err != nil {
		t.Fatal(err)
	}
	if got := count(`SELECT count(*) FROM api_token`); got != 1 {
		t.Errorf("organization B sees %d tokens, want its own one", got)
	}
	if got := count(`SELECT count(*) FROM api_token WHERE id = $1 OR org_id = $2`, idA, a.org); got != 0 {
		t.Errorf("organization B sees %d of A's tokens", got)
	}
	tag, err := conn.Exec(ctx, `UPDATE api_token SET expires_at = NULL, scopes = '{}', org_id = $2 WHERE id = $1`, idA, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("organization B changed A's token: %v rows, %v", tag.RowsAffected(), err)
	}
	tag, err = conn.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, idA)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("organization B revoked A's token: %v rows, %v", tag.RowsAffected(), err)
	}
	_, digest, _ := auth.GenerateAPIToken()
	if _, err := conn.Exec(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash) VALUES ($1, $2, 'planted', $3)`, a.org, a.user, digest); !isPolicyViolation(err) {
		t.Errorf("organization B planted a token in A: %v", err)
	}
	// Within its own organization, B still cannot make a token for somebody
	// who is not a member there, nor one that carries a scope nobody enforces.
	if _, err := conn.Exec(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash) VALUES ($1, $2, 'stranger', $3)`, b.org, a.user, digest); err == nil {
		t.Error("B made a token for a person who is not its member")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes) VALUES ($1, $2, 'root', $3, '{admin}')`, b.org, b.user, digest); err == nil {
		t.Error("B made a token with a scope nobody enforces")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash) VALUES ($1, $2, 'clear', 'stator_pat_in_the_clear')`, b.org, b.user); err == nil {
		t.Error("B stored something other than a SHA-256")
	}

	if got := h.count(t, a.ctx, `SELECT count(*) FROM api_token WHERE id = $1 AND expires_at IS NULL AND org_id = $2`, idA, a.org); got != 1 {
		t.Errorf("A's token is not as A left it")
	}
}
