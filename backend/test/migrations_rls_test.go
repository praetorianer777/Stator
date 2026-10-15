//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/migrations"
)

// Under CloudNativePG the migrations run as the database's owner, an
// ordinary role without BYPASSRLS, and every table forces row level security
// on its owner (#423). Until the tenant policies went (00750) they admitted
// no row to a session that named no organization, so a backfill run as that
// owner silently changed nothing. The stack migrates as a superuser, whom no
// policy binds, and the chart's install test starts from an empty database.
//
// Here an instance is written to at three releases and upgraded across the
// next migrations by such an owner; a copy of it upgraded by the superuser at
// each step must hold the same rows, and each backfill must have reached its
// own.
func TestTheMigrationsReachEveryRowWhereTheOwnerIsBoundByRowLevelSecurity(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	inst := boundInstance(t, boundOwner(t))
	_, beforeVersions := migrationsAround(t, "00070_versions.sql")
	_, beforeViews := migrationsAround(t, "00300_page_views.sql")
	_, beforeRenewal := migrationsAround(t, "00670_example_renewal.sql")

	inst.migrate(t, inst.name, true, beforeVersions)

	db := inst.super(t)
	one := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	org := one(`INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`)
	ada := one(`INSERT INTO app_user (email, name) VALUES ('ada@acme.test', 'Ada') RETURNING id`)
	bob := one(`INSERT INTO app_user (email, name) VALUES ('bob@acme.test', 'Bob') RETURNING id`)
	for user, role := range map[uuid.UUID]string{ada: "owner", bob: "member"} {
		one(`INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3) RETURNING user_id`, org, user, role)
	}
	type space struct{ id, home, child uuid.UUID }
	var spaces []space
	for key, creator := range map[string]uuid.UUID{"DOCS": ada, "TEAM": bob} {
		var s space
		s.id = one(`INSERT INTO space (org_id, key, name, created_by) VALUES ($1, $2, $2, $3) RETURNING id`, org, key, creator)
		s.home = one(`INSERT INTO page (org_id, space_id, rank, title, created_by, updated_by, updated_at)
			VALUES ($1, $2, 'a', 'Home', $3, $3, now() - interval '2 days') RETURNING id`, org, s.id, creator)
		s.child = one(`INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, updated_by, updated_at)
			VALUES ($1, $2, $3, 'a', 'Plans', $4, $4, now() - interval '1 day') RETURNING id`, org, s.id, s.home, creator)
		one(`UPDATE space SET home_page_id = $2 WHERE id = $1 RETURNING id`, s.id, s.home)
		spaces = append(spaces, s)
	}
	db.Close()

	inst.upgradeAlongside(t, beforeViews)

	db = inst.tenant(t, org)
	visits := 0
	for _, s := range spaces {
		for _, user := range []uuid.UUID{ada, bob} {
			one(`INSERT INTO page_visit (org_id, user_id, page_id) VALUES ($1, $2, $3) RETURNING page_id`, org, user, s.child)
			visits++
		}
	}
	plans := spaces[0].child
	for range 2 {
		one(`INSERT INTO attachment (org_id, page_id, uploaded_by, file_name, content_type, size_bytes)
			VALUES ($1, $2, $3, 'plan.txt', 'text/plain', 9) RETURNING id`, org, plans, ada)
	}
	one(`INSERT INTO attachment (org_id, page_id, uploaded_by, file_name, content_type, size_bytes)
		VALUES ($1, $2, $3, 'Plan.TXT', 'text/plain', 9) RETURNING id`, org, plans, bob)
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	one(`INSERT INTO api_token (org_id, user_id, name, token_hash) VALUES ($1, $2, 'ci', $3) RETURNING id`, org, ada, token)
	db.Close()

	inst.upgradeAlongside(t, beforeRenewal)

	db = inst.tenant(t, org)
	example := one(`INSERT INTO space (org_id, key, name, example) VALUES ($1, 'EXAMPLE', 'Example', true) RETURNING id`, org)
	for _, job := range []struct {
		language string
		age      string
	}{{"en", "3 days"}, {"de", "1 day"}} {
		one(`INSERT INTO example_job (id, org_id, requested_by, language, state, space_id, started_at, finished_at)
			VALUES (uuidv7(), $1, $2, $3, 'done', $4, now() - $5::interval, now() - $5::interval) RETURNING id`,
			org, ada, job.language, example, job.age)
	}
	theme := one(`INSERT INTO theme (org_id, owner_id, name) VALUES ($1, $2, 'Kept') RETURNING id`, org, ada)
	asset := one(`INSERT INTO theme_asset (org_id, theme_id, name, content_type, size) VALUES ($1, $2, 'font.woff2', 'font/woff2', 9) RETURNING id`, org, theme)
	one(`INSERT INTO org_brand (org_id, logo_type, logo_size, logo_version) VALUES ($1, 'image/png', 8, 3) RETURNING org_id`, org)
	db.Close()

	inst.upgradeAlongside(t, 0)

	db = inst.super(t)
	defer db.Close()
	for _, check := range []struct {
		what string
		sql  string
		args []any
		want int
	}{
		{"pages without their first version (00070)", `SELECT count(*) FROM page p WHERE NOT EXISTS (SELECT 1 FROM page_version v WHERE v.page_id = p.id AND v.number = 1) AND p.space_id <> $1`, []any{example}, 0},
		{"the organization's grant to everyone (00090)", `SELECT count(*) FROM global_grant WHERE permission = 'use' AND subject_type = 'everyone'`, nil, 1},
		{"the grants of the spaces made before permissions (00090)", `SELECT count(*) FROM space_grant g JOIN space s ON s.id = g.space_id WHERE s.key IN ('DOCS', 'TEAM')`, nil, 2*4 + 2},
		{"people whose stored language is the old default (00195)", `SELECT count(*) FROM app_user WHERE locale IS NOT NULL`, nil, 0},
		{"pages without their publication (00210)", `SELECT count(*) FROM page WHERE published_at IS NULL AND space_id <> $1`, []any{example}, 0},
		{"views counted from the visits (00300)", `SELECT count(*) FROM page_view`, nil, visits},
		{"versions of plan.txt (00420)", `SELECT count(DISTINCT version) FROM attachment WHERE page_id = $1`, []any{plans}, 3},
		{"the example's version and the language of its last job (00670)", `SELECT count(*) FROM space WHERE id = $1 AND example_version = 0 AND example_language = 'de'`, []any{example}, 1},
		{"the theme file under the key it was stored with (00760)", `SELECT count(*) FROM theme_asset WHERE id = $1 AND object_key = $2`, []any{asset, "org/" + org.String() + "/theme/" + theme.String() + "/" + asset.String()}, 1},
		{"the logo under the key it was stored with (00760)", `SELECT count(*) FROM brand WHERE logo_key = $1`, []any{"org/" + org.String() + "/brand/logo-3"}, 1},
	} {
		var got int
		if err := db.QueryRow(ctx, check.sql, check.args...).Scan(&got); err != nil {
			t.Fatalf("%s: %v", check.what, err)
		}
		if got != check.want {
			t.Errorf("%s: %d, want %d", check.what, got, check.want)
		}
	}
}

