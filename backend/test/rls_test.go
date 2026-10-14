//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// A transaction the Go code opens for nobody reaches nothing: no space or page
// to read, and no organization to make. Nothing in Go refuses it first: the
// database does.
func TestATransactionForNobodyReachesNothing(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "nobody")
	owner := api.as(t, home.user)
	newTree(t, owner, uniqueKey("NOB"), "Nobody's")
	h.settle(t)

	for _, table := range []string{"space", "page", "page_version", "space_grant"} {
		if n := h.count(t, context.Background(), `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("a transaction for nobody sees %d rows of %s", n, table)
		}
	}
	_, err := h.cluster.Write(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO site (slug, name) VALUES ('nobody', 'Nobody')`)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "23505") {
		t.Fatalf("an organization made for nobody = %v, want refused", err)
	}
}

// The service refusing is not proof. Straight through SQL as stator_app, with
// no help from the Go code, nothing is visible without a caller, and row
// level security cannot be switched off, dropped or stepped around.
func TestRawSQLAsTheAppRoleCannotLiftRowSecurity(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "gamma")
	owner := api.as(t, home.user)
	newTree(t, owner, uniqueKey("GAM"), "Gamma")
	h.settle(t)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	var role string
	var privileged bool
	if err := conn.QueryRow(ctx, `SELECT current_user, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&role, &privileged); err != nil {
		t.Fatal(err)
	}
	if role != "stator_app" || privileged {
		t.Fatalf("connected as %s (privileged %v), want the unprivileged stator_app", role, privileged)
	}

	t.Run("with nobody set nothing is visible", func(t *testing.T) {
		for _, table := range []string{"space", "page", "page_version", "space_grant"} {
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("an unscoped connection sees %d rows in %s, want none", n, table)
			}
		}
	})

	for _, attempt := range []struct{ name, sql string }{
		{"turning row security off", `SET row_security = off; SELECT count(*) FROM site`},
		{"disabling the policies", `ALTER TABLE site DISABLE ROW LEVEL SECURITY`},
		{"dropping a policy", `DROP POLICY site_app ON site`},
		{"becoming the exempt role", `SET ROLE stator_admin`},
	} {
		t.Run(attempt.name+" is refused", func(t *testing.T) {
			if _, err := conn.Exec(ctx, `RESET ALL`); err != nil {
				t.Fatal(err)
			}
			_, err := conn.Exec(ctx, attempt.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("%s succeeded (err = %v)", attempt.sql, err)
			}
			if !strings.HasPrefix(pgErr.Code, "42") {
				t.Errorf("%s failed for the wrong reason: %s %s", attempt.sql, pgErr.Code, pgErr.Message)
			}
		})
	}

	// The site's key may speak first, so either refusal will do; removing it
	// is filtered by the policies, which admit no delete.
	t.Run("making or removing the organization is refused", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `RESET ALL`); err != nil {
			t.Fatal(err)
		}
		_, err := conn.Exec(ctx, `INSERT INTO site (slug, name) VALUES ('sneaky', 'Sneaky')`)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "23505") {
			t.Errorf("making an organization = %v, want refused", err)
		}
		tag, err := conn.Exec(ctx, `DELETE FROM site`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("the app role removed %d organizations (%v)", tag.RowsAffected(), err)
		}
	})
}

// Signup and login run before anybody is known; the admin role is exempt by
// an explicit policy, and only the admin transactions use it.
func TestTheAdminRoleIsExemptFromThePolicies(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "epsilon")

	var role string
	var n int
	err := h.cluster.ReadAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM member WHERE user_id = $1`, a.user).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if role != "stator_admin" || n != 1 {
		t.Fatalf("the admin pool is %s and sees %d of the organization's one member", role, n)
	}
}

