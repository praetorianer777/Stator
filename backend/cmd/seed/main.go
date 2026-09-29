// Command seed makes the development database worth opening: the demo
// organization and, when configured, its first local administrator.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/seed"
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

	res, err := seed.Run(ctx, cluster)
	if err != nil {
		return err
	}
	log.Info("seed done", "org", seed.DemoOrgSlug, "created", res.OrgCreated)

	// Somebody has to be able to get in before any identity provider is set
	// up, and that somebody is named by the operator rather than invented here.
	if cfg.Bootstrap.AdminEmail == "" {
		return nil
	}
	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	made, err := accounts.EnsureAdmin(ctx, seed.DemoOrgSlug, cfg.Bootstrap.AdminEmail, bootstrapAdminName, cfg.Bootstrap.AdminPassword)
	if err != nil {
		return err
	}
	log.Info("bootstrap administrator ready", "email", cfg.Bootstrap.AdminEmail, "org", seed.DemoOrgSlug, "created", made)
	return nil
}

// bootstrapAdminName is what the bootstrap administrator is called until they
// sign in through a provider, which then names them.
const bootstrapAdminName = "Administrator"
