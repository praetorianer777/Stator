package oauth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
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

// The token and revocation endpoints, adapted from Armature's
// backend/internal/oauth/token.go so a partner meets the same answers.

// Credentials are how an app says who it is: HTTP Basic, or both fields in
// the form, never both ways.
type Credentials struct {
	ID, Secret string
	// Basic is set when they came in the Authorization header.
	Basic bool
}

// ReadCredentials takes an app's credentials from the Authorization header
// or the form.
func ReadCredentials(header string, form url.Values) (Credentials, error) {
	badBasic := &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Description: "Send the client id and secret as HTTP Basic, form-encoded first.", Basic: true}
	if encoded, ok := strings.CutPrefix(header, "Basic "); ok {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		id, secret, found := strings.Cut(string(raw), ":")
		if err != nil || !found {
			return Credentials{}, badBasic
		}
		// RFC 6749 section 2.3.1 form-encodes both before joining them.
		if id, err = url.QueryUnescape(id); err == nil {
			secret, err = url.QueryUnescape(secret)
		}
		if err != nil {
			return Credentials{}, badBasic
		}
		if form.Has("client_secret") || (form.Has("client_id") && form.Get("client_id") != id) {
			return Credentials{}, refuse(http.StatusBadRequest, "invalid_request", "Authenticate one way: HTTP Basic or the form, not both.")
		}
		return Credentials{ID: id, Secret: secret, Basic: true}, nil
	}
	if form.Get("client_id") == "" || form.Get("client_secret") == "" {
		return Credentials{}, refuse(http.StatusUnauthorized, "invalid_client", "Say which app is asking, with its client id and secret.")
	}
	return Credentials{ID: form.Get("client_id"), Secret: form.Get("client_secret")}, nil
}

type clientRow struct {
	ID               uuid.UUID
	Name             string
	Scopes           []string
	KeycloakClientID string
}

