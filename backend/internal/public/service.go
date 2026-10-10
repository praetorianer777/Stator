package public

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Site finds the organization a public address names. One that is archived,
// or lets nobody read without signing in, is not found, as one that does not
// exist: an address tells nobody which organizations there are.
func (s *Service) Site(ctx context.Context, slug string) (*Site, error) {
	var out Site
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT o.id, o.slug::text, o.name, o.anonymous_indexable, COALESCE(b.footer_en, ''), COALESCE(b.footer_de, ''),
			       CASE WHEN b.logo_type IS NOT NULL THEN b.logo_version END
			FROM org o LEFT JOIN org_brand b ON b.org_id = o.id
			WHERE o.slug = $1 AND o.anonymous_access AND o.archived_at IS NULL`, strings.ToLower(strings.TrimSpace(slug))).
			Scan(&out.ID, &out.Slug, &out.Name, &out.Indexable, &out.Footer.En, &out.Footer.De, &out.LogoVersion)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotPublic
	}
	if err != nil {
		return nil, fmt.Errorf("find the organization: %w", err)
	}
	return &out, nil
}

// Reading binds ctx to the site as an anonymous reader, whom the database
// holds to the spaces open to anybody whatever a query forgets to ask.
func Reading(ctx context.Context, site *Site) context.Context {
	return db.WithAnonymous(tenant.WithOrg(ctx, tenant.Org{ID: site.ID, Slug: site.Slug}))
}

// A space or page an anonymous reader may read, asked again in the service
// though the policies ask it too: the answer then never rests on one of them.
const (
	publicSpace = `perm_space_holds(NULL::uuid, s.id, 'view')`
	publicPage  = `p.trashed_at IS NULL AND p.version > 0 AND perm_page_viewable(p.id, NULL::uuid)`
)

func requireAnonymous(ctx context.Context) error {
	if !db.AnonymousFrom(ctx) {
		return errors.New("a public read is made as an anonymous reader")
	}
	return nil
}

// Spaces lists the spaces anybody may read, by name.
func (s *Service) Spaces(ctx context.Context) ([]Space, error) {
	if err := requireAnonymous(ctx); err != nil {
		return nil, err
	}
	out := []Space{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT s.key, s.name, s.description, s.home_page_id FROM space s
			WHERE s.home_page_id IS NOT NULL AND `+publicSpace+`
			ORDER BY lower(s.name), s.key`)
		if err != nil {
			return fmt.Errorf("list the public spaces: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Space])
		return err
	})
	if out == nil {
		out = []Space{}
	}
	return out, err
}

