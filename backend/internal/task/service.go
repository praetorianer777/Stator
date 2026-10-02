package task

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// noDay is where a task without a due day sorts, after every real one; the
// open tasks' index orders by the same.
var noDay = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// Service reads people's tasks.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// selectTasks reads tasks with their page as it is now. Whether the reader
// may view the page is asked on every read, so a task on a page closed to
// them since it was assigned is not listed.
const selectTasks = `
SELECT t.id, t.task_id, p.id, p.title, s.key, s.name, t.summary, t.done, to_char(t.due_on, 'YYYY-MM-DD'),
       t.assignee_id, COALESCE(au.name, ''), COALESCE(ab.name, ''), t.assigned_at, t.done_at,
       perm_page_editable(p.id, $1::uuid)
FROM page_task t
JOIN page p ON p.org_id = t.org_id AND p.id = t.page_id
JOIN space s ON s.id = p.space_id
LEFT JOIN app_user au ON au.id = t.assignee_id
LEFT JOIN app_user ab ON ab.id = t.assigned_by
WHERE t.org_id = current_org_id() AND p.trashed_at IS NULL AND p.archived_at IS NULL AND s.archived_at IS NULL
  AND perm_page_viewable(p.id, $1::uuid)`

func scanTasks(rows pgx.Rows) ([]Task, error) {
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.row, &t.ID, &t.Page.ID, &t.Page.Title, &t.Page.SpaceKey, &t.Page.SpaceName, &t.Text, &t.Done, &t.DueOn,
			&t.AssigneeID, &t.AssigneeName, &t.AssignedByName, &t.AssignedAt, &t.DoneAt, &t.CanEdit); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Mine is a window of the tasks the caller is assigned, open or done, and the
// cursor for the window after, nil at the end.
func (s *Service) Mine(ctx context.Context, actor perm.Actor, state State, after *keyset.Cursor, limit int) ([]Task, *string, error) {
	at, id := after.Args()
	var day *string
	if at != nil {
		d := at.UTC().Format(time.DateOnly)
		day = &d
	}
	sql := selectTasks + ` AND t.assignee_id = $1 AND NOT t.done
		AND ($2::date IS NULL OR (COALESCE(t.due_on, DATE '9999-12-31'), t.id) > ($2::date, $3::uuid))
		ORDER BY COALESCE(t.due_on, DATE '9999-12-31'), t.id LIMIT $4`
	args := []any{actor.UserID, day, id, limit + 1}
	if state == StateDone {
		sql = selectTasks + ` AND t.assignee_id = $1 AND t.done
			AND ($2::timestamptz IS NULL OR (t.done_at, t.id) < ($2::timestamptz, $3::uuid))
			ORDER BY t.done_at DESC, t.id DESC LIMIT $4`
		args = []any{actor.UserID, at, id, limit + 1}
	}
	var out []Task
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		out, err = scanTasks(rows)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	if len(out) == 0 {
		return out, nil, nil
	}
	last := out[min(len(out), limit)-1]
	cursor := keyset.Cursor{At: noDay, ID: last.row}
	switch {
	case state == StateDone && last.DoneAt != nil:
		cursor.At = *last.DoneAt
	case last.DueOn != nil:
		if due, err := time.Parse(time.DateOnly, *last.DueOn); err == nil {
			cursor.At = due
		}
	}
	next := keyset.Next(limit, len(out), cursor)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, next, nil
}

// Get is one task of a page the actor may view, read in their transaction.
func Get(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID, taskID uuid.UUID) (*Task, error) {
	rows, err := tx.Query(ctx, selectTasks+` AND t.page_id = $2 AND t.task_id = $3`, actor.UserID, pageID, taskID)
	if err != nil {
		return nil, err
	}
	out, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}
