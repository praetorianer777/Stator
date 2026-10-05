// Package calendar keeps the calendars of spaces and their events: meetings,
// releases and who is away, which a calendar block draws a month of.
package calendar

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on calendars and events. MaxPerSpace is calendar_max() and
// MaxEventDays the calendar_event_span check in the database; a test holds
// each pair together.
const (
	MaxPerSpace     = 20
	MaxNameLength   = 100
	MaxTitleLength  = 200
	MaxEventDays    = 366
	MaxRangeDays    = 62
	MaxRangedEvents = 500
)

// day is one calendar day, as an event that lasts all day counts them.
const day = 24 * time.Hour

var (
	// ErrNotFound is a calendar or an event that is not there, or not one the
	// caller may see.
	ErrNotFound = errors.New("calendar not found")
	// ErrEventNotFound is an event that is not in its calendar.
	ErrEventNotFound = errors.New("event not found")
)

// FieldError is a refusal of one field of the request, in a sentence the form
// shows under it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// FullError is a space that holds as many calendars as it may.
type FullError struct{}

func (e *FullError) Error() string {
	return fmt.Sprintf("This space already has %d calendars. Remove one before adding another.", MaxPerSpace)
}

// Kind is what an event marks.
type Kind string

const (
	KindEvent   Kind = "event"
	KindAbsence Kind = "absence"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindEvent, KindAbsence}

// Calendar is one of a space's calendars.
type Calendar struct {
	ID        uuid.UUID `json:"id"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	Name      string    `json:"name"`
	// CanEdit says the caller may add, change and remove its events, and
	// rename or remove it.
	CanEdit   bool      `json:"canEdit"`
	CreatedAt time.Time `json:"createdAt"`
}

// CalendarInput names a new calendar, or renames one.
type CalendarInput struct {
	Name string `json:"name"`
}

// CalendarEvent is one entry of a calendar. An event that lasts all day
// starts and ends at midnight UTC, its end the last day it covers.
type CalendarEvent struct {
	ID            uuid.UUID `json:"id"`
	CalendarID    uuid.UUID `json:"calendarId"`
	Title         string    `json:"title"`
	Kind          Kind      `json:"kind"`
	AllDay        bool      `json:"allDay"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	CreatedByName string    `json:"createdByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// CalendarEventInput is a new event, or all of one changed.
type CalendarEventInput struct {
	Title string `json:"title"`
	// Kind is event when absent.
	Kind   Kind      `json:"kind,omitempty"`
	AllDay bool      `json:"allDay"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
}

// CalendarEvents is a calendar with its events in a span of time.
type CalendarEvents struct {
	Calendar Calendar        `json:"calendar"`
	Events   []CalendarEvent `json:"events"`
	// Truncated says more events fell in the span than are answered.
	Truncated bool `json:"truncated"`
}

// Clean answers the name to store, or why it cannot be one.
func (in CalendarInput) Clean() (string, error) {
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		return "", &FieldError{Field: "name", Message: "Name the calendar, such as Team or Releases."}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &FieldError{Field: "name", Message: fmt.Sprintf("Name the calendar in at most %d characters.", MaxNameLength)}
	}
	return name, nil
}

// Clean answers the event to store, its kind filled in and its times in UTC,
// or the first field that is wrong.
func (in CalendarEventInput) Clean() (CalendarEventInput, error) {
	out := in
	out.Title = strings.TrimSpace(in.Title)
	switch {
	case out.Title == "":
		return out, &FieldError{Field: "title", Message: "Give the event a title, such as Release or Ann on holiday."}
	case utf8.RuneCountInString(out.Title) > MaxTitleLength:
		return out, &FieldError{Field: "title", Message: fmt.Sprintf("Write a title of at most %d characters.", MaxTitleLength)}
	}
	if out.Kind == "" {
		out.Kind = KindEvent
	}
	if !slices.Contains(Kinds, out.Kind) {
		return out, &FieldError{Field: "kind", Message: "Mark it as an event or as an absence."}
	}
	if in.Start.IsZero() {
		return out, &FieldError{Field: "start", Message: "Give the day or the time the event starts."}
	}
	if in.End.IsZero() {
		return out, &FieldError{Field: "end", Message: "Give the day or the time the event ends."}
	}
	out.Start, out.End = in.Start.UTC(), in.End.UTC()
	if out.AllDay {
		if !out.Start.Equal(out.Start.Truncate(day)) {
			return out, &FieldError{Field: "start", Message: "An event that lasts all day starts at midnight UTC. Give the first day alone."}
		}
		if !out.End.Equal(out.End.Truncate(day)) {
			return out, &FieldError{Field: "end", Message: "An event that lasts all day ends at midnight UTC of its last day. Give the last day alone."}
		}
	}
	if out.End.Before(out.Start) {
		return out, &FieldError{Field: "end", Message: "End the event on or after the day it starts."}
	}
	if out.End.Sub(out.Start) > MaxEventDays*day {
		return out, &FieldError{Field: "end", Message: fmt.Sprintf("An event lasts at most %d days. Split a longer one into several.", MaxEventDays)}
	}
	return out, nil
}

// Range reads the span a calendar block asks for, two instants in RFC 3339:
// the first included, the second not, at most MaxRangeDays apart.
func Range(from, to string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return time.Time{}, time.Time{}, &FieldError{Field: "from", Message: "Give the start of the span as a time, such as 2026-10-01T00:00:00Z."}
	}
	end, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return time.Time{}, time.Time{}, &FieldError{Field: "to", Message: "Give the end of the span as a time, such as 2026-11-01T00:00:00Z."}
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, &FieldError{Field: "to", Message: "End the span after it starts."}
	}
	if end.Sub(start) > MaxRangeDays*day {
		return time.Time{}, time.Time{}, &FieldError{Field: "to", Message: fmt.Sprintf("Ask for at most %d days at a time.", MaxRangeDays)}
	}
	return start.UTC(), end.UTC(), nil
}

// Covers says whether an event falls in the span [from, to): one that lasts
// all day holds its last day whole, and one without length its instant.
func (e CalendarEvent) Covers(from, to time.Time) bool {
	end := e.End
	if e.AllDay {
		end = end.Add(day)
	}
	return e.Start.Before(to) && (end.After(from) || !e.Start.Before(from))
}
