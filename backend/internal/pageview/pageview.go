// Package pageview counts how often a page is read and by how many people,
// once per person, page and day, and names readers only to the page's editors.
package pageview

import (
	"time"

	"github.com/google/uuid"
)

const (
	// RecentDays is the period of the recent counts, today included.
	RecentDays = 30
	// MinRetention is the least the worker keeps, so the recent counts always
	// have their whole period; page_view_min_days() says the same in SQL.
	MinRetention = RecentDays * 24 * time.Hour
	// DefaultRetention keeps a season of named views.
	DefaultRetention = 90 * 24 * time.Hour
	// RetentionInterval is how often the worker prunes.
	RetentionInterval = 24 * time.Hour
	// DefaultLimit and MaxLimit bound a window of the readers.
	DefaultLimit = 25
	MaxLimit     = 100
)

// ViewCounts is how often a page was read, in all and lately, without any names.
type ViewCounts struct {
	// Views counts each person once per day they opened the page.
	Views int64 `json:"views"`
	// Readers are the members who ever opened it.
	Readers int64 `json:"readers"`
	// RecentViews and RecentReaders are the same over the last Days.
	RecentViews   int64 `json:"recentViews"`
	RecentReaders int64 `json:"recentReaders"`
	Days          int   `json:"days"`
	// CanListReaders says the caller may see who read the page.
	CanListReaders bool `json:"canListReaders"`
}

// Reader is somebody who read the page within the retention.
type Reader struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
	// ViewedAt is when they last opened it.
	ViewedAt time.Time `json:"viewedAt"`
	// Days is on how many days they opened it within the retention.
	Days int64 `json:"days"`
}

// Readers is a window of the people who read a page, with how many more chose
// not to be named and how long names are kept.
type Readers struct {
	Readers []Reader `json:"readers"`
	// Unnamed counts the readers who chose not to be named, on any window.
	Unnamed int64 `json:"unnamed"`
	// RetentionDays is how far back readers are kept, 0 for ever.
	RetentionDays int `json:"retentionDays"`
	// Next is the cursor for the window after, null at the end.
	Next *string `json:"next"`
}
