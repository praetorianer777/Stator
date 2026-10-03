package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

var (
	ErrReportAssignee = errors.New("pick whom the tasks are assigned to: you, nobody, or a person by their id")
	ErrReportDue      = errors.New("pick any due day, overdue, today, this week, or no due day")
	ErrReportState    = errors.New("pick open tasks, done ones, or both")
	ErrReportLimit    = fmt.Errorf("show 1 to %d tasks", document.MaxReportedTasks)
)

// FieldError says which of a report's settings is wrong, in words the reader can act on.
type FieldError struct {
	Field   string
	Message string
	err     error
}

func (e *FieldError) Error() string { return e.Message }
func (e *FieldError) Unwrap() error { return e.err }

func fieldError(field string, err error) *FieldError {
	return &FieldError{Field: field, Message: err.Error(), err: err}
}

// ReportInput is what a task report block asks for: the tasks of one space or
// all, assigned to somebody or nobody, due on some days, in a state.
type ReportInput struct {
	SpaceKey string
	// Assignee is empty for anybody, document.AssigneeReader, document.AssigneeNobody or a person's id.
	Assignee string
	Due      string
	State    string
	Limit    int
}

// Report is the tasks a report's filter picks, as the reader may read them.
type Report struct {
	Tasks []Task `json:"tasks"`
	// AssigneeName names the person the report asks for; empty for anybody
	// else, or for somebody who has left the organization.
	AssigneeName string `json:"assigneeName"`
	// Truncated says more tasks matched than the report shows.
	Truncated bool `json:"truncated"`
}

// checkReport refuses what a report cannot ask for, and reads a person's id.
func checkReport(in ReportInput) (uuid.UUID, error) {
	var person uuid.UUID
	switch in.Assignee {
	case "", document.AssigneeReader, document.AssigneeNobody:
	default:
		id, err := uuid.Parse(in.Assignee)
		if err != nil || id.String() != in.Assignee {
			return uuid.Nil, fieldError("assignee", ErrReportAssignee)
		}
		person = id
	}
	if !slices.Contains(document.TaskReportDues, in.Due) {
		return uuid.Nil, fieldError("due", ErrReportDue)
	}
	if !slices.Contains(document.TaskReportStates, in.State) {
		return uuid.Nil, fieldError("state", ErrReportState)
	}
	if in.Limit < 1 || in.Limit > document.MaxReportedTasks {
		return uuid.Nil, fieldError("limit", ErrReportLimit)
	}
	return person, nil
}

// dueFilters holds what each due day choice adds to the query; the day is
// the database's, so every reader's report agrees on what today is.
var dueFilters = map[string]string{
	document.DueAny:     ``,
	document.DueOverdue: ` AND t.due_on < task_today()`,
	document.DueToday:   ` AND t.due_on = task_today()`,
	document.DueWeek:    ` AND t.due_on >= task_today() AND t.due_on < task_today() + 7`,
	document.DueNone:    ` AND t.due_on IS NULL`,
}

// Report is the tasks the filter picks on pages the actor may view: open ones
// soonest due first and those without a day last, then done ones latest first.
func (s *Service) Report(ctx context.Context, actor perm.Actor, in ReportInput) (*Report, error) {
	person, err := checkReport(in)
	if err != nil {
		return nil, err
	}
	out := &Report{Tasks: []Task{}}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var where strings.Builder
		args := []any{actor.UserID}
		arg := func(v any) string {
			args = append(args, v)
			return "$" + strconv.Itoa(len(args))
		}
		if key := strings.TrimSpace(in.SpaceKey); key != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
			if err != nil {
				return err
			}
			where.WriteString(` AND p.space_id = ` + arg(sp.ID))
		}
		switch {
		case in.Assignee == document.AssigneeReader:
			where.WriteString(` AND t.assignee_id = $1`)
		case in.Assignee == document.AssigneeNobody:
			where.WriteString(` AND t.assignee_id IS NULL`)
		case person != uuid.Nil:
			where.WriteString(` AND t.assignee_id = ` + arg(person))
			err := tx.QueryRow(ctx, `
				SELECT COALESCE((SELECT u.name FROM org_member m JOIN app_user u ON u.id = m.user_id
				                 WHERE m.org_id = current_org_id() AND m.user_id = $1), '')`, person).Scan(&out.AssigneeName)
			if err != nil {
				return fmt.Errorf("name the assignee: %w", err)
			}
		}
		where.WriteString(dueFilters[in.Due])
		switch in.State {
		case document.TaskStateOpen:
			where.WriteString(` AND NOT t.done`)
		case document.TaskStateDone:
			where.WriteString(` AND t.done`)
		}
		rows, err := tx.Query(ctx, selectTasks+where.String()+`
			ORDER BY t.done, CASE WHEN NOT t.done THEN COALESCE(t.due_on, DATE '9999-12-31') END, t.done_at DESC,
			         lower(p.title), p.id, t.position
			LIMIT `+arg(in.Limit+1), args...)
		if err != nil {
			return fmt.Errorf("report the tasks: %w", err)
		}
		out.Tasks, err = scanTasks(rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(out.Tasks) > in.Limit {
		out.Tasks, out.Truncated = out.Tasks[:in.Limit], true
	}
	return out, nil
}
