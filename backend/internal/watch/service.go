package watch

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

const (
	// DefaultLimit and MaxLimit bound a page of watches or watchers.
	DefaultLimit = 20
	MaxLimit     = 100
)

// ErrPageNotFound is also the answer for a page the caller may not view, or
// one in the trash.
var ErrPageNotFound = errors.New("page not found")

// Service keeps each person's own watches. Who hears about a page is read
// across people only by the database's page_watchers and the worker.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// viewablePage refuses a page the actor may not view or that is in the trash.
func viewablePage(ctx context.Context, tx db.DBTX, actor uuid.UUID, pageID uuid.UUID) error {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT true FROM page p
		WHERE p.id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 2), pageID, actor).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPageNotFound
	}
	return err
}

// PageWatching is how a person follows a page: their own watch on it, and
// the nearest subtree watch above it or their watch on its space.
func PageWatching(ctx context.Context, tx db.DBTX, userID, pageID uuid.UUID) (Watching, error) {
	var out Watching
	var own string
	err := tx.QueryRow(ctx, `SELECT kind FROM watch WHERE user_id = $1 AND page_id = $2`, userID, pageID).Scan(&own)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return out, err
	}
	out.Page, out.Subtree = own == string(KindPage), own == string(KindSubtree)
	var (
		kind  string
		above *uuid.UUID
		title *string
	)
	err = tx.QueryRow(ctx, `
		WITH RECURSIVE up (id, parent_id, space_id, title, depth) AS (
			SELECT p.id, p.parent_id, p.space_id, p.title, 0 FROM page p WHERE p.id = $2
			UNION ALL
			SELECT p.id, p.parent_id, p.space_id, p.title, up.depth + 1 FROM page p JOIN up ON p.id = up.parent_id
		)
		SELECT kind, page_id, title FROM (
			SELECT w.kind, up.id AS page_id, up.title, up.depth
			FROM watch w JOIN up ON w.page_id = up.id
			WHERE w.user_id = $1 AND w.kind = 'subtree' AND up.depth > 0
			UNION ALL
			SELECT w.kind, NULL, NULL, 2147483647
			FROM watch w
			WHERE w.user_id = $1 AND w.kind = 'space' AND w.space_id = (SELECT space_id FROM up WHERE depth = 0)
		) covering
		ORDER BY depth
		LIMIT 1`, userID, pageID).Scan(&kind, &above, &title)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return out, nil
	case err != nil:
		return out, err
	}
	out.Inherited = &Inherited{Kind: Kind(kind)}
	if above != nil && title != nil {
		out.Inherited.Page = &PageTitle{ID: *above, Title: *title}
	}
	return out, nil
}

