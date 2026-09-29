//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
		{`INSERT INTO user_identity (issuer, subject, user_id) VALUES ('https://id.test', gen_random_uuid()::text, $1)`, []any{m.user}},
		{`INSERT INTO user_session (user_id, token_hash, current_org_id, proof_org_id, proof, expires_at) VALUES ($1, $2, $3, $3, 'oidc', $4)`, []any{m.user, digest, m.org, time.Now().Add(time.Hour)}},
	} {
		if _, err := h.super.Exec(ctx, sql.query, sql.args...); err != nil {
			t.Fatalf("%s: %v", sql.query, err)
		}
	}
}

// Straight through SQL as stator_app: tenant tables show a tenant its own rows
// only, and the tables that span tenants show the application nothing at all.
func TestRawSQLCannotReachAnotherTenantsSignIn(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "iota")
	b := h.makeMember(t, "kappa")
	h.signInRows(t, a)
	h.signInRows(t, b)

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
	tenantTables := []string{"groups", "group_member", "oidc_provider", "oidc_login"}
	globalTables := []string{"user_session", "user_identity"}

	t.Run("with no tenant set nothing is visible", func(t *testing.T) {
		for _, table := range append(tenantTables, globalTables...) {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 0 {
				t.Errorf("an unscoped connection sees %d rows in %s", got, table)
			}
		}
	})

	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, a.org.String()); err != nil {
		t.Fatal(err)
	}

	t.Run("scoped to one tenant only its own rows show", func(t *testing.T) {
		for _, table := range tenantTables {
			if got := countOf(t, `SELECT count(*) FROM `+table); got != 1 {
				t.Errorf("org A sees %d rows in %s, want its own 1", got, table)
			}
			if got := countOf(t, `SELECT count(*) FROM `+table+` WHERE org_id = $1`, b.org); got != 0 {
				t.Errorf("org A sees org B's rows in %s", table)
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
		{"configuring another tenant's provider", `INSERT INTO oidc_provider (org_id, issuer, client_id) VALUES ($1, 'https://evil.test', 'x')`, []any{b.org}},
		{"starting a sign-in for another tenant", `INSERT INTO oidc_login (state, org_id, nonce, code_verifier, expires_at) VALUES ('s', $1, 'n', 'v', now())`, []any{b.org}},
		{"joining another tenant's group", `INSERT INTO group_member (org_id, group_id, user_id) SELECT $1, id, $2 FROM groups WHERE external_ref = 'staff' LIMIT 1`, []any{b.org, a.user}},
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

	t.Run("another tenant's provider cannot be changed or read", func(t *testing.T) {
		tag, err := conn.Exec(ctx, `UPDATE oidc_provider SET issuer = 'https://evil.test' WHERE org_id = $1`, b.org)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatalf("update = %v, %v", tag, err)
		}
		if got := countOf(t, `SELECT count(*) FROM oidc_provider WHERE org_id = $1 AND client_secret IS NOT NULL`, b.org); got != 0 {
			t.Fatal("org B's sealed secret is readable from org A")
		}
		tag, err = conn.Exec(ctx, `DELETE FROM user_session`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatalf("the app role deleted %v sessions (%v)", tag, err)
		}
	})
}

// The admin role is exempt from the policies, so the trigger is what keeps a
// group's members inside its organization there.
func TestAGroupKeepsItsMembersInItsOrganization(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "lambda")
	b := h.makeMember(t, "mu")
	h.signInRows(t, a)
	h.signInRows(t, b)

	for name, row := range map[string][]any{
		"a row naming one organization and another's group":        {a.org, a.user, b.org},
		"somebody who is not a member of the group's organization": {b.org, a.user, b.org},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO group_member (org_id, group_id, user_id)
					SELECT $1, id, $2 FROM groups WHERE org_id = $3`, row...)
				return err
			})
			if !isCheckViolation(err) {
				t.Fatalf("the admin role wrote it: %v", err)
			}
		})
	}
}
