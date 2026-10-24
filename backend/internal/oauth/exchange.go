package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/oidc"
)

// The token exchange of #461, adapted from Armature's fromIdentityProvider in
// backend/internal/oauth/token.go, for partner apps alone.

// exchange trades a person's access token of the shared identity provider
// for a grant. The administrator who named the app's Keycloak client
// consented for everybody, so no page is shown.
func (s *Service) exchange(ctx context.Context, client *clientRow, form url.Values) (*TokenResponse, error) {
	if err := required(form, "subject_token", "subject_token_type"); err != nil {
		return nil, err
	}
	if form.Get("subject_token_type") != TokenTypeAccess {
		return nil, refuse(http.StatusBadRequest, "invalid_request", "Stator exchanges access tokens only: subject_token_type is "+TokenTypeAccess+".")
	}
	if kind := form.Get("requested_token_type"); kind != "" && kind != TokenTypeAccess {
		return nil, refuse(http.StatusBadRequest, "invalid_request", "Stator issues access tokens only: requested_token_type is "+TokenTypeAccess+".")
	}
	if client.KeycloakClientID == nil {
		return nil, refuse(http.StatusBadRequest, "unauthorized_client",
			client.Name+" may not exchange identity provider tokens. A person connects it on Stator's consent page instead.")
	}
	scopes, err := asked(form.Get("scope"), client.Scopes)
	if err != nil {
		return nil, err
	}
	identity, err := s.sso.VerifyAccessToken(ctx, form.Get("subject_token"), *client.KeycloakClientID)
	switch {
	case errors.Is(err, oidc.ErrNotConfigured):
		return nil, refuse(http.StatusBadRequest, "unauthorized_client", "Single sign-on is not set up in this Stator, so there is no identity provider token to exchange.")
	case errors.Is(err, oidc.ErrUnreachable):
		return nil, err
	case err != nil:
		s.log.Info("a partner's identity provider token was refused", "client_id", client.ID, "error", err)
		return nil, refuse(http.StatusBadRequest, "invalid_grant", "The identity provider's token was refused: "+sentence(err))
	}
	userID, role, err := s.sso.Admit(ctx, identity)
	switch {
	case errors.Is(err, oidc.ErrNotAMember) || errors.Is(err, auth.ErrUserInactive) || (err == nil && role == auth.RoleGuest):
		return nil, refuse(http.StatusBadRequest, "invalid_grant",
			identity.Name+" has no Stator account yet. Sign in to Stator once, or ask an administrator to let them in.")
	case err != nil:
		return nil, err
	}
	return s.grant(ctx, client, userID, scopes)
}

// sentence is a refusal's reason as the end of a sentence.
func sentence(err error) string {
	return strings.TrimSuffix(err.Error(), ".") + "."
}

// grant makes or renews the person's one grant for the app. A live grant
// keeps its scopes and gains the ones asked, as Armature's does
// (Cloudster1/Armature#298); only a narrower refresh narrows it.
func (s *Service) grant(ctx context.Context, client *clientRow, userID uuid.UUID, scopes []string) (*TokenResponse, error) {
	access, accessDigest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, err
	}
	refresh, refreshDigest, err := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var (
		id   uuid.UUID
		held []string
	)
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Held as the app may still give them, so a narrowed app gives no more.
		var before []string
		err := tx.QueryRow(ctx, `SELECT scopes FROM api_token WHERE user_id = $1 AND client_id = $2 AND expires_at > now()`, userID, client.ID).Scan(&before)
		switch {
		case err == nil:
			for _, scope := range grantScopes(before) {
				if !slices.Contains(scopes, scope) && slices.Contains(client.Scopes, scope) {
					scopes = append(scopes, scope)
				}
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (user_id, client_id) WHERE client_id IS NOT NULL DO UPDATE SET
			    name = EXCLUDED.name, token_hash = EXCLUDED.token_hash, scopes = EXCLUDED.scopes,
			    expires_at = EXCLUDED.expires_at, refresh_hash = EXCLUDED.refresh_hash,
			    access_expires_at = EXCLUDED.access_expires_at
			RETURNING id, scopes`,
			userID, client.Name+auth.ConnectedAppSuffix, accessDigest, tokenScopes(scopes), now.Add(RefreshTokenTTL),
			client.ID, refreshDigest, now.Add(AccessTokenTTL)).Scan(&id, &held); err != nil {
			return fmt.Errorf("store the grant: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthGrantCreated, TargetType: "api_token", TargetID: &id, Actor: userID,
			Data: map[string]any{"clientId": client.ID, "app": client.Name, "scopes": grantScopes(held)}})
	})
	if err != nil {
		return nil, err
	}
	return &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTokenTTL / time.Second),
		RefreshToken: refresh, Scope: joinScope(grantScopes(held)), IssuedTokenType: TokenTypeAccess}, nil
}
