package page

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MaxCommentLength bounds a version comment, matching the web client's.
const MaxCommentLength = 500

// DraftRef tells the reader that the caller has an unpublished edit.
type DraftRef struct {
	// BaseVersion is the published version the draft was started from.
	BaseVersion int       `json:"baseVersion"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Draft is one person's unpublished edit of a page; nobody else sees it.
type Draft struct {
	PageID uuid.UUID `json:"pageId"`
	Title  string    `json:"title"`
	// Body is the document as the editor last autosaved it.
	Body json.RawMessage `json:"body"`
	// BaseVersion is the published version the draft was started from; a
	// publish is refused once the page has moved past it.
	BaseVersion int       `json:"baseVersion"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// DraftInput is an autosave: the whole title and body, and the version the
// editor started from.
type DraftInput struct {
	Title string          `json:"title"`
	Body  json.RawMessage `json:"body"`
	// BaseVersion is the page's version when editing began; saving it again
	// with the current version takes the newer publish as the new base.
	BaseVersion int `json:"baseVersion"`
}

// PublishInput publishes the caller's draft as the next version.
type PublishInput struct {
	// Comment says what changed, shown in the history; optional.
	Comment string `json:"comment,omitempty"`
	// NotifyWatchers tells the page's watchers about the new version.
	NotifyWatchers bool `json:"notifyWatchers,omitempty"`
}

// VersionEntry is one published version as the history lists it.
type VersionEntry struct {
	// Number counts publishes of the page from 1, with no gaps.
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Comment string `json:"comment"`
	// AuthorID is null once the person who published it is gone.
	AuthorID   *uuid.UUID `json:"authorId"`
	AuthorName string     `json:"authorName"`
	CreatedAt  time.Time  `json:"createdAt"`
	// RestoredFrom names the version this one brought back, if it did.
	RestoredFrom *int `json:"restoredFrom"`
	// Live says the version was saved as its editors typed, not published
	// from a draft; it took every save within its span.
	Live bool `json:"live"`
	// UpdatedAt is the last save a live version took, else when it was published.
	UpdatedAt time.Time `json:"updatedAt"`
	// CoEditors names the others who saved into a live version, by name.
	CoEditors []string `json:"coEditors"`
	// OriginalAuthor is who wrote an imported version, by the archive's name
	// for them, when nobody of theirs is here; null otherwise.
	OriginalAuthor *string `json:"originalAuthor"`
}

// Version is one published version with its body, for reading it.
type Version struct {
	VersionEntry
	Body json.RawMessage `json:"body"`
}

// RestoreInput publishes an older version's title and body again.
type RestoreInput struct {
	// BaseVersion is the page's latest version as the caller saw it; a newer
	// one refuses the restore.
	BaseVersion int `json:"baseVersion"`
	// Comment replaces the default, which names the version restored.
	Comment string `json:"comment,omitempty"`
	// NotifyWatchers tells the page's watchers about the new version.
	NotifyWatchers bool `json:"notifyWatchers,omitempty"`
}

// DiffChange says what happened to one top-level block between two sides.
type DiffChange string

const (
	DiffEqual    DiffChange = "equal"
	DiffInserted DiffChange = "inserted"
	DiffDeleted  DiffChange = "deleted"
	DiffModified DiffChange = "modified"
)

// DiffChanges lists every DiffChange, for the API document.
var DiffChanges = []DiffChange{DiffEqual, DiffInserted, DiffDeleted, DiffModified}

// DiffBlock is one top-level block of the comparison, in reading order.
type DiffBlock struct {
	Change DiffChange `json:"change"`
	// Node is the block as a document node. A modified block is the newer
	// side's, with inline diffInsert and diffDelete marks on changed text.
	Node json.RawMessage `json:"node"`
}

// CompareSide is one side of a comparison: a version, or the caller's draft
// with number 0.
type CompareSide struct {
	Number     int       `json:"number"`
	Draft      bool      `json:"draft"`
	Title      string    `json:"title"`
	AuthorName string    `json:"authorName"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Comparison is the difference between two sides of a page's history.
type Comparison struct {
	From   CompareSide `json:"from"`
	To     CompareSide `json:"to"`
	Blocks []DiffBlock `json:"blocks"`
}
