package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

// LiveVersionSpan is how long a live page's version takes further saves
// after the one that began it; live_version_span() in the database.
const LiveVersionSpan = 10 * time.Minute

// ModeInput chooses how a page is edited.
type ModeInput struct {
	Mode Mode `json:"mode"`
	// DiscardDrafts confirms that making the page live throws away the
	// drafts nobody published, which is refused with drafts_pending otherwise.
	DiscardDrafts bool `json:"discardDrafts,omitempty"`
}

// ModeChange is how a page is edited now, and whose drafts went with the change.
type ModeChange struct {
	Mode Mode `json:"mode"`
	// DiscardedDrafts names the people whose unpublished drafts were thrown away.
	DiscardedDrafts []string `json:"discardedDrafts"`
}

// DraftsPendingError refuses making a page live while people hold drafts of
// it that were never published, naming them.
type DraftsPendingError struct {
	Names []string
}

func (e *DraftsPendingError) Error() string {
	return fmt.Sprintf("%s %s of this page that nobody published. Making the page live throws them away; ask for them to be published first, or make it live anyway.",
		joinNames(e.Names), map[bool]string{true: "has a draft", false: "have drafts"}[len(e.Names) == 1])
}

// joinNames reads names as a sentence lists them.
func joinNames(names []string) string {
	shown := make([]string, len(names))
	for i, n := range names {
		shown[i] = n
		if n == "" {
			shown[i] = "Somebody"
		}
	}
	if len(shown) < 2 {
		return strings.Join(shown, "")
	}
	return strings.Join(shown[:len(shown)-1], ", ") + " and " + shown[len(shown)-1]
}

// LiveInput is a live save: the whole title and body as the editor holds them.
type LiveInput struct {
	Title string          `json:"title"`
	Body  json.RawMessage `json:"body"`
	// Room is the shared draft the editor saves from, absent for one editing
	// alone; a room started afresh since refuses the save with room_gone.
	Room *uuid.UUID `json:"room,omitempty"`
}

// LiveSaved is the version a live save went into.
type LiveSaved struct {
	Version VersionEntry `json:"version"`
	// Amended says the save went into the open version rather than beginning one.
	Amended bool `json:"amended"`
}

