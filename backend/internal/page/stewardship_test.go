package page

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/migrations"
)

// The setting's default and the watch's own are one number written twice,
// since config cannot import this package.
func TestTheLapseIntervalAgreesWithTheSetting(t *testing.T) {
	if DefaultLapseInterval != config.DefaultVerificationCheck {
		t.Fatalf("page.DefaultLapseInterval %s and config.DefaultVerificationCheck %s disagree", DefaultLapseInterval, config.DefaultVerificationCheck)
	}
	if w := NewLapseWatch(nil, nil, 0); w.interval != DefaultLapseInterval {
		t.Errorf("a watch without an interval looks every %s", w.interval)
	}
}

// The longest term is held by the database too, and the two must agree or
// a term the service allows would fail as an internal error.
func TestTheLongestTermIsTheDatabases(t *testing.T) {
	sql, err := migrations.FS.ReadFile("00250_page_stewardship.sql")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("verified_at + interval '%d days'", MaxVerifyDays); !strings.Contains(string(sql), want) {
		t.Errorf("page_verification_term does not say %q", want)
	}
	if DefaultVerifyDays < 1 || DefaultVerifyDays > MaxVerifyDays {
		t.Errorf("the default term %d is not one a verification may have", DefaultVerifyDays)
	}
}

// A term outside one day to MaxVerifyDays is refused on its field, before
// the database is asked anything.
func TestATermOutOfRangeIsRefusedOnItsField(t *testing.T) {
	s := &Service{}
	for _, days := range []int{-1, MaxVerifyDays + 1} {
		_, _, err := s.Verify(context.Background(), perm.Actor{UserID: uuid.New()}, uuid.New(), VerifyInput{Days: days})
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "days" || !strings.HasSuffix(field.Message, ".") {
			t.Errorf("%d days: %v", days, err)
		}
	}
}
