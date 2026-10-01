// Package page keeps a space's pages: where each sits in the tree, its title
// and its current body, a document the editor wrote and document validated.
package page

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

// MaxTitleLength bounds a title, matching the web client's.
const MaxTitleLength = 255

var (
	// ErrNotFound is also the answer for a page the caller may not see.
	ErrNotFound = errors.New("page not found")
	// ErrStale refuses a save made from a copy somebody else has saved over since.
	ErrStale = errors.New("somebody else saved this page after you opened it")
	// ErrNoDraft refuses publishing a published page the caller has no draft of.
	ErrNoDraft = errors.New("you have no draft of this page to publish; edit it first")
	// ErrPublishConflict refuses a draft begun before somebody else's publish.
	ErrPublishConflict = errors.New("somebody published this page after you began your draft")
	// ErrDraftNotFound answers a comparison with a draft the caller does not have.
	ErrDraftNotFound = errors.New("you have no draft of this page")
	// ErrVersionNotFound answers a version number the page never reached.
	ErrVersionNotFound = errors.New("that version of the page was not found")
	// ErrRestoreStale refuses a restore made from an outdated history.
	ErrRestoreStale = errors.New("somebody published this page after you opened its history")
	// ErrRestoreLatest refuses restoring the version the page already is.
	ErrRestoreLatest = errors.New("that is already the latest version of the page")
)

// FieldError is a refusal of one field of the request, which the client
// shows next to it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Page is one page with its body, as the reader and the editor see it.
type Page struct {
	ID       uuid.UUID  `json:"id"`
	SpaceID  uuid.UUID  `json:"spaceId"`
	SpaceKey string     `json:"spaceKey"`
	ParentID *uuid.UUID `json:"parentId"`
	Title    string     `json:"title"`
	// Body is the document, ProseMirror JSON the allowlist accepts.
	Body json.RawMessage `json:"body"`
	// Version is the number of the published version the title and body
	// are, 0 while the page has never been published.
	Version int  `json:"version"`
	Home    bool `json:"home"`
	// Unpublished pages are seen by their creator alone.
	Unpublished bool `json:"unpublished"`
	// Draft is the caller's own unpublished edit of the page, if any.
	Draft *DraftRef `json:"draft"`
	// Restricted says whether a view or edit restriction on the page or
	// above it narrows who may do so.
	Restricted Restricted `json:"restricted"`
	// Can says what the caller may do to this page, restrictions included.
	Can perm.PageCan `json:"can"`
	// Ancestors are the pages above this one, the home page first.
	Ancestors []Ref `json:"ancestors"`
	// Labels are the words on the page, by name.
	Labels []string `json:"labels"`
	// Comments counts the discussion for the page's header.
	Comments comment.Counts `json:"comments"`
	// Watching is how the caller follows the page.
	Watching watch.Watching `json:"watching"`
	// Starred says the caller keeps the page among their stars.
	Starred bool `json:"starred"`
	// Reactions are the emoji on the page itself.
	Reactions     []reaction.Reaction `json:"reactions"`
	CreatedByName string              `json:"createdByName"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedByName string              `json:"updatedByName"`
	UpdatedAt     time.Time           `json:"updatedAt"`

	access perm.PageAccess
}

// UpdateInput publishes a new title or body at once, without a draft, as
// scripts do; nil leaves a field alone.
type UpdateInput struct {
	Title *string `json:"title,omitempty"`
	// Body is the whole new document.
	Body json.RawMessage `json:"body,omitempty"`
	// Version is the one the change was made from; a newer one refuses it.
	Version int `json:"version"`
}

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("a page needs a title")
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return "", fmt.Errorf("keep the title to %d characters", MaxTitleLength)
	}
	return title, nil
}
