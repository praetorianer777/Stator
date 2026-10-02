package page

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/task"
)

// assignability is what the database says of a person a task names.
type assignability struct {
	member, views bool
	name          string
}

// syncTasks brings a page's task rows in line with its published body, as the
// actor; strict refuses a task newly assigned to a member who may not view it.
func syncTasks(ctx context.Context, tx db.DBTX, pageID uuid.UUID, strict bool) error {
	// Read back rather than handed in, so the rows follow what the database
	// stored, after whatever it strips from a published body.
	var body json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT body FROM page WHERE id = $1`, pageID).Scan(&body); err != nil {
		return fmt.Errorf("read the published page: %w", err)
	}
	var tasks []document.Task
	for _, t := range document.TasksIn(body) {
		// Only a published item has an id; a copy of a page never published may hold one without.
		if t.ID != uuid.Nil {
			tasks = append(tasks, t)
		}
	}
	held := map[uuid.UUID]*uuid.UUID{}
	rows, err := tx.Query(ctx, `SELECT task_id, assignee_id FROM page_task WHERE page_id = $1`, pageID)
	if err != nil {
		return fmt.Errorf("read the page's tasks: %w", err)
	}
	var (
		id       uuid.UUID
		assignee *uuid.UUID
	)
	for rows.Next() {
		if err := rows.Scan(&id, &assignee); err != nil {
			rows.Close()
			return err
		}
		held[id] = assignee
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var named []uuid.UUID
	for _, t := range tasks {
		if t.Assignee != nil && !sameAssignee(held[t.ID], t.Assignee) {
			named = append(named, *t.Assignee)
		}
	}
	people, err := assignable(ctx, tx, pageID, named)
	if err != nil {
		return err
	}

	ids, positions, summaries, dones := make([]uuid.UUID, len(tasks)), make([]int32, len(tasks)), make([]string, len(tasks)), make([]bool, len(tasks))
	assignees, dues := make([]*uuid.UUID, len(tasks)), make([]*string, len(tasks))
	for i, t := range tasks {
		ids[i], positions[i], summaries[i], dones[i] = t.ID, int32(i), t.Text, t.Done
		if t.Due != "" {
			due := t.Due
			dues[i] = &due
		}
		assignees[i] = t.Assignee
		if t.Assignee == nil || sameAssignee(held[t.ID], t.Assignee) {
			continue
		}
		p := people[*t.Assignee]
		switch {
		case !p.member:
			// A mention of somebody who left, or never belonged, stays in the
			// text and assigns nobody, as it tells nobody.
			assignees[i] = nil
		case !p.views && strict:
			return &FieldError{Field: "body", Message: fmt.Sprintf(
				"%s cannot view this page, so the task %q cannot be assigned to them. Assign it to somebody who can view the page, or change who may view it first.",
				p.name, t.Text)}
		case !p.views:
			assignees[i] = nil
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM page_task WHERE page_id = $1 AND NOT (task_id = ANY($2::uuid[]))`, pageID, ids); err != nil {
		return fmt.Errorf("drop the tasks the page no longer holds: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_task (org_id, page_id, task_id, position, summary, done, assignee_id, due_on)
		SELECT current_org_id(), $1, t.task_id, t.position, t.summary, t.done, t.assignee_id, t.due_on::date
		FROM unnest($2::uuid[], $3::int[], $4::text[], $5::bool[], $6::uuid[], $7::text[])
		     AS t (task_id, position, summary, done, assignee_id, due_on)
		ON CONFLICT (org_id, page_id, task_id) DO UPDATE
		SET position = EXCLUDED.position, summary = EXCLUDED.summary, done = EXCLUDED.done,
		    assignee_id = EXCLUDED.assignee_id, due_on = EXCLUDED.due_on
		WHERE (page_task.position, page_task.summary, page_task.done, page_task.assignee_id, page_task.due_on)
		      IS DISTINCT FROM (EXCLUDED.position, EXCLUDED.summary, EXCLUDED.done, EXCLUDED.assignee_id, EXCLUDED.due_on)`,
		pageID, ids, positions, summaries, dones, assignees, dues); err != nil {
		return fmt.Errorf("write the page's tasks: %w", err)
	}
	return nil
}

func sameAssignee(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// assignable reads whether each person is a member who may view the page,
// with their name for a refusal.
func assignable(ctx context.Context, tx db.DBTX, pageID uuid.UUID, people []uuid.UUID) (map[uuid.UUID]assignability, error) {
	out := map[uuid.UUID]assignability{}
	if len(people) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id, perm_is_member(m.id), perm_is_member(m.id) AND perm_page_viewable($1, m.id), COALESCE(u.name, '')
		FROM unnest($2::uuid[]) AS m (id) LEFT JOIN app_user u ON u.id = m.id`, pageID, people)
	if err != nil {
		return nil, fmt.Errorf("read who may be assigned: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			a  assignability
		)
		if err := rows.Scan(&id, &a.member, &a.views, &a.name); err != nil {
			return nil, err
		}
		if a.name == "" {
			a.name = "That person"
		}
		out[id] = a
	}
	return out, rows.Err()
}

// SetTaskDone ticks a task of a published page off, or opens it again, by
// publishing the page with its box changed. Its state already is no change.
func (s *Service) SetTaskDone(ctx context.Context, actor perm.Actor, pageID, taskID uuid.UUID, in task.SetDoneInput) (*task.Task, db.LSN, error) {
	var out *task.Task
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, pageID, true)
		if err != nil {
			return err
		}
		if p.Version == 0 {
			return task.ErrNotFound
		}
		root, err := document.Parse(p.Body)
		if err != nil {
			return err
		}
		item := findTask(&root, taskID)
		if item == nil {
			return task.ErrNotFound
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if done, _ := item.Attrs["checked"].(bool); done != in.Done {
			item.Attrs["checked"] = in.Done
			body, err := json.Marshal(root)
			if err != nil {
				return err
			}
			words := "Ticked off a task"
			if !in.Done {
				words = "Opened a task again"
			}
			comment, _ := cleanComment(clipComment(words + ": " + document.TaskOf(*item).Text))
			if _, err := publish(ctx, tx, actor, p, release{title: p.Title, body: body, comment: comment}); err != nil {
				return err
			}
		}
		out, err = task.Get(ctx, tx, actor, pageID, taskID)
		return err
	})
	return out, lsn, err
}

func clipComment(text string) string {
	if r := []rune(text); len(r) > MaxCommentLength {
		return string(r[:MaxCommentLength])
	}
	return text
}

func findTask(root *document.Node, taskID uuid.UUID) *document.Node {
	var found *document.Node
	var walk func(nodes []document.Node, depth int)
	walk = func(nodes []document.Node, depth int) {
		if depth > document.MaxDepth || found != nil {
			return
		}
		for i := range nodes {
			if nodes[i].Type == document.NodeTaskItem && nodes[i].Attrs[document.AttrTaskID] == taskID.String() {
				found = &nodes[i]
				return
			}
			walk(nodes[i].Content, depth+1)
		}
	}
	walk(root.Content, 0)
	return found
}
