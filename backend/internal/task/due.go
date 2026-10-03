package task

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// DefaultDueInterval is how often the worker looks for tasks whose day came.
	DefaultDueInterval = 10 * time.Minute
	// dueBatch is how many tasks one look takes before the next.
	dueBatch = 500
)

// DueWatch is the worker's reminder: a task's day comes while nobody makes a
// request, so it looks for days that came and has the assignee told.
type DueWatch struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
}

func NewDueWatch(cluster *db.Cluster, log *slog.Logger, interval time.Duration) *DueWatch {
	if interval <= 0 {
		interval = DefaultDueInterval
	}
	return &DueWatch{db: cluster, log: log, interval: interval}
}

// Run looks every interval until the context ends.
func (w *DueWatch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			n, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("the task reminder failed", "error", err)
				break
			}
			if n > 0 {
				w.log.Info("tasks came due", "count", n)
			}
			if n < dueBatch {
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

// Once notes up to one batch of tasks whose day came, across every
// organization, each with an event for its assignee, and says how many.
func (w *DueWatch) Once(ctx context.Context) (int, error) {
	type due struct{ orgID, id uuid.UUID }
	var found []due
	// Finding them crosses tenants, which is what the admin role is for.
	err := w.db.ReadAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT org_id, id FROM page_task
			WHERE NOT done AND assignee_id IS NOT NULL AND due_noticed_at IS NULL AND due_on <= task_today()
			ORDER BY due_on LIMIT $1`, dueBatch)
		if err != nil {
			return err
		}
		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
			var d due
			return d, row.Scan(&d.orgID, &d.id)
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	noted := 0
	for _, d := range found {
		org := tenant.WithOrg(db.PinPrimary(ctx), tenant.Org{ID: d.orgID})
		happened := false
		_, err := w.db.WriteAdmin(org, func(ctx context.Context, tx db.DBTX) error {
			var e events.TaskDue
			// A second worker, or a task done or moved to another day meanwhile, finds no row.
			err := tx.QueryRow(ctx, `
				UPDATE page_task SET due_noticed_at = now()
				WHERE org_id = current_org_id() AND id = $1 AND NOT done AND assignee_id IS NOT NULL
				  AND due_noticed_at IS NULL AND due_on <= task_today()
				RETURNING page_id, task_id, assignee_id, to_char(due_on, 'YYYY-MM-DD')`, d.id).Scan(&e.PageID, &e.TaskID, &e.AssigneeID, &e.DueOn)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			happened = true
			return events.Emit(ctx, tx, events.TopicTaskDue, e)
		})
		if err != nil {
			w.log.Warn("a task that came due could not be noted", "task", d.id, "error", err)
			continue
		}
		if happened {
			noted++
		}
	}
	return noted, nil
}
