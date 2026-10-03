package document

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// NodeTaskItem is one checklist item; AttrTaskID is what keeps it the same
	// task from one version to the next.
	NodeTaskItem = "taskItem"
	AttrTaskID   = "taskId"
	// MaxTaskTextLength bounds the words a task is listed and told by.
	MaxTaskTextLength = 500
)

// Task is a checklist item: its own words, the first person it mentions as
// its assignee and the first date in it as its due day.
type Task struct {
	// ID is uuid.Nil until SettleTasks gives the item one.
	ID       uuid.UUID
	Done     bool
	Text     string
	Assignee *uuid.UUID
	// Due is a day written YYYY-MM-DD, empty when the task has none.
	Due string
}

// Tasks lists a document's checklist items in reading order, each before the
// items nested in it, which are tasks of their own.
func Tasks(root Node) []Task {
	var out []Task
	var walk func(nodes []Node, depth int)
	walk = func(nodes []Node, depth int) {
		if depth > MaxDepth {
			return
		}
		for _, n := range nodes {
			if n.Type == NodeTaskItem {
				out = append(out, TaskOf(n))
			}
			walk(n.Content, depth+1)
		}
	}
	walk(root.Content, 0)
	return out
}

// TasksIn reads the tasks of a stored body; none, or one that no longer
// parses, holds none.
func TasksIn(body json.RawMessage) []Task {
	if len(body) == 0 || string(body) == "null" {
		return nil
	}
	root, err := Parse(body)
	if err != nil {
		return nil
	}
	return Tasks(root)
}

// TaskOf reads one checklist item as a task, leaving the items nested in it.
func TaskOf(item Node) Task {
	t := Task{ID: taskID(item)}
	t.Done, _ = item.Attrs["checked"].(bool)
	var words []string
	for _, block := range item.Content {
		if !IsTextblock(block) {
			continue
		}
		words = append(words, InlineText(block))
		for _, c := range block.Content {
			switch c.Type {
			case "mention":
				if t.Assignee != nil {
					continue
				}
				raw, _ := c.Attrs["id"].(string)
				if id, err := uuid.Parse(raw); err == nil && id.String() == raw {
					t.Assignee = &id
				}
			case NodeDate:
				if t.Due != "" {
					continue
				}
				if day, _ := c.Attrs["date"].(string); validDay(day) {
					t.Due = day
				}
			}
		}
	}
	t.Text = clip(strings.Join(strings.Fields(strings.Join(words, " ")), " "), MaxTaskTextLength)
	return t
}

func taskID(item Node) uuid.UUID {
	raw, _ := item.Attrs[AttrTaskID].(string)
	id, err := uuid.Parse(raw)
	if err != nil || id.String() != raw {
		return uuid.Nil
	}
	return id
}

func validDay(day string) bool {
	if !patterns[DatePattern].MatchString(day) {
		return false
	}
	_, err := time.Parse(time.DateOnly, day)
	return err == nil
}

func clip(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:limit]))
}

// SettleTasks gives every checklist item an id of its own, and says whether
// it changed anything.
func SettleTasks(root *Node, previous []Task) bool {
	// An item keeps its id unless an earlier one holds it. One without takes
	// the id of an unclaimed task of the version before with the same words,
	// so a body written without ids, as Markdown is, keeps its tasks.
	var items []*Node
	var walk func(nodes []Node, depth int)
	walk = func(nodes []Node, depth int) {
		if depth > MaxDepth {
			return
		}
		for i := range nodes {
			if nodes[i].Type == NodeTaskItem {
				items = append(items, &nodes[i])
			}
			walk(nodes[i].Content, depth+1)
		}
	}
	walk(root.Content, 0)

	claimed := map[uuid.UUID]bool{}
	var unsettled []*Node
	for _, item := range items {
		id := taskID(*item)
		if id == uuid.Nil || claimed[id] {
			unsettled = append(unsettled, item)
			continue
		}
		claimed[id] = true
	}
	if len(unsettled) == 0 {
		return false
	}
	byText := map[string][]uuid.UUID{}
	for _, t := range previous {
		if t.ID != uuid.Nil && !claimed[t.ID] {
			byText[t.Text] = append(byText[t.Text], t.ID)
		}
	}
	for _, item := range unsettled {
		text := TaskOf(*item).Text
		id := uuid.Nil
		for len(byText[text]) > 0 && id == uuid.Nil {
			candidate := byText[text][0]
			byText[text] = byText[text][1:]
			if !claimed[candidate] {
				id = candidate
			}
		}
		if id == uuid.Nil {
			id = uuid.Must(uuid.NewV7())
		}
		claimed[id] = true
		if item.Attrs == nil {
			item.Attrs = map[string]any{}
		}
		item.Attrs[AttrTaskID] = id.String()
	}
	return true
}

// SettleTasksIn is SettleTasks on a stored body, handed back unchanged when
// every item has an id of its own already.
func SettleTasksIn(body json.RawMessage, previous []Task) (json.RawMessage, error) {
	if len(body) == 0 || string(body) == "null" {
		return body, nil
	}
	root, err := Parse(body)
	if err != nil {
		return nil, err
	}
	if !SettleTasks(&root, previous) {
		return body, nil
	}
	return json.Marshal(root)
}