// An instance of several organizations is refused with the operator's
// message by an owner row level security binds too (00740), not let past the
// count to fail on the index after it.
func TestTheUpgradeRefusesSeveralOrganizationsWhereTheOwnerIsBoundByRowLevelSecurity(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	inst := boundInstance(t, boundOwner(t))
	refusing, before := migrationsAround(t, oneOrganizationMigration)
	inst.migrate(t, inst.name, true, before)

	db := inst.super(t)
	for _, slug := range []string{"acme", "globex"} {
		if _, err := db.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2)`, slug, slug); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	err := inst.upTo(inst.name, true, refusing)
	if err == nil || !strings.Contains(err.Error(), "holds 2 organizations (acme, globex)") {
		t.Fatalf("the upgrade of two organizations by the bound owner: %v", err)
	}
}

// boundOwner is a role like the owner CloudNativePG makes: neither superuser
// nor BYPASSRLS. It is dropped when the test ends, after its databases.
func boundOwner(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	super := mustPool(t, lastingSuperuserURL(t))
	suffix := make([]byte, cloneSuffixBytes)
	_, _ = rand.Read(suffix)
	role := "stator_test_owner_" + hex.EncodeToString(suffix)
	if _, err := super.Exec(ctx, `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
		t.Fatalf("make the bound owner: %v", err)
	}
	t.Cleanup(func() {
		if _, err := super.Exec(context.Background(), `DROP ROLE IF EXISTS `+pgx.Identifier{role}.Sanitize()); err != nil {
			t.Errorf("drop the bound owner: %v", err)
		}
	})
	return role
}

// boundDB is a database the bound owner owns, as CloudNativePG's initdb
// makes it, so that every object the migrations create is the owner's.
type boundDB struct {
	superURL string
	owner    string
	name     string
}

func boundInstance(t *testing.T, owner string) boundDB {
	t.Helper()
	inst := boundDB{superURL: lastingSuperuserURL(t), owner: owner}
	inst.name = inst.create(t, "")
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	return inst
}

