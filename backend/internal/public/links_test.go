package public

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

func TestALinkAsksForAShortLabelAndAnExpiryAhead(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	ahead, past := now.Add(time.Hour), now.Add(-time.Second)
	cases := []struct {
		name  string
		in    LinkInput
		field string
		label string
	}{
		{name: "nothing at all", in: LinkInput{}},
		{name: "a label trimmed", in: LinkInput{Label: "  For the auditors  "}, label: "For the auditors"},
		{name: "a label at the limit", in: LinkInput{Label: strings.Repeat("ä", MaxLinkLabelLength)}, label: strings.Repeat("ä", MaxLinkLabelLength)},
		{name: "a label too long", in: LinkInput{Label: strings.Repeat("a", MaxLinkLabelLength+1)}, field: "label"},
		{name: "an expiry ahead", in: LinkInput{ExpiresAt: &ahead}},
		{name: "an expiry past", in: LinkInput{ExpiresAt: &past}, field: "expiresAt"},
		{name: "an expiry now", in: LinkInput{ExpiresAt: &now}, field: "expiresAt"},
	}
	for _, c := range cases {
		label, err := c.in.Clean(now)
		var fe *FieldError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%s: refused with %v", c.name, err)
		case c.field != "" && (!errors.As(err, &fe) || fe.Field != c.field):
			t.Errorf("%s: %v, want a refusal of %s", c.name, err, c.field)
		case c.field == "" && label != c.label:
			t.Errorf("%s: label %q, want %q", c.name, label, c.label)
		}
	}
}

func TestEveryRefusalSaysWhatToDoInASentence(t *testing.T) {
	seen := map[string]bool{}
	for _, reason := range Refusals {
		msg := (&RefusedError{Reason: reason}).Error()
		if !strings.HasSuffix(msg, ".") || strings.Count(msg, ".") < 2 {
			t.Errorf("%s: %q is not a sentence and what to do about it", reason, msg)
		}
		if seen[msg] {
			t.Errorf("%s says what another refusal says: %q", reason, msg)
		}
		seen[msg] = true
	}
	if msg := (&RefusedError{Reason: RefusalFull}).Error(); !strings.Contains(msg, "5 public links") {
		t.Errorf("the full page's refusal does not name the limit: %q", msg)
	}
}

func TestATokenIsWhatGenerateTokenMakesAndNothingElse(t *testing.T) {
	for range 20 {
		token, _, err := auth.GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		if !IsToken(token) {
			t.Fatalf("a generated token %q is not taken for one", token)
		}
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44), strings.Repeat("a", 42) + "/", strings.Repeat("a", 42) + "="} {
		if IsToken(bad) {
			t.Errorf("%q is taken for a token", bad)
		}
	}
}

func TestALinksAddressNamesTheOrganizationThenTheToken(t *testing.T) {
	if got := LinkPath("acme", "abc_DEF-123"); got != "/public/acme/link/abc_DEF-123" {
		t.Errorf("LinkPath = %q", got)
	}
}
