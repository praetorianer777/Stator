package calendar

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func refusedOn(t *testing.T, err error, field string) {
	t.Helper()
	var refused *FieldError
	if !errors.As(err, &refused) || refused.Field != field || !strings.HasSuffix(refused.Message, ".") {
		t.Errorf("want a sentence on %s, got %v", field, err)
	}
}

func TestACalendarIsNamedInWords(t *testing.T) {
	if name, err := (CalendarInput{Name: "  Releases "}).Clean(); err != nil || name != "Releases" {
		t.Errorf("the name reads %q: %v", name, err)
	}
	_, err := CalendarInput{Name: "   "}.Clean()
	refusedOn(t, err, "name")
	_, err = CalendarInput{Name: strings.Repeat("ä", MaxNameLength+1)}.Clean()
	refusedOn(t, err, "name")
	if _, err := (CalendarInput{Name: strings.Repeat("ä", MaxNameLength)}).Clean(); err != nil {
		t.Errorf("a name of the longest length is refused: %v", err)
	}
}

func TestAnEventIsCleanedOrRefusedField(t *testing.T) {
	good := CalendarEventInput{Title: " Release ", AllDay: true, Start: at("2026-10-15T00:00:00Z"), End: at("2026-10-16T00:00:00Z")}
	clean, err := good.Clean()
	if err != nil || clean.Title != "Release" || clean.Kind != KindEvent {
		t.Fatalf("the event cleans to %+v: %v", clean, err)
	}
	// A time with an offset is the same instant, kept in UTC.
	timed := CalendarEventInput{Title: "Stand-up", Kind: KindEvent, Start: at("2026-10-15T09:00:00+02:00"), End: at("2026-10-15T09:15:00+02:00")}
	if clean, err := timed.Clean(); err != nil || clean.Start.Location() != time.UTC || clean.Start.Hour() != 7 {
		t.Errorf("a timed event cleans to %+v: %v", clean, err)
	}
	// An instant without length, such as a deadline, is an event too.
	instant := timed
	instant.End = instant.Start
	if _, err := instant.Clean(); err != nil {
		t.Errorf("an event without length is refused: %v", err)
	}
	for field, change := range map[string]func(*CalendarEventInput){
		"title": func(in *CalendarEventInput) { in.Title = "  " },
		"kind":  func(in *CalendarEventInput) { in.Kind = "holiday" },
		"start": func(in *CalendarEventInput) { in.Start = at("2026-10-15T08:00:00Z") },
		"end":   func(in *CalendarEventInput) { in.End = at("2026-10-14T00:00:00Z") },
	} {
		in := good
		change(&in)
		_, err := in.Clean()
		refusedOn(t, err, field)
	}
	long := good
	long.Title = strings.Repeat("x", MaxTitleLength+1)
	_, err = long.Clean()
	refusedOn(t, err, "title")
	missing := good
	missing.Start = time.Time{}
	_, err = missing.Clean()
	refusedOn(t, err, "start")
	missing = good
	missing.End = time.Time{}
	_, err = missing.Clean()
	refusedOn(t, err, "end")
	// An all day event in the middle of a day is not whole days.
	half := good
	half.End = at("2026-10-16T12:00:00Z")
	_, err = half.Clean()
	refusedOn(t, err, "end")
	year := good
	year.End = year.Start.Add(MaxEventDays * day)
	if _, err := year.Clean(); err != nil {
		t.Errorf("an event of the longest span is refused: %v", err)
	}
	year.End = year.End.Add(day)
	_, err = year.Clean()
	refusedOn(t, err, "end")
	absence := good
	absence.Kind = KindAbsence
	if clean, err := absence.Clean(); err != nil || clean.Kind != KindAbsence {
		t.Errorf("an absence cleans to %+v: %v", clean, err)
	}
}

func TestARangeIsTwoTimesAFewWeeksApart(t *testing.T) {
	from, to, err := Range("2026-10-01T00:00:00+02:00", "2026-11-01T00:00:00+01:00")
	if err != nil || !from.Equal(at("2026-09-30T22:00:00Z")) || !to.Equal(at("2026-10-31T23:00:00Z")) || from.Location() != time.UTC {
		t.Errorf("the range reads %v to %v: %v", from, to, err)
	}
	_, _, err = Range("October", "2026-11-01T00:00:00Z")
	refusedOn(t, err, "from")
	_, _, err = Range("2026-10-01T00:00:00Z", "")
	refusedOn(t, err, "to")
	_, _, err = Range("2026-10-01T00:00:00Z", "2026-10-01T00:00:00Z")
	refusedOn(t, err, "to")
	_, _, err = Range("2026-10-01T00:00:00Z", "2026-12-15T00:00:00Z")
	refusedOn(t, err, "to")
	if _, _, err := Range("2026-10-01T00:00:00Z", "2026-12-02T00:00:00Z"); err != nil {
		t.Errorf("a span of the most days is refused: %v", err)
	}
}

func TestAnEventCoversTheDaysItSpans(t *testing.T) {
	from, to := at("2026-10-01T00:00:00Z"), at("2026-11-01T00:00:00Z")
	for _, c := range []struct {
		what  string
		event CalendarEvent
		want  bool
	}{
		{"a day in the month", CalendarEvent{AllDay: true, Start: at("2026-10-15T00:00:00Z"), End: at("2026-10-15T00:00:00Z")}, true},
		{"the last day of the month before", CalendarEvent{AllDay: true, Start: at("2026-09-30T00:00:00Z"), End: at("2026-09-30T00:00:00Z")}, false},
		{"days that run into the month", CalendarEvent{AllDay: true, Start: at("2026-09-28T00:00:00Z"), End: at("2026-10-01T00:00:00Z")}, true},
		{"the first day of the next month", CalendarEvent{AllDay: true, Start: at("2026-11-01T00:00:00Z"), End: at("2026-11-02T00:00:00Z")}, false},
		{"a meeting that ends as the month starts", CalendarEvent{Start: at("2026-09-30T23:00:00Z"), End: at("2026-10-01T00:00:00Z")}, false},
		{"a meeting over midnight into the month", CalendarEvent{Start: at("2026-09-30T23:00:00Z"), End: at("2026-10-01T01:00:00Z")}, true},
		{"a deadline at the month's first instant", CalendarEvent{Start: from, End: from}, true},
		{"a deadline at the next month's first instant", CalendarEvent{Start: to, End: to}, false},
		{"a holiday around the whole month", CalendarEvent{AllDay: true, Start: at("2026-09-01T00:00:00Z"), End: at("2026-11-30T00:00:00Z")}, true},
	} {
		if got := c.event.Covers(from, to); got != c.want {
			t.Errorf("%s: covers %v, want %v", c.what, got, c.want)
		}
	}
}

func TestRefusalsAreSentences(t *testing.T) {
	if msg := (&FullError{}).Error(); !strings.HasSuffix(msg, ".") || !strings.Contains(msg, "20") {
		t.Errorf("a full space reads %q", msg)
	}
}
