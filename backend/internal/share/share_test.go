package share

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

func user() perm.SubjectRef {
	id := uuid.New()
	return perm.SubjectRef{Type: perm.SubjectUser, ID: &id}
}

// An input names somebody, at most MaxRecipients of them, never everyone, and
// a note short enough for the notification to carry whole.
func TestAnInputIsCheckedBeforeAnythingIsRead(t *testing.T) {
	note, err := Input{Recipients: []perm.SubjectRef{user()}, Message: "  Read this first.  "}.Clean()
	if err != nil || note != "Read this first." {
		t.Fatalf("a plain share cleans to %q, %v", note, err)
	}
	many := make([]perm.SubjectRef, MaxRecipients+1)
	for i := range many {
		many[i] = user()
	}
	for name, c := range map[string]struct {
		in    Input
		field string
	}{
		"nobody":    {Input{}, "recipients"},
		"too many":  {Input{Recipients: many}, "recipients"},
		"everyone":  {Input{Recipients: []perm.SubjectRef{{Type: perm.SubjectEveryone}}}, "recipients"},
		"long note": {Input{Recipients: []perm.SubjectRef{user()}, Message: strings.Repeat("ä", MaxMessageLength+1)}, "message"},
	} {
		_, err := c.in.Clean()
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != c.field || !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("%s is refused with %v, want a sentence on %s", name, err, c.field)
		}
	}
	if _, err := (Input{Recipients: []perm.SubjectRef{user()}, Message: strings.Repeat("ä", MaxMessageLength)}).Clean(); err != nil {
		t.Errorf("a note of exactly %d characters is refused: %v", MaxMessageLength, err)
	}
}

// The refusal names whom the page is closed to and says what to do, and
// never claims anything was sent.
func TestARefusalNamesWhomThePageIsClosedTo(t *testing.T) {
	one := (&CannotViewError{Closed: []perm.Subject{{Type: perm.SubjectUser, Name: "Bob Reader"}}}).Error()
	if !strings.HasPrefix(one, "This page is closed to Bob Reader, so nothing was shared.") || !strings.Contains(one, "let them in first.") {
		t.Errorf("one person reads %q", one)
	}
	three := (&CannotViewError{Closed: []perm.Subject{
		{Type: perm.SubjectUser, Name: "Ann"}, {Type: perm.SubjectUser, Name: "Ben"}, {Type: perm.SubjectGroup, Name: "Design"},
	}}).Error()
	if !strings.HasPrefix(three, "This page is closed to Ann, Ben and everybody in Design,") {
		t.Errorf("three read %q", three)
	}
}

// The brake says how long to wait, in whole minutes and never zero.
func TestTheBrakeSaysWhenToTryAgain(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if got := RetryAfter(now.Add(-50*time.Minute), now); got != 10*time.Minute {
		t.Errorf("a window opened 50 minutes ago has room in %s", got)
	}
	if got := RetryAfter(now.Add(-2*time.Hour), now); got != 0 {
		t.Errorf("a window long past waits %s", got)
	}
	if got := (&RateLimitedError{RetryAfter: 10 * time.Minute}).Error(); !strings.HasSuffix(got, "in 10 minutes.") {
		t.Errorf("ten minutes read %q", got)
	}
	if got := (&RateLimitedError{RetryAfter: 5 * time.Second}).Error(); !strings.HasSuffix(got, "in 1 minute.") {
		t.Errorf("a few seconds read %q", got)
	}
}
