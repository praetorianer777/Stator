package page

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

var (
	// ErrHomeNotTrashed refuses deleting the root of a space on its own.
	ErrHomeNotTrashed = errors.New("the home page cannot go to the trash; delete the whole space in its settings instead")
	// ErrNotInTrash is the answer for an item that is not in the space's trash.
	ErrNotInTrash = errors.New("that page is not in this space's trash")
)

// TrashItem is a deleted page and everything that went with it.
type TrashItem struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	TrashedAt     time.Time `json:"trashedAt"`
	TrashedByName string    `json:"trashedByName"`
	// Pages counts the pages in the item, the deleted one included.
	Pages int `json:"pages"`
	// ParentTitle names where a restore puts it back; ParentInTree says
	// whether that page is still there, or the item goes under the home page.
	ParentTitle  string `json:"parentTitle"`
	ParentInTree bool   `json:"parentInTree"`
}

// Trash moves a page and every page below it still in the tree to the
// space's trash, as one item.
func (s *Service) Trash(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := lockTrees(ctx, tx, id); err != nil {
			return err
		}
		current, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := current.must(perm.DeletePages); err != nil {
			return err
		}
		// The database marks the pages below too, those the actor cannot
		// see included, once it has checked the page itself.
		if _, err = tx.Exec(ctx, `SELECT page_trash($1)`, id); err != nil {
			if isConstraint(err, homeTrashConstraint) {
				return ErrHomeNotTrashed
			}
			return err
		}
		return syncLinksBelow(ctx, tx, []uuid.UUID{id}, true)
	})
}

// trashSpace reads a space by key and checks the actor may act on its trash.
func trashSpace(ctx context.Context, tx db.DBTX, actor perm.Actor, key string, action perm.Action) (*space.Space, error) {
	sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
	if err != nil {
		return nil, err
	}
	return sp, perm.Check(ctx, tx, actor, action, sp.ID)
}

