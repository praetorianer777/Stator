package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// DefaultVersionLimit and MaxVersionLimit bound a page of history.
	DefaultVersionLimit = 20
	MaxVersionLimit     = 100
	// DraftSide is how a comparison names the caller's draft.
	DraftSide = "draft"
)

// release is what a publish writes: the content, and what the history says
// about it.
type release struct {
	title        string
	body         json.RawMessage
	comment      string
	notify       bool
	restoredFrom *int
}

// publish writes the page's next version and copies it onto the page. The
// caller has loaded the page locked and checked the actor may edit it.
func publish(ctx context.Context, tx db.DBTX, actor perm.Actor, p *Page, r release) (*VersionEntry, error) {
	number := p.Version + 1
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_version (org_id, page_id, number, title, body, comment, notify_watchers, restored_from, created_by)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, number, r.title, r.body, r.comment, r.notify, r.restoredFrom, actor.UserID); err != nil {
		return nil, fmt.Errorf("write version %d: %w", number, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE page SET title = $2, body = $3, version = $4, updated_by = $5 WHERE id = $1`,
		p.ID, r.title, r.body, number, actor.UserID); err != nil {
		return nil, fmt.Errorf("publish the page: %w", err)
	}
	return versionEntry(ctx, tx, p.ID, number)
}

const selectVersions = `
SELECT v.number, v.title, v.comment, u.id, COALESCE(u.name, ''), v.created_at, v.restored_from
FROM page_version v
LEFT JOIN app_user u ON u.id = v.created_by`

func versionEntry(ctx context.Context, tx db.DBTX, pageID uuid.UUID, number int) (*VersionEntry, error) {
	rows, err := tx.Query(ctx, selectVersions+` WHERE v.page_id = $1 AND v.number = $2`, pageID, number)
	if err != nil {
		return nil, err
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[VersionEntry])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	return &v, err
}

func cleanComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxCommentLength {
		return "", &FieldError{Field: "comment", Message: fmt.Sprintf("Keep the comment to %d characters.", MaxCommentLength)}
	}
	return comment, nil
}

// draftOf is the actor's draft of a page, nil when they have none; lock
// takes it for writing.
func draftOf(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID, lock bool) (*Draft, error) {
	sql := `SELECT page_id, title, body, base_version, updated_at FROM page_draft WHERE page_id = $1 AND user_id = $2`
	if lock {
		sql += ` FOR UPDATE`
	}
	var d Draft
	err := tx.QueryRow(ctx, sql, pageID, actor.UserID).Scan(&d.PageID, &d.Title, &d.Body, &d.BaseVersion, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDraft is the caller's draft of a page, nil when they have none.
func (s *Service) GetDraft(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Draft, error) {
	var out *Draft
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := load(ctx, tx, actor, id, false); err != nil {
			return err
		}
		var err error
		out, err = draftOf(ctx, tx, actor, id, false)
		return err
	})
	return out, err
}

// SaveDraft replaces the caller's draft of a page with the one sent.
func (s *Service) SaveDraft(ctx context.Context, actor perm.Actor, id uuid.UUID, in DraftInput) (*Draft, db.LSN, error) {
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, 0, err
	}
	if in.Body == nil {
		return nil, 0, &FieldError{Field: "body", Message: "A draft needs its whole body. Send the document as the editor holds it."}
	}
	if err := document.Validate(in.Body); err != nil {
		return nil, 0, err
	}
	var out *Draft
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if in.BaseVersion < 0 || in.BaseVersion > p.Version {
			return &FieldError{Field: "baseVersion", Message: fmt.Sprintf("The page is at version %d, so a draft cannot start from version %d. Reload the page and edit again.", p.Version, in.BaseVersion)}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, page_id, user_id) DO UPDATE
			SET title = EXCLUDED.title, body = EXCLUDED.body, base_version = EXCLUDED.base_version`,
			id, actor.UserID, title, in.Body, in.BaseVersion); err != nil {
			return fmt.Errorf("save the draft: %w", err)
		}
		out, err = draftOf(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}

// DiscardDraft throws the caller's draft of a page away, if there is one.
func (s *Service) DiscardDraft(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := load(ctx, tx, actor, id, false); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM page_draft WHERE page_id = $1 AND user_id = $2`, id, actor.UserID)
		return err
	})
}

// Publish makes the caller's draft the page's next version, or, for a page
// never published, the content it was made with when there is no draft.
func (s *Service) Publish(ctx context.Context, actor perm.Actor, id uuid.UUID, in PublishInput) (*Page, *VersionEntry, db.LSN, error) {
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, nil, 0, err
	}
	var (
		out     *Page
		version *VersionEntry
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		draft, err := draftOf(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		r := release{title: p.Title, body: p.Body, comment: comment, notify: in.NotifyWatchers}
		switch {
		case draft != nil && draft.BaseVersion != p.Version:
			return ErrPublishConflict
		case draft != nil:
			r.title, r.body = draft.Title, draft.Body
		case !p.Unpublished:
			return ErrNoDraft
		}
		if version, err = publish(ctx, tx, actor, p, r); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM page_draft WHERE page_id = $1 AND user_id = $2`, id, actor.UserID); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, version, lsn, err
}

