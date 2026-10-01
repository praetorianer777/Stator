package star

import (
	"context"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

// Service keeps each person's own stars.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// StarPage stars a page the caller may view, out of the trash; starring it
// again is no change.
func (s *Service) StarPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO star (org_id, user_id, page_id)
			SELECT p.org_id, $1, p.id FROM page p
			WHERE p.id = $2 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`
			ON CONFLICT (org_id, user_id, page_id) WHERE page_id IS NOT NULL DO NOTHING`, actor.UserID, pageID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return viewablePage(ctx, tx, actor, pageID)
		}
		return nil
	})
}

// UnstarPage takes the caller's star off a page they may view; one that has
// none is no change.
func (s *Service) UnstarPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := viewablePage(ctx, tx, actor, pageID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM star WHERE user_id = $1 AND page_id = $2`, actor.UserID, pageID)
		return err
	})
}

// StarSpace stars a space the caller may view.
func (s *Service) StarSpace(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO star (org_id, user_id, space_id) VALUES (current_org_id(), $1, $2)
			ON CONFLICT DO NOTHING`, actor.UserID, sp.ID)
		return err
	})
}

// UnstarSpace takes the caller's star off a space; its pages' stars stay.
func (s *Service) UnstarSpace(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM star WHERE user_id = $1 AND space_id = $2`, actor.UserID, sp.ID)
		return err
	})
}

func viewablePage(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = $2 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`)`,
		actor.UserID, pageID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return page.ErrNotFound
	}
	return nil
}

// List is the caller's stars on what they may still view, out of the trash,
// the latest first, a window of limit after the cursor.
func (s *Service) List(ctx context.Context, actor perm.Actor, after *keyset.Cursor, limit int) ([]Star, *string, error) {
	out := []Star{}
	afterAt, afterID := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT st.id, s.key, s.name, p.id, p.title, st.created_at
			FROM star st
			LEFT JOIN page p ON p.id = st.page_id
			JOIN space s ON s.id = COALESCE(st.space_id, p.space_id)
			WHERE st.user_id = $1
			  AND ($2::timestamptz IS NULL OR (st.created_at, st.id) < ($2, $3::uuid))
			  AND (st.page_id IS NULL OR (p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`))
			  AND (st.space_id IS NULL OR `+perm.ViewableSpace("s", 1)+`)
			ORDER BY st.created_at DESC, st.id DESC
			LIMIT $4`, actor.UserID, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				st    Star
				id    *uuid.UUID
				title *string
			)
			if err := rows.Scan(&st.id, &st.SpaceKey, &st.SpaceName, &id, &title, &st.StarredAt); err != nil {
				return err
			}
			st.Kind = KindSpace
			if id != nil && title != nil {
				st.Kind = KindPage
				st.Page = &watch.PageTitle{ID: *id, Title: *title}
			}
			out = append(out, st)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		last := out[limit-1]
		next = keyset.Next(limit, len(out), keyset.Cursor{At: last.StarredAt, ID: last.id})
		out = out[:limit]
	}
	return out, next, nil
}