// Both runtime roles leave row level security in force, and a connection that
// would ignore it is refused at startup.
func TestPrivilegedRolesAreRefused(t *testing.T) {
	h := newHarness(t)
	if err := h.cluster.RefuseSuperuser(context.Background()); err != nil {
		t.Fatalf("stator_app was taken for a privileged role: %v", err)
	}

	cfg := h.cfg.DB
	cfg.PrimaryURL = os.Getenv(envSuperuser)
	cfg.AdminURL = cfg.PrimaryURL
	cfg.ReplicaURLs = nil
	super, err := db.Open(context.Background(), cfg, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close()
	if err := super.RefuseSuperuser(context.Background()); !errors.Is(err, db.ErrPrivilegedRole) {
		t.Fatalf("a superuser connection = %v, want db.ErrPrivilegedRole", err)
	}
}

// adminOnlyTables are read and written by the admin role alone, so the app
// role has no permissive policy on them and reaches no row.
var adminOnlyTables = map[string]bool{"user_identity": true, "user_session": true}

// A table a migration adds without forcing row level security, without the
// admin's bypass, or without the permissive policy every restrictive access
// policy of the app role needs to admit anything, is caught here rather than
// in production.
func TestEveryTableForcesRowSecurity(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity AND c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname AND 'stator_admin' = ANY (p.roles)),
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname AND p.permissive = 'PERMISSIVE'
		                 AND p.cmd IN ('ALL', 'SELECT') AND (p.roles && ARRAY['stator_app', 'public']::name[]))
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var forced, bypass, admits bool
		if err := rows.Scan(&name, &forced, &bypass, &admits); err != nil {
			t.Fatal(err)
		}
		seen++
		if !forced {
			t.Errorf("%s does not force row level security", name)
		}
		if !bypass {
			t.Errorf("%s has no policy letting stator_admin through", name)
		}
		if !admits && !adminOnlyTables[name] {
			t.Errorf("%s has no permissive policy for stator_app, so its access policies admit nothing", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// A run that found no tables would pass for the wrong reason.
	if seen < 3 {
		t.Fatalf("only %d tables found; the schema is not migrated", seen)
	}
}

// An instance serves one organization, so the schema names none: no column is
// org_id, there is no current_org_id(), and no policy or function reads
// either, nor the app.org_id setting. A migration that brings one back fails
// here.
func TestNothingReadsTheTenant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	const tenant = `(current_org_id|app\.org_id|\morg_id\M)`
	found := func(sql string) []string {
		t.Helper()
		rows, err := h.super.Query(ctx, sql, tenant)
		if err != nil {
			t.Fatal(err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	for _, name := range found(`
		SELECT tablename || '.' || policyname FROM pg_policies
		WHERE schemaname = 'public' AND (qual ~ $1 OR with_check ~ $1)
		ORDER BY 1`) {
		t.Errorf("policy %s reads the tenant", name)
	}
	for _, name := range found(`
		SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND (p.prosrc ~ $1 OR p.proname ~ $1)
		ORDER BY 1`) {
		t.Errorf("function %s reads the tenant", name)
	}
	for _, name := range found(`
		SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name ~ $1
		ORDER BY 1`) {
		t.Errorf("column %s names the organization", name)
	}
	// A pattern that matched nothing at all would pass for the wrong reason;
	// Armature's own organization is no tenant and must not match.
	if len(found(`SELECT 'x' WHERE 'a.org_id = b.org_id' ~ $1 AND 'current_org_id()' ~ $1
		AND 'current_setting(''app.org_id'')' ~ $1 AND 'armature_org_id' !~ $1`)) != 1 {
		t.Fatal("the pattern no longer recognises a read of the tenant")
	}
}

// The tables named after the organization took the site's name with the
// schema change, and nothing in the catalog keeps the old ones.
func TestNoTableIsNamedAfterTheOrganization(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname ~ '^org(_|$)'
		UNION ALL
		SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND p.proname ~ '^org(_|$)'
		UNION ALL
		SELECT policyname FROM pg_policies WHERE schemaname = 'public' AND policyname ~ '^org(_|$)'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		t.Errorf("%s is still named after the organization", name)
	}
	var site bool
	if err := h.super.QueryRow(context.Background(), `SELECT to_regclass('public.site') IS NOT NULL AND to_regclass('public.member') IS NOT NULL`).Scan(&site); err != nil || !site {
		t.Fatalf("site and member are not there (%v); the schema is not migrated", err)
	}
}

// isPolicyViolation recognises the error a WITH CHECK clause raises.
func isPolicyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
