// Package space keeps spaces: a key, a name, a description and a home page,
// the root of the space's page tree.
package space

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Limits on what a person types, matching the web client's.
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 1000
	MaxKeyLength         = 10
)

var (
	// ErrNotFound is also the answer for a space the caller may not see:
	// existence itself is privileged.
	ErrNotFound = errors.New("space not found")
)

// FieldError is a refusal of one field, in a sentence the form shows under it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Space is one space as the directory and its pages see it.
type Space struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	HomePageID  uuid.UUID `json:"homePageId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Can says what the reader may do here, so the interface offers only that.
	Can perm.Can `json:"can"`
	// Watching says whether the caller watches the whole space.
	Watching bool `json:"watching"`
	// Starred says the caller keeps the space among their stars.
	Starred bool `json:"starred"`
}

// CreateInput is a new space as the form sends it.
type CreateInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// UpdateInput changes a space's details; nil leaves a field alone. The key
// stays, since every address in the space carries it.
type UpdateInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// keyShape matches the database constraint: two to ten characters, starting
// with a letter, upper case letters and digits only, as Armature's project keys.
var keyShape = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// ValidKey reports whether a key is usable.
func ValidKey(key string) bool { return keyShape.MatchString(key) }

// NormalizeKey upper-cases and trims a key somebody typed or put in an address.
func NormalizeKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }

// SuggestKey derives a key from a name the way a person would: initials for
// several words, the leading letters of one. Empty when nothing usable is left.
func SuggestKey(name string) string {
	words := strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	clean := make([]string, 0, len(words))
	for _, w := range words {
		var b strings.Builder
		for _, r := range w {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			clean = append(clean, b.String())
		}
	}
	if len(clean) == 0 {
		return ""
	}
	key := clean[0]
	if len(clean) > 1 {
		var b strings.Builder
		for _, w := range clean {
			b.WriteByte(w[0])
		}
		key = b.String()
	}
	if len(key) > MaxKeyLength {
		key = key[:MaxKeyLength]
	}
	if key[0] < 'A' || key[0] > 'Z' {
		return ""
	}
	if len(key) == 1 {
		key += key
	}
	return key
}

func checkKey(key string) error {
	if key == "" {
		return &FieldError{Field: "key", Message: "A space needs a key: two to ten letters or digits, such as DOCS."}
	}
	if !ValidKey(key) {
		return &FieldError{Field: "key", Message: "A key is two to ten capital letters or digits and starts with a letter, such as DOCS or TEAM2."}
	}
	return nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &FieldError{Field: "name", Message: "A space needs a name."}
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", &FieldError{Field: "name", Message: fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
	}
	return name, nil
}

func cleanDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		return "", &FieldError{Field: "description", Message: fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength)}
	}
	return description, nil
}
