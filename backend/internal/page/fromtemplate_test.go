package page

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/template"
)

func TestATemplateButtonNamesItsPage(t *testing.T) {
	notes := template.Template{Name: "Meeting notes", Title: "Meeting notes {date}"}
	guide := template.Template{Name: "How-to guide"}
	// Late in the evening west of UTC is already the next day in UTC.
	now := time.Date(2026, 10, 5, 22, 30, 0, 0, time.FixedZone("west", -4*3600))
	for _, c := range []struct {
		what    string
		pattern string
		tpl     template.Template
		want    string
	}{
		{"its own pattern, the day in UTC", "Weekly sync {date}", notes, "Weekly sync 2026-10-06"},
		{"its own words without a day", "  Kick-off  ", notes, "Kick-off"},
		{"the template's title", "", notes, "Meeting notes 2026-10-06"},
		{"the template's name when it has no title", " ", guide, "How-to guide"},
		{"every date token", "{date} to {date}", guide, "2026-10-06 to 2026-10-06"},
	} {
		if got := TitleFor(c.pattern, c.tpl, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}
}

func TestATemplateButtonRefusesATemplateThatIsNotThere(t *testing.T) {
	// A built-in's key is no id, so it is found without the database.
	_, err := templateFor(context.Background(), nil, &space.Space{}, "no-such-template")
	var field *FieldError
	if !errors.As(err, &field) || field.Field != "template" {
		t.Fatalf("a missing template is %v", err)
	}
	if tpl, err := templateFor(context.Background(), nil, &space.Space{}, "meeting-notes"); err != nil || tpl.Name == "" {
		t.Errorf("a built-in is %+v, %v", tpl, err)
	}
}

func TestAContributorsBlockAsksForWhatItMayHold(t *testing.T) {
	for _, c := range []struct {
		q     ContributorsQuery
		field string
	}{
		{ContributorsQuery{Scope: document.ContributorsPage, Limit: 1}, ""},
		{ContributorsQuery{Scope: document.ContributorsTree, Limit: document.MaxContributors}, ""},
		{ContributorsQuery{Scope: "space", Limit: 10}, "scope"},
		{ContributorsQuery{Scope: document.ContributorsPage, Limit: 0}, "limit"},
		{ContributorsQuery{Scope: document.ContributorsPage, Limit: document.MaxContributors + 1}, "limit"},
	} {
		err := c.q.Check()
		var field *FieldError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%+v is refused: %v", c.q, err)
		case c.field != "" && (!errors.As(err, &field) || field.Field != c.field):
			t.Errorf("%+v is %v, want a refusal of %s", c.q, err, c.field)
		}
	}
}
