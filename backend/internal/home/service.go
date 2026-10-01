package home

import (
	"context"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/keyset"
)

// Service reads the home page's lists, each through a database function that
// judges every row with perm_page_viewable for the transaction's actor.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// UpdatesQuery is the statement Updates runs, for the plan check in the
// integration suite: scope, then the cursor's time and id, then the limit.
const UpdatesQuery = `
	SELECT page_id, title, space_key, space_name, version, published_at, author_name, comment
	FROM home_updates($1, $2, $3, $4)`

// Updates is what others published, the latest first, a window of limit
// after the cursor.
func (s *Service) Updates(ctx context.Context, scope Scope, after *keyset.Cursor, limit int) ([]PageUpdate, *string, error) {
	out := []PageUpdate{}
	afterAt, afterID := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, UpdatesQuery, scope == ScopeWatched, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u PageUpdate
			if err := rows.Scan(&u.ID, &u.Title, &u.SpaceKey, &u.SpaceName, &u.Version, &u.PublishedAt, &u.AuthorName, &u.Comment); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	out, next := window(out, limit, func(u PageUpdate) keyset.Cursor { return keyset.Cursor{At: u.PublishedAt, ID: u.ID} })
	return out, next, nil
}

// Edited is what the caller edited, the latest first, a window of limit after
// the cursor.
func (s *Service) Edited(ctx context.Context, after *keyset.Cursor, limit int) ([]EditedPage, *string, error) {
	out := []EditedPage{}
	afterAt, afterID := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT page_id, title, space_key, space_name, edited_at, draft, unpublished
			FROM home_edited($1, $2, $3)`, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e EditedPage
			if err := rows.Scan(&e.ID, &e.Title, &e.SpaceKey, &e.SpaceName, &e.EditedAt, &e.Draft, &e.Unpublished); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	out, next := window(out, limit, func(e EditedPage) keyset.Cursor { return keyset.Cursor{At: e.EditedAt, ID: e.ID} })
	return out, next, nil
}

// window cuts a read of limit+1 rows to limit, with the cursor after the last
// one kept when there was more.
func window[T any](rows []T, limit int, at func(T) keyset.Cursor) ([]T, *string) {
	if len(rows) <= limit {
		return rows, nil
	}
	return rows[:limit], keyset.Next(limit, len(rows), at(rows[limit-1]))
}
