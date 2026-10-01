// Package reaction keeps the emoji members put on pages and comments as quick
// feedback, and who put each.
package reaction

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// MaxEmojiBytes bounds one emoji; the longest sequences, such as a family
	// with skin tones or a subdivision flag, stay well under it.
	MaxEmojiBytes = 32
	// MaxPerPerson bounds the different emoji one person puts on one page or comment.
	MaxPerPerson = 20
	// MaxPeople bounds the names each emoji carries for the who-reacted tooltip;
	// Count says how many there are in all.
	MaxPeople = 10
)

var (
	// ErrPageNotFound answers a page the caller may not view or that is in the trash.
	ErrPageNotFound = errors.New("page not found")
	// ErrCommentNotFound also answers a deleted comment, which takes no reactions.
	ErrCommentNotFound = errors.New("comment not found")
	// ErrUnpublished refuses a reaction on a page nobody but its creator can read yet.
	ErrUnpublished = errors.New("publish the page before reacting to it")
	// ErrMayNotReact refuses somebody who may read but not comment.
	ErrMayNotReact = errors.New("you may not react in this space; ask an administrator of the space for access")
	// ErrNotEmoji refuses anything but one emoji.
	ErrNotEmoji = errors.New("pick one emoji to react with")
	// ErrTooMany refuses a reaction past MaxPerPerson on one page or comment.
	ErrTooMany = fmt.Errorf("you can put at most %d different reactions on one page or comment; take one away first", MaxPerPerson)
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
	err     error
}

func (e *FieldError) Error() string { return e.Message }
func (e *FieldError) Unwrap() error { return e.err }

// Reactor is somebody who reacted, as the tooltip names them.
type Reactor struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Reaction is one emoji on a page or comment: how many put it there, whether
// the caller did, and the first of them by name.
type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
	// People are the first MaxPeople to react, earliest first.
	People []Reactor `json:"people"`
}

// Input is the emoji to put on.
type Input struct {
	Emoji string `json:"emoji"`
}

const (
	zwj             = '\u200d'
	variation16     = '\ufe0f'
	variation15     = '\ufe0e'
	keycap          = '\u20e3'
	tagFirst        = '\U000E0020'
	tagLast         = '\U000E007F'
	keycapBaseChars = "0123456789#*"
)

// pictographic are the ranges that hold emoji proper; it is wider than the
// emoji set, which is fine for a check that only has to keep words out.
var pictographic = [][2]rune{
	{0x00A9, 0x00A9}, {0x00AE, 0x00AE}, {0x203C, 0x203C}, {0x2049, 0x2049},
	{0x2122, 0x2122}, {0x2139, 0x2139}, {0x2194, 0x21AA}, {0x231A, 0x23FF},
	{0x24C2, 0x24C2}, {0x25AA, 0x25FE}, {0x2600, 0x27BF}, {0x2934, 0x2935},
	{0x2B05, 0x2B55}, {0x3030, 0x3030}, {0x303D, 0x303D}, {0x3297, 0x3299},
	{0x1F000, 0x1FAFF},
}

func isPictographic(r rune) bool {
	for _, span := range pictographic {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}
	return false
}

// Clean holds what was sent to one emoji: pictographs joined by zero width
// joiners, with skin tones, variation selectors, flag tags and keycaps.
func Clean(raw string) (string, error) {
	emoji := strings.TrimSpace(raw)
	if emoji == "" || len(emoji) > MaxEmojiBytes || !utf8.ValidString(emoji) {
		return "", notEmoji()
	}
	runes := []rune(emoji)
	pictographs := 0
	for i, r := range runes {
		switch {
		case isPictographic(r):
			pictographs++
		case r == zwj:
			if i == 0 || i == len(runes)-1 {
				return "", notEmoji()
			}
		case r == variation16 || r == variation15 || (r >= tagFirst && r <= tagLast):
			if i == 0 {
				return "", notEmoji()
			}
		case r == keycap:
			if i == 0 || !strings.ContainsRune(keycapBaseChars, runes[0]) {
				return "", notEmoji()
			}
			pictographs++
		case strings.ContainsRune(keycapBaseChars, r):
			if i != 0 || !strings.ContainsRune(emoji, keycap) {
				return "", notEmoji()
			}
		default:
			return "", notEmoji()
		}
	}
	if pictographs == 0 {
		return "", notEmoji()
	}
	return emoji, nil
}

func notEmoji() error {
	return &FieldError{Field: "emoji", Message: ErrNotEmoji.Error(), err: ErrNotEmoji}
}