// Versions lists a page's published versions, the latest first, and how
// many there are in all.
func (s *Service) Versions(ctx context.Context, actor perm.Actor, id uuid.UUID, limit, offset int) ([]VersionEntry, int, error) {
	out := []VersionEntry{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		total = p.Version
		rows, err := tx.Query(ctx, selectVersions+` WHERE v.page_id = $1 ORDER BY v.number DESC LIMIT $2 OFFSET $3`, id, limit, offset)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[VersionEntry]); out == nil {
			out = []VersionEntry{}
		}
		return err
	})
	return out, total, err
}

// GetVersion is one published version of a page with its body.
func (s *Service) GetVersion(ctx context.Context, actor perm.Actor, id uuid.UUID, number int) (*Version, error) {
	var out *Version
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := load(ctx, tx, actor, id, false); err != nil {
			return err
		}
		var err error
		out, err = version(ctx, tx, id, number)
		return err
	})
	return out, err
}

func version(ctx context.Context, tx db.DBTX, pageID uuid.UUID, number int) (*Version, error) {
	entry, err := versionEntry(ctx, tx, pageID, number)
	if err != nil {
		return nil, err
	}
	out := &Version{VersionEntry: *entry}
	err = tx.QueryRow(ctx, `SELECT body FROM page_version WHERE page_id = $1 AND number = $2`, pageID, number).Scan(&out.Body)
	return out, err
}

// RestoreVersion publishes an older version's title and body again as the next
// version. Nobody's draft is touched.
func (s *Service) RestoreVersion(ctx context.Context, actor perm.Actor, id uuid.UUID, number int, in RestoreInput) (*Page, *VersionEntry, db.LSN, error) {
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, nil, 0, err
	}
	if comment == "" {
		comment = fmt.Sprintf("Restored version %d", number)
	}
	var (
		out   *Page
		entry *VersionEntry
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		old, err := version(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if in.BaseVersion != p.Version {
			return ErrRestoreStale
		}
		if number == p.Version {
			return ErrRestoreLatest
		}
		if entry, err = publish(ctx, tx, actor, p, release{title: old.Title, body: old.Body, comment: comment, restoredFrom: &number}); err != nil {
			return err
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, entry, lsn, err
}

// SideRef names one side of a comparison: a version, 0 for the empty page,
// or the caller's draft.
type SideRef struct {
	Number int
	Draft  bool
}

// ParseSide reads a side as a query names it; empty is nil, left to default.
func ParseSide(raw string) (*SideRef, error) {
	if raw == "" {
		return nil, nil
	}
	if raw == DraftSide {
		return &SideRef{Draft: true}, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil, errors.New("name a version number, 0 for the empty page, or draft")
	}
	return &SideRef{Number: n}, nil
}

// Compare shows what changed between two sides of a page's history. To is
// the latest version unless named, and from the one before it, or the
// version the draft began from when to is the draft.
func (s *Service) Compare(ctx context.Context, actor perm.Actor, id uuid.UUID, from, to *SideRef) (*Comparison, error) {
	var out *Comparison
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		draft, err := draftOf(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		if to == nil {
			if p.Version == 0 {
				return ErrVersionNotFound
			}
			to = &SideRef{Number: p.Version}
		}
		if from == nil {
			switch {
			case to.Draft && draft != nil:
				from = &SideRef{Number: draft.BaseVersion}
			case to.Draft:
				return ErrDraftNotFound
			default:
				from = &SideRef{Number: max(to.Number-1, 0)}
			}
		}
		fromSide, fromBody, err := side(ctx, tx, actor, p, draft, *from)
		if err != nil {
			return err
		}
		toSide, toBody, err := side(ctx, tx, actor, p, draft, *to)
		if err != nil {
			return err
		}
		blocks, err := Diff(fromBody, toBody)
		if err != nil {
			return err
		}
		out = &Comparison{From: *fromSide, To: *toSide, Blocks: blocks}
		return nil
	})
	return out, err
}

func side(ctx context.Context, tx db.DBTX, actor perm.Actor, p *Page, draft *Draft, ref SideRef) (*CompareSide, json.RawMessage, error) {
	switch {
	case ref.Draft && draft == nil:
		return nil, nil, ErrDraftNotFound
	case ref.Draft:
		var name string
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT name FROM app_user WHERE id = $1), '')`, actor.UserID).Scan(&name); err != nil {
			return nil, nil, err
		}
		return &CompareSide{Draft: true, Title: draft.Title, AuthorName: name, CreatedAt: draft.UpdatedAt}, draft.Body, nil
	case ref.Number == 0:
		return &CompareSide{}, nil, nil
	}
	v, err := version(ctx, tx, p.ID, ref.Number)
	if err != nil {
		return nil, nil, err
	}
	return &CompareSide{Number: v.Number, Title: v.Title, AuthorName: v.AuthorName, CreatedAt: v.CreatedAt}, v.Body, nil
}
