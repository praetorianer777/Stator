// Package seed makes the development database worth opening. Every step finds
// what an earlier run made, so running it again changes nothing.
package seed

import (
	"context"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/oidc"
)

// Seeded names. Nobody types or reads the slug; it only answers the public
// addresses of the time the site was an organization.
const (
	DemoOrgSlug = "demo"
	DemoOrgName = "Demo"
)

// Result says what a run made, as opposed to what it found already there.
type Result struct {
	OrgCreated bool
}

// Run seeds the cluster through the admin role, since making the site
// precedes any member.
func Run(ctx context.Context, cluster *db.Cluster) (Result, error) {
	return Ensure(ctx, cluster, DemoOrgName)
}

// Ensure makes the instance's site when there is none, and leaves an existing
// one, whatever it is now called, as it is.
func Ensure(ctx context.Context, cluster *db.Cluster, name string) (Result, error) {
	var res Result
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `INSERT INTO site (slug, name) SELECT $1, $2 WHERE NOT EXISTS (SELECT FROM site)`, DemoOrgSlug, name)
		res.OrgCreated = tag.RowsAffected() == 1
		return err
	})
	return res, err
}

// Member is one address let in ahead of its first sign-in.
type Member struct {
	Email string
	Role  auth.Role
}

// People is who the site is seeded with: members named ahead of time,
// and the identity provider they sign in through, when there is one.
type People struct {
	Members  []Member
	Provider *oidc.Provider
}

// Bootstrapped is who STATOR_BOOTSTRAP_MEMBERS and STATOR_BOOTSTRAP_OIDC_*
// name: the people of the site seed or seed bootstrap makes.
func Bootstrapped(cfg config.Bootstrap) People {
	var people People
	for _, m := range cfg.Members {
		people.Members = append(people.Members, Member{Email: m.Email, Role: auth.Role(m.Role)})
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

// Populate lets the people in and points the site at the provider, once: a
// provider already there is an administrator's to change.
func Populate(ctx context.Context, accounts *auth.Service, sso *oidc.Service, people People) (Populated, error) {
	var out Populated
	for _, m := range people.Members {
		added, err := accounts.EnsureMember(ctx, m.Email, m.Role)
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
	created, err := sso.Bootstrap(ctx, *people.Provider)
	if err != nil {
		return out, err
	}
	out.ProviderCreated, out.ProviderExisting = created, !created
	return out, nil
}
