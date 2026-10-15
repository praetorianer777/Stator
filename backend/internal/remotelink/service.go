package remotelink

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service puts links on pages and takes them off. Whether the actor may see
// or change one is decided on its page, by the service and the database.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

const columns = `l.id, l.url, l.title, l.source, l.icon_url, l.created_by, COALESCE(u.name, ''), l.created_at, l.updated_at`

const from = ` FROM page_remote_link l LEFT JOIN app_user u ON u.id = l.created_by`

func scan(row pgx.Row) (Link, error) {
	var l Link
	err := row.Scan(&l.ID, &l.URL, &l.Title, &l.Source, &l.IconURL, &l.CreatedBy, &l.CreatedByName, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// OnPage lists a page's links by source, then in the order they were put.
func (s *Service) OnPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID) ([]Link, error) {
	out := []Link{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := page.Load(ctx, tx, actor, pageID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+columns+from+` WHERE l.page_id = $1 ORDER BY lower(l.source), l.source, l.created_at, l.id`, pageID)
		if err != nil {
			return fmt.Errorf("read the remote links: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// editable loads a page the actor may change the links of.
func editable(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) error {
	p, _, err := page.Load(ctx, tx, actor, pageID)
	if err != nil {
		return err
	}
	if !p.Can.Edit {
		return p.Refusal(perm.EditPages)
	}
	return nil
}

// Put puts a link on a page, or replaces the one at the same url, so a tool
// can send all of its links again; created says which it was. A replaced
// link keeps who first put it.
func (s *Service) Put(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in Input) (link Link, created bool, lsn db.LSN, err error) {
	if in, err = Clean(in); err != nil {
		return Link{}, false, 0, err
	}
	var icon *string
	if in.IconURL != "" {
		icon = &in.IconURL
	}
	lsn, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := editable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		// xmax is zero only on a row this statement inserted, which tells a
		// new link from a replaced one without a second round trip.
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO page_remote_link (page_id, url, title, source, icon_url, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (page_id, url) DO UPDATE
				SET title = EXCLUDED.title, source = EXCLUDED.source, icon_url = EXCLUDED.icon_url, updated_at = now()
			RETURNING id, xmax = 0`,
			pageID, in.URL, in.Title, in.Source, icon, actor.UserID).Scan(&id, &created); err != nil {
			return fmt.Errorf("put the remote link: %w", err)
		}
		link, err = scan(tx.QueryRow(ctx, `SELECT `+columns+from+` WHERE l.id = $1`, id))
		return err
	})
	if err != nil {
		return Link{}, false, lsn, err
	}
	return link, created, lsn, nil
}

// Remove takes a link off a page and tells webhooks, whoever removed it. The
// link has to be on the page named, so no page reaches another's links.
func (s *Service) Remove(ctx context.Context, actor perm.Actor, pageID, linkID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := editable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		var removed events.PageRemoteLinkRemoved
		err := tx.QueryRow(ctx, `
			DELETE FROM page_remote_link WHERE id = $1 AND page_id = $2
			RETURNING id, url, title, source`, linkID, pageID).Scan(&removed.LinkID, &removed.URL, &removed.Title, &removed.Source)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("remove the remote link: %w", err)
		}
		removed.PageID, removed.ActorID = pageID, actor.UserID
		return events.Emit(ctx, tx, events.TopicPageRemoteLinkRemoved, removed)
	})
}
