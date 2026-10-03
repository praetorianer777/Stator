package page

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

var (
	// ErrCycle refuses a move under the page itself or one of its own pages.
	ErrCycle = errors.New("a page cannot move under itself or one of its own pages; choose a parent outside it")
	// ErrHomeFixed refuses moving the root of a space.
	ErrHomeFixed = errors.New("the home page stays at the top of its space; move the pages under it instead")
	// ErrNotASibling refuses a position next to a page that is not under the parent.
	ErrNotASibling = errors.New("the page to place it next to is not under that parent; reload the tree and try again")
)

// cycleConstraint is what the database's own tree guard names in its refusal.
const cycleConstraint = "page_tree_no_cycle"

// homeTrashConstraint is the check that asks page_trashable, the one rule on
// what may go to the trash.
const homeTrashConstraint = "page_home_never_trashed"

// Ref is a page named by id and title, as a breadcrumb or a tree shows it.
type Ref struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Home  bool      `json:"home"`
}

// TreeNode is one page in the sidebar's tree, loaded a level at a time.
type TreeNode struct {
	ID          uuid.UUID `json:"id"`
	ParentID    uuid.UUID `json:"parentId"`
	Title       string    `json:"title"`
	HasChildren bool      `json:"hasChildren"`
	// Archived marks a page listed under an archived parent; elsewhere the
	// tree leaves archived pages out.
	Archived bool `json:"archived"`
	// Unpublished marks a page only its creator sees; Restricted, one whose
	// view is narrowed here or above.
	Unpublished bool `json:"unpublished"`
	Restricted  bool `json:"restricted"`
	// Kind is page or folder.
	Kind Kind `json:"kind"`
	// Icon is the emoji before the page's title, null when it has none.
	Icon *string `json:"icon"`
}

// OutlineEntry is one page of a whole space in reading order, for choosing
// where a page goes.
type OutlineEntry struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parentId"`
	Title    string     `json:"title"`
	Depth    int        `json:"depth"`
}

// Placement is where a page goes: under a parent, before or after one of its
// children, or last when neither is named.
type Placement struct {
	ParentID uuid.UUID  `json:"parentId"`
	BeforeID *uuid.UUID `json:"beforeId,omitempty"`
	AfterID  *uuid.UUID `json:"afterId,omitempty"`
}

// CreateInput is a new page.
type CreateInput struct {
	Placement
	Title string `json:"title"`
	// Body is the first document; empty starts with an empty one.
	Body json.RawMessage `json:"body,omitempty"`
	// Publish makes the page version 1 at once, seen by everybody who may see
	// the space; otherwise it stays an unpublished page of its creator's.
	Publish bool `json:"publish,omitempty"`
	// Kind folder makes a folder, which takes no body and is seen at once by
	// everybody who may see where it is; empty or page makes a page.
	Kind Kind `json:"kind,omitempty"`
}

// MoveInput moves a page. Without its children they stay where the page was.
type MoveInput struct {
	Placement
	WithChildren *bool `json:"withChildren,omitempty"`
}

// CopyInput copies a page, and with its children the pages below it too.
type CopyInput struct {
	Placement
	WithChildren bool    `json:"withChildren"`
	Title        *string `json:"title,omitempty"`
}

// live narrows a query on page p to what the tree shows: nothing in the trash.
const live = ` p.trashed_at IS NULL`

// childrenOf lists a parent's children in order, leaving one out: all of
// them, the ones the actor cannot see included, so a new rank fits among all.
func childrenOf(ctx context.Context, tx db.DBTX, parent, except uuid.UUID) ([]sibling, error) {
	rows, err := tx.Query(ctx, `SELECT id, rank FROM page_sibling_ranks($1, $2)`, parent, except)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[sibling])
}

type sibling struct {
	ID   uuid.UUID
	Rank string
}

