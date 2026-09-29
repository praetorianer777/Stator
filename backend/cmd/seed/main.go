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
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/secret"
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

	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	// Somebody has to be able to get in before any identity provider is set
	// up, and that somebody is named by the operator rather than invented here.
	if cfg.Bootstrap.AdminEmail != "" {
		made, err := accounts.EnsureAdmin(ctx, seed.DemoOrgSlug, cfg.Bootstrap.AdminEmail, bootstrapAdminName, cfg.Bootstrap.AdminPassword)
		if err != nil {
			return err
		}
		log.Info("bootstrap administrator ready", "email", cfg.Bootstrap.AdminEmail, "org", seed.DemoOrgSlug, "created", made)
	}

	var box *secret.Box
	if cfg.SecretKey != nil {
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	}
	org, err := accounts.OrgBySlug(ctx, seed.DemoOrgSlug)
	if err != nil {
		return err
	}
	// Named ahead of time, so they sign in through the provider without
	// waiting for anybody to let them in.
	sso := oidc.NewService(cluster, box, cfg.Auth.OIDCRedirectURL)
	done, err := seed.Populate(ctx, accounts, sso, *org, seed.Bootstrapped(cfg.Bootstrap))
	if err != nil {
		return err
	}
	for _, m := range done.Added {
		log.Info("member let in ahead of sign-in", "email", m.Email, "role", m.Role, "org", seed.DemoOrgSlug)
	}
	switch {
	case done.ProviderCreated:
		log.Info("identity provider configured", "org", seed.DemoOrgSlug, "issuer", cfg.Bootstrap.OIDCIssuer)
	case done.ProviderExisting:
		log.Info("the demo organization's identity provider is already set up", "org", seed.DemoOrgSlug)
	}
	return nil
}

// bootstrapAdminName is what the bootstrap administrator is called until they
// sign in through a provider, which then names them.
const bootstrapAdminName = "Administrator"