// authenticate finds the app the credentials prove, or refuses them.
func authenticate(ctx context.Context, tx db.DBTX, cred Credentials) (*clientRow, error) {
	refused := &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Basic: cred.Basic,
		Description: "Stator does not know this app, or its secret is wrong. A Stator administrator registers it under the connected apps."}
	id, err := uuid.Parse(cred.ID)
	if err != nil {
		return nil, refused
	}
	var (
		c      clientRow
		digest []byte
	)
	err = tx.QueryRow(ctx, `SELECT id, name, scopes, keycloak_client_id, secret_hash FROM oauth_client WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Scopes, &c.KeycloakClientID, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, refused
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(auth.HashToken(cred.Secret), digest) != 1 {
		return nil, refused
	}
	return &c, nil
}

// singleParams may each be sent once at most.
var singleParams = []string{"grant_type", "refresh_token", "scope", "subject_token", "subject_token_type",
	"requested_token_type", "client_id", "client_secret", "resource", "audience"}

// Token answers the token endpoint.
func (s *Service) Token(ctx context.Context, cred Credentials, form url.Values) (*TokenResponse, error) {
	for _, name := range singleParams {
		if len(form[name]) > 1 {
			return nil, refuse(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s once.", name))
		}
	}
	grant := form.Get("grant_type")
	if grant == "" {
		return nil, refuse(http.StatusBadRequest, "invalid_request", "Say which grant, with grant_type.")
	}
	if !slices.Contains(GrantTypes, grant) {
		return nil, refuse(http.StatusBadRequest, "unsupported_grant_type", "Stator offers "+strings.Join(GrantTypes, ", ")+".")
	}
	var client *clientRow
	if err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		client, err = authenticate(ctx, tx, cred)
		return err
	}); err != nil {
		return nil, err
	}
	if grant == GrantRefreshToken {
		return s.refresh(ctx, client, form)
	}
	return s.exchange(ctx, client, form)
}

func required(form url.Values, names ...string) error {
	for _, name := range names {
		if form.Get(name) == "" {
			return refuse(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s.", name))
		}
	}
	return nil
}

// asked is the scope parameter held to what may be had: read among them,
// nothing beyond. Left out, it is everything that may be had.
func asked(raw string, allowed []string) ([]string, error) {
	scopes := splitScope(raw)
	if len(scopes) == 0 {
		return slices.Clone(allowed), nil
	}
	if !slices.Contains(scopes, ScopeRead) || slices.ContainsFunc(scopes, func(s string) bool { return !slices.Contains(allowed, s) }) {
		return nil, refuse(http.StatusBadRequest, "invalid_scope", "Ask for read and at most "+joinScope(allowed)+".")
	}
	return scopes, nil
}

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
	scopes, err := asked(form.Get("scope"), client.Scopes)
	if err != nil {
		return nil, err
	}
	identity, err := s.sso.VerifyAccessToken(ctx, form.Get("subject_token"), client.KeycloakClientID)
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

// refresh rotates a grant's tokens. A refresh token rotated already ends the
// grant: the partner refreshes one grant in one place at a time.
func (s *Service) refresh(ctx context.Context, client *clientRow, form url.Values) (*TokenResponse, error) {
	if err := required(form, "refresh_token"); err != nil {
		return nil, err
	}
	digest := auth.HashToken(form.Get("refresh_token"))
	access, accessDigest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, err
	}
	refresh, refreshDigest, err := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
	if err != nil {
		return nil, err
	}
	var (
		out *TokenResponse
		// refusal is answered after the transaction commits, so ending a
		// grant on a replayed refresh token is kept.
		refusal error
	)
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			id      uuid.UUID
			held    []string
			expires time.Time
			member  bool
		)
		err := tx.QueryRow(ctx, `
			SELECT t.id, t.scopes, t.expires_at,
			       EXISTS (SELECT 1 FROM member m JOIN app_user u ON u.id = m.user_id
			               WHERE m.user_id = t.user_id AND u.is_active AND m.role <> 'guest')
			FROM api_token t WHERE t.refresh_hash = $1 AND t.client_id = $2 FOR UPDATE OF t`, digest, client.ID).
			Scan(&id, &held, &expires, &member)
		if errors.Is(err, pgx.ErrNoRows) {
			refusal = ended()
			_, err = tx.Exec(ctx, `
				DELETE FROM api_token t USING oauth_rotated_refresh r
				WHERE r.refresh_hash = $1 AND t.id = r.grant_id AND t.client_id = $2`, digest, client.ID)
			return err
		}
		if err != nil {
			return err
		}
		if !s.now().Before(expires) || !member {
			refusal = ended()
			return nil
		}
		scopes, refused := asked(form.Get("scope"), grantScopes(held))
		if refused != nil {
			refusal = refused
			return nil
		}
		now := s.now()
		if _, err := tx.Exec(ctx, `INSERT INTO oauth_rotated_refresh (refresh_hash, grant_id) VALUES ($1, $2)`, digest, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_rotated_refresh WHERE rotated_at < $1`, now.Add(-RefreshTokenTTL)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE api_token SET token_hash = $2, refresh_hash = $3, scopes = $4, expires_at = $5, access_expires_at = $6
			WHERE id = $1`, id, accessDigest, refreshDigest, tokenScopes(scopes), now.Add(RefreshTokenTTL), now.Add(AccessTokenTTL)); err != nil {
			return err
		}
		out = &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTokenTTL / time.Second),
			RefreshToken: refresh, Scope: joinScope(grantScopes(tokenScopes(scopes)))}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, refusal
	}
	return out, nil
}

// Revoke is RFC 7009: a grant's access or refresh token ends the whole grant.
// Every token is answered alike, another app's left alone.
func (s *Service) Revoke(ctx context.Context, cred Credentials, token string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		client, err := authenticate(ctx, tx, cred)
		if err != nil || token == "" {
			return err
		}
		digest := auth.HashToken(token)
		var id, userID uuid.UUID
		err = tx.QueryRow(ctx, `
			DELETE FROM api_token WHERE client_id = $2 AND (token_hash = $1 OR refresh_hash = $1)
			RETURNING id, user_id`, digest, client.ID).Scan(&id, &userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "api_token", TargetID: &id, Actor: userID,
			Data: map[string]any{"owner": userID, "clientId": client.ID, "by": "app"}})
	})
	return err
}
