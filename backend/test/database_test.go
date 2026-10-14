//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/migrations"
)

// Every test runs in a database of its own, cloned from a template the suite
// migrates once, so tests stay apart without an organization each (#380).
const (
	envReplicas = "STATOR_DB_REPLICA_URLS"
	envAdmin    = "STATOR_DB_ADMIN_URL"

	// testDatabasePrefix names the clones, templateDatabasePrefix the
	// templates; both are dropped by name, so nothing else may share them.
	testDatabasePrefix     = "stator_test_"
	templateDatabasePrefix = "stator_template_"
	// templateHashLength is how much of the migrations' digest names a
	// template: a changed migration makes a new one.
	templateHashLength = 12
	// cloneSuffixBytes is the random part of a clone's name.
	cloneSuffixBytes = 6
	// dropDeadline bounds dropping the last clones when the run ends.
	dropDeadline = time.Minute
	// cloneReplayWait bounds waiting for the standby to replay a clone and
	// the drops before it, which can wait out a restartpoint.
	cloneReplayWait = time.Minute
	// maintenanceDatabase is where the replica's superuser waits for a clone
	// that may not be there yet.
	maintenanceDatabase = "postgres"
)

// templateOnce migrates the template once per run of the suite.
var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// retired are the clones of tests that have ended. The next clone drops them
// before it is made and waits for the standby, because replaying a drop
// there waits for the standby's restartpoint, which on a busy machine took
// twenty seconds, longer than a running test waits for its replica.
// Dropping twenty at a time made the suite slower, not faster.
var retired struct {
	sync.Mutex
	superURL string
	names    []string
}

func retire(superURL, name string) {
	retired.Lock()
	defer retired.Unlock()
	retired.superURL = superURL
	retired.names = append(retired.names, name)
}

