// Package remotelink keeps the links other tools put on pages, each to an
// object of theirs about the page, as Armature's issues keep remote links.
// The tool keeps a link in step by its address: putting an address the page
// carries already replaces that link.
package remotelink

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/via"
)

// The bounds on a link, which page_remote_link's checks repeat.
const (
	MaxURLLength    = 2048
	MaxTitleLength  = 255
	MaxSourceLength = 100
)

// ErrNotFound is a link that is not on the page named.
var ErrNotFound = errors.New("remote link not found")

// FieldError is a refusal of one field of the request, in a sentence.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Link is an object in another tool that is about a page.
type Link struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
	// Title is the object's own title, as the tool last put it.
	Title string `json:"title"`
	// Source names the tool the object lives in; the page groups links by it.
	Source  string  `json:"source"`
	IconURL *string `json:"iconUrl"`
	// CreatedBy is who first put the link, null once they are gone;
	// CreatedByName is their name then, empty when gone.
	CreatedBy     *uuid.UUID `json:"createdBy"`
	CreatedByName string     `json:"createdByName"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	// Via is the token or connected app it was first put through, null for a session (#498).
	Via *via.Via `json:"via"`
}

// Input puts a link on a page, or replaces the one at the same url.
type Input struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// Source names the tool the object lives in, such as Exciter.
	Source  string `json:"source"`
	IconURL string `json:"iconUrl,omitempty"`
}

// Clean trims what was sent and refuses what the table would, each field in
// a sentence that says how to fix it.
func Clean(in Input) (Input, error) {
	in.URL = strings.TrimSpace(in.URL)
	in.Title = strings.TrimSpace(in.Title)
	in.Source = strings.TrimSpace(in.Source)
	in.IconURL = strings.TrimSpace(in.IconURL)
	if err := webAddress("url", in.URL, "the object's address"); err != nil {
		return in, err
	}
	if in.IconURL != "" {
		if err := webAddress("iconUrl", in.IconURL, "the icon's address"); err != nil {
			return in, err
		}
	}
	switch {
	case in.Title == "":
		return in, &FieldError{"title", "Give the link a title."}
	case utf8.RuneCountInString(in.Title) > MaxTitleLength:
		return in, &FieldError{"title", fmt.Sprintf("Shorten the title to %d characters or fewer.", MaxTitleLength)}
	case in.Source == "":
		return in, &FieldError{"source", "Name the tool the object lives in as the link's source."}
	case utf8.RuneCountInString(in.Source) > MaxSourceLength:
		return in, &FieldError{"source", fmt.Sprintf("Shorten the source to %d characters or fewer.", MaxSourceLength)}
	}
	return in, nil
}

func webAddress(field, raw, what string) error {
	if raw == "" {
		return &FieldError{field, fmt.Sprintf("Give %s.", what)}
	}
	if utf8.RuneCountInString(raw) > MaxURLLength {
		return &FieldError{field, fmt.Sprintf("Shorten %s to %d characters or fewer.", what, MaxURLLength)}
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsFunc(raw, isSpace) {
		return &FieldError{field, fmt.Sprintf("Make %s a web address starting with http:// or https://.", what)}
	}
	return nil
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\v' || r == '\f'
}