// SetMode chooses whether a page is published from drafts or saved live. It
// answers the shared draft it threw away, whose editors load the page afresh.
func (s *Service) SetMode(ctx context.Context, actor perm.Actor, id uuid.UUID, in ModeInput) (*ModeChange, *uuid.UUID, db.LSN, error) {
	if in.Mode != ModeDraft && in.Mode != ModeLive {
		return nil, nil, 0, &FieldError{Field: "mode", Message: "Choose draft or live."}
	}
	var (
		out      *ModeChange
		replaced *uuid.UUID
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if p.Kind == KindFolder {
			return ErrFolder
		}
		out = &ModeChange{Mode: in.Mode, DiscardedDrafts: []string{}}
		if p.Mode == in.Mode {
			return nil
		}
		if in.Mode == ModeLive {
			rows, err := tx.Query(ctx, `SELECT name FROM page_pending_drafts($1)`, id)
			if err != nil {
				return fmt.Errorf("read the page's drafts: %w", err)
			}
			names, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			if len(names) > 0 && !in.DiscardDrafts {
				return &DraftsPendingError{Names: names}
			}
			out.DiscardedDrafts = append(out.DiscardedDrafts, names...)
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET mode = $2 WHERE id = $1`, id, in.Mode); err != nil {
			return fmt.Errorf("change how the page is edited: %w", err)
		}
		// Whoever has the editor open was editing the other way, so they load
		// the page afresh.
		var room uuid.UUID
		err = tx.QueryRow(ctx, `DELETE FROM page_collab WHERE page_id = $1 RETURNING id`, id).Scan(&room)
		switch {
		case err == nil:
			replaced = &room
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("close the shared draft: %w", err)
		}
		return record(ctx, tx, actor, audit.ActionPageModeChanged, id, map[string]any{
			"title": p.Title, "from": p.Mode, "mode": in.Mode, "discardedDrafts": out.DiscardedDrafts,
		})
	})
	if err != nil {
		return nil, nil, lsn, err
	}
	return out, replaced, lsn, nil
}

// SaveLive makes what a live page's editor holds the page at once, in the
// open version or a new one. It answers the shared draft it threw away: a
// save from elsewhere goes over the room, and a room the page moved on from
// without it starts afresh, refusing its save with ErrRoomGone.
func (s *Service) SaveLive(ctx context.Context, actor perm.Actor, id uuid.UUID, in LiveInput) (*LiveSaved, *uuid.UUID, db.LSN, error) {
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, nil, 0, err
	}
	if in.Body == nil {
		return nil, nil, 0, &FieldError{Field: "body", Message: "A live save needs the whole body. Send the document as the editor holds it."}
	}
	if err := document.ValidatePage(in.Body, id.String()); err != nil {
		return nil, nil, 0, err
	}
	var (
		out      *LiveSaved
		replaced *uuid.UUID
		stale    bool
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if p.Kind == KindFolder {
			return ErrFolder
		}
		if p.Mode != ModeLive {
			return ErrNotLive
		}
		var (
			room uuid.UUID
			base int
		)
		err = tx.QueryRow(ctx, `SELECT id, base_version FROM page_collab WHERE page_id = $1 FOR UPDATE`, id).Scan(&room, &base)
		held := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		switch {
		case in.Room != nil && (!held || room != *in.Room):
			return ErrRoomGone
		case in.Room != nil && base != p.Version, in.Room == nil && held:
			if _, err := tx.Exec(ctx, `DELETE FROM page_collab WHERE id = $1`, room); err != nil {
				return fmt.Errorf("start the shared draft afresh: %w", err)
			}
			replaced = &room
			// A restore, an import or a script wrote the page since the room
			// last saved; that write stands, and the room loads it.
			if in.Room != nil {
				stale = true
				return nil
			}
		}
		if out, err = saveLive(ctx, tx, actor, p, title, in.Body); err != nil {
			return err
		}
		if in.Room != nil {
			_, err = tx.Exec(ctx, `
				UPDATE page_collab SET base_version = $2,
				       published_through = COALESCE((SELECT max(seq) FROM page_collab_update WHERE room_id = $1), 0)
				WHERE id = $1`, room, out.Version.Number)
		}
		return err
	})
	if err == nil && stale {
		err = ErrRoomGone
	}
	if err != nil {
		return nil, replaced, lsn, err
	}
	return out, replaced, lsn, nil
}

// saveLive writes a live save: into the open version when there is one,
// else as the next version, which stays open for LiveVersionSpan.
func saveLive(ctx context.Context, tx db.DBTX, actor perm.Actor, p *Page, title string, body json.RawMessage) (*LiveSaved, error) {
	body, err := settleBody(ctx, tx, p, body)
	if err != nil {
		return nil, err
	}
	var open, same bool
	if err := tx.QueryRow(ctx, `
		SELECT page_version_open(current_org_id(), p.id, p.version), p.title = $2 AND p.body = $3::jsonb
		FROM page p WHERE p.id = $1`, p.ID, title, body).Scan(&open, &same); err != nil {
		return nil, fmt.Errorf("read the open version: %w", err)
	}
	if p.Version > 0 && same {
		entry, err := versionEntry(ctx, tx, p.ID, p.Version)
		if err != nil {
			return nil, err
		}
		return &LiveSaved{Version: *entry, Amended: open}, nil
	}
	if !open {
		if _, err := publish(ctx, tx, actor, p, release{title: title, body: body, live: true}); err != nil {
			return nil, err
		}
		return savedBy(ctx, tx, p.ID, p.Version+1, false)
	}

	// Read before the version takes the save, so only what this save added
	// tells anybody.
	mentioned, err := newlyMentioned(ctx, tx, p.ID, p.Version, body)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE page_version SET title = $3, body = $4 WHERE page_id = $1 AND number = $2`, p.ID, p.Version, title, body)
	if err != nil {
		return nil, fmt.Errorf("amend version %d: %w", p.Version, err)
	}
	if tag.RowsAffected() != 1 {
		return nil, fmt.Errorf("amend version %d: the version is no longer open", p.Version)
	}
	if _, err := tx.Exec(ctx, `UPDATE page SET title = $2, body = $3, updated_by = $4 WHERE id = $1`, p.ID, title, body, actor.UserID); err != nil {
		return nil, fmt.Errorf("save the page: %w", err)
	}
	if err := watch.Auto(ctx, tx, actor.UserID, p.ID); err != nil {
		return nil, fmt.Errorf("watch the page: %w", err)
	}
	if err := syncTasks(ctx, tx, p.ID, true); err != nil {
		return nil, err
	}
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&at); err != nil {
		return nil, err
	}
	if err := events.Emit(ctx, tx, events.TopicPageAmended, events.PageAmended{
		PageID: p.ID, Version: p.Version, ActorID: actor.UserID, Mentioned: mentioned, At: at,
	}); err != nil {
		return nil, err
	}
	// Armature hears of the issues a page names when they change, not at
	// every keystroke's save.
	if before, after := armature.KeysIn(p.Body), armature.KeysIn(body); !sameKeys(before, after) {
		if err := syncLinksOf(ctx, tx, actor, p.ID); err != nil {
			return nil, err
		}
	}
	return savedBy(ctx, tx, p.ID, p.Version, true)
}

// savedBy names the actor among a live version's editors and reads it back.
func savedBy(ctx context.Context, tx db.DBTX, pageID uuid.UUID, number int, amended bool) (*LiveSaved, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_version_editor (org_id, page_id, number, user_id)
		VALUES (current_org_id(), $1, $2, current_actor_id())
		ON CONFLICT DO NOTHING`, pageID, number); err != nil {
		return nil, fmt.Errorf("name the version's editor: %w", err)
	}
	entry, err := versionEntry(ctx, tx, pageID, number)
	if err != nil {
		return nil, err
	}
	return &LiveSaved{Version: *entry, Amended: amended}, nil
}

func sameKeys(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
