package armature

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCheckCalendarRefusesWhatArmatureWould(t *testing.T) {
	if err := CheckCalendar("CP", "2026-10"); err != nil {
		t.Errorf("a good month was refused: %v", err)
	}
	for _, c := range []struct{ project, month, field string }{
		{"cp", "2026-10", "project"},
		{"", "2026-10", "project"},
		{"CP", "2026-13", "month"},
		{"CP", "2026-1", "month"},
		{"CP", "October", "month"},
	} {
		var refused *FieldError
		if err := CheckCalendar(c.project, c.month); !errors.As(err, &refused) || refused.Field != c.field || refused.Message == "" {
			t.Errorf("%s %s: %v, want a sentence on %s", c.project, c.month, err, c.field)
		}
	}
}

func TestACalendarKeepsArmaturesIssuesByTheirFirstDay(t *testing.T) {
	var items []calendarItem
	raw := `[
	  {"kind":"sprint","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","title":"Sprint 4","from":"2026-10-01","to":"2026-10-14"},
	  {"kind":"issue","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02","key":"CP-10","title":"Ship it","from":"2026-10-15T00:00:00Z","to":"2026-10-15T00:00:00Z","category":"todo"},
	  {"kind":"issue","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a03","key":"CP-9","title":"Write the notes","from":"2026-10-15","to":"2026-10-15","done":true,"category":"done"},
	  {"kind":"issue","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a04","key":"CP-2","title":"Plan","from":"2026-10-20","to":"2026-10-03","category":"in_progress"},
	  {"kind":"milestone","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a05","title":"Beta","from":"2026-10-31","to":"2026-10-31"},
	  {"kind":"issue","id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a06","key":"CP-3","title":"Undated","from":"","to":""}
	]`
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatal(err)
	}
	got := buildCalendar("2026-10", items, "https://armature.example.com/")
	var keys []string
	for _, is := range got.Issues {
		keys = append(keys, is.Key+"@"+is.From+".."+is.To)
	}
	if want := []string{"CP-2@2026-10-03..2026-10-20", "CP-9@2026-10-15..2026-10-15", "CP-10@2026-10-15..2026-10-15"}; len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] || keys[2] != want[2] {
		t.Errorf("the month's issues are %v, want %v", keys, want)
	}
	if first := got.Issues[1]; !first.Done || first.Category != "done" || first.Summary != "Write the notes" || first.URL != "https://armature.example.com/issues/CP-9" {
		t.Errorf("an issue reads %+v", first)
	}
	if got.Month != "2026-10" {
		t.Errorf("the month is %q", got.Month)
	}
	if empty := buildCalendar("2026-11", nil, "https://armature.example.com"); empty.Issues == nil {
		t.Error("an empty month answers null rather than no issues")
	}
}

func TestACalendarIsCachedPerPersonProjectAndMonth(t *testing.T) {
	token := uuid.New()
	a := CalendarField(token, "CP", "2026-10")
	for _, other := range []string{CalendarField(uuid.New(), "CP", "2026-10"), CalendarField(token, "SEC", "2026-10"), CalendarField(token, "CP", "2026-11")} {
		if other == a {
			t.Errorf("two answers share the field %s", a)
		}
	}
	if a != CalendarField(token, "CP", "2026-10") {
		t.Error("the same month is cached under two fields")
	}
}
