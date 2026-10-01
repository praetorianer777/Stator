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
)

var (
	// ErrHomeNotArchived refuses archiving the root of a space on its own.
	ErrHomeNotArchived = errors.New("the home page is not archived on its own; archive the whole space in its settings instead")
	// ErrParentArchived refuses unarchiving a page whose parent stays archived.
	ErrParentArchived = errors.New("the page above it is archived too; unarchive that page first")
)

// homeArchiveConstraint and parentFirstConstraint name the database's own
// refusals of the two.
const (
	homeArchiveConstraint = "page_home_never_archived"
	parentFirstConstraint = "page_archive_parent_first"
)

// ArchivedWithError refuses unarchiving a page that was archived together with
// a page above it, which is the one to unarchive.
type ArchivedWithError struct{ With Ref }

func (e *ArchivedWithError) Error() string {
	return fmt.Sprintf("this page was archived together with %s above it; unarchive that page instead", e.With.Title)
}

// Archive says how a page came to be archived: with an archived page, itself
// or one above it, with its whole space, or both.
type Archive struct {
	// Page is the page archived, null when only the space is.
	Page *Ref `json:"page"`
	// Space says the page's whole space is archived.
	Space bool `json:"space"`
	// ArchivedAt and ArchivedByName are of the page's archive, else of the space's.
	ArchivedAt     time.Time `json:"archivedAt"`
	ArchivedByName string    `json:"archivedByName"`
}

// ArchiveItem is an archived page and the pages archived with it, as the
// space's archive lists them.
type ArchiveItem struct {
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	ArchivedAt     time.Time `json:"archivedAt"`
	ArchivedByName string    `json:"archivedByName"`
	// Pages counts the pages archived with it, itself included.
	Pages int `json:"pages"`
	// ParentTitle names the page it hangs under in the tree.
	ParentTitle string `json:"parentTitle"`
}

// archiveOf reads how a page is archived, nil when it is not.
func archiveOf(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Archive, error) {
	var (
		withID              *uuid.UUID
		with                Ref
		pageAt, spaceAt     *time.Time
		pageName, spaceName string
	)
	err := tx.QueryRow(ctx, `
		SELECT a.id, COALESCE(a.title, ''), COALESCE(a.parent_id IS NULL, false),
		       p.archived_at, COALESCE(pu.name, ''), s.archived_at, COALESCE(su.name, '')
		FROM page p
		JOIN space s ON s.id = p.space_id
		LEFT JOIN page a ON a.id = p.archive_id
		LEFT JOIN app_user pu ON pu.id = p.archived_by
		LEFT JOIN app_user su ON su.id = s.archived_by
		WHERE p.id = $1`, id).Scan(&withID, &with.Title, &with.Home, &pageAt, &pageName, &spaceAt, &spaceName)
	if err != nil {
		return nil, fmt.Errorf("read whether the page is archived: %w", err)
	}
	switch {
	case pageAt != nil:
		out := &Archive{Space: spaceAt != nil, ArchivedAt: *pageAt, ArchivedByName: pageName}
		if withID != nil {
			with.ID = *withID
			out.Page = &with
		}
		return out, nil
	case spaceAt != nil:
		return &Archive{Space: true, ArchivedAt: *spaceAt, ArchivedByName: spaceName}, nil
	}
	return nil, nil
}

// archivable loads a page for archiving or unarchiving it, which its space's
// administrators may, while the space itself is not archived.
func archivable(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Page, *space.Space, error) {
	if err := lockTrees(ctx, tx, id); err != nil {
		return nil, nil, err
	}
	p, sp, err := load(ctx, tx, actor, id, true)
	if err != nil {
		return nil, nil, err
	}
	if !p.access.Archive {
		return nil, nil, perm.Refuse(perm.ArchivePages, perm.NotArchived)
	}
	if p.access.Archived == perm.ArchivedSpace {
		return nil, nil, perm.Refuse(perm.ArchivePages, perm.ArchivedSpace)
	}
	if p.Home {
		return nil, nil, ErrHomeNotArchived
	}
	return p, sp, nil
}

// Archive marks a page and every page below it still in the tree as one item
// of its space's archive; a page archived already is no change.
func (s *Service) Archive(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Page, db.LSN, error) {
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := archivable(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var marked int64
		if err := tx.QueryRow(ctx, `SELECT page_archive($1)`, id).Scan(&marked); err != nil {
			if isConstraint(err, homeArchiveConstraint) {
				return ErrHomeNotArchived
			}
			return fmt.Errorf("archive the page: %w", err)
		}
		if marked > 0 {
			if err := record(ctx, tx, actor, audit.ActionPageArchived, id, map[string]any{"space": sp.Key, "title": p.Title, "pages": marked}); err != nil {
				return err
			}
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

// Unarchive clears the item a page heads; a page that is not archived is no
// change, and one archived with a page above it is refused for that one.
func (s *Service) Unarchive(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Page, db.LSN, error) {
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := archivable(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if p.Archived == nil {
			out = p
			return nil
		}
		if p.Archived.Page != nil && p.Archived.Page.ID != id {
			return &ArchivedWithError{With: *p.Archived.Page}
		}
		var cleared int64
		if err := tx.QueryRow(ctx, `SELECT page_unarchive($1)`, id).Scan(&cleared); err != nil {
			if isConstraint(err, parentFirstConstraint) {
				return ErrParentArchived
			}
			return fmt.Errorf("unarchive the page: %w", err)
		}
		if err := record(ctx, tx, actor, audit.ActionPageUnarchived, id, map[string]any{"space": sp.Key, "title": p.Title, "pages": cleared}); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

// ListArchive lists the archived pages of a space that the actor may view,
// the latest first, each with the pages archived with it counted.
func (s *Service) ListArchive(ctx context.Context, actor perm.Actor, spaceKey string) ([]ArchiveItem, error) {
	out := []ArchiveItem{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, p.archived_at, COALESCE(u.name, ''),
			       (SELECT count(*) FROM page i WHERE i.archive_id = p.id AND i.trashed_at IS NULL),
			       COALESCE(parent.title, '')
			FROM page p
			LEFT JOIN page parent ON parent.id = p.parent_id
			LEFT JOIN app_user u ON u.id = p.archived_by
			WHERE p.space_id = $1 AND p.archive_id = p.id AND`+live+` AND `+perm.ViewablePage("p", 2)+`
			ORDER BY p.archived_at DESC, p.id`, sp.ID, actor.UserID)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ArchiveItem]); out == nil {
			out = []ArchiveItem{}
		}
		return err
	})
	return out, err
}
