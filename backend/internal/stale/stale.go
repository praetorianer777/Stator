// Package stale reads the stale content report: published pages nobody opened
// or published for a while, for the administrators of their spaces.
package stale

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/page"
)

const (
	// DefaultDays is how long a page has to go unopened and unpublished
	// before the report lists it, when the reader names no period.
	DefaultDays = 180
	// MinDays and MaxDays bound the period a reader may name.
	MinDays = 1
	MaxDays = 3650
	// DefaultLimit and MaxLimit bound a window of the report.
	DefaultLimit = 25
	MaxLimit     = 100
	// Unowned is the owner filter for pages nobody answers for.
	Unowned = "none"
)

// Verification is where a page stands on being checked.
type Verification string

const (
	Verified   Verification = "verified"
	Expired    Verification = "expired"
	Unverified Verification = "none"
)

// Verifications lists every Verification, for the API document.
var Verifications = []Verification{Verified, Expired, Unverified}

// Filter narrows the report. The zero value is every space the reader
// administers, any owner and verification, nothing archived, after DefaultDays.
type Filter struct {
	// SpaceKey keeps the report to one space; empty is every space the
	// reader administers.
	SpaceKey string
	// Owner keeps pages one person answers for; Unowned those nobody does.
	Owner        *uuid.UUID
	Unowned      bool
	Verification Verification
	// Archived lists archived pages too, which the report leaves out otherwise.
	Archived bool
	// Days is how long since a page was last published or opened.
	Days int
}

// ParseFilter reads a filter from a query string, with a sentence for each
// parameter it cannot take.
func ParseFilter(q url.Values) (Filter, map[string]string) {
	f := Filter{SpaceKey: strings.TrimSpace(q.Get("space")), Days: DefaultDays}
	problems := map[string]string{}
	switch owner := strings.TrimSpace(q.Get("owner")); owner {
	case "":
	case Unowned:
		f.Unowned = true
	default:
		id, err := uuid.Parse(owner)
		if err != nil {
			problems["owner"] = "Name the owner by their id, or ask for pages without an owner with none."
		} else {
			f.Owner = &id
		}
	}
	switch v := Verification(strings.TrimSpace(q.Get("verification"))); v {
	case "":
	case Verified, Expired, Unverified:
		f.Verification = v
	default:
		problems["verification"] = "Ask for verified, expired or none, or leave the verification out."
	}
	switch strings.TrimSpace(q.Get("archived")) {
	case "", "false":
	case "true":
		f.Archived = true
	default:
		problems["archived"] = "Ask for archived pages with true, or leave archived out."
	}
	if raw := strings.TrimSpace(q.Get("olderThan")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < MinDays || days > MaxDays {
			problems["olderThan"] = "Give the period as a whole number of days from " + strconv.Itoa(MinDays) + " to " + strconv.Itoa(MaxDays) + "."
		} else {
			f.Days = days
		}
	}
	return f, problems
}

// StalePage is a page nobody opened or published within the period.
type StalePage struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	Version   int       `json:"version"`
	// PublishedAt is when its latest version was published.
	PublishedAt time.Time `json:"publishedAt"`
	// ViewedAt is when anybody last opened it, null if nobody did since
	// visits were kept.
	ViewedAt *time.Time `json:"viewedAt"`
	// ActiveAt is the later of the two, which the report is ordered by.
	ActiveAt time.Time `json:"activeAt"`
	// Owner is who answers for the page, null for nobody.
	Owner        *page.Owner  `json:"owner"`
	Verification Verification `json:"verification"`
	// VerificationExpiresAt is when a verification runs or ran out.
	VerificationExpiresAt *time.Time `json:"verificationExpiresAt"`
	// Archived says the page is archived, itself or with its space.
	Archived bool `json:"archived"`
	// Archivable says archiving can take the page: not a home page, nor archived already.
	Archivable bool `json:"archivable"`
}
