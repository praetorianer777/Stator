package page

import (
	"strings"
	"testing"
)

func TestCleanTitle(t *testing.T) {
	if got, err := cleanTitle("  Onboarding  "); err != nil || got != "Onboarding" {
		t.Errorf("cleanTitle trims to %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("t", MaxTitleLength+1)} {
		if _, err := cleanTitle(bad); err == nil {
			t.Errorf("cleanTitle took %d characters", len(bad))
		}
	}
	if _, err := cleanTitle(strings.Repeat("ü", MaxTitleLength)); err != nil {
		t.Errorf("a title of %d letters was refused by its bytes: %v", MaxTitleLength, err)
	}
}
