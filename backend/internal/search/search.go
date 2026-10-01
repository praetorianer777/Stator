// Package search finds pages, attachments and comments by their words, with
// PostgreSQL full-text search, among what the caller may see.
package search

import (
	"time"

	"github.com/google/uuid"
)

// Limits on a query and on how much one answer carries.
const (
	MaxQueryLength     = 200
	DefaultLimit       = 20
	MaxLimit           = 100
	DefaultQuickLimit  = 8
	MaxQuickLimit      = 20
	DefaultRecentLimit = 10
	MaxRecentLimit     = 20
)

// HitType says what a hit is.
type HitType string

const (
	HitPage       HitType = "page"
	HitAttachment HitType = "attachment"
	HitComment    HitType = "comment"
)

// HitTypes lists every HitType, for the API document.
var HitTypes = []HitType{HitPage, HitAttachment, HitComment}

// Sorts are the orders hits come in: relevance, the default, or the last change.
var Sorts = []string{"relevance", "updated"}

// Segment is a run of text; Match marks the runs the query matched, which
// the reader highlights. Plain text, never markup.
type Segment struct {
	Text  string `json:"text"`
	Match bool   `json:"match"`
}

// PageRef is the page a hit belongs to, and where it lives.
type PageRef struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
}

// Hit is one result of a full search.
type Hit struct {
	Type HitType `json:"type"`
	// Page is the page itself, or the one the attachment or comment is on.
	Page PageRef `json:"page"`
	// AttachmentID is set for an attachment, CommentID for a comment.
	AttachmentID *uuid.UUID `json:"attachmentId"`
	CommentID    *uuid.UUID `json:"commentId"`
	// Title is the page's title or the file's name, with the matches marked.
	Title []Segment `json:"title"`
	// Snippet is the best passage of the text around the matches.
	Snippet []Segment `json:"snippet"`
	Labels  []string  `json:"labels"`
	// Verified says the page carries a verification that still holds.
	Verified bool `json:"verified"`
	// Archived says the page, or its whole space, is archived.
	Archived      bool      `json:"archived"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByName string    `json:"updatedByName"`
}

// PageHit is a page offered by quick search or as a recent page.
type PageHit struct {
	PageRef
	// Path is the titles of the pages above it, the home page first.
	Path []string `json:"path"`
}

// RecentPage is a page the caller visited, the latest visit first.
type RecentPage struct {
	PageHit
	VisitedAt time.Time `json:"visitedAt"`
}
