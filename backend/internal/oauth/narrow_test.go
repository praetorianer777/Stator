package oauth

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// An exchange narrows the grant's scope or keeps it, and never widens it.
func TestAnExchangeOnlyNarrowsTheScope(t *testing.T) {
	writes, reads := []string{}, []string{ScopeRead}
	for _, c := range []struct {
		asked string
		held  []string
		want  []string
	}{
		{"", writes, writes},
		{"", reads, reads},
		{"read", writes, reads},
		{"read write", writes, writes},
		{"write read read", writes, writes},
		{"read", reads, reads},
	} {
		if got, ok := narrowScope(c.asked, c.held); !ok || !slices.Equal(got, c.want) {
			t.Errorf("%q from %v gives %v, %v; want %v", c.asked, c.held, got, ok, c.want)
		}
	}
	for _, c := range []struct {
		asked string
		held  []string
	}{
		{"read write", reads},
		{"write", writes},
		{"read admin", writes},
	} {
		if got, ok := narrowScope(c.asked, c.held); ok {
			t.Errorf("%q from %v was given %v", c.asked, c.held, got)
		}
	}
}

// A narrower token lasts what is asked, up to an hour, and never longer than
// its subject has left.
func TestANarrowerTokenNeverOutlivesItsSubject(t *testing.T) {
	const left = 50 * time.Minute
	for asked, want := range map[string]time.Duration{
		"":     left,
		"1":    time.Second,
		"900":  15 * time.Minute,
		"3600": left,
	} {
		if got, ok := narrowLifetime(asked, left); !ok || got != want {
			t.Errorf("%q asked gives %v, %v; want %v", asked, got, ok, want)
		}
	}
	if got, ok := narrowLifetime("", 2*time.Hour); !ok || got != MaxExchangedTTL {
		t.Errorf("nothing asked of a long subject gives %v, %v", got, ok)
	}
	for _, refused := range []string{"0", "-5", "3601", "1.5", "an hour", " 60"} {
		if got, ok := narrowLifetime(refused, left); ok {
			t.Errorf("%q was given %v", refused, got)
		}
	}
}

// Spaces are named by key as a token's are, and no more than a token names.
func TestANarrowerTokenNamesSpacesAsATokenDoes(t *testing.T) {
	if got, ok := spaceKeys(" docs  TEAM docs "); !ok || !slices.Equal(got, []string{"DOCS", "TEAM"}) {
		t.Errorf("the keys read as %v, %v", got, ok)
	}
	if got, ok := spaceKeys(""); !ok || len(got) != 0 {
		t.Errorf("no keys read as %v, %v", got, ok)
	}
	many := make([]string, auth.MaxTokenSpaces+1)
	for i := range many {
		many[i] = fmt.Sprintf("S%d", i)
	}
	if _, ok := spaceKeys(strings.Join(many[:auth.MaxTokenSpaces], " ")); !ok {
		t.Error("as many spaces as a token names were refused")
	}
	if _, ok := spaceKeys(strings.Join(many, " ")); ok {
		t.Error("more spaces than a token names were taken")
	}
}
