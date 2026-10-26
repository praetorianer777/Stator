//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// A narrower token for one run (#499), shaped like Exciter's
// backend/test/oauth_narrow_test.go.

// narrowAs exchanges an access token of the app's own grant for a narrower
// one, with the form Rotor sends.
func narrowAs(t *testing.T, api *apiServer, app selfApp, subject string, extra url.Values) oauthAnswer {
	t.Helper()
	form := url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {oauth.TokenTypeAccess},
		"requested_token_type": {oauth.TokenTypeAccess}, "resource": {consentIssuer + "/api/v1/mcp"}}
	for k, v := range extra {
		form[k] = v
	}
	return appPost(t, api, "/oauth/token", app, form)
}

// wantRefusal holds a token endpoint answer to a refusal with its RFC code and a sentence.
func wantRefusal(t *testing.T, what string, got oauthAnswer, code string) {
	t.Helper()
	if got.Status != http.StatusBadRequest || got.str("error") != code || !strings.HasSuffix(got.str("error_description"), ".") {
		t.Errorf("%s answered %d %v, want 400 %s with a sentence", what, got.Status, got.Body, code)
	}
}

// writesPage says whether a token's create_page under parent went through.
func writesPage(t *testing.T, api *apiServer, token, parent string) bool {
	t.Helper()
	return toolResult(t, api, token, "create_page", map[string]any{"parentId": parent, "title": "Through a narrower token " + uuid.NewString()[:6]})["isError"] != true
}

// listsSpaces is what list_spaces answers a token, as text.
func listsSpaces(t *testing.T, api *apiServer, token string) string {
	t.Helper()
	res := toolResult(t, api, token, "list_spaces", map[string]any{})
	if res["isError"] == true {
		t.Fatalf("list_spaces refused: %v", res)
	}
	return fmt.Sprint(res["structuredContent"])
}

