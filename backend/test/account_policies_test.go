//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The service refusing is not proof (#535): straight through SQL as
// stator_app, a member changes only their own settings, reaches no pending
// sign-in and no join request, and an administrator still answers requests.
func TestAccountsSignInsAndJoinRequestsAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := h.makeMember(t, "account-guards")
	adminID := h.addPerson(t, "admin")
	memberID := h.addPerson(t, "member")
	otherID := h.addPerson(t, "member")
	ctx := context.Background()

	stranger := func(name string) (uuid.UUID, string) {
		t.Helper()
		var id uuid.UUID
		email := name + "-" + uuid.NewString()[:8] + "@example.test"
		if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, email, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, id) })
		return id, email
	}
	waitingID, waitingEmail := stranger("waiting")
	taken := "taken-" + uuid.NewString()[:8] + "@example.test"
	outsiderID, _ := stranger("outsider")
	if _, err := h.super.Exec(ctx, `INSERT INTO join_request (user_id) VALUES ($1)`, waitingID); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := h.super.QueryRow(ctx, `
		INSERT INTO oidc_login (state, nonce, code_verifier, expires_at)
		VALUES (gen_random_uuid()::text, 'n', 'v', now() + interval '1 hour') RETURNING state`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM oidc_login WHERE state = $1`, state) })
	h.settle(t)

	conn := appConn(t)
	seen := func(t *testing.T, sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("a plain member is refused", func(t *testing.T) {
		actAs(t, conn, memberID)
		denied(t, conn, "a member changing somebody else's settings", `UPDATE app_user SET timezone = 'Europe/Berlin' WHERE id = $1`, otherID)
		denied(t, conn, "a member switching somebody off", `UPDATE app_user SET is_active = false WHERE id = $1`, otherID)
		denied(t, conn, "a member renaming somebody", `UPDATE app_user SET name = 'Taken' WHERE id = $1`, otherID)
		denied(t, conn, "a member changing their own address", `UPDATE app_user SET email = $2 WHERE id = $1`, memberID, taken)
		denied(t, conn, "a member setting their own password", `UPDATE app_user SET password_hash = 'x' WHERE id = $1`, memberID)
		denied(t, conn, "a member switching themselves on", `UPDATE app_user SET is_active = true WHERE id = $1`, memberID)
		denied(t, conn, "a member renaming themselves", `UPDATE app_user SET name = 'Somebody else' WHERE id = $1`, memberID)
		denied(t, conn, "a member pointing their picture elsewhere", `UPDATE app_user SET avatar_url = 'https://elsewhere.test/p.png' WHERE id = $1`, memberID)
		denied(t, conn, "a member making an account", `INSERT INTO app_user (email, name) VALUES ($1, 'Planted')`, "planted-"+uuid.NewString()[:8]+"@example.test")
		untouched(t, conn, "a member removing an account", `DELETE FROM app_user WHERE id = $1`, otherID)

		if n := seen(t, `SELECT count(*) FROM oidc_login`); n != 0 {
			t.Errorf("a member sees %d pending sign-ins", n)
		}
		denied(t, conn, "a member starting a sign-in", `INSERT INTO oidc_login (state, nonce, code_verifier, expires_at) VALUES ('planted', 'n', 'v', now() + interval '1 hour')`)
		untouched(t, conn, "a member redirecting a sign-in", `UPDATE oidc_login SET redirect = 'https://elsewhere.test' WHERE state = $1`, state)
		untouched(t, conn, "a member cancelling a sign-in", `DELETE FROM oidc_login WHERE state = $1`, state)

		if n := seen(t, `SELECT count(*) FROM join_request`); n != 0 {
			t.Errorf("a member sees %d requests to join", n)
		}
		denied(t, conn, "a member asking for somebody to join", `INSERT INTO join_request (user_id) VALUES ($1)`, outsiderID)
		untouched(t, conn, "a member moving a request", `UPDATE join_request SET user_id = $2 WHERE user_id = $1`, waitingID, outsiderID)
		untouched(t, conn, "a member answering a request", `DELETE FROM join_request WHERE user_id = $1`, waitingID)

		var timezone, email string
		var active bool
		if err := h.super.QueryRow(ctx, `SELECT o.timezone, o.is_active, m.email FROM app_user o, app_user m WHERE o.id = $1 AND m.id = $2`,
			otherID, memberID).Scan(&timezone, &active, &email); err != nil {
			t.Fatal(err)
		}
		if timezone != "UTC" || !active || email == taken {
			t.Errorf("a member changed accounts: timezone %q, active %v, address %q", timezone, active, email)
		}
		if n := h.countRows(t, `SELECT count(*) FROM oidc_login WHERE state = $1 AND redirect = ''`, state); n != 1 {
			t.Error("a member changed the pending sign-in")
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request`); n != 1 {
			t.Errorf("the requests to join are %d, want the 1 there was", n)
		}
	})

	t.Run("a member still changes their own settings", func(t *testing.T) {
		actAs(t, conn, memberID)
		tag, err := conn.Exec(ctx, `UPDATE app_user SET timezone = 'Europe/Berlin', locale = 'de', show_in_readers = false WHERE id = $1`, memberID)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("a member's own settings reached %d rows (%v)", tag.RowsAffected(), err)
		}
		want(t, api.as(t, memberID).patch(t, "/api/v1/auth/me", map[string]any{"locale": "en", "showInReaders": true}), http.StatusOK, "the profile route")
	})

	t.Run("an administrator answers requests but writes no account or sign-in", func(t *testing.T) {
		actAs(t, conn, adminID)
		denied(t, conn, "an administrator changing somebody's settings", `UPDATE app_user SET timezone = 'Europe/Berlin' WHERE id = $1`, otherID)
		untouched(t, conn, "an administrator removing an account", `DELETE FROM app_user WHERE id = $1`, otherID)
		if n := seen(t, `SELECT count(*) FROM oidc_login`); n != 0 {
			t.Errorf("an administrator sees %d pending sign-ins", n)
		}
		untouched(t, conn, "an administrator cancelling a sign-in", `DELETE FROM oidc_login WHERE state = $1`, state)

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM join_request`).Scan(&n); err != nil || n != 1 {
			t.Errorf("an administrator sees %d requests to join (%v)", n, err)
		}
		for _, try := range []struct {
			what string
			sql  string
			args []any
		}{
			{"noting a request", `INSERT INTO join_request (user_id) VALUES ($1)`, []any{outsiderID}},
			{"answering one", `DELETE FROM join_request WHERE user_id = $1`, []any{waitingID}},
		} {
			tag, err := tx.Exec(ctx, try.sql, try.args...)
			if err != nil || tag.RowsAffected() != 1 {
				t.Errorf("an administrator %s reached %d rows (%v)", try.what, tag.RowsAffected(), err)
			}
		}
	})

	t.Run("inviting somebody waiting as a guest still answers their request", func(t *testing.T) {
		admin := api.as(t, owner.user)
		newTree(t, admin, "ACCT", "Accounts")
		want(t, admin.post(t, "/api/v1/spaces/ACCT/guests", map[string]any{"email": waitingEmail, "role": "viewer"}), http.StatusCreated, "invite the waiting person")
		if n := h.countRows(t, `SELECT count(*) FROM join_request WHERE user_id = $1`, waitingID); n != 0 {
			t.Error("the invitation left the request to join")
		}
	})
}
