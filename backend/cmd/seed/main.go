// Command seed makes the development database worth opening: for now one
// organization, created once however often it runs.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
)

// Seeded names, fixed so a second run finds what the first made.
const (
	demoOrgSlug = "demo"
	demoOrgName = "Demo"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx := context.Background()
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	// Making an organization precedes any tenant, which is what the admin
	// role is for.
	var created bool
	_, err = cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) ON CONFLICT (slug) DO NOTHING`, demoOrgSlug, demoOrgName)
		created = tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return err
	}
	log.Info("seed done", "org", demoOrgSlug, "created", created)
	return nil
}
