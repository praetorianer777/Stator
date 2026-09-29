// Command seed makes the development database worth opening: the demo
// organization and, when configured, its first local administrator.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/seed"
	"github.com/praetorianer777/stator/backend/internal/tenant"
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
	if cfg.Bootstrap.OIDCIssuer != "" {
		return seedProvider(ctx, cfg, cluster, accounts, log)
	}
	return nil
}

// seedProvider points the demo organization at the configured provider, once:
// a provider already there is an administrator's to change, not the seed's.
func seedProvider(ctx context.Context, cfg config.Config, cluster *db.Cluster, accounts *auth.Service, log *slog.Logger) error {
	var box *secret.Box
	if cfg.SecretKey != nil {
		var err error
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	}
	org, err := accounts.OrgBySlug(ctx, seed.DemoOrgSlug)
	if err != nil {
		return err
	}
	ctx = tenant.WithOrg(ctx, *org)
	sso := oidc.NewService(cluster, box, cfg.Auth.OIDCRedirectURL)
	switch _, err := sso.Provider(ctx); {
	case err == nil:
		log.Info("the demo organization's identity provider is already set up", "org", seed.DemoOrgSlug)
		return nil
	case !errors.Is(err, oidc.ErrNotConfigured):
		return err
	}
	_, _, err = sso.Save(ctx, oidc.Provider{
		Issuer:       cfg.Bootstrap.OIDCIssuer,
		ClientID:     cfg.Bootstrap.OIDCClientID,
		ClientSecret: cfg.Bootstrap.OIDCClientSecret,
		CreateGroups: true,
		Enabled:      true,
	})
	if err != nil {
		return err
	}
	log.Info("identity provider configured", "org", seed.DemoOrgSlug, "issuer", cfg.Bootstrap.OIDCIssuer)
	return nil
}

// bootstrapAdminName is what the bootstrap administrator is called until they
// sign in through a provider, which then names them.
const bootstrapAdminName = "Administrator"
