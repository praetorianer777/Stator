package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The authorization code grant, adapted from Armature's redeemCode in
// backend/internal/oauth/token.go; the grant it makes acts at MCP alone.

// redeemCode trades a code for a grant's tokens. A code used twice ends the
// grant it made, as RFC 6749 section 10.5 asks, kept though the answer refuses.
func (s *Service) redeemCode(ctx context.Context, client *clientRow, form url.Values) (*TokenResponse, error) {
	if client.Kind != KindRegistered {
		return nil, refuse(http.StatusBadRequest, "unauthorized_client", client.Name+" trades identity provider tokens; it is never given a code.")
	}
	if err := required(form, "code", "redirect_uri", "code_verifier"); err != nil {
		return nil, err
	}
	var (
		out     *TokenResponse
		refusal error
	)
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, refusal, err = s.redeemIn(ctx, tx, client, form)
		return err
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, refusal
	}
	return out, nil
}

// code is a stored authorization code.
type code struct {
	clientID, userID    uuid.UUID
	scopes              []string
	spaces              []uuid.UUID
	redirect, challenge string
	resource            string
	expires             time.Time
	used                *time.Time
	grantID             *uuid.UUID
}

func (s *Service) redeemIn(ctx context.Context, tx db.DBTX, client *clientRow, form url.Values) (*TokenResponse, error, error) {
	var c code
	digest := auth.HashToken(form.Get("code"))
	err := tx.QueryRow(ctx, `
		SELECT client_id, user_id, scopes, spaces, redirect_uri, challenge, resource, expires_at, used_at, grant_id
		FROM oauth_code WHERE code_hash = $1 FOR UPDATE`, digest).Scan(
		&c.clientID, &c.userID, &c.scopes, &c.spaces, &c.redirect, &c.challenge, &c.resource, &c.expires, &c.used, &c.grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ended(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if c.clientID != client.ID {
		return nil, ended(), nil
	}
	if c.used != nil {
		if c.grantID != nil {
			if err := endGrant(ctx, tx, *c.grantID, "code_reused"); err != nil {
				return nil, nil, err
			}
		}
		return nil, ended(), nil
	}
	if !s.now().Before(c.expires) || c.redirect != form.Get("redirect_uri") || !VerifyPKCE(c.challenge, form.Get("code_verifier")) {
		return nil, ended(), nil
	}
	var member bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM member m JOIN app_user u ON u.id = m.user_id
		               WHERE m.user_id = $1 AND u.is_active AND m.role <> 'guest')`, c.userID).Scan(&member); err != nil {
		return nil, nil, err
	}
	if !member {
		return nil, ended(), nil
	}
	out, id, err := s.grantFromCode(ctx, tx, client, c)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_code SET used_at = now(), grant_id = $2 WHERE code_hash = $1`, digest, id); err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// grantFromCode makes the person's one grant for the app afresh, replacing an
// earlier one: the consent just given says what it may do, and where.
func (s *Service) grantFromCode(ctx context.Context, tx db.DBTX, client *clientRow, c code) (*TokenResponse, uuid.UUID, error) {
	access, accessDigest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, uuid.Nil, err
	}
	refresh, refreshDigest, err := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM api_token WHERE user_id = $1 AND client_id = $2`, c.userID, client.ID); err != nil {
		return nil, uuid.Nil, err
	}
	now := s.now()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at, mcp_only, spaces_only)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9) RETURNING id`,
		c.userID, client.Name+auth.ConnectedAppSuffix, accessDigest, tokenScopes(c.scopes), now.Add(RefreshTokenTTL),
		client.ID, refreshDigest, now.Add(AccessTokenTTL), c.spaces != nil).Scan(&id); err != nil {
		return nil, uuid.Nil, fmt.Errorf("store the grant: %w", err)
	}
	// A space deleted since the consent is left out, not refused.
	if c.spaces != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO api_token_space (token_id, space_id) SELECT $1, s.id FROM space s WHERE s.id = ANY ($2)`, id, c.spaces); err != nil {
			return nil, uuid.Nil, fmt.Errorf("limit the grant to its spaces: %w", err)
		}
	}
	scopes := grantScopes(tokenScopes(c.scopes))
	if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthGrantCreated, TargetType: "api_token", TargetID: &id, Actor: c.userID,
		Data: map[string]any{"clientId": client.ID, "app": client.Name, "scopes": scopes, "mcpOnly": true, "spaces": len(c.spaces)}}); err != nil {
		return nil, uuid.Nil, err
	}
	return &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTokenTTL / time.Second),
		RefreshToken: refresh, Scope: joinScope(scopes)}, id, nil
}

// endGrant deletes a grant whose secret was replayed, and says why in the log.
func endGrant(ctx context.Context, tx db.DBTX, id uuid.UUID, why string) error {
	var (
		userID   uuid.UUID
		clientID *uuid.UUID
	)
	err := tx.QueryRow(ctx, `DELETE FROM api_token WHERE id = $1 RETURNING user_id, client_id`, id).Scan(&userID, &clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "api_token", TargetID: &id, Actor: userID,
		Data: map[string]any{"owner": userID, "clientId": clientID, "by": why}})
}