// Auto makes a person watch a page they created or published, unless they
// watch it already, stopped watching it, or turned auto watch off.
func Auto(ctx context.Context, tx db.DBTX, userID, pageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO watch (org_id, user_id, kind, page_id)
		SELECT current_org_id(), $1, 'page', $2
		WHERE COALESCE((SELECT auto_watch FROM notification_preference WHERE user_id = $1), true)
		  AND NOT EXISTS (SELECT 1 FROM watch_optout WHERE user_id = $1 AND page_id = $2)
		ON CONFLICT DO NOTHING`, userID, pageID)
	return err
}

// WatchPage makes the caller watch a page, alone or with every page below it,
// replacing their own watch on it and clearing an opt out.
func (s *Service) WatchPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in Input) (Watching, db.LSN, error) {
	kind := KindPage
	if in.Subtree {
		kind = KindSubtree
	}
	var out Watching
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := viewablePage(ctx, tx, actor.UserID, pageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO watch (org_id, user_id, kind, page_id) VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (org_id, user_id, page_id) WHERE page_id IS NOT NULL
			DO UPDATE SET kind = EXCLUDED.kind, created_at = now()`, actor.UserID, kind, pageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM watch_optout WHERE user_id = $1 AND page_id = $2`, actor.UserID, pageID); err != nil {
			return err
		}
		var err error
		out, err = PageWatching(ctx, tx, actor.UserID, pageID)
		return err
	})
	return out, lsn, err
}

// UnwatchPage removes the caller's own watch on a page and records that they
// opted out, so their own edits do not make them watch it again.
func (s *Service) UnwatchPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := viewablePage(ctx, tx, actor.UserID, pageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM watch WHERE user_id = $1 AND page_id = $2`, actor.UserID, pageID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO watch_optout (org_id, user_id, page_id) VALUES (current_org_id(), $1, $2)
			ON CONFLICT DO NOTHING`, actor.UserID, pageID)
		return err
	})
}

// Watchers lists who hears about a new version of a page, by name.
func (s *Service) Watchers(ctx context.Context, actor perm.Actor, pageID uuid.UUID, limit, offset int) ([]Watcher, int, error) {
	out := []Watcher{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := viewablePage(ctx, tx, actor.UserID, pageID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM page_watchers($1)`, pageID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT c.user_id, u.name, u.email, c.via, c.via_page, vp.title
			FROM page_watchers($1) c
			JOIN app_user u ON u.id = c.user_id
			LEFT JOIN page vp ON vp.id = c.via_page
			ORDER BY lower(u.name), u.id
			LIMIT $2 OFFSET $3`, pageID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				w     Watcher
				via   string
				above *uuid.UUID
				title *string
			)
			if err := rows.Scan(&w.UserID, &w.Name, &w.Email, &via, &above, &title); err != nil {
				return err
			}
			w.Via = Kind(via)
			if above != nil && title != nil {
				w.ViaPage = &PageTitle{ID: *above, Title: *title}
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, total, err
}

// WatchSpace makes the caller watch every page of a space, now and later.
func (s *Service) WatchSpace(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), $1, 'space', $2)
			ON CONFLICT DO NOTHING`, actor.UserID, sp.ID)
		return err
	})
}

// UnwatchSpace stops the caller watching a space; watches on its pages and
// its blog stay.
func (s *Service) UnwatchSpace(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.unwatch(ctx, actor, key, KindSpace)
}

// WatchBlog makes the caller hear of every post first published in a
// space's blog; later versions of a post are its own watchers' to hear.
func (s *Service) WatchBlog(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), $1, 'blog', $2)
			ON CONFLICT DO NOTHING`, actor.UserID, sp.ID)
		return err
	})
}

// UnwatchBlog stops the caller hearing of a blog's new posts.
func (s *Service) UnwatchBlog(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.unwatch(ctx, actor, key, KindBlog)
}

func (s *Service) unwatch(ctx context.Context, actor perm.Actor, key string, kind Kind) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM watch WHERE user_id = $1 AND space_id = $2 AND kind = $3`, actor.UserID, sp.ID, kind)
		return err
	})
}

// BlogWatching says whether a person watches a space's blog.
func BlogWatching(ctx context.Context, tx db.DBTX, userID, spaceID uuid.UUID) (bool, error) {
	var on bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM watch WHERE user_id = $1 AND space_id = $2 AND kind = 'blog')`, userID, spaceID).Scan(&on)
	return on, err
}

// listed is the caller's watches on what they may still view, out of the trash.
const listed = `
FROM watch w
LEFT JOIN page p ON p.id = w.page_id
JOIN space s ON s.id = COALESCE(w.space_id, p.space_id)
WHERE w.user_id = $1
  AND (w.page_id IS NULL OR (p.trashed_at IS NULL AND ` + "perm_page_viewable(p.id, $1::uuid)" + `))
  AND (w.space_id IS NULL OR ` + "perm_space_holds($1::uuid, s.id, 'view')" + `)`

// List is the caller's own watches, the latest first.
func (s *Service) List(ctx context.Context, actor perm.Actor, limit, offset int) ([]Watch, int, error) {
	out := []Watch{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) `+listed, actor.UserID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT w.kind, s.key, s.name, p.id, p.title, w.created_at `+listed+`
			ORDER BY w.created_at DESC, w.id DESC
			LIMIT $2 OFFSET $3`, actor.UserID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				w     Watch
				kind  string
				id    *uuid.UUID
				title *string
			)
			if err := rows.Scan(&kind, &w.SpaceKey, &w.SpaceName, &id, &title, &w.CreatedAt); err != nil {
				return err
			}
			w.Kind = Kind(kind)
			if id != nil && title != nil {
				w.Page = &PageTitle{ID: *id, Title: *title}
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, total, err
}
