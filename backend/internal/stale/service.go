package stale

import (
	"context"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Service reads the report through stale_pages, which keeps to the spaces the
// transaction's actor administers and judges each row with perm_page_viewable.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Query is the statement List runs, for the plan check in the integration
// suite: space, owner, unowned, verification, days, cursor, then the limit.
const Query = `
	SELECT page_id, title, space_key, space_name, version, published_at, viewed_at, active_at,
	       owner_id, owner_name, owner_can_view, verification_state, verification_expires_at
	FROM stale_pages($1, $2, $3, $4, $5, $6, $7, $8)`

// List is a window of stale pages after the cursor, the longest untouched
// first, refused to a reader who administers no space or not the one named.
func (s *Service) List(ctx context.Context, actor perm.Actor, f Filter, after *keyset.Cursor, limit int) ([]StalePage, *string, error) {
	out := []StalePage{}
	afterAt, afterID := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var only *uuid.UUID
		if f.SpaceKey != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, f.SpaceKey)
			if err != nil {
				return err
			}
			if err := perm.Check(ctx, tx, actor, perm.ReviewStale, sp.ID); err != nil {
				return err
			}
			only = &sp.ID
		} else {
			var reviewer bool
			if err := tx.QueryRow(ctx, `SELECT stale_reviewer()`).Scan(&reviewer); err != nil {
				return err
			}
			if !reviewer {
				return &perm.DeniedError{Action: perm.ReviewStale}
			}
		}
		var verification *string
		if f.Verification != "" {
			v := string(f.Verification)
			verification = &v
		}
		rows, err := tx.Query(ctx, Query, only, f.Owner, f.Unowned, verification, f.Days, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				p        StalePage
				ownerID  *uuid.UUID
				name     string
				canView  bool
				verified string
			)
			if err := rows.Scan(&p.ID, &p.Title, &p.SpaceKey, &p.SpaceName, &p.Version, &p.PublishedAt, &p.ViewedAt, &p.ActiveAt,
				&ownerID, &name, &canView, &verified, &p.VerificationExpiresAt); err != nil {
				return err
			}
			if ownerID != nil {
				p.Owner = &page.Owner{ID: *ownerID, Name: name, CanView: canView}
			}
			p.Verification = Verification(verified)
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	last := out[limit-1]
	return out[:limit], keyset.Next(limit, len(out), keyset.Cursor{At: last.ActiveAt, ID: last.ID}), nil
}
