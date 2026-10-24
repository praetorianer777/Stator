//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// toolNames lists what MCP offers a token.
func toolNames(t *testing.T, api *apiServer, token string) map[string]bool {
	t.Helper()
	status, _, out := mcpCall(t, api, token, "tools/list", nil)
	result, _ := out["result"].(map[string]any)
	if status != http.StatusOK || result == nil {
		t.Fatalf("tools/list answered %d %v", status, out)
	}
	names := map[string]bool{}
	for _, each := range result["tools"].([]any) {
		names[each.(map[string]any)["name"].(string)] = true
	}
	return names
}

// grantOf is the id of a person's grant for an app.
func grantOf(t *testing.T, h *harness, user uuid.UUID, app selfApp) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM api_token WHERE user_id = $1 AND client_id = $2`, user, app.id).Scan(&id); err != nil {
		t.Fatalf("no grant of %s for %s: %v", app.id, user, err)
	}
	return id
}

// A consented grant does what the person allowed and no more: reading only,
// or in the spaces chosen; the database holds it there too.
func TestAConsentedGrantIsHeldToWhatThePersonAllowed(t *testing.T) {
	w := newConsentWorld(t, "consent-limits")
	h, api, admin, alice := w.h, w.api, w.admin, w.alice
	rotor := w.allowed(t, "Rotor", "https://rotor.example/oauth/callback", true)
	docs, team, secret := uniqueKey("DOC"), uniqueKey("TEAM"), uniqueKey("SEC")
	for _, key := range []string{docs, team} {
		want(t, admin.post(t, httpapi.APIPrefix+"/spaces", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	}
	secretID := idOf(t, want(t, admin.post(t, httpapi.APIPrefix+"/spaces", map[string]any{"key": secret, "name": secret}), http.StatusCreated, "make "+secret), "space")
	h.closeSpace(t, secretID)

	t.Run("a grant allowed to read writes nothing through MCP", func(t *testing.T) {
		got := connect(t, h, api, alice, rotor, map[string]any{"scope": "read"})
		if got.str("scope") != "read" {
			t.Fatalf("the narrowed grant holds %q", got.str("scope"))
		}
		token := got.str("access_token")
		if names := toolNames(t, api, token); names["create_page"] || !names["list_spaces"] {
			t.Errorf("a reading grant is offered %v", names)
		}
		res := toolResult(t, api, token, "create_page", map[string]any{"parentId": uuid.NewString(), "title": "No"})
		if res["isError"] != true {
			t.Errorf("a reading grant called create_page: %v", res)
		}
	})

	t.Run("a grant limited to a space reaches that space alone", func(t *testing.T) {
		got := connect(t, h, api, alice, rotor, map[string]any{"spaces": []string{strings.ToLower(docs)}})
		res := toolResult(t, api, got.str("access_token"), "list_spaces", map[string]any{})
		listed := fmt.Sprint(res["structuredContent"])
		if res["isError"] == true || !strings.Contains(listed, docs) || strings.Contains(listed, team) {
			t.Errorf("the limited grant lists %s", listed)
		}
		// Straight through SQL as stator_app, alice cannot widen her grant
		// nor free it of MCP, and the token's spaces bound what she reads.
		grant := grantOf(t, h, w.aliceID, rotor)
		conn := appConn(t)
		actAs(t, conn, w.aliceID)
		var teamID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM space WHERE key = $1`, team).Scan(&teamID); err != nil {
			t.Fatal(err)
		}
		denied(t, conn, "widening the grant", `INSERT INTO api_token_space (token_id, space_id) VALUES ($1, $2)`, grant, teamID)
		for what, sql := range map[string]string{
			"freeing it of MCP":  `UPDATE api_token SET mcp_only = false WHERE id = $1`,
			"freeing its spaces": `UPDATE api_token SET spaces_only = false WHERE id = $1`,
		} {
			if tag, err := conn.Exec(context.Background(), sql, grant); err == nil && tag.RowsAffected() != 0 {
				t.Errorf("%s changed %d rows", what, tag.RowsAffected())
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM api_token_space ts JOIN space s ON s.id = ts.space_id WHERE ts.token_id = $1 AND s.key = $2`, grant, docs); n != 1 {
			t.Errorf("the grant names %d rows for %s", n, docs)
		}
		if n := h.countRows(t, `SELECT count(*) FROM api_token WHERE id = $1 AND mcp_only AND spaces_only`, grant); n != 1 {
			t.Error("the grant is no longer for MCP alone, limited to its spaces")
		}
	})

	t.Run("a space the person cannot see is refused, not granted", func(t *testing.T) {
		_, challenge := pkce(t)
		got := decideConsent(t, alice, authorizeQuery(rotor, challenge, nil), "allow", map[string]any{"spaces": []string{secret}})
		if got.Status != http.StatusUnprocessableEntity || !strings.Contains(string(got.Raw), "spaces") {
			t.Errorf("a hidden space answered %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a partner's token from #461 still works on every route, a consented one only at MCP", func(t *testing.T) {
		made := want(t, admin.post(t, httpapi.APIPrefix+"/oauth/clients", map[string]any{"name": "Partner", "scopes": []string{"read", "write"},
			"keycloakClientId": "partner-" + uuid.NewString()[:8]}), http.StatusCreated, "register a partner")
		partner := obj(t, made, "client")["id"].(string)
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM oauth_client WHERE id = $1`, partner) })
		token, digest, _ := auth.GeneratePrefixed(auth.AccessTokenPrefix)
		_, refresh, _ := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at)
			VALUES ($1, 'Partner (connected app)', $2, '{}', now() + interval '1 day', $3, $4, now() + interval '1 hour')`,
			w.aliceID, digest, partner, refresh); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if got := api.withToken(token).get(t, httpapi.APIPrefix+"/auth/me"); got.Status != http.StatusOK {
			t.Errorf("a partner's token at /auth/me answered %d %s", got.Status, got.Raw)
		}
		consented := connect(t, h, api, alice, rotor, nil)
		if got := api.withToken(consented.str("access_token")).get(t, httpapi.APIPrefix+"/auth/me"); got.Status != http.StatusUnauthorized {
			t.Errorf("a consented token at /auth/me answered %d %s", got.Status, got.Raw)
		}
	})
}

// The database keeps an app nobody allowed from every code and grant, and
// lets administrators alone allow it.
func TestTheDatabaseKeepsAppsToWhatAnAdministratorAllowed(t *testing.T) {
	w := newConsentWorld(t, "consent-wall")
	h, ctx := w.h, context.Background()
	pending := seedApp(t, h, "Pending", "https://pending.example/cb", false)
	checkRefused := func(what, sql string, args ...any) {
		t.Helper()
		_, err := h.super.Exec(ctx, sql, args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Errorf("%s was not refused by the database: %v", what, err)
		}
	}
	checkRefused("a code for a pending app", `
		INSERT INTO oauth_code (code_hash, client_id, user_id, scopes, redirect_uri, challenge, expires_at)
		VALUES (sha256('c'::bytea), $1, $2, '{read}', 'https://pending.example/cb', 'x', now() + interval '1 minute')`, pending.id, w.aliceID)
	checkRefused("a grant for a pending app", `
		INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at, mcp_only)
		VALUES ($1, 'Pending (connected app)', sha256('a'::bytea), '{}', now() + interval '1 day', $2, sha256('r'::bytea), now() + interval '1 hour', true)`,
		w.aliceID, pending.id)
	checkRefused("a token for MCP alone without an app", `
		INSERT INTO api_token (user_id, name, token_hash, scopes, mcp_only) VALUES ($1, 'Mine', sha256('m'::bytea), '{}', true)`, w.aliceID)

	conn := appConn(t)
	actAs(t, conn, w.aliceID)
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM oauth_client`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a member reads %d apps: %v", n, err)
	}
	denied(t, conn, "reading the codes", `SELECT count(*) FROM oauth_code`)
	if tag, err := conn.Exec(ctx, `UPDATE oauth_client SET status = 'allowed' WHERE id = $1`, pending.id); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("a member allowing an app changed %d rows: %v", tag.RowsAffected(), err)
	}
	actAs(t, conn, w.ownerID)
	if tag, err := conn.Exec(ctx, `UPDATE oauth_client SET status = 'allowed' WHERE id = $1`, pending.id); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("an administrator allowing an app changed %d rows: %v", tag.RowsAffected(), err)
	}
	denied(t, conn, "changing an app's return addresses", `UPDATE oauth_client SET redirect_uris = '{https://evil.example/cb}' WHERE id = $1`, pending.id)
}

