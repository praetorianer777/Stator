package backup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/migrations"
)

// Every table, function and grant lives in public, so emptying it is emptying
// the site; the dump recreates the extensions it held as well. The schema is
// made as initdb makes it, since a dump leaves out what matches that default.
const restorePrelude = `DROP SCHEMA public CASCADE;
CREATE SCHEMA public AUTHORIZATION pg_database_owner;
GRANT USAGE ON SCHEMA public TO PUBLIC;
COMMENT ON SCHEMA public IS 'standard public schema';
`

// Dump runs pg_dump against url. Ownership is left out, so a restore owns
// what it makes as whichever role runs it; grants and policies are kept.
func Dump(url string) func(ctx context.Context, w io.Writer) error {
	return func(ctx context.Context, w io.Writer) error {
		cmd := exec.CommandContext(ctx, "pg_dump", "--format=plain", "--no-owner", "--dbname="+url)
		cmd.Stdout = w
		return run(cmd)
	}
}

// Load replaces the database at url with a dump in one transaction: a restore
// that fails leaves the site as it was.
func Load(url string) func(ctx context.Context, r io.Reader) error {
	return func(ctx context.Context, r io.Reader) error {
		cmd := exec.CommandContext(ctx, "psql", "--no-psqlrc", "--quiet", "--single-transaction",
			"--set=ON_ERROR_STOP=1", "--dbname="+url)
		cmd.Stdin = io.MultiReader(strings.NewReader(restorePrelude), r)
		cmd.Stdout = io.Discard
		return run(cmd)
	}
}

func run(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w: %s", cmd.Args[0], err, msg)
		}
		return fmt.Errorf("%s: %w", cmd.Args[0], err)
	}
	return nil
}

// Applied lists the schema versions the database has.
func Applied(ctx context.Context, pool *pgxpool.Pool) ([]int64, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT version_id FROM goose_db_version WHERE is_applied AND version_id > 0 ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("read the schema version: %w", err)
	}
	var versions []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

var migrationName = regexp.MustCompile(`^(\d+)_.*\.sql$`)

// Known lists the schema versions this build carries.
func Known() ([]int64, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var versions []int64
	for _, e := range entries {
		if m := migrationName.FindStringSubmatch(e.Name()); m != nil {
			v, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				return nil, err
			}
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

// InUse refuses while anybody but a superuser is connected to the database:
// the api or the worker writing during a restore would interleave with it.
// Superusers are left out because operators and their monitoring connect as one.
func InUse(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT a.usename
		FROM pg_stat_activity a JOIN pg_roles r ON r.oid = a.usesysid
		WHERE a.datname = current_database() AND a.pid <> pg_backend_pid()
		  AND a.backend_type = 'client backend' AND NOT r.rolsuper
		ORDER BY 1`)
	if err != nil {
		return fmt.Errorf("check who is connected: %w", err)
	}
	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(users) > 0 {
		return fmt.Errorf("The database is in use by %s. Stop the api and the worker, then restore again.", strings.Join(users, ", "))
	}
	return nil
}

// Migrate brings a restored database up to this build's schema, as the
// migrate command does.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, ".", goose.WithAllowMissing()); err != nil {
		return errors.Join(errors.New("bring the restored database up to date"), err)
	}
	return nil
}
