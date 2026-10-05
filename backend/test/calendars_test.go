//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/calendar"
)

// Team calendars (#60): a space's calendars and their events, read by
// everybody who reads the space and kept by whoever may add pages to it.

func calendarsPath(key string) string { return "/api/v1/spaces/" + key + "/calendars" }

func calendarPath(id string, rest ...string) string {
	return "/api/v1/calendars/" + id + strings.Join(rest, "")
}

func eventsPath(id, from, to string) string {
	return calendarPath(id, "/events?"+url.Values{"from": {from}, "to": {to}}.Encode())
}

// eventTitles lists the titles of a calendar's events in a span, in the
// order they are answered, and the whole answer.
func eventTitles(t *testing.T, c *client, id, from, to string) ([]string, map[string]any) {
	t.Helper()
	r := want(t, c.get(t, eventsPath(id, from, to)), http.StatusOK, "the events of "+from)
	var titles []string
	for _, each := range list(t, r, "events") {
		titles = append(titles, each.(map[string]any)["title"].(string))
	}
	return titles, r.Body
}

func allDay(title, kind, first, last string) map[string]any {
	return map[string]any{"title": title, "kind": kind, "allDay": true, "start": first + "T00:00:00Z", "end": last + "T00:00:00Z"}
}

func TestTeamCalendarsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "calendars")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.namedPerson(t, org.org, "Ann Planner")
	readerID := h.addPerson(t, org.org, "member")
	ann, reader := api.as(t, annID, org.org, slug), api.as(t, readerID, org.org, slug)

	newTree(t, owner, "CAL", "Calendars")
	want(t, owner.put(t, "/api/v1/spaces/CAL/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everybody reads CAL, ann adds pages")
	h.settle(t)

	var team string
	t.Run("whoever may add pages adds a calendar, named once per space", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, calendarsPath("CAL"), map[string]any{"name": "  Team "}), http.StatusCreated, "ann adds Team"), "calendar")
		if made["name"] != "Team" || made["spaceKey"] != "CAL" || made["spaceName"] != "Calendars" || made["canEdit"] != true {
			t.Fatalf("the calendar reads %v", made)
		}
		team = made["id"].(string)
		fieldError(t, want(t, ann.post(t, calendarsPath("CAL"), map[string]any{"name": "TEAM"}), http.StatusUnprocessableEntity, "a second Team"), "name")
		fieldError(t, want(t, ann.post(t, calendarsPath("CAL"), map[string]any{"name": "   "}), http.StatusUnprocessableEntity, "a calendar without a name"), "name")
		want(t, ann.post(t, calendarsPath("CAL"), map[string]any{"name": "Team", "colour": "red"}), http.StatusBadRequest, "a field nobody knows")
		r := want(t, reader.post(t, calendarsPath("CAL"), map[string]any{"name": "Mine"}), http.StatusForbidden, "a reader adds a calendar")
		if msg := obj(t, r, "error")["message"].(string); !strings.Contains(msg, "calendars") {
			t.Errorf("the refusal reads %q", msg)
		}
		want(t, ann.post(t, calendarsPath("NOPE"), map[string]any{"name": "Lost"}), http.StatusNotFound, "a calendar in no space")
		h.settle(t)
		calendars := list(t, want(t, reader.get(t, calendarsPath("CAL")), http.StatusOK, "the reader lists"), "calendars")
		if len(calendars) != 1 || calendars[0].(map[string]any)["name"] != "Team" || calendars[0].(map[string]any)["canEdit"] != false {
			t.Errorf("the reader's calendars are %v", calendars)
		}
		want(t, reader.get(t, calendarsPath("NOPE")), http.StatusNotFound, "the calendars of no space")
	})

	ids := map[string]string{}
	t.Run("events and absences go in over whole days or between two times", func(t *testing.T) {
		for _, body := range []map[string]any{
			allDay("Release", "event", "2026-10-15", "2026-10-15"),
			allDay("Ann away", "absence", "2026-10-28", "2026-11-03"),
			{"title": "Stand-up", "allDay": false, "start": "2026-10-15T09:00:00+02:00", "end": "2026-10-15T09:15:00+02:00"},
			allDay("Kick-off", "event", "2026-09-10", "2026-09-10"),
		} {
			e := obj(t, want(t, ann.post(t, calendarPath(team, "/events"), body), http.StatusCreated, "add "+body["title"].(string)), "event")
			ids[e["title"].(string)] = e["id"].(string)
			if e["createdByName"] != "Ann Planner" || e["calendarId"] != team {
				t.Errorf("the event reads %v", e)
			}
		}
		fieldError(t, want(t, ann.post(t, calendarPath(team, "/events"), map[string]any{"title": "Half", "allDay": true, "start": "2026-10-15T10:00:00Z", "end": "2026-10-15T10:00:00Z"}), http.StatusUnprocessableEntity, "a whole day at ten"), "start")
		fieldError(t, want(t, ann.post(t, calendarPath(team, "/events"), allDay("Backwards", "event", "2026-10-15", "2026-10-14")), http.StatusUnprocessableEntity, "an event that ends before it starts"), "end")
		fieldError(t, want(t, ann.post(t, calendarPath(team, "/events"), allDay("Holiday", "holiday", "2026-10-15", "2026-10-15")), http.StatusUnprocessableEntity, "a kind of no kind"), "kind")
		fieldError(t, want(t, ann.post(t, calendarPath(team, "/events"), allDay(" ", "event", "2026-10-15", "2026-10-15")), http.StatusUnprocessableEntity, "an event without a title"), "title")
		want(t, reader.post(t, calendarPath(team, "/events"), allDay("Mine", "event", "2026-10-15", "2026-10-15")), http.StatusForbidden, "a reader adds an event")
		want(t, ann.post(t, calendarPath(uuid.NewString(), "/events"), allDay("Lost", "event", "2026-10-15", "2026-10-15")), http.StatusNotFound, "an event in no calendar")
	})

	t.Run("a month holds what falls in it, by when it starts", func(t *testing.T) {
		h.settle(t)
		october, body := eventTitles(t, reader, team, "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z")
		sameList(t, "October", october, "Release", "Stand-up", "Ann away")
		if c := body["calendar"].(map[string]any); c["name"] != "Team" || c["canEdit"] != false || body["truncated"] != false {
			t.Errorf("the reader's October reads %v", body)
		}
		november, _ := eventTitles(t, ann, team, "2026-11-01T00:00:00Z", "2026-12-01T00:00:00Z")
		sameList(t, "November", november, "Ann away")
		// A reader east of UTC asks for their own midnights, and the
		// whole day before the month is still left out.
		local, _ := eventTitles(t, ann, team, "2026-09-30T22:00:00Z", "2026-10-15T22:00:00Z")
		sameList(t, "October to the 15th in Berlin", local, "Release", "Stand-up")
		for _, events := range list(t, want(t, ann.get(t, eventsPath(team, "2026-10-15T00:00:00Z", "2026-10-16T00:00:00Z")), http.StatusOK, "the 15th"), "events") {
			e := events.(map[string]any)
			if e["title"] == "Stand-up" && (e["start"] != "2026-10-15T07:00:00Z" || e["allDay"] != false || e["kind"] != "event") {
				t.Errorf("the stand-up reads %v", e)
			}
			if e["title"] == "Release" && (e["start"] != "2026-10-15T00:00:00Z" || e["end"] != "2026-10-15T00:00:00Z" || e["allDay"] != true) {
				t.Errorf("the release reads %v", e)
			}
		}
		fieldError(t, want(t, ann.get(t, eventsPath(team, "2026-10-01T00:00:00Z", "2026-09-01T00:00:00Z")), http.StatusUnprocessableEntity, "a span backwards"), "to")
		fieldError(t, want(t, ann.get(t, eventsPath(team, "2026-10-01T00:00:00Z", "2027-01-01T00:00:00Z")), http.StatusUnprocessableEntity, "a span of a quarter"), "to")
		fieldError(t, want(t, ann.get(t, eventsPath(team, "October", "2026-11-01T00:00:00Z")), http.StatusUnprocessableEntity, "a span from no time"), "from")
		want(t, ann.get(t, eventsPath(uuid.NewString(), "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z")), http.StatusNotFound, "the events of no calendar")
	})

	t.Run("whoever may add pages changes an event, and nobody else", func(t *testing.T) {
		moved := obj(t, want(t, ann.put(t, calendarPath(team, "/events/", ids["Stand-up"]), map[string]any{
			"title": "Stand-up", "allDay": false, "start": "2026-10-16T07:00:00Z", "end": "2026-10-16T07:15:00Z",
		}), http.StatusOK, "move the stand-up"), "event")
		if moved["start"] != "2026-10-16T07:00:00Z" {
			t.Errorf("the moved stand-up reads %v", moved)
		}
		want(t, reader.put(t, calendarPath(team, "/events/", ids["Release"]), allDay("Mine", "event", "2026-10-15", "2026-10-15")), http.StatusForbidden, "a reader changes an event")
		want(t, ann.put(t, calendarPath(team, "/events/", uuid.NewString()), allDay("Lost", "event", "2026-10-15", "2026-10-15")), http.StatusNotFound, "change no event")
		fieldError(t, want(t, ann.put(t, calendarPath(team, "/events/", ids["Release"]), allDay("Release", "event", "2026-10-15", "2028-10-15")), http.StatusUnprocessableEntity, "an event of two years"), "end")
		h.settle(t)
		got, _ := eventTitles(t, reader, team, "2026-10-16T00:00:00Z", "2026-10-17T00:00:00Z")
		sameList(t, "the 16th", got, "Stand-up")
	})

	t.Run("an archived space's calendars change no more", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/CAL/archive", nil), http.StatusOK, "archive CAL")
		r := ann.post(t, calendarPath(team, "/events"), allDay("Late", "event", "2026-10-20", "2026-10-20"))
		want(t, owner.delete(t, "/api/v1/spaces/CAL/archive"), http.StatusOK, "unarchive CAL")
		if r.Status != http.StatusConflict || errorCode(t, r) != "archived" {
			t.Errorf("ann adds to an archived space: %d %s", r.Status, r.Raw)
		}
	})

	t.Run("a calendar is renamed and removed with its events", func(t *testing.T) {
		renamed := obj(t, want(t, ann.patch(t, calendarPath(team), map[string]any{"name": "Team plans"}), http.StatusOK, "rename Team"), "calendar")
		if renamed["name"] != "Team plans" {
			t.Errorf("the renamed calendar reads %v", renamed)
		}
		want(t, reader.patch(t, calendarPath(team), map[string]any{"name": "Mine"}), http.StatusForbidden, "a reader renames")
		want(t, ann.patch(t, calendarPath("nope"), map[string]any{"name": "Mine"}), http.StatusBadRequest, "rename no id")

		want(t, reader.delete(t, calendarPath(team, "/events/", ids["Kick-off"])), http.StatusForbidden, "a reader removes an event")
		want(t, ann.delete(t, calendarPath(team, "/events/", ids["Kick-off"])), http.StatusNoContent, "ann removes the kick-off")
		want(t, ann.delete(t, calendarPath(team, "/events/", ids["Kick-off"])), http.StatusNotFound, "remove it again")

		want(t, reader.delete(t, calendarPath(team)), http.StatusForbidden, "a reader removes the calendar")
		want(t, ann.delete(t, calendarPath(team)), http.StatusNoContent, "ann removes the calendar")
		want(t, ann.delete(t, calendarPath(team)), http.StatusNotFound, "remove it again")
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM calendar_event WHERE calendar_id = $1`, team).Scan(&left); err != nil || left != 0 {
			t.Errorf("a removed calendar left %d events (%v)", left, err)
		}
	})

	t.Run("another organization finds nothing", func(t *testing.T) {
		away := h.makeMember(t, "calendars-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		other := obj(t, want(t, owner.post(t, calendarsPath("CAL"), map[string]any{"name": "Ops"}), http.StatusCreated, "add Ops"), "calendar")["id"].(string)
		want(t, stranger.get(t, calendarsPath("CAL")), http.StatusNotFound, "a stranger lists")
		want(t, stranger.get(t, eventsPath(other, "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z")), http.StatusNotFound, "a stranger reads")
		want(t, stranger.post(t, calendarPath(other, "/events"), allDay("Mine", "event", "2026-10-15", "2026-10-15")), http.StatusNotFound, "a stranger adds")
		want(t, api.anonymous().get(t, calendarsPath("CAL")), http.StatusUnauthorized, "nobody lists")
	})
}

// A space holds at most calendar.MaxPerSpace calendars, and the refusal says
// what to do.
func TestASpaceHoldsAFewCalendars(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "calendars-full")
	owner := api.as(t, org.user, org.org, h.slugOf(t, org.org))
	newTree(t, owner, "CFU", "Full")
	for i := range calendar.MaxPerSpace {
		want(t, owner.post(t, calendarsPath("CFU"), map[string]any{"name": fmt.Sprintf("Calendar %d", i)}), http.StatusCreated, "add one more")
	}
	r := want(t, owner.post(t, calendarsPath("CFU"), map[string]any{"name": "One too many"}), http.StatusConflict, "add one too many")
	if msg := obj(t, r, "error")["message"].(string); msg != (&calendar.FullError{}).Error() {
		t.Errorf("the refusal reads %q", msg)
	}
	var max int
	if err := h.super.QueryRow(context.Background(), `SELECT calendar_max()`).Scan(&max); err != nil || max != calendar.MaxPerSpace {
		t.Errorf("the database holds a space to %d calendars, the service to %d (%v)", max, calendar.MaxPerSpace, err)
	}
}

// Straight through SQL as stator_app: a reader changes no calendar, a
// stranger reads none, an editor keeps an event to whole days, its order and
// its length, and to its calendar, nobody signs one in another's name, and
// an archived space freezes them.
func TestCalendarsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "calendar-wall")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.addPerson(t, org.org, "member")
	readerID := h.addPerson(t, org.org, "member")
	away := h.makeMember(t, "calendar-wall-away")

	walled := newTree(t, owner, "CWL", "Walled calendars")
	other := newTree(t, owner, "CWO", "Other calendars")
	want(t, owner.put(t, "/api/v1/spaces/CWL/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everybody reads CWL, ann adds pages")
	team := obj(t, want(t, owner.post(t, calendarsPath("CWL"), map[string]any{"name": "Team"}), http.StatusCreated, "add Team"), "calendar")["id"].(string)
	elsewhere := obj(t, want(t, owner.post(t, calendarsPath("CWO"), map[string]any{"name": "Elsewhere"}), http.StatusCreated, "add Elsewhere"), "calendar")["id"].(string)
	want(t, owner.post(t, calendarPath(team, "/events"), allDay("Release", "event", "2026-10-15", "2026-10-15")), http.StatusCreated, "add the release")
	h.settle(t)
	spaceID, otherID := walled.spaceID(t), other.spaceID(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	insertEvent := `INSERT INTO calendar_event (org_id, space_id, calendar_id, title, kind, all_day, starts_at, ends_at) VALUES ($1, $2, $3, $4, 'event', $5, $6, $7)`

	t.Run("a reader reads and changes nothing", func(t *testing.T) {
		actAs(t, conn, org.org, readerID)
		if n := count(`SELECT count(*) FROM calendar_event WHERE calendar_id = $1`, team); n != 1 {
			t.Errorf("the reader reads %d events", n)
		}
		denied(t, conn, "a reader adds a calendar", `INSERT INTO calendar (org_id, space_id, name) VALUES ($1, $2, 'Mine')`, org.org, spaceID)
		denied(t, conn, "a reader adds an event", insertEvent, org.org, spaceID, team, "Mine", true, "2026-10-16T00:00:00Z", "2026-10-16T00:00:00Z")
		untouched(t, conn, "a reader renames", `UPDATE calendar SET name = 'Mine' WHERE id = $1`, team)
		untouched(t, conn, "a reader retitles", `UPDATE calendar_event SET title = 'Mine' WHERE calendar_id = $1`, team)
		untouched(t, conn, "a reader removes events", `DELETE FROM calendar_event WHERE calendar_id = $1`, team)
		untouched(t, conn, "a reader removes the calendar", `DELETE FROM calendar WHERE id = $1`, team)
	})

	t.Run("a stranger reads nothing", func(t *testing.T) {
		actAs(t, conn, away.org, away.user)
		if n := count(`SELECT count(*) FROM calendar WHERE id = $1`, team) + count(`SELECT count(*) FROM calendar_event WHERE calendar_id = $1`, team); n != 0 {
			t.Errorf("a stranger reads %d rows", n)
		}
	})

	t.Run("an editor keeps events to their shape, their calendar and their author", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		if _, err := conn.Exec(ctx, insertEvent, org.org, spaceID, team, "Planning", false, "2026-10-16T09:00:00Z", "2026-10-16T10:00:00Z"); err != nil {
			t.Fatalf("ann cannot add an event: %v", err)
		}
		var author uuid.UUID
		if err := conn.QueryRow(ctx, `SELECT created_by FROM calendar_event WHERE title = 'Planning' AND calendar_id = $1`, team).Scan(&author); err != nil || author != annID {
			t.Errorf("the event was signed by %v (%v)", author, err)
		}
		denied(t, conn, "an all day event at ten", insertEvent, org.org, spaceID, team, "Half", true, "2026-10-16T10:00:00Z", "2026-10-16T10:00:00Z")
		denied(t, conn, "an event that ends before it starts", insertEvent, org.org, spaceID, team, "Backwards", false, "2026-10-16T10:00:00Z", "2026-10-16T09:00:00Z")
		// calendar.MaxEventDays is the database's longest event to the day.
		if _, err := conn.Exec(ctx, insertEvent, org.org, spaceID, team, "Year", true, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("ann cannot add a day: %v", err)
		}
		longest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, calendar.MaxEventDays)
		if _, err := conn.Exec(ctx, `UPDATE calendar_event SET ends_at = $2 WHERE calendar_id = $1 AND title = 'Year'`, team, longest); err != nil {
			t.Errorf("an event of %d days is refused: %v", calendar.MaxEventDays, err)
		}
		denied(t, conn, "an event a day longer than the longest", `UPDATE calendar_event SET ends_at = $2 WHERE calendar_id = $1 AND title = 'Year'`, team, longest.AddDate(0, 0, 1))
		denied(t, conn, "an event of two years", insertEvent, org.org, spaceID, team, "Long", true, "2026-01-01T00:00:00Z", "2028-01-01T00:00:00Z")
		denied(t, conn, "an event without a title", insertEvent, org.org, spaceID, team, " ", true, "2026-10-16T00:00:00Z", "2026-10-16T00:00:00Z")
		denied(t, conn, "an event of no kind", `INSERT INTO calendar_event (org_id, space_id, calendar_id, title, kind, all_day, starts_at, ends_at) VALUES ($1, $2, $3, 'Odd', 'holiday', true, '2026-10-16', '2026-10-16')`, org.org, spaceID, team)
		denied(t, conn, "an event in somebody else's name", `INSERT INTO calendar_event (org_id, space_id, calendar_id, title, all_day, starts_at, ends_at, created_by) VALUES ($1, $2, $3, 'Forged', true, '2026-10-16', '2026-10-16', $4)`, org.org, spaceID, team, readerID)
		denied(t, conn, "an event moved to another calendar", `UPDATE calendar_event SET calendar_id = $2 WHERE calendar_id = $1`, team, elsewhere)
		denied(t, conn, "an event moved to another space", `UPDATE calendar_event SET space_id = $2 WHERE calendar_id = $1`, team, otherID)
		denied(t, conn, "a calendar moved to another space", `UPDATE calendar SET space_id = $2 WHERE id = $1`, team, otherID)
		refused(t, conn, "an event whose space is not its calendar's", insertEvent, org.org, spaceID, elsewhere, "Astray", true, "2026-10-16T00:00:00Z", "2026-10-16T00:00:00Z")
		refused(t, conn, "a second calendar of one name", `INSERT INTO calendar (org_id, space_id, name) VALUES ($1, $2, 'team')`, org.org, spaceID)
		if _, err := conn.Exec(ctx, `UPDATE calendar_event SET title = 'Release day' WHERE calendar_id = $1 AND title = 'Release'`, team); err != nil {
			t.Errorf("ann cannot retitle: %v", err)
		}
	})

	t.Run("an archived space freezes its calendars", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/CWL/archive", nil), http.StatusOK, "archive CWL")
		actAs(t, conn, org.org, annID)
		denied(t, conn, "an event in an archived space", insertEvent, org.org, spaceID, team, "Late", true, "2026-10-17T00:00:00Z", "2026-10-17T00:00:00Z")
		untouched(t, conn, "a retitle in an archived space", `UPDATE calendar_event SET title = 'Late' WHERE calendar_id = $1`, team)
		want(t, owner.delete(t, "/api/v1/spaces/CWL/archive"), http.StatusOK, "unarchive CWL")
	})

	t.Run("the limit holds whatever path adds them", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		refused(t, conn, "a space filled past its limit", `
			INSERT INTO calendar (org_id, space_id, name)
			SELECT $1, $2, 'Calendar ' || n FROM generate_series(1, calendar_max() + 1) AS n`, org.org, otherID)
		if n := count(`SELECT count(*) FROM calendar WHERE space_id = $1`, otherID); n != 1 {
			t.Errorf("a refused statement left %d calendars", n)
		}
	})
}

// The calendar block's Armature source: a project's dated issues in a month,
// read with each reader's own token.
func TestACalendarMonthOfArmatureIssues(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-cal")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bob := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	monthPath := func(project, month string) string {
		return "/api/v1/armature/calendar?" + url.Values{"project": {project}, "month": {month}}.Encode()
	}
	month := func(c *client, path string) (string, map[string]any) {
		t.Helper()
		r := want(t, c.get(t, path), http.StatusOK, "the month "+path)
		status, _ := r.Body["status"].(string)
		got, _ := r.Body["month"].(map[string]any)
		return status, got
	}

	if status, got := month(bob, monthPath("CP", "2026-10")); status != "not_configured" || got != nil {
		t.Errorf("before a connection the month is %s %v", status, got)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	stubControl(t, http.MethodPatch, slug+"/issues/CP-1", map[string]any{"startDate": "2026-10-01", "dueDate": "2026-10-09"})
	h.settle(t)

	status, got := month(bob, monthPath("CP", "2026-10"))
	var dated []string
	for _, each := range got["issues"].([]any) {
		is := each.(map[string]any)
		dated = append(dated, fmt.Sprint(is["key"], "@", is["from"], "..", is["to"]))
		if !strings.HasSuffix(is["url"].(string), "/issues/"+is["key"].(string)) {
			t.Errorf("the issue opens at %v", is["url"])
		}
	}
	if status != "ok" || got["month"] != "2026-10" {
		t.Fatalf("bob's October is %s %v", status, got)
	}
	sameList(t, "October in CP", dated, "CP-1@2026-10-01..2026-10-09", "CP-4@2026-10-15..2026-10-15")
	if _, got := month(bob, monthPath("CP", "2026-11")); len(got["issues"].([]any)) != 0 {
		t.Errorf("November in CP holds %v", got["issues"])
	}

	fieldError(t, want(t, bob.get(t, monthPath("SEC", "2026-10")), http.StatusUnprocessableEntity, "bob's month of SEC"), "project")
	if status, _ := month(owner, monthPath("SEC", "2026-10")); status != "ok" {
		t.Errorf("the owner's month of SEC is %s", status)
	}
	fieldError(t, want(t, bob.get(t, monthPath("CP", "October")), http.StatusUnprocessableEntity, "a month of no shape"), "month")
	fieldError(t, want(t, bob.get(t, monthPath("cp", "2026-10")), http.StatusUnprocessableEntity, "a project in lower case"), "project")
	if status, got := month(carol, monthPath("CP", "2026-10")); status != "not_connected" || got != nil {
		t.Errorf("carol without a token reads %s %v", status, got)
	}
}
