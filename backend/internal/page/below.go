package page

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// MaxBelow caps how many pages one list of the pages below a page holds; the
// shallowest are kept, so a cut list is still a whole tree down to its cut.
const MaxBelow = 500

var (
	// ErrBadScope and ErrBadSort refuse a list of the pages below a page that
	// names something a child pages block cannot.
	ErrBadScope = errors.New("choose children or subtree")
	ErrBadSort  = errors.New("choose tree, title or updated")
	// ErrBadDepth refuses a depth a child pages block cannot hold.
	ErrBadDepth = errors.New("choose a depth of 1 to 10, or leave it out for every level")
)

// BelowQuery is what a child pages block asks for: the direct children or the
// whole subtree, down to Depth levels when it is set, siblings in Sort order.
type BelowQuery struct {
	Scope string
	Depth *int
	Sort  string
}

// Check refuses what the document allowlist would refuse in a block.
func (q BelowQuery) Check() error {
	if !slices.Contains(document.ChildPagesScopes, q.Scope) {
		return ErrBadScope
	}
	if !slices.Contains(document.ChildPagesSorts, q.Sort) {
		return ErrBadSort
	}
	if q.Depth != nil && (*q.Depth < 1 || *q.Depth > document.MaxChildPagesDepth) {
		return ErrBadDepth
	}
	return nil
}

// BelowPage is one page under the page a list starts from, in reading order.
type BelowPage struct {
	ID       uuid.UUID `json:"id"`
	ParentID uuid.UUID `json:"parentId"`
	Title    string    `json:"title"`
	// Depth is 1 for a direct child, 2 for a grandchild and so on.
	Depth       int       `json:"depth"`
	Unpublished bool      `json:"unpublished"`
	UpdatedAt   time.Time `json:"updatedAt"`
	rank        string
}

// Below lists the pages under a page the actor may view, out of the trash,
// their own unpublished ones included, and says whether MaxBelow cut it.
func (s *Service) Below(ctx context.Context, actor perm.Actor, id uuid.UUID, q BelowQuery) ([]BelowPage, bool, error) {
	if err := q.Check(); err != nil {
		return nil, false, err
	}
	depth := 0
	switch {
	case q.Scope == "children":
		depth = 1
	case q.Depth != nil:
		depth = *q.Depth
	}
	var found []BelowPage
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = $1 AND`+live+` AND `+perm.ViewablePage("p", 2)+`)`,
			id, actor.UserID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		// Breadth first, so the cut at MaxBelow keeps every parent of what it
		// keeps; a page the actor may not view hides the pages below it too.
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE below (id, parent_id, title, depth, unpublished, updated_at, rank) AS (
				SELECT p.id, p.parent_id, p.title, 1, p.version = 0, p.updated_at, p.rank
				FROM page p WHERE p.parent_id = $1 AND`+live+` AND `+perm.ViewablePage("p", 2)+`
				UNION ALL
				SELECT p.id, p.parent_id, p.title, b.depth + 1, p.version = 0, p.updated_at, p.rank
				FROM page p JOIN below b ON p.parent_id = b.id
				WHERE ($3 = 0 OR b.depth < $3) AND`+live+` AND `+perm.ViewablePage("p", 2)+`
			)
			SELECT id, parent_id, title, depth, unpublished, updated_at, rank FROM below
			ORDER BY depth, rank, id LIMIT $4`, id, actor.UserID, depth, MaxBelow+1)
		if err != nil {
			return err
		}
		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BelowPage, error) {
			var b BelowPage
			err := row.Scan(&b.ID, &b.ParentID, &b.Title, &b.Depth, &b.Unpublished, &b.UpdatedAt, &b.rank)
			return b, err
		})
		return err
	})
	if err != nil {
		return nil, false, err
	}
	cut := len(found) > MaxBelow
	if cut {
		found = found[:MaxBelow]
	}
	return readingOrder(found, id, q.Sort), cut, nil
}

// readingOrder puts each page after its parent and orders siblings by sort:
// their place in the tree, their title, or the latest change first.
func readingOrder(pages []BelowPage, root uuid.UUID, sort string) []BelowPage {
	children := map[uuid.UUID][]BelowPage{}
	for _, p := range pages {
		children[p.ParentID] = append(children[p.ParentID], p)
	}
	compare := func(a, b BelowPage) int {
		switch sort {
		case "title":
			if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
				return c
			}
		case "updated":
			if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(a.rank, b.rank); c != 0 {
			return c
		}
		return cmp.Compare(a.ID.String(), b.ID.String())
	}
	out := make([]BelowPage, 0, len(pages))
	var walk func(parent uuid.UUID)
	walk = func(parent uuid.UUID) {
		level := children[parent]
		slices.SortStableFunc(level, compare)
		for _, p := range level {
			out = append(out, p)
			walk(p.ID)
		}
	}
	walk(root)
	return out
}