// dropRetired drops the clones of the tests that have ended.
func dropRetired(ctx context.Context, super *pgxpool.Pool) error {
	retired.Lock()
	defer retired.Unlock()
	for len(retired.names) > 0 {
		name := retired.names[0]
		if _, err := super.Exec(ctx, `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			return fmt.Errorf("drop %s: %w", name, err)
		}
		retired.names = retired.names[1:]
	}
	return nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	retired.Lock()
	superURL := retired.superURL
	retired.Unlock()
	if superURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), dropDeadline)
		if super, err := pgxpool.New(ctx, superURL); err == nil {
			if err := dropRetired(ctx, super); err != nil {
				fmt.Fprintf(os.Stderr, "%v; the next run drops it\n", err)
			}
			super.Close()
		}
		cancel()
	}
	os.Exit(code)
}

// databaseURL is u with its database replaced by name.
func databaseURL(u, name string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

// databaseOf is the database a URL names.
func databaseOf(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Path, "/")
}

// migrationsDigest is a digest of every embedded migration, names and bodies.
func migrationsDigest() (string, error) {
	sum := sha256.New()
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(migrations.FS, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", path, len(body))
		sum.Write(body)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil))[:templateHashLength], nil
}

// migratedTemplate is the name of a database migrated to the embedded
// migrations and marked as a template, made on the first call of a run.
func migratedTemplate(ctx context.Context, super *pgxpool.Pool) (string, error) {
	templateOnce.Do(func() { templateName, templateErr = makeTemplate(ctx, super) })
	return templateName, templateErr
}

func makeTemplate(ctx context.Context, super *pgxpool.Pool) (string, error) {
	digest, err := migrationsDigest()
	if err != nil {
		return "", fmt.Errorf("read the migrations: %w", err)
	}
	name := templateDatabasePrefix + digest

	// Clones a killed run left behind, and templates of other migrations.
	rows, err := super.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE $1 OR (datname LIKE $2 AND datname <> $3)`,
		testDatabasePrefix+"%", templateDatabasePrefix+"%", name)
	if err != nil {
		return "", err
	}
	stale, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	for _, old := range stale {
		if _, err := super.Exec(ctx, `ALTER DATABASE `+pgx.Identifier{old}.Sanitize()+` IS_TEMPLATE false`); err != nil {
			return "", err
		}
		if _, err := super.Exec(ctx, `DROP DATABASE `+pgx.Identifier{old}.Sanitize()+` WITH (FORCE)`); err != nil {
			return "", fmt.Errorf("drop %s, left from an earlier run: %w", old, err)
		}
	}

	var exists bool
	if err := super.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1 AND datistemplate)`, name).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}
	// Migrated under another name and renamed when done, so a run killed
	// halfway leaves nothing that looks finished.
	building := name + "_building"
	if _, err := super.Exec(ctx, `DROP DATABASE IF EXISTS `+pgx.Identifier{building}.Sanitize()+` WITH (FORCE)`); err != nil {
		return "", err
	}
	if _, err := super.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{building}.Sanitize()); err != nil {
		return "", fmt.Errorf("make the template database: %w", err)
	}
	if err := migrate(ctx, building); err != nil {
		return "", fmt.Errorf("migrate the template database: %w", err)
	}
	for _, q := range []string{
		`ALTER DATABASE ` + pgx.Identifier{building}.Sanitize() + ` RENAME TO ` + pgx.Identifier{name}.Sanitize(),
		`ALTER DATABASE ` + pgx.Identifier{name}.Sanitize() + ` IS_TEMPLATE true`,
		`GRANT CONNECT ON DATABASE ` + pgx.Identifier{name}.Sanitize() + ` TO stator_app, stator_admin`,
	} {
		if _, err := super.Exec(ctx, q); err != nil {
			return "", err
		}
	}
	return name, nil
}

// migrate runs the embedded migrations against a database the way
// cmd/migrate does, as the superuser that owns the stack's schema.
func migrate(ctx context.Context, database string) error {
	target, err := databaseURL(os.Getenv(envSuperuser), database)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, target)
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, ".", goose.WithAllowMissing())
}

// clone is a database cloned from the template and replayed on the standby,
// or why it could not be made.
type clone struct {
	name string
	err  error
}

// makeClone is made while its test waits rather than one test ahead: a clone
// made beside a running test held that test's replica back by seconds on a
// busy machine, past what its own waits allow.
func makeClone(ctx context.Context, super *pgxpool.Pool, replicaURL string) clone {
	template, err := migratedTemplate(ctx, super)
	if err != nil {
		return clone{err: fmt.Errorf("the template database: %w", err)}
	}
	if err := dropRetired(ctx, super); err != nil {
		return clone{err: err}
	}
	suffix := make([]byte, cloneSuffixBytes)
	_, _ = rand.Read(suffix)
	name := testDatabasePrefix + hex.EncodeToString(suffix)
	if _, err := super.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()+` TEMPLATE `+pgx.Identifier{template}.Sanitize()); err != nil {
		return clone{err: fmt.Errorf("clone the template database: %w", err)}
	}
	// The clone, and the drops before it, must have reached the standby
	// before anything connects to it there, or pauses replay in front of it.
	if replicaURL != "" {
		if err := waitForStandby(ctx, super, replicaURL); err != nil {
			return clone{name: name, err: err}
		}
	}
	return clone{name: name}
}

// cloneDatabase gives the test a database of its own, cloned from the
// template, and points every connection the suite reads from the
// environment at it, on the primary and the replica alike, until it ends.
func cloneDatabase(t *testing.T) {
	t.Helper()
	// A subtest, or a second harness in one test, shares its test's database.
	if strings.HasPrefix(databaseOf(os.Getenv(envPrimary)), testDatabasePrefix) {
		return
	}
	moveToClone(t)
}

// lastingSuperuserURL reaches the superuser in a database that outlives the
// test, since the run's last drops connect through the last one retired.
func lastingSuperuserURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv(envSuperuser)
	if !strings.HasPrefix(databaseOf(u), testDatabasePrefix) {
		return u
	}
	lasting, err := databaseURL(u, maintenanceDatabase)
	if err != nil {
		t.Fatalf("%s: %v", envSuperuser, err)
	}
	return lasting
}

// newFreshHarness connects to another database of the test's own, freshly
// cloned: a second instance, for what travels from one to another (#380).
// What the suite reads from the environment afterwards reaches the new one.
func newFreshHarness(t *testing.T) *harness {
	t.Helper()
	moveToClone(t)
	return connectHarness(t)
}

// moveToClone clones the template and points every connection the suite
// reads from the environment at the clone until the test ends.
func moveToClone(t *testing.T) {
	t.Helper()
	superURL := lastingSuperuserURL(t)
	ctx := context.Background()
	super, err := pgxpool.New(ctx, superURL)
	if err != nil {
		t.Fatalf("connect superuser: %v", err)
	}
	c := makeClone(ctx, super, os.Getenv(envReplicaSuperuser))
	super.Close()
	if c.name != "" {
		// Registered first, so it runs after every cleanup that still uses it.
		t.Cleanup(func() { retire(superURL, c.name) })
	}
	if c.err != nil {
		t.Fatal(c.err)
	}
	pointAt(t, c.name)
}

// pointAt sends every connection the suite reads from the environment to the
// database named until the test ends.
func pointAt(t *testing.T, name string) {
	t.Helper()
	for _, env := range []string{envPrimary, envAdmin, envSuperuser, envReplicaSuperuser} {
		if v := os.Getenv(env); v != "" {
			moved, err := databaseURL(v, name)
			if err != nil {
				t.Fatalf("%s: %v", env, err)
			}
			t.Setenv(env, moved)
		}
	}
	if v := os.Getenv(envReplicas); v != "" {
		var moved []string
		for _, u := range strings.Split(v, ",") {
			m, err := databaseURL(strings.TrimSpace(u), name)
			if err != nil {
				t.Fatalf("%s: %v", envReplicas, err)
			}
			moved = append(moved, m)
		}
		t.Setenv(envReplicas, strings.Join(moved, ","))
	}
}

// waitForStandby waits until the standby has replayed everything the primary
// has written so far.
func waitForStandby(ctx context.Context, super *pgxpool.Pool, replicaURL string) error {
	var text string
	if err := super.QueryRow(ctx, `SELECT pg_logical_emit_message(false, 'stator-clone', '', true)::text`).Scan(&text); err != nil {
		return fmt.Errorf("flush the primary's WAL: %w", err)
	}
	lsn, err := db.ParseLSN(text)
	if err != nil {
		return err
	}
	// The maintenance database: the clone may not be there yet.
	maintenance, err := databaseURL(replicaURL, maintenanceDatabase)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, maintenance)
	if err != nil {
		return fmt.Errorf("connect superuser on the replica: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	deadline := time.Now().Add(cloneReplayWait)
	for {
		var at string
		if err := conn.QueryRow(ctx, `SELECT COALESCE(pg_last_wal_replay_lsn(), '0/0')::text`).Scan(&at); err != nil {
			return fmt.Errorf("read the replica's position: %w", err)
		}
		if got, err := db.ParseLSN(at); err == nil && got >= lsn {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the replica did not replay the clone within %s", cloneReplayWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(replicaPollInterval):
		}
	}
}
