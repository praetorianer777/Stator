package page

import (
	"context"
	"errors"

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
	db            *db.Cluster
	copyObservers []CopyObserver
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
	p.Unpublished = p.Version == 0
	return &p, err
}

// load reads a page and its space, refusing with ErrNotFound what the actor
// may not view and what is in the trash; lock takes the page for writing.
func load(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID, lock bool) (*Page, *space.Space, error) {
	sql := selectPages + ` WHERE p.id = $1 AND` + live + ` AND ` + perm.ViewablePage("p", 2)
	if lock {
		sql += ` FOR UPDATE OF p`
	}
	p, err := scan(tx.QueryRow(ctx, sql, id, actor.UserID))
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
	access, _, err := perm.ForPage(ctx, tx, actor, id)
	if err != nil {
		return nil, nil, err
	}
	if !access.View {
		return nil, nil, ErrNotFound
	}
	p.access = access
	p.Can = access.Can()
	p.Restricted = Restricted{View: access.ViewRestricted, Edit: access.EditRestricted}
	if p.Ancestors, err = ancestors(ctx, tx, id); err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT name FROM page_label WHERE page_id = $1 ORDER BY name`, id)
	if err != nil {
		return nil, nil, err
	}
	if p.Labels, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return nil, nil, err
	}
	if p.Labels == nil {
		p.Labels = []string{}
	}
	var draft DraftRef
	err = tx.QueryRow(ctx, `SELECT base_version, updated_at FROM page_draft WHERE page_id = $1 AND user_id = $2`,
		id, actor.UserID).Scan(&draft.BaseVersion, &draft.UpdatedAt)
	switch {
	case err == nil:
		p.Draft = &draft
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, nil, err
	}
	return p, sp, nil
}

// must refuses an action on a loaded page that its access does not allow.
func (p *Page) must(action perm.Action) error {
	allowed := false
	switch action {
	case perm.EditPages:
		allowed = p.access.Edit
	case perm.DeletePages:
		allowed = p.access.Delete
	case perm.AddComments:
		allowed = p.access.Comment
	case perm.ViewSpace:
		allowed = p.access.View
	}
	if !allowed {
		return &perm.DeniedError{Action: action}
	}
	return nil
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

// Update publishes a new title or body as the next version, with no
// comment, over the version it was made from. Drafts are left alone.
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
		current, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := current.must(perm.EditPages); err != nil {
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
		if _, err := publish(ctx, tx, actor, current, release{title: title, body: body}); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}
