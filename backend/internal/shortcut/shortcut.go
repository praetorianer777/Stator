// Package shortcut keeps the links the administrators of a space pin above
// its page tree, each to a page or to an address on the web.
package shortcut

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on a shortcut. MaxPerSpace is space_shortcut_max() in the database,
// and a test holds the two together.
const (
	MaxPerSpace    = 30
	MaxLabelLength = 100
	MaxURLLength   = 2000
)

// ErrNotFound is a shortcut that is not in the space, or not one the caller
// may see.
var ErrNotFound = errors.New("shortcut not found")

// FieldError is a refusal of one field of the request, in a sentence the form
// shows under it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// FullError is a space that holds as many shortcuts as it may.
type FullError struct{}

func (e *FullError) Error() string {
	return fmt.Sprintf("This space already has %d shortcuts. Remove one before adding another.", MaxPerSpace)
}

// Kind is what a shortcut points at.
type Kind string

const (
	KindPage Kind = "page"
	KindLink Kind = "link"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindPage, KindLink}

// Shortcut is one link above a space's page tree.
type Shortcut struct {
	ID   uuid.UUID `json:"id"`
	Kind Kind      `json:"kind"`
	// Label is what the administrator named it; empty on a page shortcut,
	// which then shows the page's title.
	Label string `json:"label"`
	// URL is the address of a link, null for a page.
	URL *string `json:"url"`
	// Page is the page a page shortcut opens, null for a link.
	Page *ShortcutPage `json:"page"`
}

// ShortcutPage is the page a shortcut opens, as the reader may see it now.
type ShortcutPage struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	SpaceKey string    `json:"spaceKey"`
	// Home says it is its space's home page, whose address is the space's.
	Home     bool `json:"home"`
	Archived bool `json:"archived"`
}

// ShortcutInput is a new shortcut: a page or an address, and a label.
type ShortcutInput struct {
	PageID *uuid.UUID `json:"pageId,omitempty"`
	URL    string     `json:"url,omitempty"`
	// Label is required of a link only in effect: an empty one is its host.
	Label string `json:"label,omitempty"`
}

// ShortcutMove places a shortcut after another of its space, or first when
// After is null.
type ShortcutMove struct {
	After *uuid.UUID `json:"after"`
}

// Clean checks an input before anything is read, and answers the address in
// its kept form and the label to store.
func (in ShortcutInput) Clean() (link string, label string, err error) {
	raw := strings.TrimSpace(in.URL)
	switch {
	case in.PageID == nil && raw == "":
		return "", "", &FieldError{Field: "url", Message: "Choose a page or enter an address for the shortcut."}
	case in.PageID != nil && raw != "":
		return "", "", &FieldError{Field: "url", Message: "A shortcut opens a page or an address, not both. Clear one of them."}
	}
	label = strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > MaxLabelLength {
		return "", "", &FieldError{Field: "label", Message: fmt.Sprintf("Keep the label to %d characters.", MaxLabelLength)}
	}
	if in.PageID != nil {
		return "", label, nil
	}
	link, host, err := CleanURL(raw)
	if err != nil {
		return "", "", err
	}
	if label == "" {
		label = host
	}
	return link, label, nil
}

// CleanURL accepts an absolute http or https address with a host and no name
// or password in it, and answers it with its scheme in lower case, and its host.
func CleanURL(raw string) (string, string, error) {
	bad := func(msg string) (string, string, error) { return "", "", &FieldError{Field: "url", Message: msg} }
	if len(raw) > MaxURLLength {
		return bad(fmt.Sprintf("Keep the address to %d characters.", MaxURLLength))
	}
	if strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return bad("An address has no spaces or line breaks in it. Copy it again from the browser's address bar.")
	}
	scheme, rest, ok := strings.Cut(raw, "://")
	scheme = strings.ToLower(scheme)
	if !ok || (scheme != "http" && scheme != "https") {
		return bad("An address starts with https:// or http://, such as https://example.com.")
	}
	link := scheme + "://" + rest
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return bad("That is not an address a browser can open. Enter one such as https://example.com.")
	}
	if u.User != nil {
		return bad("Leave the name and password out of the address; the site asks for them itself.")
	}
	return link, u.Hostname(), nil
}
