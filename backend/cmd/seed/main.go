// Command seed makes a database worth opening. Bare, it makes the demo
// organization of a development stack; `seed bootstrap` makes the organization
// STATOR_BOOTSTRAP_ORG_* names, its first local administrator and, when
// configured, its identity provider and members, which is what a fresh
// production install needs and nothing more.
package main

import (
	"context"
	"errors"
	"fmt"
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
	bootstrap := len(os.Args) > 1 && os.Args[1] == "bootstrap"
	if len(os.Args) > 1 && !bootstrap {
		return fmt.Errorf("unknown command %q; run seed bare for the development organization, or seed bootstrap", os.Args[1])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() && !bootstrap {
		return errors.New("seed makes the development organization, which has no place in production; run seed bootstrap instead")
	}
	if bootstrap && cfg.Bootstrap.AdminEmail == "" {
		return errors.New("seed bootstrap needs STATOR_BOOTSTRAP_ADMIN_EMAIL and STATOR_BOOTSTRAP_ADMIN_PASSWORD: without them nobody could sign in to the organization it makes")
	}
	boot := cfg.Bootstrap
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx := context.Background()
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	res, err := seed.Ensure(ctx, cluster, boot.OrgSlug, boot.OrgName)
	if err != nil {
		return err
	}
	log.Info("seed done", "org", boot.OrgSlug, "created", res.OrgCreated)

	accounts := auth.NewService(cluster, auth.DefaultPasswordParams(), cfg.Auth.SessionTTL)
	// Somebody has to be able to get in before any identity provider is set
	// up, and that somebody is named by the operator rather than invented here.
	if cfg.Bootstrap.AdminEmail != "" {
		made, err := accounts.EnsureAdmin(ctx, boot.OrgSlug, cfg.Bootstrap.AdminEmail, bootstrapAdminName, cfg.Bootstrap.AdminPassword)
		if err != nil {
			return err
		}
		log.Info("bootstrap administrator ready", "email", cfg.Bootstrap.AdminEmail, "org", boot.OrgSlug, "created", made)
	}

	var box *secret.Box
	if cfg.SecretKey != nil {
		if box, err = secret.New(cfg.SecretKey); err != nil {
			return err
		}
	}
	org, err := accounts.OrgBySlug(ctx, boot.OrgSlug)
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
		log.Info("member let in ahead of sign-in", "email", m.Email, "role", m.Role, "org", boot.OrgSlug)
	}
	switch {
	case done.ProviderCreated:
		log.Info("identity provider configured", "org", boot.OrgSlug, "issuer", cfg.Bootstrap.OIDCIssuer)
	case done.ProviderExisting:
		log.Info("the organization's identity provider is already set up", "org", boot.OrgSlug)
	}
	return nil
}

// bootstrapAdminName is what the bootstrap administrator is called until they
// sign in through a provider, which then names them.
const bootstrapAdminName = "Administrator"
