// Package public serves the spaces an organization lets anybody read without
// signing in, as the database lets an anonymous reader see them, and the
// switch with which its administrators allow that at all.
package public

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/page"
)

// MaxTreePages bounds the pages one public space lists; a space larger than
// that lists its first pages in reading order.
const MaxTreePages = 2000

// ErrNotPublic answers whatever an anonymous reader may not read, and an
// organization that lets nobody in without signing in, alike: what is not
// public does not exist for them.
var ErrNotPublic = errors.New("not open to readers who are not signed in")

// ErrNotAdmin refuses the switch to anybody but an administrator of the
// organization.
var ErrNotAdmin = errors.New("only an administrator of the organization lets people read without signing in")

// Settings is whether the organization lets anybody read the spaces that
// allow it, and whether search engines are asked in.
type Settings struct {
	// Enabled lets readers who are not signed in read the spaces that allow
	// it; while it is off no space is public, whatever it allows.
	Enabled bool `json:"enabled"`
	// Indexable lets search engines list the public pages; while it is off
	// every public answer asks them not to.
	Indexable bool `json:"indexable"`
}

// Site is an organization as an anonymous reader finds it.
type Site struct {
	ID        uuid.UUID `json:"-"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Indexable bool      `json:"indexable"`
}

// Space is a public space, as its readers see it.
type Space struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	HomePageID  uuid.UUID `json:"homePageId"`
}

// TreePage is one page of a public space's tree, in reading order.
type TreePage struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parentId"`
	Title    string     `json:"title"`
	Kind     page.Kind  `json:"kind"`
	Icon     *string    `json:"icon"`
	Depth    int        `json:"depth"`
}

// Ref is a page named by id and title.
type Ref struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Kind  page.Kind `json:"kind"`
}

// Page is a published page as an anonymous reader reads it: its words, with
// every person in them left unnamed, and nothing about who wrote or read it.
type Page struct {
	ID         uuid.UUID       `json:"id"`
	Title      string          `json:"title"`
	Kind       page.Kind       `json:"kind"`
	Space      Space           `json:"space"`
	Appearance page.Appearance `json:"appearance"`
	// Body is the published document with its mentions unnamed.
	Body    json.RawMessage `json:"body"`
	Version int             `json:"version"`
	Home    bool            `json:"home"`
	Updated time.Time       `json:"updatedAt"`
	// Ancestors are the pages above this one, the home page first.
	Ancestors []Ref `json:"ancestors"`
	// Children are the public pages right below it, in order.
	Children []Ref `json:"children"`
}
