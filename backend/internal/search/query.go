package search

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/label"
)

// FieldError is a refusal of one query parameter, which the client shows next
// to the control that set it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// dayLayout is how a day is written in updatedAfter and updatedBefore.
const dayLayout = "2006-01-02"

// Query is a full search: words, filters and order. Limit and offset are the
// handler's to read, as for every list that pages.
type Query struct {
	Text    string
	Spaces  []string
	Authors []uuid.UUID
	Labels  []string
	Types   []HitType
	// After is the first day counted, Before the first day not counted, both
	// at midnight UTC.
	After  *time.Time
	Before *time.Time
	// ByUpdate orders by the latest change rather than by relevance.
	ByUpdate bool
	// Archived finds archived pages and pages of archived spaces too, which a
	// search leaves out unless asked.
	Archived bool
	Limit    int
	Offset   int
}

// ParseQuery reads a full search from the query string of GET /search.
func ParseQuery(v url.Values) (Query, error) {
	q := Query{Text: strings.TrimSpace(v.Get("q"))}
	if utf8.RuneCountInString(q.Text) > MaxQueryLength {
		return q, &FieldError{"q", fmt.Sprintf("Keep the search to %d characters.", MaxQueryLength)}
	}
	for _, key := range v["space"] {
		if key = strings.ToUpper(strings.TrimSpace(key)); key != "" {
			q.Spaces = append(q.Spaces, key)
		}
	}
	for _, raw := range v["author"] {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return q, &FieldError{"author", "An author is a person's id. Pick the person from the list instead."}
		}
		q.Authors = append(q.Authors, id)
	}
	for _, raw := range v["label"] {
		// A name that is no label matches nothing, as an unknown space does.
		name, err := label.Normalize(raw)
		if err != nil {
			name = strings.TrimSpace(raw)
		}
		if name != "" {
			q.Labels = append(q.Labels, name)
		}
	}
	for _, raw := range v["type"] {
		t := HitType(strings.TrimSpace(raw))
		if !slices.Contains(HitTypes, t) {
			return q, &FieldError{"type", "Search for pages, attachments or comments."}
		}
		q.Types = append(q.Types, t)
	}
	var err error
	if q.After, err = day(v.Get("updatedAfter"), "updatedAfter"); err != nil {
		return q, err
	}
	if q.Before, err = day(v.Get("updatedBefore"), "updatedBefore"); err != nil {
		return q, err
	}
	switch v.Get("archived") {
	case "", "false":
	case "true":
		q.Archived = true
	default:
		return q, &FieldError{"archived", "Say true to find archived pages too, or false."}
	}
	switch sort := v.Get("sort"); sort {
	case "", "relevance":
		q.ByUpdate = q.Text == ""
	case "updated":
		q.ByUpdate = true
	default:
		return q, &FieldError{"sort", "Sort by relevance or by updated."}
	}
	return q, nil
}

func day(raw, field string) (*time.Time, error) {
	if raw = strings.TrimSpace(raw); raw == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(dayLayout, raw, time.UTC)
	if err != nil {
		return nil, &FieldError{field, "Give the day as YYYY-MM-DD, such as 2026-09-30."}
	}
	return &t, nil
}

// wants reports whether the query asks for hits of type t.
func (q Query) wants(t HitType) bool {
	return len(q.Types) == 0 || slices.Contains(q.Types, t)
}

// PrefixQuery turns what somebody has typed so far into a tsquery that asks
// for every word as the prefix of a title word. Words are runs of letters and
// digits, so nothing typed can reach the tsquery syntax; empty means none.
func PrefixQuery(typed string) string {
	words := strings.FieldsFunc(typed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > maxQuickWords {
		words = words[:maxQuickWords]
	}
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = "'" + strings.ToLower(w) + "':*A"
	}
	return strings.Join(terms, " & ")
}

// maxQuickWords bounds the words of a quick search, which only ever types a
// title's first few.
const maxQuickWords = 16
