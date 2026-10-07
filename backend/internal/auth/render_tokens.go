package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// RenderTokenTTL is how long the token a print is made with lasts: long enough
// to print, short enough that nothing else ever uses it. The database refuses
// one that lasts more than five minutes.
const RenderTokenTTL = 2 * time.Minute

// renderTokenName is what a print's token is called, though nobody lists it.
const renderTokenName = "PDF export"

// MintRenderToken makes the token the render service prints a page with, as
// the caller: read only, for RenderTokenTTL, listed nowhere and not audited;
// the export it serves is. The caller's expired print tokens go with it. A
// caller with a token limited to spaces may make no token, which the
// database refuses too.
func (s *Service) MintRenderToken(ctx context.Context, p *Principal) (string, uuid.UUID, db.LSN, error) {
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		return "", uuid.Nil, 0, err
	}
	var id uuid.UUID
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM api_token WHERE for_render AND user_id = $1 AND expires_at <= now()`, p.UserID); err != nil {
			return fmt.Errorf("forget spent print tokens: %w", err)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, for_render)
			VALUES (current_org_id(), $1, $2, $3, ARRAY['read'], now() + make_interval(secs => $4), true)
			RETURNING id`,
			p.UserID, renderTokenName, digest, RenderTokenTTL.Seconds(),
		).Scan(&id); err != nil {
			return fmt.Errorf("make the print's token: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", uuid.Nil, 0, err
	}
	return secret, id, lsn, nil
}

// RevokeRenderToken deletes a print's token once the PDF is back.
func (s *Service) RevokeRenderToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM api_token WHERE id = $1 AND for_render`, id)
		return err
	})
	return err
}