// The sweep removes apps nobody allowed in time and codes long expired.
func TestTheSweepRemovesWhatOAuthNoLongerNeeds(t *testing.T) {
	w := newConsentWorld(t, "consent-sweep")
	h, ctx := w.h, context.Background()
	fresh := seedApp(t, h, "Fresh", "https://fresh.example/cb", false)
	stale := seedApp(t, h, "Stale", "https://stale.example/cb", false)
	if _, err := h.super.Exec(ctx, `UPDATE oauth_client SET created_at = now() - interval '2 days' WHERE id = $1`, stale.id); err != nil {
		t.Fatal(err)
	}
	rotor := w.allowed(t, "Rotor", "https://rotor.example/cb", true)
	if _, err := h.super.Exec(ctx, `
		INSERT INTO oauth_code (code_hash, client_id, user_id, scopes, redirect_uri, challenge, expires_at)
		VALUES (sha256(gen_random_uuid()::text::bytea), $1, $2, '{read}', 'https://rotor.example/cb', 'x', now() - interval '2 days')`, rotor.id, w.aliceID); err != nil {
		t.Fatal(err)
	}
	if err := oauth.NewSweeper(h.cluster, discard()).Once(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{fresh.id: 1, stale.id: 0, rotor.id: 1} {
		if n := h.countRows(t, `SELECT count(*) FROM oauth_client WHERE id = $1`, id); n != want {
			t.Errorf("%s is there %d times after the sweep, want %d", id, n, want)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM oauth_code WHERE client_id = $1 AND expires_at < now() - interval '1 day'`, rotor.id); n != 0 {
		t.Errorf("%d long expired codes outlived the sweep", n)
	}
}