// create makes a database owned by the bound owner, empty or as a copy of
// template, and drops it when the test ends.
func (inst boundDB) create(t *testing.T, template string) string {
	t.Helper()
	ctx := context.Background()
	suffix := make([]byte, cloneSuffixBytes)
	_, _ = rand.Read(suffix)
	// Named like a clone, so a run killed halfway leaves it to the next one.
	name := testDatabasePrefix + "bound_" + hex.EncodeToString(suffix)
	q := `CREATE DATABASE ` + pgx.Identifier{name}.Sanitize() + ` OWNER ` + pgx.Identifier{inst.owner}.Sanitize()
	if template != "" {
		q += ` TEMPLATE ` + pgx.Identifier{template}.Sanitize()
	}
	super := mustPool(t, inst.superURL)
	if _, err := super.Exec(ctx, q); err != nil {
		t.Fatalf("make a database for the bound owner: %v", err)
	}
	t.Cleanup(func() {
		if _, err := super.Exec(context.Background(), `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	return name
}

// connect reaches a database as the superuser, or as the bound owner. The
// owner is taken on with SET ROLE, which leaves the superuser's rights behind:
// row level security and every privilege follow the current role.
func (inst boundDB) connect(name string, asOwner bool) (*pgxpool.Pool, error) {
	target, err := databaseURL(inst.superURL, name)
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(target)
	if err != nil {
		return nil, err
	}
	if asOwner {
		cfg.ConnConfig.RuntimeParams["role"] = inst.owner
	}
	return pgxpool.NewWithConfig(context.Background(), cfg)
}

// super is the superuser on the instance, for writing what a release wrote.
// The caller closes it before the instance is copied.
func (inst boundDB) super(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := inst.connect(inst.name, false)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// tenant is the superuser naming org as the app did, which the triggers that
// run as the bound owner need until the tenant policies go.
func (inst boundDB) tenant(t *testing.T, org uuid.UUID) *pgxpool.Pool {
	t.Helper()
	target, err := databaseURL(inst.superURL, inst.name)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(target)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["app.org_id"] = org.String()
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// upTo migrates a database to version, or all the way at 0.
func (inst boundDB) upTo(name string, asOwner bool, version int64) error {
	pool, err := inst.connect(name, asOwner)
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	return gooseUp(sqlDB, version)
}

func (inst boundDB) migrate(t *testing.T, name string, asOwner bool, version int64) {
	t.Helper()
	who := "the superuser"
	if asOwner {
		who = "the bound owner"
	}
	if err := inst.upTo(name, asOwner, version); err != nil {
		t.Fatalf("migrate %s as %s to %d: %v", name, who, version, err)
	}
}

func gooseUp(sqlDB *sql.DB, version int64) error {
	if version == 0 {
		return goose.UpContext(context.Background(), sqlDB, ".", goose.WithAllowMissing())
	}
	return goose.UpToContext(context.Background(), sqlDB, ".", version, goose.WithAllowMissing())
}

// upgradeAlongside migrates the instance to version as the bound owner, and a
// copy of it as the superuser, and holds the two to the same rows.
func (inst boundDB) upgradeAlongside(t *testing.T, version int64) {
	t.Helper()
	reference := inst.create(t, inst.name)
	inst.migrate(t, reference, false, version)
	inst.migrate(t, inst.name, true, version)
	want, got := inst.rows(t, reference), inst.rows(t, inst.name)
	for _, table := range slices.Sorted(maps.Keys(want)) {
		if got[table] != want[table] {
			t.Errorf("upgraded to %d by the bound owner, %s holds %s; by the superuser %s", version, table, got[table], want[table])
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

// rows sums up every table: its row count and a digest of its rows, without
// the columns a default fills with a fresh id or the time, which two
// upgrades of the same rows fill differently.
func (inst boundDB) rows(t *testing.T, name string) map[string]string {
	t.Helper()
	ctx := context.Background()
	pool, err := inst.connect(name, false)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cols, err := pool.Query(ctx, `
		SELECT c.relname, string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		  AND COALESCE(pg_get_expr(d.adbin, d.adrelid), '') !~ '(uuidv7|gen_random_uuid|now|clock_timestamp)\('
		GROUP BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	type table struct{ name, columns string }
	tables, err := pgx.CollectRows(cols, func(r pgx.CollectableRow) (table, error) {
		var tb table
		return tb, r.Scan(&tb.name, &tb.columns)
	})
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(tables))
	for _, tb := range tables {
		var count int
		var digest string
		q := fmt.Sprintf(`SELECT count(*), md5(COALESCE(string_agg(r, E'\n' ORDER BY r), ''))
			FROM (SELECT ROW(%s)::text AS r FROM %s) s`, tb.columns, pgx.Identifier{tb.name}.Sanitize())
		if err := pool.QueryRow(ctx, q).Scan(&count, &digest); err != nil {
			t.Fatalf("sum up %s: %v", tb.name, err)
		}
		out[tb.name] = fmt.Sprintf("%d rows (%s)", count, digest[:8])
	}
	return out
}
