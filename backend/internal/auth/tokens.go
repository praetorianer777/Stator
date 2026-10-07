package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Personal access tokens, adapted from Armature's. A token belongs to one
// person in one organization and acts as them there, never more.

// MaxTokenNameLength bounds a token's name, which is only a reminder.
const MaxTokenNameLength = 100

// MaxTokenSpaces bounds how many spaces one token names.
const MaxTokenSpaces = 50

// APIToken is a personal access token. The secret is only ever returned once,
// by CreateAPIToken.
type APIToken struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Scopes []string  `json:"scopes"`
	// Spaces are the keys of the spaces the token is limited to, as far as
	// the reader may see them; empty for one that reaches every space its
	// owner does, unless AllSpaces is false.
	Spaces []string `json:"spaces"`
	// AllSpaces is false for a token limited to Spaces, which then reaches
	// nothing else, and nothing at all once every one of them is deleted.
	AllSpaces bool `json:"allSpaces"`
	// LastUsedAt is refreshed at most once a minute; null until first use.
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// ExpiresAt is null for a token that lasts until it is revoked.
	ExpiresAt *time.Time `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
	Secret    string     `json:"secret,omitempty"`
}

// OrgAPIToken is a token as an administrator sees it: with whose it is.
type OrgAPIToken struct {
	APIToken
	Owner User `json:"owner"`
}

// NewAPIToken is what a person asks for when making a token.
type NewAPIToken struct {
	Name   string
	Scopes []string
	// Spaces are space keys; none leaves the token every space its owner reaches.
	Spaces    []string
	ExpiresAt *time.Time
}

var (
	// ErrTokenName is returned for a token without a name, or with a long one.
	ErrTokenName = errors.New("give the token a name of at most 100 characters")
	// ErrTokenScope is returned for a scope nobody enforces, so a token never
	// carries a promise the API does not keep.
	ErrTokenScope = errors.New("that is not a token scope; use read, or leave the scopes empty")
	// ErrTokenExpiry is returned for an expiry that has already passed.
	ErrTokenExpiry = errors.New("the expiry has to be in the future")
	// ErrNoSuchToken is returned when the token named is not the caller's to revoke.
	ErrNoSuchToken = errors.New("that token was not found")
	// ErrTokenSpaces is returned for more spaces than a token names.
	ErrTokenSpaces = errors.New("a token names at most 50 spaces; leave them all out for one that reaches every space you do")
	// ErrNoSuchSpace is returned when a token names a space its maker cannot
	// see: naming it would not widen the token, but it would read as if it had.
	ErrNoSuchSpace = errors.New("a token can only name spaces you can already see; check the keys")
)

// ValidateScopes refuses any scope but read.
func ValidateScopes(scopes []string) error {
	for _, s := range scopes {
		if s != ScopeRead {
			return ErrTokenScope
		}
	}
	return nil
}

func (s *Service) validateNewToken(in *NewAPIToken) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > MaxTokenNameLength {
		return ErrTokenName
	}
	if err := ValidateScopes(in.Scopes); err != nil {
		return err
	}
	// Never nil: the column is not null, and the answer lists no scopes as [].
	in.Scopes = append([]string{}, slices.Compact(slices.Sorted(slices.Values(in.Scopes)))...)
	keys := make([]string, 0, len(in.Spaces))
	for _, k := range in.Spaces {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	in.Spaces = append([]string{}, slices.Compact(slices.Sorted(slices.Values(keys)))...)
	if len(in.Spaces) > MaxTokenSpaces {
		return ErrTokenSpaces
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return ErrTokenExpiry
	}
	return nil
}

// CreateAPIToken makes a token for the caller in the organization the context
// names. It runs under row level security like any other tenant data.
func (s *Service) CreateAPIToken(ctx context.Context, orgID, userID uuid.UUID, in NewAPIToken, ip string) (*APIToken, db.LSN, error) {
	if err := s.validateNewToken(&in); err != nil {
		return nil, 0, err
	}
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		return nil, 0, err
	}
	tok := APIToken{Name: in.Name, Scopes: in.Scopes, Spaces: in.Spaces, AllSpaces: len(in.Spaces) == 0, ExpiresAt: in.ExpiresAt, Secret: secret}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, spaces_only)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			RETURNING id, created_at`,
			userID, tok.Name, digest, tok.Scopes, tok.ExpiresAt, !tok.AllSpaces,
		).Scan(&tok.ID, &tok.CreatedAt); err != nil {
			return fmt.Errorf("create api token: %w", err)
		}
		if !tok.AllSpaces {
			// The spaces are read under the maker's own permissions, so a key
			// they cannot see inserts nothing and the count refuses it.
			tag, err := tx.Exec(ctx, `
				INSERT INTO api_token_space (org_id, token_id, space_id)
				SELECT current_org_id(), $1, s.id FROM space s WHERE s.key = ANY ($2)`, tok.ID, tok.Spaces)
			if err != nil {
				return fmt.Errorf("limit the token to its spaces: %w", err)
			}
			if int(tag.RowsAffected()) != len(tok.Spaces) {
				return ErrNoSuchSpace
			}
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionTokenCreated, TargetType: "api_token", TargetID: &tok.ID, Actor: userID, IP: ip,
			Data: map[string]any{"name": tok.Name, "scopes": tok.Scopes, "spaces": tok.Spaces}})
	})
	if err != nil {
		return nil, 0, err
	}
	return &tok, lsn, nil
}

