package page

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// MaxScheduleAhead bounds how far ahead a publish may be set.
const MaxScheduleAhead = 366 * 24 * time.Hour

var (
	// ErrNoSchedule answers cancelling a publish nobody scheduled.
	ErrNoSchedule = errors.New("nobody scheduled this page to publish")
)

// ScheduleFailure is why the worker did not publish when the time came.
type ScheduleFailure string

const (
	// ScheduleGone is a page deleted, or no longer seen by its author.
	ScheduleGone ScheduleFailure = "gone"
	// ScheduleForbidden is an author who may no longer edit the page.
	ScheduleForbidden ScheduleFailure = "forbidden"
	// ScheduleArchived is a page, or its space, archived meanwhile.
	ScheduleArchived ScheduleFailure = "archived"
	// ScheduleConflict is somebody else's publish after the draft began.
	ScheduleConflict ScheduleFailure = "conflict"
)

// ScheduleFailures is every failure, for the API's description.
var ScheduleFailures = []ScheduleFailure{ScheduleGone, ScheduleForbidden, ScheduleArchived, ScheduleConflict}

// Schedule is a publish of somebody's draft set for a time, seen by its
// author and the page's editors.
type Schedule struct {
	// PublishAt is when the worker publishes the draft, as an instant.
	PublishAt  time.Time `json:"publishAt"`
	AuthorID   uuid.UUID `json:"authorId"`
	AuthorName string    `json:"authorName"`
	// Mine says the caller scheduled it, so it is their draft that goes out.
	Mine           bool      `json:"mine"`
	Comment        string    `json:"comment"`
	NotifyWatchers bool      `json:"notifyWatchers"`
	CreatedAt      time.Time `json:"createdAt"`
	// Failure says why the publish did not go out at its time; null while it waits.
	Failure  *ScheduleFailure `json:"failure"`
	FailedAt *time.Time       `json:"failedAt"`
}

// ScheduleInput sets when the caller's draft is published, and how.
type ScheduleInput struct {
	// PublishAt is an instant in the future, with its offset.
	PublishAt time.Time `json:"publishAt"`
	// Comment says what changed, shown in the history; optional.
	Comment string `json:"comment,omitempty"`
	// NotifyWatchers tells the page's watchers about the new version.
	NotifyWatchers bool `json:"notifyWatchers,omitempty"`
}

// ScheduleTakenError refuses a schedule while somebody else's waits.
type ScheduleTakenError struct {
	Name string
}

func (e *ScheduleTakenError) Error() string {
	name := e.Name
	if name == "" {
		name = "Somebody"
	}
	return fmt.Sprintf("%s already scheduled this page to publish. Cancel that schedule first, or publish your draft now.", name)
}

// scheduleOf is the page's schedule as the actor may see it, nil when there
// is none or it is not theirs to see.
func scheduleOf(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) (*Schedule, error) {
	var s Schedule
	err := tx.QueryRow(ctx, `
		SELECT sc.publish_at, sc.user_id, COALESCE(u.name, ''), sc.user_id = $2, sc.comment, sc.notify_watchers, sc.created_at, sc.failure, sc.failed_at
		FROM page_schedule sc LEFT JOIN app_user u ON u.id = sc.user_id
		WHERE sc.page_id = $1`, pageID, actor.UserID).
		Scan(&s.PublishAt, &s.AuthorID, &s.AuthorName, &s.Mine, &s.Comment, &s.NotifyWatchers, &s.CreatedAt, &s.Failure, &s.FailedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SchedulePublish sets the caller's draft to be published at a time, in
// their name, replacing their own schedule or a failed one of anybody's.
// A page never published and never drafted is drafted as it stands.
func (s *Service) SchedulePublish(ctx context.Context, actor perm.Actor, id uuid.UUID, in ScheduleInput) (*Schedule, db.LSN, error) {
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	switch {
	case in.PublishAt.IsZero():
		return nil, 0, &FieldError{Field: "publishAt", Message: "Choose when to publish."}
	case !in.PublishAt.After(now):
		return nil, 0, &FieldError{Field: "publishAt", Message: "That time has passed. Choose a time ahead, or publish now."}
	case in.PublishAt.After(now.Add(MaxScheduleAhead)):
		return nil, 0, &FieldError{Field: "publishAt", Message: "Choose a time within a year from now."}
	}
	var out *Schedule
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
		if p.Mode == ModeLive {
			return ErrLivePage
		}
		draft, err := draftOf(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		switch {
		case draft != nil && draft.BaseVersion != p.Version:
			return ErrPublishConflict
		case draft == nil && !p.Unpublished:
			return ErrNoDraft
		case draft == nil:
			// The schedule hangs off a draft, so the first publish of a page
			// nobody edited yet publishes a draft of it as it stands.
			if _, err := tx.Exec(ctx, `
				INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
				VALUES (current_org_id(), $1, $2, $3, $4, 0)`, id, actor.UserID, p.Title, p.Body); err != nil {
				return fmt.Errorf("draft the page to schedule it: %w", err)
			}
		}
		current, err := scheduleOf(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if current != nil && !current.Mine {
			if current.Failure == nil {
				return &ScheduleTakenError{Name: current.AuthorName}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM page_schedule WHERE page_id = $1`, id); err != nil {
				return fmt.Errorf("clear a failed schedule: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_schedule (org_id, page_id, user_id, publish_at, comment, notify_watchers)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, page_id) DO UPDATE
			SET publish_at = EXCLUDED.publish_at, comment = EXCLUDED.comment, notify_watchers = EXCLUDED.notify_watchers,
			    failed_at = NULL, failure = NULL`,
			id, actor.UserID, in.PublishAt, comment, in.NotifyWatchers); err != nil {
			return fmt.Errorf("schedule the publish: %w", err)
		}
		out, err = scheduleOf(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// CancelSchedule calls off the page's scheduled publish, the caller's or,
// for an editor of the page, anybody's. The draft stays.
func (s *Service) CancelSchedule(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		current, err := scheduleOf(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNoSchedule
		}
		if !current.Mine {
			if err := p.must(perm.EditPages); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM page_schedule WHERE page_id = $1`, id)
		return err
	})
}
