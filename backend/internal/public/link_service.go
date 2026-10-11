package public

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// ErrPageNotFound answers a page the caller may not view or that is in the trash.
var ErrPageNotFound = errors.New("page not found")

// fullConstraint is what the database names when a page passes MaxLinksPerPage.
const fullConstraint = "page_link_per_page"

// linked is a page as its links' managers see it: its title and why no link
// may be made for it now, if anything.
type linked struct {
	title   string
	refusal *Refusal
}

func linkable(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) (linked, error) {
	var out linked
	err := tx.QueryRow(ctx, `
		SELECT p.title, page_link_refusal(p.id, $2::uuid) FROM page p
		WHERE p.id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 2), pageID, actor.UserID).Scan(&out.title, &out.refusal)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrPageNotFound
	}
	if err != nil {
		return out, fmt.Errorf("read whether the page may have links: %w", err)
	}
	return out, nil
}

const selectLinks = `
	SELECT l.id, l.label, l.created_at, l.expires_at, u.id, u.name, u.email::text
	FROM page_link l LEFT JOIN app_user u ON u.id = l.created_by
	WHERE l.page_id = $1 AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())`

func scanLink(row pgx.CollectableRow) (Link, error) {
	var (
		l           Link
		maker       *uuid.UUID
		name, email *string
	)
	if err := row.Scan(&l.ID, &l.Label, &l.CreatedAt, &l.ExpiresAt, &maker, &name, &email); err != nil {
		return l, err
	}
	if maker != nil {
		l.CreatedBy = &perm.Person{ID: *maker, Name: deref(name), Email: deref(email)}
	}
	return l, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Links are a page's live public links and whether another may be made. Who
// may not manage them is told why and shown none.
func (s *Service) Links(ctx context.Context, actor perm.Actor, pageID uuid.UUID) (*PageLinks, error) {
	out := PageLinks{Links: []Link{}, Max: MaxLinksPerPage}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := linkable(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		out.Refusal = p.refusal
		if p.refusal != nil && *p.refusal == RefusalCannotManage {
			return nil
		}
		rows, err := tx.Query(ctx, selectLinks+` ORDER BY l.created_at, l.id`, pageID)
		if err != nil {
			return fmt.Errorf("read the page's links: %w", err)
		}
		links, err := pgx.CollectRows(rows, scanLink)
		if err != nil {
			return err
		}
		if links != nil {
			out.Links = links
		}
		if out.Refusal == nil && len(out.Links) >= MaxLinksPerPage {
			full := RefusalFull
			out.Refusal = &full
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateLink makes a public link for a page. The token leaves once, in the
// answer, with the address it opens; the row keeps its digest.
func (s *Service) CreateLink(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in LinkInput) (*Link, string, string, db.LSN, error) {
	label, err := in.Clean(time.Now())
	if err != nil {
		return nil, "", "", 0, err
	}
	token, digest, err := auth.GenerateToken()
	if err != nil {
		return nil, "", "", 0, err
	}
	out := Link{Label: label, ExpiresAt: in.ExpiresAt}
	var slug string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := linkable(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if p.refusal != nil {
			return &RefusedError{Reason: *p.refusal}
		}
		var live int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM (`+selectLinks+`) l`, pageID).Scan(&live); err != nil {
			return fmt.Errorf("count the page's links: %w", err)
		}
		if live >= MaxLinksPerPage {
			return &RefusedError{Reason: RefusalFull}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO page_link (org_id, page_id, token_hash, label, created_by, expires_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			RETURNING id, created_at`, pageID, digest, label, actor.UserID, in.ExpiresAt).Scan(&out.ID, &out.CreatedAt); err != nil {
			return overFull(err)
		}
		maker := perm.Person{ID: actor.UserID}
		if err := tx.QueryRow(ctx, `SELECT u.name, u.email::text, o.slug::text FROM app_user u, org o WHERE u.id = $1 AND o.id = current_org_id()`,
			actor.UserID).Scan(&maker.Name, &maker.Email, &slug); err != nil {
			return fmt.Errorf("read who made the link: %w", err)
		}
		out.CreatedBy = &maker
		data := map[string]any{"link": out.ID, "title": p.title, "label": label, "expiresAt": nil}
		if in.ExpiresAt != nil {
			data["expiresAt"] = in.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return perm.Record(ctx, tx, actor, audit.ActionPageLinkCreated, "page", &pageID, data)
	})
	if err != nil {
		return nil, "", "", lsn, err
	}
	return &out, token, LinkPath(slug, token), lsn, nil
}

// overFull words the database's own refusal of a link past MaxLinksPerPage, which
// a link made at the same moment can meet after the count let it through.
func overFull(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == fullConstraint {
		return &RefusedError{Reason: RefusalFull}
	}
	return fmt.Errorf("make the link: %w", err)
}

// RevokeLink ends a link for good. The row stays, so the record of who
// opened the page to whom survives the link.
func (s *Service) RevokeLink(ctx context.Context, actor perm.Actor, pageID, linkID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := linkable(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if p.refusal != nil && *p.refusal == RefusalCannotManage {
			return &RefusedError{Reason: RefusalCannotManage}
		}
		var label string
		err = tx.QueryRow(ctx, `
			UPDATE page_link SET revoked_at = now(), revoked_by = $3
			WHERE id = $1 AND page_id = $2 AND revoked_at IS NULL
			RETURNING label`, linkID, pageID, actor.UserID).Scan(&label)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return fmt.Errorf("revoke the link: %w", err)
		}
		return perm.Record(ctx, tx, actor, audit.ActionPageLinkRevoked, "page", &pageID,
			map[string]any{"link": linkID, "title": p.title, "label": label})
	})
}

// LinkSettings is whether the organization allows public links. For administrators.
func (s *Service) LinkSettings(ctx context.Context, actor perm.Actor) (*LinkSettings, error) {
	var out LinkSettings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT public_links FROM org WHERE id = current_org_id()`).Scan(&out.Enabled)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SetLinkSettings allows public links in the organization or stops every one
// of them; stopped links work again when they are allowed again.
func (s *Service) SetLinkSettings(ctx context.Context, actor perm.Actor, in LinkSettings) (*LinkSettings, db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var before bool
		if err := tx.QueryRow(ctx, `SELECT public_links FROM org WHERE id = current_org_id()`).Scan(&before); err != nil {
			return fmt.Errorf("read whether the organization allows public links: %w", err)
		}
		if before == in.Enabled {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE org SET public_links = $1 WHERE id = current_org_id()`, in.Enabled); err != nil {
			return fmt.Errorf("allow public links: %w", err)
		}
		return perm.Record(ctx, tx, actor, audit.ActionOrgPublicLinksSet, "org", nil, map[string]any{"enabled": in.Enabled})
	})
	if err != nil {
		return nil, lsn, err
	}
	return &in, lsn, nil
}

// LinkSite finds the organization a link's address names, while it allows
// links; otherwise the link is gone, as one nobody holds.
func (s *Service) LinkSite(ctx context.Context, slug string) (*Site, error) {
	var out Site
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT o.id, o.slug::text, o.name, o.anonymous_indexable, COALESCE(b.footer_en, ''), COALESCE(b.footer_de, ''),
			       CASE WHEN b.logo_type IS NOT NULL THEN b.logo_version END
			FROM org o LEFT JOIN org_brand b ON b.org_id = o.id
			WHERE o.slug = $1 AND o.public_links AND o.archived_at IS NULL`, strings.ToLower(strings.TrimSpace(slug))).
			Scan(&out.ID, &out.Slug, &out.Name, &out.Indexable, &out.Footer.En, &out.Footer.De, &out.LogoVersion)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkGone
	}
	if err != nil {
		return nil, fmt.Errorf("find the organization: %w", err)
	}
	return &out, nil
}

