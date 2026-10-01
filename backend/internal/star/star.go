// Package star keeps each person's starred pages and spaces, the things they
// want close at hand on their home page.
package star

import (
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/watch"
)

const (
	// DefaultLimit and MaxLimit bound a window of stars.
	DefaultLimit = 20
	MaxLimit     = 100
)

// Kind is what a star is on.
type Kind string

const (
	KindPage  Kind = "page"
	KindSpace Kind = "space"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindPage, KindSpace}

// Star is one of the caller's own stars.
type Star struct {
	Kind      Kind   `json:"kind"`
	SpaceKey  string `json:"spaceKey"`
	SpaceName string `json:"spaceName"`
	// Page is null for a star on the space.
	Page      *watch.PageTitle `json:"page"`
	StarredAt time.Time        `json:"starredAt"`

	id uuid.UUID
}
