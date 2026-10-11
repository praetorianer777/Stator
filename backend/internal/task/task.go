// Package task reads the tasks of published pages: the checklist items a
// person is assigned, and the worker's reminder when a task's day comes.
package task

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultLimit and MaxLimit bound a window of a person's tasks.
	DefaultLimit = 20
	MaxLimit     = 100
)

// ErrNotFound answers a task its page does not hold, or a page the caller
// may not view.
var ErrNotFound = errors.New("task not found")

// State is which of a person's tasks a list holds.
type State string

const (
	// StateOpen lists the tasks not done yet, the soonest due first and those
	// without a day last.
	StateOpen State = "open"
	// StateDone lists the tasks done, the latest first.
	StateDone State = "done"
)

// States lists every State, for the API document.
var States = []State{StateOpen, StateDone}

// PageRef names the page a task is on.
type PageRef struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
}

// Task is one checklist item of a published page, as its words, assignee and
// date read when the page was last published.
type Task struct {
	// ID is the item's taskId in the page's document.
	ID   uuid.UUID `json:"id"`
	Page PageRef   `json:"page"`
	// Path is where the task is on its page, for a link that opens the page at it.
	Path string `json:"path"`
	Text string `json:"text"`
	Done bool   `json:"done"`
	// DueOn is a day, YYYY-MM-DD, read in UTC as a date node is; null when
	// the task has none.
	DueOn *string `json:"dueOn"`
	// AssigneeID is null for a task nobody is assigned.
	AssigneeID     *uuid.UUID `json:"assigneeId"`
	AssigneeName   string     `json:"assigneeName"`
	AssignedByName string     `json:"assignedByName"`
	AssignedAt     *time.Time `json:"assignedAt"`
	DoneAt         *time.Time `json:"doneAt"`
	// CanEdit says whether the caller may tick the task off, which publishes
	// the page again.
	CanEdit bool `json:"canEdit"`

	row uuid.UUID
}

// SetDoneInput ticks a task off, or opens it again.
type SetDoneInput struct {
	Done bool `json:"done"`
}