// ReadingLink binds ctx to the site as an anonymous reader holding the token,
// whom the database lets read the one page a live link with it opens.
func ReadingLink(ctx context.Context, site *Site, token string) context.Context {
	return db.WithLink(tenant.WithOrg(ctx, tenant.Org{ID: site.ID, Slug: site.Slug}), auth.HashToken(token))
}

// LinkedPage is the page the reader's link opens, with the people it names
// left unnamed; a link that opens nothing is gone.
func (s *Service) LinkedPage(ctx context.Context) (*LinkedPage, error) {
	if !db.LinkFrom(ctx) {
		return nil, errors.New("a page is read through a link by a reader holding one")
	}
	var out LinkedPage
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			body    []byte
			coverID *uuid.UUID
			focusX  int
			focusY  int
			width   string
		)
		// The page is asked for by the link alone, and its rule asked again
		// though the policy asks it too, so the answer never rests on one.
		err := tx.QueryRow(ctx, `
			SELECT p.id, p.title, p.kind, p.body, p.version, COALESCE(p.published_at, p.updated_at),
			       p.icon, p.width, p.cover_attachment_id, p.cover_focus_x, p.cover_focus_y
			FROM page p
			WHERE p.id = perm_link_page() AND `+publicPage).
			Scan(&out.ID, &out.Title, &out.Kind, &body, &out.Version, &out.Updated,
				&out.Appearance.Icon, &width, &coverID, &focusX, &focusY)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkGone
		}
		if err != nil {
			return fmt.Errorf("read the page: %w", err)
		}
		out.Appearance.Width = page.Width(width)
		if coverID != nil {
			out.Appearance.Cover = &page.Cover{AttachmentID: *coverID, FocusX: focusX, FocusY: focusY}
		}
		out.Body, err = anonymousBody(body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
