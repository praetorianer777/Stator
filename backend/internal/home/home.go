// Package home reads what colleagues published and what the caller edited,
// the home page's lists beside their stars and recent visits.
package home

import (
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultLimit and MaxLimit bound a window of either list.
	DefaultLimit = 20
	MaxLimit     = 50
)

// Scope narrows the updates.
type Scope string

const (
	// ScopeAll is every page the caller may view.
	ScopeAll Scope = "all"
	// ScopeWatched is the pages the caller's watches cover.
	ScopeWatched Scope = "watched"
)

// Scopes lists every Scope, for the API document.
var Scopes = []Scope{ScopeAll, ScopeWatched}

// PageUpdate is a page somebody else published, as it is now.
type PageUpdate struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	// Version is the number of the latest version, 1 for a new page.
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	// AuthorName is who published the latest version; empty once they are gone.
	AuthorName string `json:"authorName"`
	// Comment is what the author said about the version, often nothing.
	Comment string `json:"comment"`
	// Verified says the page carries a verification that still holds.
	Verified bool `json:"verified"`
}

// EditedPage is a page the caller published a version of, holds a draft of, or
// made and never published.
type EditedPage struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	EditedAt  time.Time `json:"editedAt"`
	// Draft says the caller has unpublished changes to it.
	Draft       bool `json:"draft"`
	Unpublished bool `json:"unpublished"`
}
