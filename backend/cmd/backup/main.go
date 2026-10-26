// Command backup writes the site's database and stored files into one archive
// (backup create), and brings a site back from one (backup restore). It
// connects through STATOR_DB_PRIMARY_URL, which must name the schema's owner.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/praetorianer777/stator/backend/internal/backup"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/version"
)

// Where the compose service and the chart's jobs mount the backup volume.
const defaultDir = "/backups"

const usage = `Usage:
  backup create  [-dir DIR]
  backup restore [-dir DIR] [-from FILE] -yes

create writes stator-<time>.tar into DIR. restore replaces the database and
every stored file with the archive's; -from defaults to the newest archive
in DIR. Stop the api and the worker before a restore.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		slog.Error("backup failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("dir", defaultDir, "the directory archives are written to and read from")
	from := flags.String("from", "", "the archive to restore; the newest in -dir when empty")
	yes := flags.Bool("yes", false, "confirm that a restore replaces the site's data")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	// Refusals come before config, the database and storage, so a refused
	// restore touches nothing and needs none of them reachable.
	switch args[0] {
	case "create":
	case "restore":
		if !*yes {
			return errors.New("A restore replaces every page, person and file of the site with the backup's. Run it again with -yes to go ahead.")
		}
	default:
		return fmt.Errorf("%q is not a backup command.\n%s", args[0], usage)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.DB.PrimaryURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := openStore(ctx, cfg.S3)
	if err != nil {
		return err
	}

	if args[0] == "create" {
		return create(ctx, out, pool, store, cfg, *dir)
	}
	return restore(ctx, out, pool, store, cfg, *dir, *from)
}

func openStore(ctx context.Context, cfg config.S3) (objectstore.Store, error) {
	if cfg.Endpoint == "" {
		return nil, nil
	}
	s3, err := objectstore.NewS3(objectstore.Config{
		Endpoint:  cfg.Endpoint,
		Bucket:    cfg.Bucket,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		Region:    cfg.Region,
		UseSSL:    cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	// A restore into a fresh installation finds no bucket yet.
	return s3, s3.EnsureBucket(ctx)
}

func create(ctx context.Context, out io.Writer, pool *pgxpool.Pool, store objectstore.Store, cfg config.Config, dir string) error {
	applied, err := backup.Applied(ctx, pool)
	if err != nil {
		return err
	}
	v := version.Current()
	path, sum, err := backup.CreateFile(ctx, dir, backup.Source{
		Version:    v.Version,
		Commit:     v.Commit,
		Migrations: applied,
		Key:        cfg.SecretKey,
		Dump:       backup.Dump(cfg.DB.PrimaryURL),
		Store:      store,
		TempDir:    dir,
		Now:        time.Now,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s with the database and %d stored files.\n", path, sum.Objects)
	for _, key := range sum.Missing {
		fmt.Fprintf(out, "Left out %s, removed while the backup ran.\n", key)
	}
	return nil
}

func restore(ctx context.Context, out io.Writer, pool *pgxpool.Pool, store objectstore.Store, cfg config.Config, dir, from string) error {
	if from == "" {
		latest, err := backup.Latest(dir)
		if err != nil {
			return err
		}
		from = latest
	}
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	defer f.Close()
	known, err := backup.Known()
	if err != nil {
		return err
	}

	m, sum, err := backup.Restore(ctx, f, backup.Target{
		Check: func(m backup.Manifest) error {
			if err := m.Compatible(known, cfg.SecretKey); err != nil {
				return err
			}
			return backup.InUse(ctx, pool)
		},
		Load:    backup.Load(cfg.DB.PrimaryURL),
		Store:   store,
		TempDir: dir,
	})
	if err != nil {
		return err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if err := backup.Migrate(ctx, db); err != nil {
		return err
	}
	fmt.Fprintf(out, "Restored %s, taken %s with Stator %s: the database and %d stored files", from, m.CreatedAt.Format(time.RFC3339), m.Version, sum.Objects)
	if sum.Removed > 0 {
		fmt.Fprintf(out, ", removing %d files the backup did not have", sum.Removed)
	}
	fmt.Fprintln(out, ".")
	return nil
}