// Space is one public space and its public pages in reading order.
func (s *Service) Space(ctx context.Context, key string) (*Space, []TreePage, error) {
	if err := requireAnonymous(ctx); err != nil {
		return nil, nil, err
	}
	var (
		sp    Space
		pages []TreePage
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT s.key, s.name, s.description, s.home_page_id FROM space s
			WHERE s.key = upper(btrim($1)) AND s.home_page_id IS NOT NULL AND `+publicSpace, key).
			Scan(&sp.Key, &sp.Name, &sp.Description, &sp.HomePageID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotPublic
		}
		if err != nil {
			return fmt.Errorf("find the space: %w", err)
		}
		// A page hidden from anybody hides every page below it, so the walk
		// stops there and never asks about the pages it would not show.
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE tree (id, parent_id, title, kind, icon, rank) AS (
				SELECT p.id, p.parent_id, p.title, p.kind, p.icon, p.rank FROM page p
				WHERE p.id = $1 AND `+publicPage+`
				UNION ALL
				SELECT p.id, p.parent_id, p.title, p.kind, p.icon, p.rank FROM page p JOIN tree t ON p.parent_id = t.id
				WHERE `+publicPage+`
			)
			SELECT id, parent_id, title, kind, icon, rank::text FROM tree`, sp.HomePageID)
		if err != nil {
			return fmt.Errorf("read the space's pages: %w", err)
		}
		type row struct {
			page TreePage
			rank string
		}
		var all []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.page.ID, &r.page.ParentID, &r.page.Title, &r.page.Kind, &r.page.Icon, &r.rank); err != nil {
				rows.Close()
				return err
			}
			all = append(all, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		below := map[uuid.UUID][]row{}
		for _, r := range all {
			if r.page.ParentID != nil {
				below[*r.page.ParentID] = append(below[*r.page.ParentID], r)
			}
		}
		for _, children := range below {
			slices.SortFunc(children, func(a, b row) int {
				return cmp.Or(strings.Compare(a.rank, b.rank), strings.Compare(a.page.ID.String(), b.page.ID.String()))
			})
		}
		var walk func(id uuid.UUID, depth int)
		walk = func(id uuid.UUID, depth int) {
			for _, child := range below[id] {
				if len(pages) >= MaxTreePages {
					return
				}
				child.page.Depth = depth
				pages = append(pages, child.page)
				walk(child.page.ID, depth+1)
			}
		}
		for _, r := range all {
			if r.page.ID == sp.HomePageID {
				pages = append(pages, r.page)
				walk(r.page.ID, 1)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if pages == nil {
		pages = []TreePage{}
	}
	return &sp, pages, nil
}

// Page is one published page anybody may read, with the people it names
// left unnamed.
func (s *Service) Page(ctx context.Context, id uuid.UUID) (*Page, error) {
	if err := requireAnonymous(ctx); err != nil {
		return nil, err
	}
	var out Page
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			body    []byte
			coverID *uuid.UUID
			focusX  int
			focusY  int
			width   string
		)
		err := tx.QueryRow(ctx, `
			SELECT p.id, p.title, p.kind, p.body, p.version, p.parent_id IS NULL AND p.kind <> 'post', COALESCE(p.published_at, p.updated_at),
			       p.icon, p.width, p.cover_attachment_id, p.cover_focus_x, p.cover_focus_y,
			       s.key, s.name, s.description, s.home_page_id
			FROM page p JOIN space s ON s.id = p.space_id
			WHERE p.id = $1 AND `+publicPage+` AND `+publicSpace, id).
			Scan(&out.ID, &out.Title, &out.Kind, &body, &out.Version, &out.Home, &out.Updated,
				&out.Appearance.Icon, &width, &coverID, &focusX, &focusY,
				&out.Space.Key, &out.Space.Name, &out.Space.Description, &out.Space.HomePageID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotPublic
		}
		if err != nil {
			return fmt.Errorf("read the page: %w", err)
		}
		out.Appearance.Width = page.Width(width)
		if coverID != nil {
			out.Appearance.Cover = &page.Cover{AttachmentID: *coverID, FocusX: focusX, FocusY: focusY}
		}
		if out.Body, err = anonymousBody(body); err != nil {
			return err
		}
		if out.Ancestors, err = refs(ctx, tx, `
			WITH RECURSIVE up (id, parent_id, title, kind, depth) AS (
				SELECT a.id, a.parent_id, a.title, a.kind, 0 FROM page a WHERE a.id = (SELECT parent_id FROM page WHERE id = $1)
				UNION ALL
				SELECT a.id, a.parent_id, a.title, a.kind, up.depth + 1 FROM page a JOIN up ON a.id = up.parent_id
			)
			SELECT id, title, kind FROM up ORDER BY depth DESC`, id); err != nil {
			return err
		}
		out.Children, err = refs(ctx, tx, `
			SELECT p.id, p.title, p.kind FROM page p
			WHERE p.parent_id = $1 AND `+publicPage+`
			ORDER BY p.rank, p.id`, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func refs(ctx context.Context, tx db.DBTX, sql string, args ...any) ([]Ref, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("read the pages around the page: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Ref])
	if out == nil {
		out = []Ref{}
	}
	return out, err
}

// anonymousBody is a stored body with the people in it unnamed. A body that
// no longer parses is shown as an empty page rather than as it is stored.
func anonymousBody(stored []byte) (json.RawMessage, error) {
	root, err := document.Parse(stored)
	if err != nil {
		return json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`), nil
	}
	out, err := json.Marshal(document.ForAnonymous(root))
	if err != nil {
		return nil, fmt.Errorf("write the page for an anonymous reader: %w", err)
	}
	return out, nil
}

// Settings is whether the organization lets anybody read the spaces that
// allow it. For administrators.
func (s *Service) Settings(ctx context.Context, actor perm.Actor) (*Settings, error) {
	var out Settings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		return read(ctx, tx, &out)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func read(ctx context.Context, tx db.DBTX, out *Settings) error {
	if err := tx.QueryRow(ctx, `SELECT anonymous_access, anonymous_indexable FROM org WHERE id = current_org_id()`).
		Scan(&out.Enabled, &out.Indexable); err != nil {
		return fmt.Errorf("read whether the organization is open to anybody: %w", err)
	}
	return nil
}

func requireAdmin(ctx context.Context, tx db.DBTX, actor perm.Actor) error {
	f, err := perm.LoadFacts(ctx, tx, actor, uuid.Nil)
	if err != nil {
		return err
	}
	if !f.OrgAdmin() {
		return ErrNotAdmin
	}
	return nil
}

// SetSettings turns reading without signing in on or off for the whole
// organization, and says whether search engines are asked in.
func (s *Service) SetSettings(ctx context.Context, actor perm.Actor, in Settings) (*Settings, db.LSN, error) {
	var out Settings
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var before Settings
		if err := read(ctx, tx, &before); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE org SET anonymous_access = $1, anonymous_indexable = $2 WHERE id = current_org_id()`,
			in.Enabled, in.Indexable); err != nil {
			return fmt.Errorf("open the organization to anybody: %w", err)
		}
		out = in
		if before == in {
			return nil
		}
		return perm.Record(ctx, tx, actor, audit.ActionOrgAnonymousAccessSet, "org", nil,
			map[string]any{"enabled": in.Enabled, "indexable": in.Indexable})
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}
