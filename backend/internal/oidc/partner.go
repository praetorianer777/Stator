package oidc

import (
	"context"
	"errors"
	"fmt"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// A partner tool of the same realm presents a person's access token of the
// site's provider to have Stator's token for them (#461). The provider's
// signature and Stator's own client in the audience are what make it the
// person's word for Stator, not only for the partner.

// ErrNotForPartner is a token that is the provider's but not one a partner
// may trade here: issued to another client, or not meant for Stator.
var ErrNotForPartner = errors.New("the token was not issued to the partner app for Stator")

// VerifyAccessToken checks an access token of the site's provider as a
// partner presents it: signed by the provider, unexpired, its aud holding
// Stator's client and its azp the partner's Keycloak client. It answers the
// person it names, by a verified address.
func (s *Service) VerifyAccessToken(ctx context.Context, raw, azp string) (*Identity, error) {
	provider, err := s.signInProvider(ctx)
	if err != nil {
		return nil, err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, err
	}
	verified, err := discovered.Verifier(&coreoidc.Config{ClientID: provider.ClientID}).Verify(s.providerContext(ctx), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotForPartner, err)
	}
	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the token's claims: %w", err)
	}
	if got := asString(claims["azp"]); got != azp {
		return nil, fmt.Errorf("%w: it was issued to %q", ErrNotForPartner, got)
	}
	return identityFromClaims(verified.Issuer, verified.Subject, claims, provider.GroupsClaim)
}

// Admit finds the member a verified identity names, or lets them in by the
// group mapping as a sign-in would, and keeps their groups as the token says;
// no session is opened. Somebody no mapping lets in is ErrNotAMember, and
// nothing about them is kept. It answers the person and their standing.
func (s *Service) Admit(ctx context.Context, identity *Identity) (uuid.UUID, auth.Role, error) {
	var (
		userID uuid.UUID
		role   auth.Role
	)
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if userID, err = upsertUser(ctx, tx, identity); err != nil {
			return err
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM app_user WHERE id = $1`, userID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return auth.ErrUserInactive
		}
		var create bool
		err = tx.QueryRow(ctx, `SELECT create_groups FROM oidc_provider WHERE enabled`).Scan(&create)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotConfigured
		}
		if err != nil {
			return err
		}
		if role, err = followMapping(ctx, tx, userID, identity.Groups); err != nil {
			return err
		}
		if role != auth.RoleGuest {
			_, _, err = syncGroups(ctx, tx, userID, identity.Groups, create)
		}
		return err
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	return userID, role, nil
}
