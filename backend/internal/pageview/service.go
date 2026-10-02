package pageview

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service reads the counts and the readers through SQL functions that keep to
// current_org_id() and judge current_actor_id(), as the home lists do.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Queries the service runs, for the plan check in the integration suite.
const (
	StatsQuery   = `SELECT views, readers, recent_views, recent_readers, can_list FROM page_view_stats($1, $2)`
	ReadersQuery = `SELECT user_id, name, avatar_url, viewed_at, days FROM page_readers($1, $2, $3, $4)`
	// CountQuery counts the actor's view of a page today; a second one that
	// day finds its row and writes nothing.
	CountQuery = `
		INSERT INTO page_view (org_id, page_id, user_id, day)
		VALUES (current_org_id(), $2, $1, page_view_today())
		ON CONFLICT (org_id, page_id, user_id, day) DO NOTHING`
)

// Count notes the actor's view of a page they may view, inside the caller's
// transaction; Visit in package search calls it with the visit.
func Count(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) error {
	if _, err := tx.Exec(ctx, CountQuery, actor.UserID, pageID); err != nil {
		return fmt.Errorf("count the view: %w", err)
	}
	return nil
}

// Counts reads a page's counts, refusing with page.ErrNotFound one the actor
// may not view or that is in the trash.
func (s *Service) Counts(ctx context.Context, actor perm.Actor, pageID uuid.UUID) (ViewCounts, error) {
	out := ViewCounts{Days: RecentDays}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, StatsQuery, pageID, RecentDays).
			Scan(&out.Views, &out.Readers, &out.RecentViews, &out.RecentReaders, &out.CanListReaders)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ViewCounts{}, page.ErrNotFound
	}
	if err != nil {
		return ViewCounts{}, fmt.Errorf("read the page's views: %w", err)
	}
	return out, nil
}

// List is a window of the page's named readers after the cursor, the latest
// first; 404 for a page the actor may not view, 403 for one they may not edit.
func (s *Service) List(ctx context.Context, actor perm.Actor, pageID uuid.UUID, after *keyset.Cursor, limit int) (Readers, error) {
	out := Readers{Readers: []Reader{}}
	afterAt, afterID := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var viewable, listable bool
		if err := tx.QueryRow(ctx, `
			SELECT perm_page_viewable(p.id, $2::uuid), perm_page_readers_listable(p.id, $2::uuid)
			FROM page p WHERE p.id = $1 AND p.trashed_at IS NULL`, pageID, actor.UserID).Scan(&viewable, &listable); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return page.ErrNotFound
			}
			return err
		}
		if !viewable {
			return page.ErrNotFound
		}
		if !listable {
			return &perm.DeniedError{Action: perm.ListReaders}
		}
		if err := tx.QueryRow(ctx, `SELECT page_readers_unnamed($1)`, pageID).Scan(&out.Unnamed); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, ReadersQuery, pageID, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		out.Readers, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reader, error) {
			var v Reader
			err := row.Scan(&v.ID, &v.Name, &v.AvatarURL, &v.ViewedAt, &v.Days)
			return v, err
		})
		return err
	})
	if err != nil {
		return Readers{}, err
	}
	if len(out.Readers) > limit {
		last := out.Readers[limit-1]
		out.Next = keyset.Next(limit, len(out.Readers), keyset.Cursor{At: last.ViewedAt, ID: last.ID})
		out.Readers = out.Readers[:limit]
	}
	return out, nil
}
