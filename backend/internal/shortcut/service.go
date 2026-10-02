package shortcut

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Service keeps the shortcuts of spaces.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// row is a shortcut with the rank it is ordered by.
type row struct {
	Shortcut
	rank string
}

// list reads a space's shortcuts in order, leaving out each one whose page
// the actor may not view or that is in the trash.
func list(ctx context.Context, tx db.DBTX, actor perm.Actor, spaceID uuid.UUID) ([]row, error) {
	rows, err := tx.Query(ctx, `
		SELECT sc.id, sc.label, sc.url, sc.rank, p.id, p.title, ps.key, ps.home_page_id = p.id, p.archived_at IS NOT NULL
		FROM space_shortcut sc
		LEFT JOIN page p ON p.id = sc.page_id
		LEFT JOIN space ps ON ps.id = p.space_id
		WHERE sc.space_id = $2
		  AND (sc.page_id IS NULL OR (p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`))
		ORDER BY sc.rank, sc.id`, actor.UserID, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var (
			r        row
			pageID   *uuid.UUID
			title    *string
			key      *string
			home     *bool
			archived *bool
		)
		if err := rows.Scan(&r.ID, &r.Label, &r.URL, &r.rank, &pageID, &title, &key, &home, &archived); err != nil {
			return nil, err
		}
		r.Kind = KindLink
		if pageID != nil && title != nil && key != nil {
			r.Kind = KindPage
			r.Page = &ShortcutPage{ID: *pageID, Title: *title, SpaceKey: *key, Home: home != nil && *home, Archived: archived != nil && *archived}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func shortcuts(rows []row) []Shortcut {
	out := make([]Shortcut, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Shortcut)
	}
	return out
}

// List is a space's shortcuts in order, as the caller may see them.
func (s *Service) List(ctx context.Context, actor perm.Actor, key string) ([]Shortcut, error) {
	var out []Shortcut
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		rows, err := list(ctx, tx, actor, sp.ID)
		out = shortcuts(rows)
		return err
	})
	return out, err
}

// administered loads a space the actor may change the shortcuts of.
func administered(ctx context.Context, tx db.DBTX, actor perm.Actor, key string) (*space.Space, error) {
	sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
	if err != nil {
		return nil, err
	}
	if err := perm.Check(ctx, tx, actor, perm.ManageShortcuts, sp.ID); err != nil {
		return nil, err
	}
	return sp, nil
}

// Create pins a shortcut last among the space's.
func (s *Service) Create(ctx context.Context, actor perm.Actor, key string, in ShortcutInput) (*Shortcut, db.LSN, error) {
	link, label, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out *Shortcut
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := administered(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if in.PageID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT space_shortcut_page_ok($1)`, *in.PageID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return &FieldError{Field: "pageId", Message: "That page was not found, or you may not open it. Choose another page."}
			}
		}
		current, err := list(ctx, tx, actor, sp.ID)
		if err != nil {
			return err
		}
		if len(current) >= MaxPerSpace {
			return &FullError{}
		}
		last := ""
		if len(current) > 0 {
			last = current[len(current)-1].rank
		}
		r, err := rank.Between(last, "")
		if err != nil {
			return fmt.Errorf("rank the shortcut: %w", err)
		}
		// The id is made here: a row the statement writes is not yet one its
		// own snapshot lets the policies see.
		id := uuid.Must(uuid.NewV7())
		var url *string
		if link != "" {
			url = &link
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO space_shortcut (id, org_id, space_id, page_id, url, label, rank)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6)`, id, sp.ID, in.PageID, url, label, r); err != nil {
			if isLimit(err) {
				return &FullError{}
			}
			return fmt.Errorf("save the shortcut: %w", err)
		}
		if err := record(ctx, tx, actor, audit.ActionShortcutAdded, sp, map[string]any{"shortcutId": id, "label": label, "url": url, "pageId": in.PageID}); err != nil {
			return err
		}
		rows, err := list(ctx, tx, actor, sp.ID)
		if err != nil {
			return err
		}
		for _, each := range rows {
			if each.ID == id {
				out = &each.Shortcut
			}
		}
		if out == nil {
			return ErrNotFound
		}
		return nil
	})
	return out, lsn, err
}

// Move places a shortcut after another of the space, or first, and answers
// the space's shortcuts in their new order.
func (s *Service) Move(ctx context.Context, actor perm.Actor, key string, id uuid.UUID, in ShortcutMove) ([]Shortcut, db.LSN, error) {
	if in.After != nil && *in.After == id {
		return nil, 0, &FieldError{Field: "after", Message: "A shortcut cannot go after itself. Choose another one to put it after."}
	}
	var out []Shortcut
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := administered(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		// Two moves at once would each rank against the order before the other.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('space_shortcut:' || $1::text, 0))`, sp.ID); err != nil {
			return err
		}
		current, err := list(ctx, tx, actor, sp.ID)
		if err != nil {
			return err
		}
		moving := -1
		for i, each := range current {
			if each.ID == id {
				moving = i
			}
		}
		if moving < 0 {
			return ErrNotFound
		}
		rest := append(append([]row{}, current[:moving]...), current[moving+1:]...)
		at := 0
		if in.After != nil {
			at = -1
			for i, each := range rest {
				if each.ID == *in.After {
					at = i + 1
				}
			}
			if at < 0 {
				return &FieldError{Field: "after", Message: "That shortcut is no longer in this space. Reload the list and try again."}
			}
		}
		ordered := append(append(append([]row{}, rest[:at]...), current[moving]), rest[at:]...)
		if err := place(ctx, tx, ordered, at); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionShortcutMoved, sp, map[string]any{"shortcutId": id, "after": in.After, "position": at + 1}); err != nil {
			return err
		}
		rows, err := list(ctx, tx, actor, sp.ID)
		out = shortcuts(rows)
		return err
	})
	return out, lsn, err
}

// place ranks the shortcut at index at between its neighbours, writing that
// one row, or numbers them all again when its neighbours share a rank.
func place(ctx context.Context, tx db.DBTX, ordered []row, at int) error {
	before, after := "", ""
	if at > 0 {
		before = ordered[at-1].rank
	}
	if at+1 < len(ordered) {
		after = ordered[at+1].rank
	}
	if r, err := rank.Between(before, after); err == nil {
		_, err := tx.Exec(ctx, `UPDATE space_shortcut SET rank = $2 WHERE id = $1`, ordered[at].ID, r)
		return err
	}
	ranks, err := rank.Sequence(len(ordered))
	if err != nil {
		return err
	}
	for i, each := range ordered {
		if _, err := tx.Exec(ctx, `UPDATE space_shortcut SET rank = $2 WHERE id = $1`, each.ID, ranks[i]); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a shortcut from the space.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, key string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := administered(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		var (
			label  string
			url    *string
			pageID *uuid.UUID
		)
		err = tx.QueryRow(ctx, `
			DELETE FROM space_shortcut WHERE id = $1 AND space_id = $2
			RETURNING label, url, page_id`, id, sp.ID).Scan(&label, &url, &pageID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionShortcutRemoved, sp, map[string]any{"shortcutId": id, "label": label, "url": url, "pageId": pageID})
	})
}

// record notes an administrator's change of a space's shortcuts in the
// organization's audit log, in the transaction that made it.
func record(ctx context.Context, tx db.DBTX, actor perm.Actor, action string, sp *space.Space, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	data["key"] = sp.Key
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "space", TargetID: &sp.ID, Actor: actor.UserID, Data: data})
}

func isLimit(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "space_shortcut_limit"
}
