// Package label keeps the words people put on pages to find related content
// across the tree. A label exists while some page carries it.
package label

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on a name, on a page and on how much one answer carries.
const (
	// MaxNameLength keeps a label a word, as Armature does.
	MaxNameLength      = 40
	MaxPerPage         = 50
	DefaultSuggestions = 10
	MaxSuggestions     = 50
	DefaultLimit       = 20
	MaxLimit           = 100
)

var (
	// ErrBadName refuses a name Normalize cannot make a label of.
	ErrBadName = errors.New("use only letters, digits, hyphens, underscores and dots in a label, and start it with a letter or a digit")
	// ErrTooLong refuses a name longer than MaxNameLength.
	ErrTooLong = fmt.Errorf("keep a label to %d characters", MaxNameLength)
	// ErrTooMany refuses a label past MaxPerPage on one page.
	ErrTooMany = fmt.Errorf("a page carries at most %d labels; remove one first", MaxPerPage)
)

// FieldError is a refusal of one field of the request, shown next to it.
type FieldError struct {
	Field   string
	Message string
	err     error
}

func (e *FieldError) Error() string { return e.Message }
func (e *FieldError) Unwrap() error { return e.err }

func fieldError(field string, err error) *FieldError {
	return &FieldError{Field: field, Message: err.Error(), err: err}
}

// Normalize makes a label of what was typed: trimmed, lower case, spaces as
// hyphens, then only letters, digits, - _ and ., led by a letter or digit.
func Normalize(raw string) (string, error) {
	name := strings.Join(strings.Fields(strings.ToLower(raw)), "-")
	if name == "" {
		return "", ErrBadName
	}
	for i, r := range name {
		word := unicode.IsLetter(r) || unicode.IsDigit(r)
		if i == 0 && !word {
			return "", ErrBadName
		}
		if !word && r != '-' && r != '_' && r != '.' {
			return "", ErrBadName
		}
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", ErrTooLong
	}
	return name, nil
}

// LabelSuggestion is a label the caller can see, with how many of the pages they
// may view carry it.
type LabelSuggestion struct {
	Name  string `json:"name"`
	Pages int    `json:"pages"`
}

// LabeledPage is a page that carries a label, and where it lives.
type LabeledPage struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	// Path is the titles of the pages above it, the home page first.
	Path []string `json:"path"`
	// Labels are all the labels on the page, by name.
	Labels        []string  `json:"labels"`
	Unpublished   bool      `json:"unpublished"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// LabelInput puts one label on a page.
type LabelInput struct {
	// Name is normalized first, so "Release Notes" is release-notes.
	Name string `json:"name"`
}