// rankAt finds the rank for a place among a parent's children. Ranks that
// leave no room, which only a tie can do, are renumbered first.
func rankAt(ctx context.Context, tx db.DBTX, where Placement, except uuid.UUID) (string, error) {
	siblings, err := childrenOf(ctx, tx, where.ParentID, except)
	if err != nil {
		return "", err
	}
	index := len(siblings)
	find := func(id uuid.UUID) int {
		for i, s := range siblings {
			if s.ID == id {
				return i
			}
		}
		return -1
	}
	switch {
	case where.AfterID != nil:
		if index = find(*where.AfterID); index < 0 {
			return "", ErrNotASibling
		}
		index++
	case where.BeforeID != nil:
		if index = find(*where.BeforeID); index < 0 {
			return "", ErrNotASibling
		}
	}
	bounds := func() (string, string) {
		var a, b string
		if index > 0 {
			a = siblings[index-1].Rank
		}
		if index < len(siblings) {
			b = siblings[index].Rank
		}
		return a, b
	}
	if r, err := rank.Between(bounds()); err == nil {
		return r, nil
	}
	fresh, err := rank.Sequence(len(siblings))
	if err != nil {
		return "", err
	}
	ids := make([]uuid.UUID, len(siblings))
	for i := range siblings {
		ids[i] = siblings[i].ID
		siblings[i].Rank = fresh[i]
	}
	if err := place(ctx, tx, ids, where.ParentID, fresh); err != nil {
		return "", err
	}
	return rank.Between(bounds())
}

// place hangs pages under a parent at the ranks given, through the database
// function that may move pages the actor cannot see.
func place(ctx context.Context, tx db.DBTX, ids []uuid.UUID, parent uuid.UUID, ranks []string) error {
	_, err := tx.Exec(ctx, `SELECT page_place($1, $2, $3)`, ids, parent, ranks)
	return err
}

// parentFor reads and locks the page a page goes under, and asks whether the
// actor may add pages there.
func parentFor(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Page, *space.Space, error) {
	p, sp, err := load(ctx, tx, actor, id, false)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM page WHERE id = $1 FOR NO KEY UPDATE`, id); err != nil {
		return nil, nil, err
	}
	if err := p.must(perm.EditPages); err != nil {
		return nil, nil, err
	}
	return p, sp, nil
}

// ancestors are the pages above one, the home page first.
func ancestors(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]Ref, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE above (id, parent_id, title, depth) AS (
			SELECT p.id, p.parent_id, p.title, 0 FROM page p
			WHERE p.id = (SELECT parent_id FROM page WHERE id = $1)
			UNION ALL
			SELECT p.id, p.parent_id, p.title, a.depth + 1 FROM page p JOIN above a ON p.id = a.parent_id
		)
		SELECT id, title, parent_id IS NULL FROM above ORDER BY depth DESC`, id)
	if err != nil {
		return nil, err
	}
	refs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Ref])
	if refs == nil {
		refs = []Ref{}
	}
	return refs, err
}

// isBelow reports whether candidate is the page itself or somewhere under it.
func isBelow(ctx context.Context, tx db.DBTX, candidate, page uuid.UUID) (bool, error) {
	var below bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE above (id, parent_id) AS (
			SELECT id, parent_id FROM page WHERE id = $1
			UNION
			SELECT p.id, p.parent_id FROM page p JOIN above a ON p.id = a.parent_id
		)
		SELECT EXISTS (SELECT 1 FROM above WHERE id = $2)`, candidate, page).Scan(&below)
	return below, err
}

// Children lists the pages directly under a parent of a space, by default
// under its home page, each saying whether it has children of its own.
func (s *Service) Children(ctx context.Context, actor perm.Actor, spaceKey string, parent *uuid.UUID) ([]TreeNode, error) {
	out := []TreeNode{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		under := sp.HomePageID
		if parent != nil {
			under = *parent
		}
		var archived *bool
		if err := tx.QueryRow(ctx, `SELECT (SELECT p.archived_at IS NOT NULL FROM page p WHERE p.id = $1 AND p.space_id = $2 AND`+live+` AND `+perm.ViewablePage("p", 3)+`)`,
			under, sp.ID, actor.UserID).Scan(&archived); err != nil {
			return err
		}
		if archived == nil {
			return ErrNotFound
		}
		above, _, err := perm.ForPage(ctx, tx, actor, under)
		if err != nil {
			return err
		}
		// Under an archived page everything is archived, so it shows its own.
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.parent_id, p.title,
			       EXISTS (SELECT 1 FROM page c WHERE c.parent_id = p.id AND`+liveChild+` AND (c.archived_at IS NULL OR $4)
			               AND `+perm.ViewablePage("c", 2)+`),
			       p.archived_at IS NOT NULL,
			       p.version = 0,
			       $3 OR EXISTS (SELECT 1 FROM page_restriction r WHERE r.page_id = p.id AND r.kind = 'view'),
			       p.kind, p.icon
			FROM page p WHERE p.parent_id = $1 AND`+live+` AND (p.archived_at IS NULL OR $4) AND `+perm.ViewablePage("p", 2)+`
			ORDER BY p.rank, p.id`, under, actor.UserID, above.ViewRestricted, *archived)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TreeNode]); out == nil {
			out = []TreeNode{}
		}
		return err
	})
	return out, err
}

