// Package share sends a page to people and groups with a note. Sharing never
// grants access: only people who may already view the page are told.
package share

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// MaxMessageLength bounds the note, so the notification carries it whole.
	MaxMessageLength = notify.MaxExcerptLength
	// MaxRecipients bounds the people and groups one share names.
	MaxRecipients = 20
	// MaxPeople bounds the people one share tells once its groups are resolved.
	MaxPeople = 100
	// MaxPerHour is how many pages one person may share in RateWindow, in one
	// organization; the database's page_share_per_hour() is the same number.
	MaxPerHour = 30
	RateWindow = time.Hour
	// DefaultViewerLimit and MaxViewerLimit bound a window of who may view a page.
	DefaultViewerLimit = 20
	MaxViewerLimit     = 100
)

var (
	// ErrPageNotFound answers a page the caller may not view or that is in the trash.
	ErrPageNotFound = errors.New("page not found")
	// ErrUnpublished refuses sharing a page nobody but its creator can read yet.
	ErrUnpublished = errors.New("publish the page before sharing it")
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// CannotViewError refuses a share naming somebody the page is closed to.
// Nothing is sent to anybody, and nobody named is told they were refused.
type CannotViewError struct {
	// Closed names the people, and the groups none of whose members may view
	// the page, as the sharer picked them.
	Closed []perm.Subject
}

func (e *CannotViewError) Error() string {
	names := make([]string, len(e.Closed))
	for i, s := range e.Closed {
		if s.Type == perm.SubjectGroup {
			names[i] = "everybody in " + s.Name
		} else {
			names[i] = s.Name
		}
	}
	return fmt.Sprintf("This page is closed to %s, so nothing was shared. Sharing does not give access: "+
		"remove them, or ask an administrator of the space, or somebody who may change the page's restrictions, to let them in first.",
		joinNames(names))
}

// RateLimitedError refuses a share past MaxPerHour; RetryAfter is when the
// oldest share of the window leaves it.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	minutes := int(e.RetryAfter.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	unit := "minutes"
	if minutes == 1 {
		unit = "minute"
	}
	return fmt.Sprintf("You have shared %d pages in the last hour, which is as many as one person may. Share this one again in %d %s.",
		MaxPerHour, minutes, unit)
}

// Input sends the page to people and groups, with an optional note.
type Input struct {
	Recipients []perm.SubjectRef `json:"recipients"`
	Message    string            `json:"message,omitempty"`
}

// Share is a page sent: whom the sharer named, and how many people that told.
type Share struct {
	ID         uuid.UUID      `json:"id"`
	PageID     uuid.UUID      `json:"pageId"`
	Recipients []perm.Subject `json:"recipients"`
	// People counts the people told, the sharer and repeats left out.
	People    int       `json:"people"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// Recipient is somebody the share picker offers, and whether they may view the page.
type Recipient struct {
	perm.Person
	CanView bool `json:"canView"`
}

// RecipientGroup is a group the share picker offers, and how many of its members may
// view the page; only those are told.
type RecipientGroup struct {
	perm.Group
	Viewers int `json:"viewers"`
}

// Viewers are the people who may view a page, a window at a time.
type Viewers struct {
	People []perm.Person
	Total  int
	// Everyone is true when every member who may use the organization may view it.
	Everyone bool
}

// Clean checks an input and returns its note trimmed.
func (in Input) Clean() (string, error) {
	message := strings.TrimSpace(in.Message)
	if utf8.RuneCountInString(message) > MaxMessageLength {
		return "", &FieldError{Field: "message", Message: fmt.Sprintf("Keep the note to %d characters; the page says the rest.", MaxMessageLength)}
	}
	if len(in.Recipients) == 0 {
		return "", &FieldError{Field: "recipients", Message: "Pick at least one person or group to share the page with."}
	}
	if len(in.Recipients) > MaxRecipients {
		return "", &FieldError{Field: "recipients", Message: fmt.Sprintf("Share with at most %d people and groups at once, then share again.", MaxRecipients)}
	}
	for _, r := range in.Recipients {
		if r.Type == perm.SubjectEveryone {
			return "", &FieldError{Field: "recipients", Message: "A page is shared with people and groups. Pick them by name instead of everyone."}
		}
	}
	return message, nil
}

// joinNames lists names as a sentence does: "A", "A and B", "A, B and C".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// RetryAfter is how long until a window of shares whose oldest was at oldest
// has room again.
func RetryAfter(oldest, now time.Time) time.Duration {
	wait := oldest.Add(RateWindow).Sub(now)
	if wait < 0 {
		return 0
	}
	return wait
}
