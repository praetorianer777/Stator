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
)

// The token and revocation endpoints, adapted from Armature's
// backend/internal/oauth/token.go so a partner meets the same answers.

// Credentials are how an app says who it is: HTTP Basic, or the form, never
// both ways. A public app sends its client_id alone.
type Credentials struct {
	ID, Secret string
	// Basic is set when they came in the Authorization header.
	Basic bool
	// sentSecret tells a public app sending a secret from one sending none.
	sentSecret bool
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
		return Credentials{ID: id, Secret: secret, Basic: true, sentSecret: true}, nil
	}
	if form.Get("client_id") == "" {
		return Credentials{}, refuse(http.StatusUnauthorized, "invalid_client", "Say which app is asking, with its client_id, and its secret when it has one.")
	}
	return Credentials{ID: form.Get("client_id"), Secret: form.Get("client_secret"), sentSecret: form.Has("client_secret")}, nil
}

// clientRow is an app as the OAuth endpoints read it, with what proves it.
type clientRow struct {
	ID           uuid.UUID
	Name         string
	Scopes       []string
	Kind, Status string
	ClientURI    string
	RedirectURIs []string
	// KeycloakClientID is nil for an app that registered itself.
	KeycloakClientID *string
	// digest is nil for a public app.
	digest []byte
}

// loadClient reads an app, one pending past PendingClientTTL reading as none.
func (s *Service) loadClient(ctx context.Context, tx db.DBTX, id uuid.UUID) (*clientRow, error) {
	var c clientRow
	err := tx.QueryRow(ctx, `
		SELECT id, name, scopes, kind, status, client_uri, redirect_uris, keycloak_client_id, secret_hash
		FROM oauth_client WHERE id = $1 AND (status = 'allowed' OR created_at >= $2)`, id, s.now().Add(-PendingClientTTL)).
		Scan(&c.ID, &c.Name, &c.Scopes, &c.Kind, &c.Status, &c.ClientURI, &c.RedirectURIs, &c.KeycloakClientID, &c.digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchClient
	}
	return &c, err
}

// authenticate finds the app the credentials prove, or refuses them; an app
// nobody allowed yet proves nothing.
func (s *Service) authenticate(ctx context.Context, tx db.DBTX, cred Credentials) (*clientRow, error) {
	refused := &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Basic: cred.Basic,
		Description: "Stator does not know this app, its secret is wrong, or no Stator administrator has allowed it yet."}
	id, err := uuid.Parse(cred.ID)
	if err != nil {
		return nil, refused
	}
	c, err := s.loadClient(ctx, tx, id)
	if errors.Is(err, ErrNoSuchClient) {
		return nil, refused
	}
	if err != nil {
		return nil, err
	}
	if c.Status != StatusAllowed {
		return nil, refused
	}
	if c.digest == nil {
		if cred.sentSecret {
			return nil, refused
		}
		return c, nil
	}
	if cred.Secret == "" || subtle.ConstantTimeCompare(auth.HashToken(cred.Secret), c.digest) != 1 {
		return nil, refused
	}
	return c, nil
}

// singleParams may each be sent once at most.
var singleParams = []string{"grant_type", "code", "redirect_uri", "code_verifier", "refresh_token", "scope", "subject_token",
	"subject_token_type", "requested_token_type", "client_id", "client_secret", "resource", "audience", ParamExpiresIn, ParamSpaces}

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
		client, err = s.authenticate(ctx, tx, cred)
		return err
	}); err != nil {
		return nil, err
	}
	// A partner's tokens are for the whole API, as #461 promised; an app that
	// registered itself is held to the one resource it may be given.
	if resource := form.Get("resource"); client.Kind == KindRegistered && resource != "" && !s.resourceMatches(resource) {
		return nil, refuse(http.StatusBadRequest, "invalid_target", "The one resource is Stator's MCP endpoint, "+s.Resource()+".")
	}
	switch grant {
	case GrantAuthorizationCode:
		return s.redeemCode(ctx, client, form)
	case GrantRefreshToken:
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

// Revoke is RFC 7009: a grant's access or refresh token ends the whole grant, an exchanged
// token only itself. Every token is answered alike, another app's left alone.
func (s *Service) Revoke(ctx context.Context, cred Credentials, token string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		client, err := s.authenticate(ctx, tx, cred)
		if err != nil || token == "" {
			return err
		}
		digest := auth.HashToken(token)
		var id, userID uuid.UUID
		err = tx.QueryRow(ctx, `
			DELETE FROM api_token WHERE client_id = $2 AND (token_hash = $1 OR refresh_hash = $1)
			RETURNING id, user_id`, digest, client.ID).Scan(&id, &userID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx, `
				DELETE FROM oauth_exchanged_token x USING api_token t
				WHERE x.token_hash = $1 AND t.id = x.grant_id AND t.client_id = $2`, digest, client.ID)
			return err
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "api_token", TargetID: &id, Actor: userID,
			Data: map[string]any{"owner": userID, "clientId": client.ID, "by": "app"}})
	})
	return err
}
