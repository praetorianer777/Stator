package page

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/migrations"
)

func TestTheScheduleIntervalAgreesWithTheSetting(t *testing.T) {
	if DefaultScheduleInterval != config.DefaultScheduleCheck {
		t.Fatalf("page.DefaultScheduleInterval %s and config.DefaultScheduleCheck %s disagree", DefaultScheduleInterval, config.DefaultScheduleCheck)
	}
	if w := NewScheduleWatch(nil, nil, 0); w.interval != DefaultScheduleInterval {
		t.Errorf("a watch without an interval looks every %s", w.interval)
	}
}

// The database knows every failure and holds a schedule's comment to a
// version's, or a refusal the service allows would fail as an internal error.
func TestTheScheduleTableKnowsWhatTheServiceWrites(t *testing.T) {
	sql, err := migrations.FS.ReadFile("00510_scheduled_publishing.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ScheduleFailures {
		if !strings.Contains(string(sql), fmt.Sprintf("'%s'", f)) {
			t.Errorf("page_schedule_failure_known does not know %s", f)
		}
	}
	if want := fmt.Sprintf("char_length(comment) <= %d", MaxCommentLength); !strings.Contains(string(sql), want) {
		t.Errorf("page_schedule_comment_length does not say %q", want)
	}
}

// A time that is missing, past or too far ahead is refused on its field,
// before the database is asked anything.
func TestAScheduleOutOfRangeIsRefusedOnItsField(t *testing.T) {
	s := &Service{}
	for name, at := range map[string]time.Time{
		"missing":      {},
		"past":         time.Now().Add(-time.Minute),
		"now":          time.Now(),
		"beyond reach": time.Now().Add(MaxScheduleAhead + time.Hour),
	} {
		_, _, err := s.SchedulePublish(context.Background(), perm.Actor{UserID: uuid.New()}, uuid.New(), ScheduleInput{PublishAt: at})
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "publishAt" || !strings.HasSuffix(field.Message, ".") {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, _, err := s.SchedulePublish(context.Background(), perm.Actor{UserID: uuid.New()}, uuid.New(),
		ScheduleInput{PublishAt: time.Now().Add(time.Hour), Comment: strings.Repeat("c", MaxCommentLength+1)})
	var field *FieldError
	if !errors.As(err, &field) || field.Field != "comment" {
		t.Errorf("a long comment: %v", err)
	}
}

// What refuses a publish is the reason its schedule failed; anything else
// may pass, and the worker tries again on its next look.
func TestARefusedPublishIsReadAsTheScheduleFailure(t *testing.T) {
	for _, c := range []struct {
		err  error
		want ScheduleFailure
		ok   bool
	}{
		{ErrPublishConflict, ScheduleConflict, true},
		{ErrNotFound, ScheduleGone, true},
		{ErrLivePage, ScheduleGone, true},
		{ErrNoDraft, ScheduleGone, true},
		{perm.Refuse(perm.EditPages, perm.ArchivedPage), ScheduleArchived, true},
		{perm.Refuse(perm.EditPages, perm.ArchivedSpace), ScheduleArchived, true},
		{perm.Refuse(perm.EditPages, ""), ScheduleForbidden, true},
		{fmt.Errorf("write version 2: %w", &pgconn.PgError{Code: "42501"}), ScheduleForbidden, true},
		{fmt.Errorf("commit: %w", &pgconn.PgError{Code: "40001"}), "", false},
		{errors.New("connection reset"), "", false},
		{nil, "", false},
	} {
		got, ok := FailureOf(c.err)
		if got != c.want || ok != c.ok {
			t.Errorf("%v read as %q, %v; want %q, %v", c.err, got, ok, c.want, c.ok)
		}
	}
}

func TestAScheduleTakenNamesWhoseItIs(t *testing.T) {
	if msg := (&ScheduleTakenError{Name: "Ann"}).Error(); !strings.HasPrefix(msg, "Ann already scheduled") || !strings.HasSuffix(msg, ".") {
		t.Errorf("reads %q", msg)
	}
	if msg := (&ScheduleTakenError{}).Error(); !strings.HasPrefix(msg, "Somebody already scheduled") {
		t.Errorf("without a name reads %q", msg)
	}
}
