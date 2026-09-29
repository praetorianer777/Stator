// Package seed makes the development database worth opening. Every step finds
// what an earlier run made, so running it again changes nothing.
package seed

import (
	"context"
	"errors"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Seeded names, fixed so a second run finds what the first made.
const (
	DemoOrgSlug = "demo"
	DemoOrgName = "Demo"
)

// Result says what a run made, as opposed to what it found already there.
type Result struct {
	OrgCreated bool
}

// Run seeds the cluster through the admin role, since making an organization
// precedes any tenant.
func Run(ctx context.Context, cluster *db.Cluster) (Result, error) {
	var res Result
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) ON CONFLICT (slug) DO NOTHING`, DemoOrgSlug, DemoOrgName)
		res.OrgCreated = tag.RowsAffected() == 1
		return err
	})
	return res, err
}

// Member is one address let into an organization ahead of its first sign-in.
type Member struct {
	Email string
	Role  auth.OrgRole
}

// People is who an organization is seeded with: members named ahead of time,
// and the identity provider they sign in through, when there is one.
type People struct {
	Members  []Member
	Provider *oidc.Provider
}

// Bootstrapped is who STATOR_BOOTSTRAP_MEMBERS and STATOR_BOOTSTRAP_OIDC_*
// name: the demo organization's people, and every throwaway one's.
func Bootstrapped(cfg config.Bootstrap) People {
	var people People
	for _, m := range cfg.Members {
		people.Members = append(people.Members, Member{Email: m.Email, Role: auth.OrgRole(m.Role)})
	}
	if cfg.OIDCIssuer != "" {
		people.Provider = &oidc.Provider{
			Issuer:       cfg.OIDCIssuer,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			CreateGroups: true,
			Enabled:      true,
		}
	}
	return people
}

// Populated says what Populate did, as opposed to what it found.
type Populated struct {
	Added            []Member
	ProviderCreated  bool
	ProviderExisting bool
}

// Populate lets the people into the organization and points it at the
// provider, once: a provider already there is an administrator's to change.
func Populate(ctx context.Context, accounts *auth.Service, sso *oidc.Service, org tenant.Org, people People) (Populated, error) {
	var out Populated
	for _, m := range people.Members {
		added, err := accounts.EnsureMember(ctx, org.Slug, m.Email, m.Role)
		if err != nil {
			return out, err
		}
		if added {
			out.Added = append(out.Added, m)
		}
	}
	if people.Provider == nil {
		return out, nil
	}
	ctx = tenant.WithOrg(ctx, org)
	switch _, err := sso.Provider(ctx); {
	case err == nil:
		out.ProviderExisting = true
		return out, nil
	case !errors.Is(err, oidc.ErrNotConfigured):
		return out, err
	}
	if _, _, err := sso.Save(ctx, *people.Provider); err != nil {
		return out, err
	}
	out.ProviderCreated = true
	return out, nil
}
