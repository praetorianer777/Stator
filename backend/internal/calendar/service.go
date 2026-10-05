package calendar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Service keeps the calendars of spaces and their events.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectCalendars = `
SELECT c.id, s.key, s.name, c.name, c.created_at, c.space_id
FROM calendar c JOIN space s ON s.id = c.space_id`

// load reads one calendar the actor may see, with whether they may change it.
func load(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Calendar, *space.Space, error) {
	var (
		c       Calendar
		spaceID uuid.UUID
	)
	err := tx.QueryRow(ctx, selectCalendars+` WHERE c.id = $1`, id).Scan(&c.ID, &c.SpaceKey, &c.SpaceName, &c.Name, &c.CreatedAt, &spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read the calendar: %w", err)
	}
	sp, err := space.Load(ctx, tx, actor, space.ByID, spaceID)
	if errors.Is(err, space.ErrNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	c.CanEdit = sp.Can.EditPages
	return &c, sp, nil
}

// writable refuses an actor who may not change the calendars of the space,
// naming the archive when that is the reason.
func writable(sp *space.Space) error {
	if sp.Can.EditPages {
		return nil
	}
	archived := perm.NotArchived
	if sp.ArchivedAt != nil {
		archived = perm.ArchivedSpace
	}
	return perm.Refuse(perm.EditCalendars, archived)
}

// List is a space's calendars by name.
func (s *Service) List(ctx context.Context, actor perm.Actor, key string) ([]Calendar, error) {
	out := []Calendar{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectCalendars+` WHERE c.space_id = $1 ORDER BY lower(c.name), c.id`, sp.ID)
		if err != nil {
			return fmt.Errorf("list the calendars: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c Calendar
			var spaceID uuid.UUID
			if err := rows.Scan(&c.ID, &c.SpaceKey, &c.SpaceName, &c.Name, &c.CreatedAt, &spaceID); err != nil {
				return err
			}
			c.CanEdit = sp.Can.EditPages
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Create adds a calendar to a space.
func (s *Service) Create(ctx context.Context, actor perm.Actor, key string, in CalendarInput) (*Calendar, db.LSN, error) {
	name, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out *Calendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		// The id is made here: a row the statement writes is not yet one its
		// own snapshot lets the policies see.
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO calendar (org_id, id, space_id, name) VALUES (current_org_id(), $1, $2, $3)`, id, sp.ID, name); err != nil {
			return refusal(err, "save the calendar")
		}
		out, _, err = load(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// Rename gives a calendar another name.
func (s *Service) Rename(ctx context.Context, actor perm.Actor, id uuid.UUID, in CalendarInput) (*Calendar, db.LSN, error) {
	name, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out *Calendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE calendar SET name = $2 WHERE id = $1`, id, name); err != nil {
			return refusal(err, "rename the calendar")
		}
		out, _, err = load(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// Delete removes a calendar with every event in it.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM calendar WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("remove the calendar: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

const selectEvents = `
SELECT e.id, e.calendar_id, e.title, e.kind, e.all_day, e.starts_at, e.ends_at, COALESCE(u.name, ''), e.updated_at
FROM calendar_event e LEFT JOIN app_user u ON u.id = e.created_by`

func scanEvent(row pgx.Row) (CalendarEvent, error) {
	var (
		e    CalendarEvent
		kind string
	)
	err := row.Scan(&e.ID, &e.CalendarID, &e.Title, &kind, &e.AllDay, &e.Start, &e.End, &e.CreatedByName, &e.UpdatedAt)
	e.Kind, e.Start, e.End = Kind(kind), e.Start.UTC(), e.End.UTC()
	return e, err
}

// Events is a calendar and the events in the span [from, to), as Covers
// says, by when they start, the day's whole ones first.
func (s *Service) Events(ctx context.Context, actor perm.Actor, id uuid.UUID, from, to time.Time) (*CalendarEvents, error) {
	var out *CalendarEvents
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, _, err := load(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out = &CalendarEvents{Calendar: *c, Events: []CalendarEvent{}}
		rows, err := tx.Query(ctx, selectEvents+`
			WHERE e.calendar_id = $1 AND e.starts_at < $3
			  AND (e.ends_at + CASE WHEN e.all_day THEN interval '1 day' ELSE interval '0' END > $2 OR e.starts_at >= $2)
			ORDER BY e.starts_at, e.all_day DESC, lower(e.title), e.id
			LIMIT $4`, id, from, to, MaxRangedEvents+1)
		if err != nil {
			return fmt.Errorf("list the events: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEvent(rows)
			if err != nil {
				return err
			}
			out.Events = append(out.Events, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out.Events) > MaxRangedEvents {
		out.Events, out.Truncated = out.Events[:MaxRangedEvents], true
	}
	return out, nil
}

// CreateEvent adds an event to a calendar.
func (s *Service) CreateEvent(ctx context.Context, actor perm.Actor, calendarID uuid.UUID, in CalendarEventInput) (*CalendarEvent, db.LSN, error) {
	clean, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out CalendarEvent
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, calendarID)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO calendar_event (org_id, id, space_id, calendar_id, title, kind, all_day, starts_at, ends_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)`,
			id, sp.ID, calendarID, clean.Title, string(clean.Kind), clean.AllDay, clean.Start, clean.End); err != nil {
			return refusal(err, "save the event")
		}
		out, err = scanEvent(tx.QueryRow(ctx, selectEvents+` WHERE e.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// UpdateEvent changes all of an event.
func (s *Service) UpdateEvent(ctx context.Context, actor perm.Actor, calendarID, eventID uuid.UUID, in CalendarEventInput) (*CalendarEvent, db.LSN, error) {
	clean, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out CalendarEvent
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, calendarID)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE calendar_event SET title = $3, kind = $4, all_day = $5, starts_at = $6, ends_at = $7
			WHERE id = $1 AND calendar_id = $2`,
			eventID, calendarID, clean.Title, string(clean.Kind), clean.AllDay, clean.Start, clean.End)
		if err != nil {
			return refusal(err, "change the event")
		}
		if tag.RowsAffected() == 0 {
			return ErrEventNotFound
		}
		out, err = scanEvent(tx.QueryRow(ctx, selectEvents+` WHERE e.id = $1`, eventID))
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// DeleteEvent removes an event from its calendar.
func (s *Service) DeleteEvent(ctx context.Context, actor perm.Actor, calendarID, eventID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, calendarID)
		if err != nil {
			return err
		}
		if err := writable(sp); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM calendar_event WHERE id = $1 AND calendar_id = $2`, eventID, calendarID)
		if err != nil {
			return fmt.Errorf("remove the event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrEventNotFound
		}
		return nil
	})
}

// refusal reads what the database refused as the service would have said it.
func refusal(err error, doing string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "calendar_limit":
			return &FullError{}
		case "calendar_name_idx":
			return &FieldError{Field: "name", Message: "This space already has a calendar of that name. Choose another name."}
		}
	}
	return fmt.Errorf("%s: %w", doing, err)
}
