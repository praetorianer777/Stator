package page

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Service keeps pages. Every method reads the page's space first, which is
// where whether the actor may see or change it is decided.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

const selectPages = `
SELECT p.id, p.space_id, s.key, p.parent_id, p.title, p.body, p.version, p.parent_id IS NULL,
       COALESCE(cu.name, ''), p.created_at, COALESCE(uu.name, ''), p.updated_at
FROM page p
JOIN space s ON s.id = p.space_id
LEFT JOIN app_user cu ON cu.id = p.created_by
LEFT JOIN app_user uu ON uu.id = p.updated_by`

func scan(row pgx.Row) (*Page, error) {
	var p Page
	err := row.Scan(&p.ID, &p.SpaceID, &p.SpaceKey, &p.ParentID, &p.Title, &p.Body, &p.Version, &p.Home,
		&p.CreatedByName, &p.CreatedAt, &p.UpdatedByName, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// load reads a page and its space, refusing with ErrNotFound what the actor
// may not see and what is in the trash; lock takes the page for writing.
func load(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID, lock bool) (*Page, *space.Space, error) {
	sql := selectPages + ` WHERE p.id = $1 AND` + live
	if lock {
		sql += ` FOR UPDATE OF p`
	}
	p, err := scan(tx.QueryRow(ctx, sql, id))
	if err != nil {
		return nil, nil, err
	}
	sp, err := space.Load(ctx, tx, actor, space.ByID, p.SpaceID)
	if errors.Is(err, space.ErrNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if p.Ancestors, err = ancestors(ctx, tx, id); err != nil {
		return nil, nil, err
	}
	return p, sp, nil
}

// Get is one page with the space it is in.
func (s *Service) Get(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Page, *space.Space, error) {
	var (
		p  *Page
		sp *space.Space
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		p, sp, err = load(ctx, tx, actor, id, false)
		return err
	})
	return p, sp, err
}

// Update saves a new title or body over the version it was made from.
func (s *Service) Update(ctx context.Context, actor perm.Actor, id uuid.UUID, in UpdateInput) (*Page, db.LSN, error) {
	var title string
	if in.Title != nil {
		var err error
		if title, err = cleanTitle(*in.Title); err != nil {
			return nil, 0, err
		}
	}
	if in.Body != nil {
		if err := document.Validate(in.Body); err != nil {
			return nil, 0, err
		}
	}
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, sp, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.EditPages, sp.ID); err != nil {
			return err
		}
		if in.Version != current.Version {
			return ErrStale
		}
		if in.Title == nil {
			title = current.Title
		}
		body := current.Body
		if in.Body != nil {
			body = in.Body
		}
		if _, err := tx.Exec(ctx, `
			UPDATE page SET title = $2, body = $3, version = version + 1, updated_by = $4 WHERE id = $1`,
			id, title, body, actor.UserID); err != nil {
			return fmt.Errorf("save the page: %w", err)
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}
