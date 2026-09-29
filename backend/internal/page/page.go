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
)

// MaxTitleLength bounds a title, matching the web client's.
const MaxTitleLength = 255

var (
	// ErrNotFound is also the answer for a page the caller may not see.
	ErrNotFound = errors.New("page not found")
	// ErrStale refuses a save made from a copy somebody else has saved over since.
	ErrStale = errors.New("somebody else saved this page after you opened it")
)

// Page is one page with its body, as the reader and the editor see it.
type Page struct {
	ID       uuid.UUID  `json:"id"`
	SpaceID  uuid.UUID  `json:"spaceId"`
	SpaceKey string     `json:"spaceKey"`
	ParentID *uuid.UUID `json:"parentId"`
	Title    string     `json:"title"`
	// Body is the document, ProseMirror JSON the allowlist accepts.
	Body json.RawMessage `json:"body"`
	// Version counts saves; a save names the one it started from.
	Version       int       `json:"version"`
	Home          bool      `json:"home"`
	CreatedByName string    `json:"createdByName"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// UpdateInput changes a page's title or body; nil leaves a field alone.
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