// tokenColumns name a token's spaces through the reader's own permissions, so
// a list never spells the key of a space its reader may not see.
const tokenColumns = `t.id, t.name, t.scopes, t.last_used_at, t.expires_at, t.created_at, NOT t.spaces_only,
	ARRAY(SELECT s.key FROM api_token_space ts JOIN space s ON s.id = ts.space_id WHERE ts.token_id = t.id ORDER BY s.key)`

func scanToken(row pgx.Row, extra ...any) (APIToken, error) {
	var t APIToken
	err := row.Scan(append([]any{&t.ID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt, &t.AllSpaces, &t.Spaces}, extra...)...)
	return t, err
}

// ListAPITokens returns the caller's tokens in the organization, newest first
// and without their secrets.
func (s *Service) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	out := []APIToken{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+tokenColumns+` FROM api_token t WHERE t.user_id = $1 AND NOT t.for_render ORDER BY t.created_at DESC, t.id`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanToken(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// ListOrgAPITokens returns every token in the organization, for an administrator.
func (s *Service) ListOrgAPITokens(ctx context.Context) ([]OrgAPIToken, error) {
	out := []OrgAPIToken{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT `+tokenColumns+`, u.id, u.email::text, u.name, COALESCE(u.avatar_url, '')
			FROM api_token t JOIN app_user u ON u.id = t.user_id
			WHERE NOT t.for_render
			ORDER BY t.created_at DESC, t.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var owner User
			t, err := scanToken(rows, &owner.ID, &owner.Email, &owner.Name, &owner.AvatarURL)
			if err != nil {
				return err
			}
			out = append(out, OrgAPIToken{APIToken: t, Owner: owner})
		}
		return rows.Err()
	})
	return out, err
}

// RevokeAPIToken deletes one of the caller's own tokens.
func (s *Service) RevokeAPIToken(ctx context.Context, orgID, userID, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.revoke(ctx, orgID, &userID, userID, tokenID, ip)
}

// RevokeOrgAPIToken deletes anybody's token in the organization, for an administrator.
func (s *Service) RevokeOrgAPIToken(ctx context.Context, orgID, actor, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.revoke(ctx, orgID, nil, actor, tokenID, ip)
}

func (s *Service) revoke(ctx context.Context, orgID uuid.UUID, owner *uuid.UUID, actor, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var holder uuid.UUID
		err := tx.QueryRow(ctx, `
			DELETE FROM api_token WHERE id = $1 AND ($2::uuid IS NULL OR user_id = $2)
			RETURNING user_id`, tokenID, owner).Scan(&holder)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchToken
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "api_token", TargetID: &tokenID, Actor: actor, IP: ip,
			Data: map[string]any{"owner": holder}})
	})
}

// apiTokenPrincipalSQL resolves a token to its owner in its organization. The
// membership is joined, not left joined: a token outlives no membership.
const apiTokenPrincipalSQL = `
SELECT t.id, t.scopes, t.last_used_at, t.spaces_only,
       ARRAY(SELECT ts.space_id FROM api_token_space ts WHERE ts.token_id = t.id ORDER BY ts.space_id),
       u.id, u.email::text, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, ''), u.show_in_readers, u.is_active,
       o.id, o.slug, o.name, m.org_role, gs.id, gs.key, gs.name
FROM api_token t
JOIN app_user u ON u.id = t.user_id
JOIN org o ON o.id = t.org_id AND o.archived_at IS NULL
JOIN org_member m ON m.org_id = t.org_id AND m.user_id = t.user_id
LEFT JOIN space gs ON gs.org_id = m.org_id AND gs.id = m.guest_space_id
WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`

func (s *Service) authenticateAPIToken(ctx context.Context, secret string) (*Principal, error) {
	if err := ParseAPIToken(secret); err != nil {
		return nil, err
	}
	var (
		p        Principal
		tokenID  uuid.UUID
		lastUsed *time.Time
		active   bool
		guest    guestSpaceColumns
		org      struct {
			id         uuid.UUID
			slug, name string
		}
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, apiTokenPrincipalSQL, HashToken(secret)).Scan(
			&tokenID, &p.Scopes, &lastUsed, &p.SpacesOnly, &p.TokenSpaces,
			&p.UserID, &p.Email, &p.Name, &p.AvatarURL, &p.Locale, &p.ShowInReaders, &active,
			&org.id, &org.slug, &org.name, &p.Role, &guest.id, &guest.key, &guest.name,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrUserInactive
	}
	p.TokenID = &tokenID
	p.GuestSpace = guest.ref()
	p.Org = &tenant.Org{ID: org.id, Slug: org.slug}
	p.OrgName = org.name
	if lastUsed == nil || s.now().Sub(*lastUsed) > lastSeenEvery {
		_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE api_token SET last_used_at = now() WHERE id = $1`, tokenID)
			return err
		})
	}
	return &p, nil
}
