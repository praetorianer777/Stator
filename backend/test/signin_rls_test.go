//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// signInRows gives one member every kind of row the sign-in tables hold,
// written as the superuser so the policies play no part in making them.
func (h *harness) signInRows(t *testing.T, m member) {
	t.Helper()
	ctx := context.Background()
	_, digest, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO groups (org_id, name, source, external_ref) VALUES ($1, 'staff', 'oidc', 'staff')`, []any{m.org}},
		{`INSERT INTO group_member (org_id, group_id, user_id) SELECT $1, id, $2 FROM groups WHERE org_id = $1`, []any{m.org, m.user}},
		{`INSERT INTO oidc_provider (org_id, issuer, client_id, client_secret) VALUES ($1, 'https://id.test', 'stator', '\x01'::bytea)`, []any{m.org}},
		{`INSERT INTO oidc_login (state, org_id, nonce, code_verifier, expires_at) VALUES (gen_random_uuid()::text, $1, 'n', 'v', now() + interval '1 hour')`, []any{m.org}},
		{`INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, []any{m.org, m.user}},
		{`INSERT INTO audit_log (org_id, actor_user_id, action, target_type) VALUES ($1, $2, 'member.admitted', 'user')`, []any{m.org, m.user}},
		{`INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://id.test', gen_random_uuid()::text, $1)`, []any{m.user}},
		{`INSERT INTO user_session (user_id, token_hash, current_org_id, proof_org_id, proof, expires_at) VALUES ($1, $2, $3, $3, 'oidc', $4)`, []any{m.user, digest, m.org, time.Now().Add(time.Hour)}},
	} {
		if _, err := h.super.Exec(ctx, sql.query, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.query, err)
		}
	}
}

// Straight through SQL as stator_app: the organization's sign-in tables show
// nothing without a caller, and the tables that hold accounts' sessions and
// identities show the application nothing at all, nor let it write them.
func TestRawSQLCannotReachTheSignInTables(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "iota")
	h.signInRows(t, a)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	countOf := func(t *testing.T, sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	orgTables := []string{"groups", "group_member", "oidc_provider", "oidc_login", "org_join_request", "audit_log"}
	globalTables := []string{"user_session", "user_identity"}

	t.Run("with nobody set nothing is visible", func(t *testing.T) {
		for _, table := range append(orgTables, globalTables...) {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 0 {
				t.Errorf("an unscoped connection sees %d rows in %s", got, table)
			}
		}
	})

	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, a.org.String()); err != nil {
		t.Fatal(err)
	}
	// Acting for the owner, since only an administrator reads the audit log.
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.UserVar, a.user.String()); err != nil {
		t.Fatal(err)
	}

	t.Run("acting for the owner only the organization's rows show", func(t *testing.T) {
		for _, table := range orgTables {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 1 {
				t.Errorf("the owner sees %d rows in %s, want the 1 there is", got, table)
			}
		}
		for _, table := range globalTables {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 0 {
				t.Errorf("the app role sees %d rows in %s, want none", got, table)
			}
		}
	})

	for _, attempt := range []struct {
		name string
		sql  string
		args []any
	}{
		{"forging a session", `INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, decode(repeat('00', 32), 'hex'), 'password', now() + interval '1 day')`, []any{a.user}},
		{"claiming an identity", `INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://evil.test', 'x', $1)`, []any{a.user}},
	} {
		t.Run(attempt.name+" is refused", func(t *testing.T) {
			_, err := conn.Exec(ctx, attempt.sql, attempt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "23514") {
				t.Fatalf("%s = %v, want a policy or check violation", attempt.sql, err)
			}
		})
	}

	t.Run("sessions cannot be deleted", func(t *testing.T) {
		tag, err := conn.Exec(ctx, `DELETE FROM user_session`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatalf("the app role deleted %v sessions (%v)", tag, err)
		}
	})
}

// The admin role is exempt from the policies, so the trigger is what keeps a
// group's members to the organization's members there.
func TestAGroupKeepsItsMembersInItsOrganization(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "lambda")
	h.signInRows(t, a)
	var stranger uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ('stranger-lambda@example.test', 'Stranger') RETURNING id`).Scan(&stranger); err != nil {
		t.Fatal(err)
	}

	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO group_member (org_id, group_id, user_id)
			SELECT $1, id, $2 FROM groups WHERE org_id = $1`, a.org, stranger)
		return err
	})
	if !isCheckViolation(err) {
		t.Fatalf("the admin role put somebody who is not a member into a group: %v", err)
	}
}
