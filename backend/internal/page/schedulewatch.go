package page

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// DefaultScheduleInterval is how often the worker looks for scheduled
	// publishes whose time came.
	DefaultScheduleInterval = 30 * time.Second
	// scheduleBatch is how many schedules one look takes before the next.
	scheduleBatch = 100
)

// errScheduleRefused rolls back a scheduled publish the page refused, so its
// failure is recorded in a transaction of its own.
var errScheduleRefused = errors.New("the scheduled publish was refused")

// ScheduleWatch is the worker's half of scheduled publishing: a time comes
// while nobody makes a request, so it publishes each draft whose time came.
type ScheduleWatch struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
}

func NewScheduleWatch(cluster *db.Cluster, log *slog.Logger, interval time.Duration) *ScheduleWatch {
	if interval <= 0 {
		interval = DefaultScheduleInterval
	}
	return &ScheduleWatch{db: cluster, log: log, interval: interval}
}

// Run looks every interval until the context ends.
func (w *ScheduleWatch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			n, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("the scheduled publishing failed", "error", err)
				break
			}
			if n > 0 {
				w.log.Info("scheduled publishes made or refused", "count", n)
			}
			if n < scheduleBatch {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type dueSchedule struct {
	orgID, pageID, userID uuid.UUID
	role                  string
}

// Once publishes, or records as refused, up to one batch of schedules whose
// time came across every organization, and says how many. A time that passed
// while no worker ran is due all the same, and goes out once.
func (w *ScheduleWatch) Once(ctx context.Context) (int, error) {
	var found []dueSchedule
	// Finding them crosses tenants, which is what the admin role is for.
	err := w.db.ReadAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT sc.org_id, sc.page_id, sc.user_id, COALESCE(m.org_role, '')
			FROM page_schedule sc
			LEFT JOIN org_member m ON m.org_id = sc.org_id AND m.user_id = sc.user_id
			WHERE sc.failed_at IS NULL AND sc.publish_at <= now()
			ORDER BY sc.publish_at LIMIT $1`, scheduleBatch)
		if err != nil {
			return err
		}
		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueSchedule, error) {
			var d dueSchedule
			return d, row.Scan(&d.orgID, &d.pageID, &d.userID, &d.role)
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, d := range found {
		handled, err := w.publish(ctx, d)
		if err != nil {
			w.log.Warn("a scheduled publish could not be made yet", "page", d.pageID, "error", err)
			continue
		}
		if handled {
			done++
		}
	}
	return done, nil
}

// publish makes one scheduled publish as its author, held by every rule a
// publish of theirs is held by. The row's lock, taken in the publish's own
// transaction, is what lets any number of workers make it exactly once.
func (w *ScheduleWatch) publish(ctx context.Context, d dueSchedule) (bool, error) {
	org := tenant.WithOrg(db.PinPrimary(ctx), tenant.Org{ID: d.orgID})
	actor := perm.Actor{UserID: d.userID, Role: auth.OrgRole(d.role)}
	var (
		refused   ScheduleFailure
		published bool
	)
	_, err := w.db.Write(db.WithUser(org, d.userID), func(ctx context.Context, tx db.DBTX) error {
		var (
			comment string
			notify  bool
		)
		// Another worker holding it, or a schedule moved or called off since,
		// leaves nothing to do here.
		err := tx.QueryRow(ctx, `
			SELECT comment, notify_watchers FROM page_schedule
			WHERE page_id = $1 AND user_id = current_actor_id() AND failed_at IS NULL AND publish_at <= now()
			FOR UPDATE SKIP LOCKED`, d.pageID).Scan(&comment, &notify)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, _, err = publishDraft(ctx, tx, actor, d.pageID, comment, notify)
		if failure, ok := FailureOf(err); ok {
			refused = failure
			return errScheduleRefused
		}
		if err != nil {
			return err
		}
		published = true
		return nil
	})
	switch {
	case errors.Is(err, errScheduleRefused):
		return true, w.fail(org, d, refused)
	case err != nil:
		return false, err
	}
	return published, nil
}

// fail records why a schedule was refused and has its author told, once
// however many workers were refused it.
func (w *ScheduleWatch) fail(org context.Context, d dueSchedule, failure ScheduleFailure) error {
	_, err := w.db.WriteAdmin(org, func(ctx context.Context, tx db.DBTX) error {
		var at time.Time
		err := tx.QueryRow(ctx, `
			UPDATE page_schedule SET failed_at = now(), failure = $3
			WHERE org_id = current_org_id() AND page_id = $1 AND user_id = $2 AND failed_at IS NULL AND publish_at <= now()
			RETURNING publish_at`, d.pageID, d.userID, string(failure)).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return events.Emit(ctx, tx, events.TopicScheduleFailed, events.ScheduleFailed{
			PageID: d.pageID, AuthorID: d.userID, PublishAt: at, Failure: string(failure)})
	})
	return err
}

// FailureOf reads a refused publish as the reason its schedule failed; ok is
// false for an error that may pass, such as a lost connection.
func FailureOf(err error) (failure ScheduleFailure, ok bool) {
	var (
		denied *perm.DeniedError
		pgErr  *pgconn.PgError
	)
	switch {
	case err == nil:
		return "", false
	case errors.Is(err, ErrPublishConflict):
		return ScheduleConflict, true
	// The draft a schedule hangs off is gone with a live page or a folder,
	// so these are a page no longer what was scheduled.
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNoDraft), errors.Is(err, ErrLivePage), errors.Is(err, ErrFolder):
		return ScheduleGone, true
	case errors.As(err, &denied) && denied.Archived != "":
		return ScheduleArchived, true
	case errors.As(err, &denied), errors.Is(err, perm.ErrDenied):
		return ScheduleForbidden, true
	case errors.As(err, &pgErr) && pgErr.Code == pgerrInsufficientPrivilege:
		return ScheduleForbidden, true
	}
	return "", false
}

// pgerrInsufficientPrivilege is how the database refuses what a policy or a
// guard does not let the author do.
const pgerrInsufficientPrivilege = "42501"