// An app trades an access token of its own grant for a narrower, shorter one
// for one run, and the grant keeps what its person allowed.
func TestANarrowerTokenLeavesItsGrantAsItWas(t *testing.T) {
	w := newConsentWorld(t, "narrow")
	h, api, admin, alice := w.h, w.api, w.admin, w.alice
	rotor := w.allowed(t, "Rotor", "https://rotor.example/oauth/callback", true)
	reader := w.allowed(t, "Reader", "https://reader.example/oauth/callback", true)
	docKey, teamKey, secret := uniqueKey("ND"), uniqueKey("NT"), uniqueKey("NS")
	docs, team := newTree(t, admin, docKey, docKey), newTree(t, admin, teamKey, teamKey)
	h.closeSpace(t, newTree(t, admin, secret, secret).space)

	tokens := connect(t, h, api, alice, rotor, nil)
	subject, refresh := tokens.str("access_token"), tokens.str("refresh_token")
	grant := grantOf(t, h, w.aliceID, rotor)

	var read string
	t.Run("a token narrowed to reading writes nothing, and the grant still writes", func(t *testing.T) {
		got := narrowAs(t, api, rotor, subject, url.Values{"scope": {"read"}, oauth.ParamExpiresIn: {"900"}})
		if got.Status != http.StatusOK || got.str("scope") != "read" || got.str("issued_token_type") != oauth.TokenTypeAccess ||
			got.Body["expires_in"] != float64(900) || got.str("token_type") != "Bearer" || got.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("the exchange answered %d %v", got.Status, got.Body)
		}
		if _, ok := got.Body["refresh_token"]; ok {
			t.Errorf("an exchange issued a refresh token: %v", got.Body)
		}
		read = got.str("access_token")
		if !strings.HasPrefix(read, "stator_oat_") || read == subject {
			t.Fatalf("the narrower token is %q", read)
		}
		if names := toolNames(t, api, read); names["create_page"] || !names["list_spaces"] {
			t.Errorf("a narrower reading token is offered %v", names)
		}
		if writesPage(t, api, read, docs.homeID) {
			t.Error("a token narrowed to reading wrote")
		}
		if !writesPage(t, api, subject, docs.homeID) {
			t.Error("the grant no longer writes after an exchange narrowed a token of it")
		}
		wantRefusal(t, "a narrower token as the subject", narrowAs(t, api, rotor, read, nil), "invalid_grant")
		if n := h.countRows(t, `SELECT count(*) FROM api_token WHERE id = $1 AND scopes = '{}' AND NOT spaces_only`, grant); n != 1 {
			t.Error("the exchange changed the grant")
		}
	})

	t.Run("an exchange never widens the grant nor outlives its subject", func(t *testing.T) {
		got := narrowAs(t, api, rotor, subject, nil)
		if left, _ := got.Body["expires_in"].(float64); got.Status != http.StatusOK || got.str("scope") != "read write" || left > 3600 || left < 3000 {
			t.Errorf("an exchange asking nothing answered %d %v", got.Status, got.Body)
		}
		wantRefusal(t, "a lifetime past an hour", narrowAs(t, api, rotor, subject, url.Values{oauth.ParamExpiresIn: {"3601"}}), "invalid_request")
		wantRefusal(t, "a lifetime of nothing", narrowAs(t, api, rotor, subject, url.Values{oauth.ParamExpiresIn: {"0"}}), "invalid_request")
		wantRefusal(t, "another resource", narrowAs(t, api, rotor, subject, url.Values{"resource": {consentIssuer + "/api/v1"}}), "invalid_target")

		held := connect(t, h, api, alice, reader, map[string]any{"scope": "read", "spaces": []string{docs.key}}).str("access_token")
		wantRefusal(t, "writing from a grant that reads", narrowAs(t, api, reader, held, url.Values{"scope": {"read write"}}), "invalid_scope")
		wantRefusal(t, "another app's token as the subject", narrowAs(t, api, reader, subject, nil), "invalid_grant")
		wantRefusal(t, "a token nobody issued", narrowAs(t, api, rotor, "stator_oat_"+strings.Repeat("A", 43), nil), "invalid_grant")

		// The subject has ten minutes left, so an hour asked is ten minutes given.
		if _, err := h.super.Exec(context.Background(), `UPDATE api_token SET access_expires_at = now() + interval '10 minutes' WHERE id = $1`, grant); err != nil {
			t.Fatal(err)
		}
		got = narrowAs(t, api, rotor, subject, url.Values{oauth.ParamExpiresIn: {"3600"}})
		if left, _ := got.Body["expires_in"].(float64); got.Status != http.StatusOK || left > 600 || left < 590 {
			t.Errorf("an hour asked of a subject with ten minutes left answered %d %v", got.Status, got.Body)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oauth_exchanged_token x JOIN api_token t ON t.id = x.grant_id
			WHERE x.token_hash = $1 AND x.expires_at <= t.access_expires_at`, auth.HashToken(got.str("access_token"))); n != 1 {
			t.Error("the exchanged token outlives its subject")
		}
	})

	t.Run("a narrower token reaches only the spaces it names, within the grant's", func(t *testing.T) {
		got := narrowAs(t, api, rotor, subject, url.Values{oauth.ParamSpaces: {strings.ToLower(docs.key)}})
		if got.Status != http.StatusOK {
			t.Fatalf("an exchange to one space answered %d %v", got.Status, got.Body)
		}
		if listed := listsSpaces(t, api, got.str("access_token")); !strings.Contains(listed, docs.key) || strings.Contains(listed, team.key) {
			t.Errorf("the narrower token lists %s", listed)
		}
		if writesPage(t, api, got.str("access_token"), team.homeID) {
			t.Error("the narrower token wrote outside its space")
		}
		if listed := listsSpaces(t, api, subject); !strings.Contains(listed, team.key) {
			t.Errorf("the grant lost a space to the exchange: %s", listed)
		}
		wantRefusal(t, "a space nobody has", narrowAs(t, api, rotor, subject, url.Values{oauth.ParamSpaces: {"NOSUCH" + strings.ToUpper(uuid.NewString()[:4])}}), "invalid_scope")
		wantRefusal(t, "a space the person cannot see", narrowAs(t, api, rotor, subject, url.Values{oauth.ParamSpaces: {secret}}), "invalid_scope")

		held := connect(t, h, api, alice, reader, map[string]any{"scope": "read", "spaces": []string{docs.key}}).str("access_token")
		wantRefusal(t, "a space the grant does not reach", narrowAs(t, api, reader, held, url.Values{oauth.ParamSpaces: {team.key}}), "invalid_scope")
		within := narrowAs(t, api, reader, held, url.Values{oauth.ParamSpaces: {docs.key}})
		if listed := listsSpaces(t, api, within.str("access_token")); within.Status != http.StatusOK || !strings.Contains(listed, docs.key) || strings.Contains(listed, team.key) {
			t.Errorf("a narrower token of a limited grant answered %d and lists %s", within.Status, listed)
		}
	})

	t.Run("straight through SQL, a member reads no exchanged token and makes none past its grant", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, w.aliceID)
		denied(t, conn, "reading the exchanged tokens", `SELECT count(*) FROM oauth_exchanged_token`)
		denied(t, conn, "making an exchanged token", `
			INSERT INTO oauth_exchanged_token (token_hash, grant_id, scopes, expires_at)
			VALUES (sha256(gen_random_uuid()::text::bytea), $1, '{}', now() + interval '1 minute')`, grant)
		ctx := context.Background()
		var personal uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO api_token (user_id, name, token_hash, scopes)
			VALUES ($1, 'Mine', sha256(gen_random_uuid()::text::bytea), '{}') RETURNING id`, w.aliceID).Scan(&personal); err != nil {
			t.Fatal(err)
		}
		for what, args := range map[string][]any{
			"an exchanged token past its grant's access token": {grant, "2 hours"},
			"an exchanged token of a personal token":           {personal, "1 minute"},
		} {
			_, err := h.super.Exec(ctx, `
				INSERT INTO oauth_exchanged_token (token_hash, grant_id, scopes, expires_at)
				VALUES (sha256(gen_random_uuid()::text::bytea), $1, '{}', now() + $2::interval)`, args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" {
				t.Errorf("%s was not refused by the database: %v", what, err)
			}
		}
	})

	t.Run("revoking a narrower token leaves the grant, a refresh keeps it, and ending the grant ends it", func(t *testing.T) {
		first, second := narrowAs(t, api, rotor, subject, nil).str("access_token"), narrowAs(t, api, rotor, subject, nil).str("access_token")
		if revoked := appPost(t, api, "/oauth/revoke", rotor, url.Values{"token": {first}}); revoked.Status != http.StatusOK {
			t.Fatalf("revoking a narrower token answered %d", revoked.Status)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, first, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("a revoked narrower token answers %d", status)
		}
		for what, token := range map[string]string{"the subject": subject, "the other narrower token": second} {
			if status, _, _ := mcpCall(t, api, token, "tools/list", nil); status != http.StatusOK {
				t.Errorf("%s answers %d after a narrower token was revoked", what, status)
			}
		}
		renewed := appPost(t, api, "/oauth/token", rotor, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
		if renewed.Status != http.StatusOK {
			t.Fatalf("the refresh answered %d %v", renewed.Status, renewed.Body)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, second, "tools/list", nil); status != http.StatusOK {
			t.Errorf("a narrower token ended with its grant's refresh: %d", status)
		}
		if revoked := appPost(t, api, "/oauth/revoke", rotor, url.Values{"token": {renewed.str("refresh_token")}}); revoked.Status != http.StatusOK {
			t.Fatalf("revoking the grant answered %d", revoked.Status)
		}
		h.settle(t)
		if status, _, _ := mcpCall(t, api, second, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("a narrower token outlives its grant: %d", status)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oauth_exchanged_token WHERE grant_id = $1`, grant); n != 0 {
			t.Errorf("%d exchanged tokens outlive their grant", n)
		}
	})
}
