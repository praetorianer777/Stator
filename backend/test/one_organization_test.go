//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/migrations"
)

// oneOrganizationMigration is the migration that refuses an instance holding
// several organizations (#380).
const oneOrganizationMigration = "_one_organization.sql"

// After the upgrade the database holds the one organization by itself: neither
// the superuser nor the admin role, whom no policy binds, makes a second.
func TestASecondOrganizationIsRefused(t *testing.T) {
	h := newHarness(t)
	h.makeMember(t, "only")
	ctx := context.Background()

	if _, err := h.super.Exec(ctx, `INSERT INTO org (slug, name) VALUES ('second', 'Second')`); !isOnlyOrganization(err) {
		t.Errorf("the superuser made a second organization: %v", err)
	}
	_, err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO org (slug, name) VALUES ('second', 'Second')`)
		return err
	})
	if !isOnlyOrganization(err) {
		t.Errorf("the admin role made a second organization: %v", err)
	}
	var n int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM org`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("organizations: %d (%v), want 1", n, err)
	}
}

// isOnlyOrganization recognises the refusal of org_is_the_only_one.
func isOnlyOrganization(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, "one per instance")
}

// An instance holding several organizations is refused at the upgrade with a
// message an operator can act on from migrate's log, and nothing changes; an
// instance with one upgrades and keeps what it holds.
func TestTheUpgradeRefusesSeveralOrganizations(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	refusing, before := migrationsAround(t)

	t.Run("several organizations are refused and nothing changes", func(t *testing.T) {
		scratch := scratchDatabase(t)
		upTo(t, scratch.db, before)
		for _, slug := range []string{"acme", "globex"} {
			if _, err := scratch.pool.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2)`, slug, slug); err != nil {
				t.Fatal(err)
			}
		}
		err := goose.UpContext(ctx, scratch.db, ".", goose.WithAllowMissing())
		if err == nil {
			t.Fatal("the upgrade went through with two organizations")
		}
		t.Logf("the operator reads: %v", err)
		for _, want := range []string{
			"holds 2 organizations (acme, globex)",
			"Export every organization (Settings, Export and import; see docs/organization-transfer.md)",
			"delete all but one, then upgrade again",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not say %q:\n%v", want, err)
			}
		}
		if v := dbVersion(t, scratch.db); v != before {
			t.Errorf("the refused upgrade left the schema at %d, want %d", v, before)
		}
		var trigger bool
		if err := scratch.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'org_is_the_only_one')`).Scan(&trigger); err != nil || trigger {
			t.Errorf("the refused upgrade left its trigger behind (%v)", err)
		}
	})

	t.Run("one organization upgrades and keeps its people, spaces and pages", func(t *testing.T) {
		scratch := scratchDatabase(t)
		upTo(t, scratch.db, before)
		var org, user, space string
		for _, step := range []struct {
			sql  string
			args []any
			into *string
		}{
			{`INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`, nil, &org},
			{`INSERT INTO app_user (email, name) VALUES ('ada@acme.test', 'Ada') RETURNING id`, nil, &user},
			{`INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner') RETURNING org_id`, []any{&org, &user}, new(string)},
			{`INSERT INTO space (org_id, key, name, created_by) VALUES ($1, 'KEPT', 'Kept', $2) RETURNING id`, []any{&org, &user}, &space},
			{`INSERT INTO page (org_id, space_id, rank, title, created_by, updated_by) VALUES ($1, $2, 'a', 'Kept page', $3, $3) RETURNING id`, []any{&org, &space, &user}, new(string)},
		} {
			args := make([]any, len(step.args))
			for i, a := range step.args {
				args[i] = *a.(*string)
			}
			if err := scratch.pool.QueryRow(ctx, step.sql, args...).Scan(step.into); err != nil {
				t.Fatalf("%s: %v", step.sql, err)
			}
		}

		if err := goose.UpContext(ctx, scratch.db, ".", goose.WithAllowMissing()); err != nil {
			t.Fatalf("the upgrade of an instance with one organization: %v", err)
		}
		if v := dbVersion(t, scratch.db); v < refusing {
			t.Errorf("the upgrade stopped at %d, before %d", v, refusing)
		}
		var members, spaces, pages int
		if err := scratch.pool.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM org_member WHERE org_id = $1 AND user_id = $2),
			       (SELECT count(*) FROM space WHERE org_id = $1 AND key = 'KEPT'),
			       (SELECT count(*) FROM page WHERE space_id = $3 AND title = 'Kept page')`, org, user, space).Scan(&members, &spaces, &pages); err != nil {
			t.Fatal(err)
		}
		if members != 1 || spaces != 1 || pages != 1 {
			t.Errorf("after the upgrade: %d members, %d spaces, %d pages, want 1 each", members, spaces, pages)
		}
		if _, err := scratch.pool.Exec(ctx, `INSERT INTO org (slug, name) VALUES ('globex', 'Globex')`); !isOnlyOrganization(err) {
			t.Errorf("the upgraded instance took a second organization: %v", err)
		}
	})
}

// migrationsAround is the version of the refusing migration and of the one
// before it, read from the embedded files as goose reads them.
func migrationsAround(t *testing.T) (refusing, before int64) {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var versions []int64
	for _, name := range names {
		v, err := strconv.ParseInt(name[:strings.IndexByte(name, '_')], 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.HasSuffix(name, oneOrganizationMigration) {
			refusing = v
		}
		versions = append(versions, v)
	}
	if refusing == 0 {
		t.Fatalf("no migration ends in %s", oneOrganizationMigration)
	}
	for _, v := range versions {
		if v < refusing && v > before {
			before = v
		}
	}
	return refusing, before
}

// scratch is an empty database of the test's own, reached as the superuser
// the way cmd/migrate reaches the stack's.
type scratch struct {
	pool *pgxpool.Pool
	db   *sql.DB
}

func scratchDatabase(t *testing.T) scratch {
	t.Helper()
	ctx := context.Background()
	superURL := lastingSuperuserURL(t)
	suffix := make([]byte, cloneSuffixBytes)
	_, _ = rand.Read(suffix)
	// Named like a clone, so a run killed halfway leaves it to the next one.
	name := testDatabasePrefix + "scratch_" + hex.EncodeToString(suffix)
	if _, err := mustPool(t, superURL).Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("make a scratch database: %v", err)
	}
	t.Cleanup(func() { retire(superURL, name) })
	target, err := databaseURL(superURL, name)
	if err != nil {
		t.Fatal(err)
	}
	pool := mustPool(t, target)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	return scratch{pool: pool, db: sqlDB}
}

func mustPool(t *testing.T, u string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func upTo(t *testing.T, sqlDB *sql.DB, version int64) {
	t.Helper()
	if err := goose.UpToContext(context.Background(), sqlDB, ".", version, goose.WithAllowMissing()); err != nil {
		t.Fatalf("migrate the scratch database to %d: %v", version, err)
	}
}

func dbVersion(t *testing.T, sqlDB *sql.DB) int64 {
	t.Helper()
	v, err := goose.GetDBVersionContext(context.Background(), sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
