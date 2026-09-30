// Package notify tells people what concerns them: a row per person per event
// in the app, and a copy by mail, sent at once or bundled as they prefer.
package notify

import (
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultLimit and MaxLimit bound a page of notifications.
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxExcerptLength bounds the words a notification quotes.
	MaxExcerptLength = 200
	// DailyDigestHour is when a daily bundle goes out, in UTC.
	DailyDigestHour = 8
)

// Kind is why somebody is told, and a key of their preferences.
type Kind string

const (
	KindMentioned Kind = "mentioned"
	// KindReplied is a reply in a thread the person wrote in.
	KindReplied Kind = "replied"
	// KindCommented is a new thread on a watched page.
	KindCommented Kind = "commented"
	// KindResolved is a thread the person wrote in resolved or reopened.
	KindResolved Kind = "resolved"
	// KindPublished is a new version of a watched page.
	KindPublished Kind = "published"
	// KindCreated is a page first published under a watched page or space.
	KindCreated Kind = "created"
)

// Kinds lists every Kind, in the order the preferences show them.
var Kinds = []Kind{KindMentioned, KindReplied, KindCommented, KindResolved, KindPublished, KindCreated}

// Digest is when mail goes out: one per notification, or bundled.
type Digest string

const (
	DigestOff    Digest = "off"
	DigestHourly Digest = "hourly"
	DigestDaily  Digest = "daily"
)

// Digests lists every Digest, for the API document.
var Digests = []Digest{DigestOff, DigestHourly, DigestDaily}

// PageLink names the page a notification is about.
type PageLink struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	SpaceKey string    `json:"spaceKey"`
}

// Notification is one thing a person was told. The client words it from
// the kind, the actor and the page.
type Notification struct {
	ID   uuid.UUID `json:"id"`
	Kind Kind      `json:"kind"`
	// ActorID is null once the person who acted is gone.
	ActorID   *uuid.UUID `json:"actorId"`
	ActorName string     `json:"actorName"`
	Page      PageLink   `json:"page"`
	// ThreadID and CommentID are set for comments, Version for publishes.
	ThreadID  *uuid.UUID `json:"threadId"`
	CommentID *uuid.UUID `json:"commentId"`
	Version   *int       `json:"version"`
	// Excerpt is plain text from the comment or the version's comment.
	Excerpt   string     `json:"excerpt"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt"`
}

// Switches turn each kind on or off for one channel.
type Switches struct {
	Mentioned bool `json:"mentioned"`
	Replied   bool `json:"replied"`
	Commented bool `json:"commented"`
	Resolved  bool `json:"resolved"`
	Published bool `json:"published"`
	Created   bool `json:"created"`
}

// Preferences say how a person hears.
type Preferences struct {
	InApp Switches `json:"inApp"`
	// Email is sent only for kinds also on in the app.
	Email  Switches `json:"email"`
	Digest Digest   `json:"digest"`
	// AutoWatch makes a person watch the pages they create and publish.
	AutoWatch bool `json:"autoWatch"`
}

// MarkReadInput marks the named notifications read, or all of them.
type MarkReadInput struct {
	IDs []uuid.UUID `json:"ids,omitempty"`
	All bool        `json:"all,omitempty"`
}
