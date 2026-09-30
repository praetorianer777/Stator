// Package watch keeps who follows which pages and spaces, which decides who
// hears about a change.
package watch

import (
	"time"

	"github.com/google/uuid"
)

// Kind is what a watch covers.
type Kind string

const (
	// KindPage is one page alone.
	KindPage Kind = "page"
	// KindSubtree is a page and every page below it, now and later.
	KindSubtree Kind = "subtree"
	// KindSpace is every page of a space, now and later.
	KindSpace Kind = "space"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindPage, KindSubtree, KindSpace}

// PageTitle names a page a watch is on.
type PageTitle struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// Watching is how the caller follows one page.
type Watching struct {
	// Page and Subtree are the caller's own watch on this page, at most one.
	Page    bool `json:"page"`
	Subtree bool `json:"subtree"`
	// Inherited is the nearest watch above that covers the page as well.
	Inherited *Inherited `json:"inherited"`
}

// Inherited is a subtree watch on a page above, or a watch on the space.
type Inherited struct {
	Kind Kind `json:"kind"`
	// Page is null for the space.
	Page *PageTitle `json:"page"`
}

// Input watches a page alone, or with every page below it.
type Input struct {
	Subtree bool `json:"subtree,omitempty"`
}

// Watcher is somebody who hears about a page, and through which watch.
type Watcher struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	Via    Kind      `json:"via"`
	// ViaPage is the page a subtree watch is on; null for the others.
	ViaPage *PageTitle `json:"viaPage"`
}

// Watch is one of the caller's own watches.
type Watch struct {
	Kind      Kind   `json:"kind"`
	SpaceKey  string `json:"spaceKey"`
	SpaceName string `json:"spaceName"`
	// Page is null for a watch on the space.
	Page      *PageTitle `json:"page"`
	CreatedAt time.Time  `json:"createdAt"`
}