// ListTrash lists a space's trash, the latest first.
func (s *Service) ListTrash(ctx context.Context, actor perm.Actor, spaceKey string) ([]TrashItem, error) {
	out := []TrashItem{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := trashSpace(ctx, tx, actor, spaceKey, perm.DeletePages)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, p.trashed_at, COALESCE(u.name, ''),
			       (SELECT count(*) FROM page i WHERE i.trash_id = p.id),
			       parent.title, parent.trashed_at IS NULL
			FROM page p
			JOIN page parent ON parent.id = p.parent_id
			LEFT JOIN app_user u ON u.id = p.trashed_by
			WHERE p.space_id = $1 AND p.trash_id = p.id AND `+perm.ViewablePage("p", 2)+`
			ORDER BY p.trashed_at DESC, p.id`, sp.ID, actor.UserID)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TrashItem]); out == nil {
			out = []TrashItem{}
		}
		return err
	})
	return out, err
}

// trashItem locks an item of a space's trash, answering ErrNotInTrash for
// anything else.
func trashItem(ctx context.Context, tx db.DBTX, sp *space.Space, id uuid.UUID) (parent uuid.UUID, title string, err error) {
	err = tx.QueryRow(ctx, `
		SELECT parent_id, title FROM page WHERE id = $1 AND space_id = $2 AND trash_id = id FOR UPDATE`, id, sp.ID).Scan(&parent, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrNotInTrash
	}
	return parent, title, err
}

// Restore puts an item back where it was, or last under the home page when
// the page it was under has gone to the trash itself or been purged.
func (s *Service) Restore(ctx context.Context, actor perm.Actor, spaceKey string, id uuid.UUID) (*Page, db.LSN, error) {
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := lockTrees(ctx, tx, id); err != nil {
			return err
		}
		sp, err := trashSpace(ctx, tx, actor, spaceKey, perm.DeletePages)
		if err != nil {
			return err
		}
		parent, _, err := trashItem(ctx, tx, sp, id)
		if err != nil {
			return err
		}
		if access, _, err := perm.ForPage(ctx, tx, actor, id); err != nil {
			return err
		} else if !access.View {
			return ErrNotInTrash
		} else if !access.Delete {
			return &perm.DeniedError{Action: perm.DeletePages}
		}
		var parentInTree bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM page WHERE id = $1 AND trashed_at IS NULL)`, parent).Scan(&parentInTree); err != nil {
			return err
		}
		var home *uuid.UUID
		var homeRank *string
		if !parentInTree {
			r, err := rankAt(ctx, tx, Placement{ParentID: sp.HomePageID}, id)
			if err != nil {
				return err
			}
			home, homeRank = &sp.HomePageID, &r
		}
		if _, err := tx.Exec(ctx, `SELECT page_untrash($1, $2, $3)`, id, home, homeRank); err != nil {
			return err
		}
		if err := syncLinksBelow(ctx, tx, []uuid.UUID{id}, true); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

// underHome hangs a page last under its space's home page.
func underHome(ctx context.Context, tx db.DBTX, sp *space.Space, id uuid.UUID) error {
	r, err := rankAt(ctx, tx, Placement{ParentID: sp.HomePageID}, id)
	if err != nil {
		return err
	}
	return place(ctx, tx, []uuid.UUID{id}, sp.HomePageID, []string{r})
}

// Purge deletes an item of the trash for good. Items deleted earlier from
// below it stay in the trash, under the home page.
func (s *Service) Purge(ctx context.Context, actor perm.Actor, spaceKey string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := lockTrees(ctx, tx, id); err != nil {
			return err
		}
		sp, err := trashSpace(ctx, tx, actor, spaceKey, perm.PurgeTrash)
		if err != nil {
			return err
		}
		_, title, err := trashItem(ctx, tx, sp, id)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT o.id FROM page o JOIN page p ON p.id = o.parent_id
			WHERE o.trash_id = o.id AND o.id <> $1 AND p.trash_id = $1
			ORDER BY o.rank, o.id`, id)
		if err != nil {
			return err
		}
		inside, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, other := range inside {
			if err := underHome(ctx, tx, sp, other); err != nil {
				return err
			}
		}
		// Before the rows go, which the database finds the pages below by.
		if err := syncLinksBelow(ctx, tx, []uuid.UUID{id}, true); err != nil {
			return err
		}
		var gone int64
		if err := tx.QueryRow(ctx, `SELECT page_purge($1)`, id).Scan(&gone); err != nil {
			return fmt.Errorf("purge the page: %w", err)
		}
		return record(ctx, tx, actor, audit.ActionPagePurged, id, map[string]any{"space": sp.Key, "title": title, "pages": gone})
	})
}

// EmptyTrash deletes every item of a space's trash for good.
func (s *Service) EmptyTrash(ctx context.Context, actor perm.Actor, spaceKey string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := trashSpace(ctx, tx, actor, spaceKey, perm.PurgeTrash)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('page-tree:' || $1::text, 0))`, sp.ID); err != nil {
			return err
		}
		if err := syncLinksOfSpace(ctx, tx, sp.ID, true); err != nil {
			return err
		}
		var gone int64
		if err := tx.QueryRow(ctx, `SELECT space_empty_trash($1)`, sp.ID).Scan(&gone); err != nil {
			return fmt.Errorf("empty the trash: %w", err)
		}
		return record(ctx, tx, actor, audit.ActionTrashEmptied, sp.ID, map[string]any{"space": sp.Key, "pages": gone})
	})
}

// record notes an administrator deleting pages for good in the audit log.
func record(ctx context.Context, tx db.DBTX, actor perm.Actor, action string, target uuid.UUID, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	targetType := "page"
	if action == audit.ActionTrashEmptied {
		targetType = "space"
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: targetType, TargetID: &target, Actor: actor.UserID, Data: data})
}