// liveChild is live for the alias c.
const liveChild = ` c.trashed_at IS NULL`

// Outline is every page of a space in reading order, with its depth; an
// archived page takes no pages, so it is left out with what is below it.
func (s *Service) Outline(ctx context.Context, actor perm.Actor, spaceKey string) ([]OutlineEntry, error) {
	var out []OutlineEntry
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		// The path joins each page's rank and id to its parent's with
		// separators below every rank digit, so it sorts parents first and
		// siblings by rank.
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE tree (id, parent_id, title, depth, path) AS (
				SELECT p.id, p.parent_id, p.title, 0, (p.rank || chr(2) || p.id::text || chr(1)) COLLATE "C"
				FROM page p WHERE p.id = $1
				UNION ALL
				SELECT p.id, p.parent_id, p.title, t.depth + 1, (t.path || p.rank || chr(2) || p.id::text || chr(1)) COLLATE "C"
				FROM page p JOIN tree t ON p.parent_id = t.id
				WHERE`+live+` AND p.archived_at IS NULL AND `+perm.ViewablePage("p", 2)+`
			)
			SELECT id, parent_id, title, depth FROM tree ORDER BY path`, sp.HomePageID, actor.UserID)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[OutlineEntry]); out == nil {
			out = []OutlineEntry{}
		}
		return err
	})
	return out, err
}

// Create adds a page under a parent, last unless a place is named.
func (s *Service) Create(ctx context.Context, actor perm.Actor, in CreateInput) (*Page, db.LSN, error) {
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, 0, err
	}
	kind := cmp.Or(in.Kind, KindPage)
	if kind != KindPage && kind != KindFolder {
		return nil, 0, ErrBadKind
	}
	if kind == KindFolder && in.Body != nil {
		return nil, 0, ErrFolder
	}
	if in.Body != nil {
		if err := document.Validate(in.Body); err != nil {
			return nil, 0, err
		}
	}
	// A folder has nothing to publish, but it is version 1 from the start: an
	// unpublished row is its creator's alone, and so would be all below it.
	version := 0
	if kind == KindFolder {
		version = 1
	}
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := parentFor(ctx, tx, actor, in.ParentID)
		if err != nil {
			return err
		}
		r, err := rankAt(ctx, tx, in.Placement, uuid.Nil)
		if err != nil {
			return err
		}
		// Made here rather than returned, which the policies would refuse:
		// the statement's own snapshot does not hold the row it writes.
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO page (id, org_id, space_id, parent_id, rank, title, body, created_by, updated_by, kind, version)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, COALESCE($6::jsonb, '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb), $7, $7, $8, $9)`,
			id, sp.ID, in.ParentID, r, title, nullJSON(in.Body), actor.UserID, kind, version); err != nil {
			return fmt.Errorf("save the page: %w", err)
		}
		if err := watch.Auto(ctx, tx, actor.UserID, id); err != nil {
			return fmt.Errorf("watch the page: %w", err)
		}
		if in.Publish && kind == KindPage {
			made, _, err := load(ctx, tx, actor, id, true)
			if err != nil {
				return err
			}
			if _, err := publish(ctx, tx, actor, made, release{title: made.Title, body: made.Body}); err != nil {
				return err
			}
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

func nullJSON(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	return string(raw)
}

// Move puts a page somewhere else in its space or in another, with its
// children, or without them, which then take its place.
func (s *Service) Move(ctx context.Context, actor perm.Actor, id uuid.UUID, in MoveInput) (*Page, db.LSN, error) {
	withChildren := in.WithChildren == nil || *in.WithChildren
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := lockTrees(ctx, tx, id, in.ParentID); err != nil {
			return err
		}
		current, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if current.Home {
			return ErrHomeFixed
		}
		if err := current.must(perm.EditPages); err != nil {
			return err
		}
		_, to, err := parentFor(ctx, tx, actor, in.ParentID)
		if err != nil {
			return err
		}
		if !withChildren {
			if err := letChildrenStay(ctx, tx, current); err != nil {
				return err
			}
		}
		if below, err := isBelow(ctx, tx, in.ParentID, id); err != nil {
			return err
		} else if below {
			return ErrCycle
		}
		r, err := rankAt(ctx, tx, in.Placement, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET parent_id = $2, space_id = $3, rank = $4 WHERE id = $1`,
			id, in.ParentID, to.ID, r); err != nil {
			if isCycle(err) {
				return ErrCycle
			}
			return fmt.Errorf("move the page: %w", err)
		}
		// The space is in every address Armature holds for the pages moved.
		if to.ID != current.SpaceID {
			if err := syncLinksBelow(ctx, tx, []uuid.UUID{id}, withChildren); err != nil {
				return err
			}
		}
		// A new place among the same siblings is an order, not a move, and an
		// unpublished page has only its author to tell.
		moved := to.ID != current.SpaceID || current.ParentID == nil || *current.ParentID != in.ParentID
		if moved && current.Version > 0 {
			parent := in.ParentID
			if err := events.Emit(ctx, tx, events.TopicPageMoved, events.PageMoved{
				PageID: id, ActorID: actor.UserID,
				FromSpaceID: current.SpaceID, FromParentID: current.ParentID,
				ToSpaceID: to.ID, ToParentID: &parent,
			}); err != nil {
				return err
			}
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

// letChildrenStay hands a page's children to its parent, in their order,
// where the page stood.
func letChildrenStay(ctx context.Context, tx db.DBTX, p *Page) error {
	children, err := childrenOf(ctx, tx, p.ID, uuid.Nil)
	if err != nil || len(children) == 0 {
		return err
	}
	var current, next string
	if err := tx.QueryRow(ctx, `SELECT rank FROM page WHERE id = $1`, p.ID).Scan(&current); err != nil {
		return err
	}
	siblings, err := childrenOf(ctx, tx, *p.ParentID, p.ID)
	if err != nil {
		return err
	}
	for _, s := range siblings {
		if s.Rank > current || (s.Rank == current && s.ID.String() > p.ID.String()) {
			next = s.Rank
			break
		}
	}
	ranks, err := rank.Spread(current, next, len(children))
	if err != nil {
		// A tie with the next sibling leaves no gap; after the page is room.
		if ranks, err = rank.Spread(current, "", len(children)); err != nil {
			return err
		}
	}
	ids := make([]uuid.UUID, len(children))
	for i, child := range children {
		ids[i] = child.ID
	}
	return place(ctx, tx, ids, *p.ParentID, ranks)
}

// Copy makes a new page like this one, with its title and body, under a
// parent in this space or another, and with its children the pages below it
// the actor sees. Copies are published at version 1, with no history, and
// without the original's inline threads.
func (s *Service) Copy(ctx context.Context, actor perm.Actor, id uuid.UUID, in CopyInput) (*Page, db.LSN, error) {
	var title *string
	if in.Title != nil {
		cleaned, err := cleanTitle(*in.Title)
		if err != nil {
			return nil, 0, err
		}
		title = &cleaned
	}
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := load(ctx, tx, actor, id, false); err != nil {
			return err
		}
		_, to, err := parentFor(ctx, tx, actor, in.ParentID)
		if err != nil {
			return err
		}
		r, err := rankAt(ctx, tx, in.Placement, uuid.Nil)
		if err != nil {
			return err
		}
		// The subtree is read from the statement's own snapshot, so a copy
		// placed under the original copies the original once.
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE below (id, parent_id, depth) AS (
				SELECT p.id, p.parent_id, 0 FROM page p WHERE p.id = $1
				UNION ALL
				SELECT p.id, p.parent_id, b.depth + 1 FROM page p JOIN below b ON p.parent_id = b.id
				WHERE $2 AND`+live+` AND `+perm.ViewablePage("p", 7)+`
			), fresh AS MATERIALIZED (
				SELECT id AS old_id, uuidv7() AS new_id, parent_id, depth FROM below
			), made AS (
				INSERT INTO page (id, org_id, space_id, parent_id, rank, title, body, created_by, updated_by, kind)
				SELECT f.new_id, p.org_id, $3,
				       CASE WHEN f.depth = 0 THEN $4 ELSE up.new_id END,
				       CASE WHEN f.depth = 0 THEN $5 ELSE p.rank END,
				       CASE WHEN f.depth = 0 THEN COALESCE($6, p.title) ELSE p.title END,
				       document_unanchored(p.body), $7, $7, p.kind
				FROM fresh f JOIN page p ON p.id = f.old_id LEFT JOIN fresh up ON up.old_id = f.parent_id
				ORDER BY f.depth
			)
			SELECT old_id, new_id FROM fresh ORDER BY depth`,
			id, in.WithChildren, to.ID, in.ParentID, r, title, actor.UserID)
		if err != nil {
			return fmt.Errorf("copy the page: %w", err)
		}
		pairs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[copied])
		if err != nil {
			return fmt.Errorf("copy the page: %w", err)
		}
		copies := make(map[uuid.UUID]uuid.UUID, len(pairs))
		for _, p := range pairs {
			copies[p.From] = p.To
		}
		for _, o := range s.copyObservers {
			if err := o.PagesCopied(ctx, tx, copies); err != nil {
				return err
			}
		}
		made := copies[id]
		olds, news := make([]uuid.UUID, len(pairs)), make([]uuid.UUID, len(pairs))
		for i, pair := range pairs {
			olds[i], news[i] = pair.From, pair.To
		}
		// A statement of its own, because the version trigger reads the page
		// rows, which the statement that inserts them cannot see. It comes
		// after the observers, so version 1 holds the body they rewrote.
		if _, err := tx.Exec(ctx, `
			WITH RECURSIVE copied (id) AS (
				SELECT $1::uuid
				UNION ALL
				SELECT p.id FROM page p JOIN copied c ON p.parent_id = c.id
			)
			INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
			SELECT p.org_id, p.id, 1, p.title, p.body, $2 FROM page p JOIN copied c ON c.id = p.id
			WHERE p.kind = 'page'`, made, actor.UserID); err != nil {
			return fmt.Errorf("publish the copy: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			WITH RECURSIVE copied (id) AS (
				SELECT $1::uuid
				UNION ALL
				SELECT p.id FROM page p JOIN copied c ON p.parent_id = c.id
			)
			UPDATE page SET version = 1 WHERE id IN (SELECT id FROM copied)`, made); err != nil {
			return fmt.Errorf("publish the copy: %w", err)
		}
		// Each copy keeps its original's own lists, and takes on those above
		// where it lands.
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id, group_id)
			SELECT r.org_id, m.new_id, r.kind, r.subject_type, r.user_id, r.group_id
			FROM page_restriction r JOIN unnest($1::uuid[], $2::uuid[]) AS m (old_id, new_id) ON r.page_id = m.old_id`, olds, news); err != nil {
			return fmt.Errorf("copy the restrictions: %w", err)
		}
		if err := watch.Auto(ctx, tx, actor.UserID, made); err != nil {
			return fmt.Errorf("watch the copy: %w", err)
		}
		if err := syncLinksBelow(ctx, tx, news, false); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, made, false)
		return err
	})
	return out, lsn, err
}

// lockTrees takes the tree lock of every space the pages are in, in the
// order the database's tree guard takes them, before any row is locked: two
// crossing moves then queue for the lock instead of deadlocking on rows.
func lockTrees(ctx context.Context, tx db.DBTX, pages ...uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended('page-tree:' || space_id::text, 0))
		FROM (SELECT DISTINCT space_id FROM page WHERE id = ANY($1) ORDER BY space_id) spaces`, pages)
	return err
}

func isCycle(err error) bool {
	return isConstraint(err, cycleConstraint)
}

func isConstraint(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == name
}
