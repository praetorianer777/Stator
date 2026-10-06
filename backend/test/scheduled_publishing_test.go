//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// scheduleWait bounds how long a test waits for a due schedule to be taken,
// by its own watch or by the stack's worker, whichever comes first.
const scheduleWait = 30 * time.Second

func inAnHour() string { return time.Now().Add(time.Hour).UTC().Format(time.RFC3339) }

func schedule(t *testing.T, c *client, id string, at string, more ...map[string]any) response {
	t.Helper()
	body := map[string]any{"publishAt": at}
	for _, m := range more {
		for k, v := range m {
			body[k] = v
		}
	}
	return c.put(t, pagePath(id, "/schedule"), body)
}

func scheduleOn(t *testing.T, c *client, id string) map[string]any {
	t.Helper()
	s, _ := obj(t, want(t, c.get(t, pagePath(id)), http.StatusOK, "read the page"), "page")["schedule"].(map[string]any)
	return s
}

func draftOn(t *testing.T, c *client, id, title, text string) {
	t.Helper()
	version := number(obj(t, want(t, c.get(t, pagePath(id)), http.StatusOK, "read the page"), "page")["version"])
	want(t, c.put(t, pagePath(id, "/draft"), map[string]any{"title": title, "body": textDoc(text), "baseVersion": version}), http.StatusOK, "draft "+title)
}

// comeDue moves a page's schedule back by ago, as if its time came then,
// or passed while no worker ran.
func (h *harness) comeDue(t *testing.T, pageID string, ago time.Duration) {
	t.Helper()
	tag, err := h.super.Exec(context.Background(), `
		UPDATE page_schedule SET publish_at = now() - make_interval(secs => $2) WHERE page_id = $1`, pageID, ago.Seconds())
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("bring the schedule of %s due: %d rows, %v", pageID, tag.RowsAffected(), err)
	}
}

// taken runs the watch until no due schedule of the page waits; the stack's
// worker may take it first, and its commit is what is waited for.
func (h *harness) taken(t *testing.T, watch *page.ScheduleWatch, pageIDs ...string) {
	t.Helper()
	deadline := time.Now().Add(scheduleWait)
	for {
		if _, err := watch.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		waiting := 0
		for _, id := range pageIDs {
			waiting += h.countRows(t, `SELECT count(*) FROM page_schedule WHERE page_id = $1 AND failed_at IS NULL AND publish_at <= now()`, id)
		}
		if waiting == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d due schedules were not taken within %s", waiting, scheduleWait)
		}
		time.Sleep(100 * time.Millisecond)
	}
	h.settle(t)
}

// A draft is scheduled by its author, shown to the page's editors, published
// at its time as it then stands in the author's name, and told as a publish;
// one that cannot go out is kept with why, and its author told once.
func TestScheduledPublishingOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "scheduling")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.namedPerson(t, home.org, "Ann Planner"), h.namedPerson(t, home.org, "Ben Reader")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	watch := page.NewScheduleWatch(h.cluster, discard(), time.Hour)

	docs := newTree(t, owner, "SCHED", "Scheduling")
	news := docs.add(docs.homeID, "News")
	want(t, restrict(t, owner, news, nil, []any{user(home.user), user(annID)}), http.StatusOK, "keep editing to the owner and ann")
	want(t, ben.put(t, pagePath(news, "/watch"), map[string]any{}), http.StatusOK, "ben watches the news")

	t.Run("a schedule takes an editor's draft and a time ahead, and is the page's one", func(t *testing.T) {
		if got := schedule(t, ann, news, inAnHour()); got.Status != http.StatusConflict || errorCode(t, got) != "no_draft" {
			t.Errorf("scheduling without a draft answered %d %s", got.Status, got.Raw)
		}
		draftOn(t, ann, news, "News", "Launch on Monday")
		for what, at := range map[string]string{
			"a time past":            time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"a time beyond a year":   time.Now().Add(page.MaxScheduleAhead + 24*time.Hour).UTC().Format(time.RFC3339),
			"a time that is no time": "next week",
		} {
			if got := schedule(t, ann, news, at); got.Status != http.StatusUnprocessableEntity && got.Status != http.StatusBadRequest {
				t.Errorf("%s answered %d %s", what, got.Status, got.Raw)
			}
		}
		if got := schedule(t, ben, news, inAnHour()); got.Status != http.StatusForbidden {
			t.Errorf("a reader scheduled a publish: %d %s", got.Status, got.Raw)
		}

		// An offset is an instant like any other: what is stored is when.
		berlin := time.FixedZone("CEST", 2*60*60)
		at := time.Now().Add(2 * time.Hour).Truncate(time.Second).In(berlin)
		got := obj(t, want(t, schedule(t, ann, news, at.Format(time.RFC3339), map[string]any{"comment": "Launch", "notifyWatchers": true}),
			http.StatusOK, "ann schedules"), "schedule")
		if got["mine"] != true || got["authorName"] != "Ann Planner" || got["comment"] != "Launch" || got["failure"] != nil {
			t.Errorf("the schedule reads %v", got)
		}
		if when, err := time.Parse(time.RFC3339, got["publishAt"].(string)); err != nil || !when.Equal(at) {
			t.Errorf("the schedule is for %v, want %s", got["publishAt"], at)
		}

		if s := scheduleOn(t, owner, news); s == nil || s["mine"] != false || s["authorName"] != "Ann Planner" {
			t.Errorf("an editor reads the schedule as %v", s)
		}
		if s := scheduleOn(t, ben, news); s != nil {
			t.Errorf("a reader reads the schedule as %v", s)
		}

		draftOn(t, owner, news, "News", "The owner's take")
		taken := schedule(t, owner, news, inAnHour())
		if taken.Status != http.StatusConflict || errorCode(t, taken) != "schedule_taken" || !strings.Contains(obj(t, taken, "error")["message"].(string), "Ann Planner") {
			t.Errorf("a second schedule answered %d %s", taken.Status, taken.Raw)
		}
		want(t, owner.delete(t, pagePath(news, "/draft")), http.StatusNoContent, "the owner drops his draft")

		want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann moves her schedule")
		if n := h.countRows(t, `SELECT count(*) FROM page_schedule WHERE page_id = $1`, news); n != 1 {
			t.Errorf("the page holds %d schedules", n)
		}
	})

	t.Run("at its time the draft as it then stands goes out in the author's name, and the watchers hear", func(t *testing.T) {
		draftOn(t, ann, news, "News: launch", "Launch on Tuesday, final")
		want(t, schedule(t, ann, news, inAnHour(), map[string]any{"comment": "Launch", "notifyWatchers": true}), http.StatusOK, "ann schedules again")
		h.comeDue(t, news, time.Second)
		h.taken(t, watch, news)

		read := obj(t, want(t, ben.get(t, pagePath(news)), http.StatusOK, "ben reads"), "page")
		if number(read["version"]) != 2 || read["title"] != "News: launch" || !strings.Contains(mustJSON(t, read["body"]), "Launch on Tuesday, final") {
			t.Fatalf("after its time the page reads %v", read)
		}
		latest := list(t, want(t, ben.get(t, pagePath(news, "/versions")), http.StatusOK, "history"), "versions")[0].(map[string]any)
		if latest["authorName"] != "Ann Planner" || latest["comment"] != "Launch" {
			t.Errorf("the scheduled version reads %v", latest)
		}
		after := obj(t, want(t, ann.get(t, pagePath(news)), http.StatusOK, "ann reads"), "page")
		if after["draft"] != nil || after["schedule"] != nil {
			t.Errorf("ann's draft or schedule outlived the publish: %v %v", after["draft"], after["schedule"])
		}
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2 AND payload->>'pageId' = $3`,
			home.org, events.TopicPagePublished, news); n != 2 {
			t.Errorf("%d publishes of the page were announced, want the first and the scheduled one", n)
		}
		var heard []map[string]any
		for _, n := range notificationsOf(t, ben, "") {
			if n["kind"] == "published" {
				heard = append(heard, n)
			}
		}
		if len(heard) != 1 || heard[0]["actorName"] != "Ann Planner" || number(heard[0]["version"]) != 2 {
			t.Errorf("ben heard %v", heard)
		}
	})

	t.Run("a schedule is called off by its author or an editor, or with its draft", func(t *testing.T) {
		draftOn(t, ann, news, "News", "Maybe later")
		want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann schedules")
		if got := ben.delete(t, pagePath(news, "/schedule")); got.Status != http.StatusNotFound {
			t.Errorf("a reader called the schedule off: %d %s", got.Status, got.Raw)
		}
		want(t, owner.delete(t, pagePath(news, "/schedule")), http.StatusNoContent, "the owner calls it off")
		if d := want(t, ann.get(t, pagePath(news, "/draft")), http.StatusOK, "ann's draft").Body["draft"]; d == nil {
			t.Error("calling the schedule off took ann's draft")
		}
		if got := owner.delete(t, pagePath(news, "/schedule")); got.Status != http.StatusNotFound || errorCode(t, got) != "not_found" {
			t.Errorf("calling off nothing answered %d %s", got.Status, got.Raw)
		}
		want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann schedules again")
		want(t, ann.delete(t, pagePath(news, "/draft")), http.StatusNoContent, "ann discards her draft")
		if s := scheduleOn(t, owner, news); s != nil {
			t.Errorf("a schedule outlived its draft: %v", s)
		}
	})

	failuresOf := func(c *client) int { return notificationsOfKind(t, c, "failed") }

	t.Run("a publish refused at its time is kept with why, and its author is told once", func(t *testing.T) {
		draftOn(t, ann, news, "News", "Ann's plan")
		want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann schedules")
		draftOn(t, owner, news, "News", "The owner got there first")
		want(t, owner.post(t, pagePath(news, "/publish"), map[string]any{}), http.StatusOK, "the owner publishes over it")
		h.comeDue(t, news, time.Second)
		h.taken(t, watch, news)
		h.taken(t, watch, news)
		h.drained(t, home.org)

		s := scheduleOn(t, ann, news)
		if s == nil || s["failure"] != "conflict" || s["failedAt"] == nil {
			t.Fatalf("a schedule overtaken reads %v", s)
		}
		if v := number(obj(t, want(t, ann.get(t, pagePath(news)), http.StatusOK, "read"), "page")["version"]); v != 3 {
			t.Errorf("the page is at version %d", v)
		}
		if n := failuresOf(ann); n != 1 {
			t.Errorf("ann was told %d times", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, home.org, events.TopicScheduleFailed); n != 1 {
			t.Errorf("the failure was recorded %d times", n)
		}

		// Setting it again asks for a draft of the page as it is now.
		if got := schedule(t, ann, news, inAnHour()); got.Status != http.StatusConflict || errorCode(t, got) != "publish_conflict" {
			t.Errorf("rescheduling an overtaken draft answered %d %s", got.Status, got.Raw)
		}
		draftOn(t, ann, news, "News", "Ann's plan, again")
		if s := obj(t, want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann schedules again"), "schedule"); s["failure"] != nil {
			t.Errorf("a schedule set again still reads as failed: %v", s)
		}

		// An author who may no longer edit keeps seeing why.
		want(t, restrict(t, owner, news, nil, []any{user(home.user)}), http.StatusOK, "editing is the owner's alone")
		h.comeDue(t, news, time.Second)
		h.taken(t, watch, news)
		h.drained(t, home.org)
		if s := scheduleOn(t, ann, news); s == nil || s["failure"] != "forbidden" {
			t.Errorf("the schedule of an author who lost edit reads %v", s)
		}
		if n := failuresOf(ann); n != 2 {
			t.Errorf("ann was told %d times, want twice", n)
		}

		// A failed schedule is no one's to keep: an editor schedules over it.
		draftOn(t, owner, news, "News", "The owner's announcement")
		want(t, schedule(t, owner, news, inAnHour()), http.StatusOK, "the owner schedules over the failed one")
		want(t, owner.put(t, pagePath(news, "/archive"), nil), http.StatusOK, "archive the page")
		h.comeDue(t, news, time.Second)
		h.taken(t, watch, news)
		if s := scheduleOn(t, owner, news); s == nil || s["failure"] != "archived" || s["mine"] != true {
			t.Errorf("the schedule of an archived page reads %v", s)
		}
		want(t, owner.delete(t, pagePath(news, "/archive")), http.StatusOK, "unarchive the page")
		want(t, owner.delete(t, pagePath(news, "/schedule")), http.StatusNoContent, "call the failed schedule off")
		want(t, restrict(t, owner, news, nil, []any{user(home.user), user(annID)}), http.StatusOK, "ann edits again")
	})

	t.Run("the first publish of a page nobody edited can be scheduled", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Press release", "body": textDoc("Embargoed")}),
			http.StatusCreated, "make an unpublished page"), "page")
		id := made["id"].(string)
		if made["unpublished"] != true {
			t.Fatalf("the new page reads %v", made)
		}
		want(t, schedule(t, owner, id, inAnHour(), map[string]any{"notifyWatchers": true}), http.StatusOK, "schedule its first publish")
		h.comeDue(t, id, time.Hour)
		h.taken(t, watch, id)
		read := obj(t, want(t, ben.get(t, pagePath(id)), http.StatusOK, "ben reads the release"), "page")
		if number(read["version"]) != 1 || !strings.Contains(mustJSON(t, read["body"]), "Embargoed") {
			t.Errorf("the release reads %v", read)
		}
	})

	t.Run("a page going live calls its schedule off with the drafts", func(t *testing.T) {
		draftOn(t, ann, news, "News", "Live soon")
		want(t, schedule(t, ann, news, inAnHour()), http.StatusOK, "ann schedules")
		want(t, owner.put(t, pagePath(news, "/mode"), map[string]any{"mode": "live", "discardDrafts": true}), http.StatusOK, "go live")
		if n := h.countRows(t, `SELECT count(*) FROM page_schedule WHERE page_id = $1`, news); n != 0 {
			t.Errorf("a live page kept %d schedules", n)
		}
		if got := schedule(t, owner, news, inAnHour()); got.Status != http.StatusConflict || errorCode(t, got) != "page_live" {
			t.Errorf("scheduling a live page answered %d %s", got.Status, got.Raw)
		}
	})
}

// However many workers look at once, each due schedule is published once,
// and a time that passed while none ran still goes out, once.
func TestAScheduledPublishGoesOutOnceAcrossWorkers(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "schedule-once")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "ONCE", "Once")
	var pages []string
	for i := range 8 {
		id := docs.add(docs.homeID, fmt.Sprintf("Page %d", i))
		draftOn(t, owner, id, fmt.Sprintf("Page %d", i), "Scheduled words")
		want(t, schedule(t, owner, id, inAnHour()), http.StatusOK, "schedule")
		pages = append(pages, id)
	}
	for i, id := range pages {
		// Half came due a day ago, as after a worker that was down.
		h.comeDue(t, id, time.Duration(i%2)*24*time.Hour+time.Second)
	}
	var wg sync.WaitGroup
	for range 4 {
		w := page.NewScheduleWatch(h.cluster, discard(), time.Hour)
		wg.Go(func() {
			if _, err := w.Once(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	h.taken(t, page.NewScheduleWatch(h.cluster, discard(), time.Hour), pages...)
	for _, id := range pages {
		if n := h.countRows(t, `SELECT count(*) FROM page_version WHERE page_id = $1`, id); n != 2 {
			t.Errorf("page %s has %d versions, want 2", id, n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload->>'pageId' = $2 AND (payload->>'version')::int = 2`,
			events.TopicPagePublished, id); n != 1 {
			t.Errorf("version 2 of %s was announced %d times", id, n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_schedule WHERE page_id = $1`, id); n != 0 {
			t.Errorf("page %s kept its schedule", id)
		}
	}
}

// The service refusing is not proof: straight through SQL as stator_app, a
// schedule is one's own draft's, set by an editor for a time ahead, one per
// page, never marked failed by anybody but the worker, moved only by its
// author, and seen and called off by the page's editors alone.
func TestSchedulesAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "schedule-db")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	readerID := h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)

	docs := newTree(t, owner, "SCHEDB", "Schedules")
	news := docs.add(docs.homeID, "News")
	plain := docs.add(docs.homeID, "Plain")
	want(t, restrict(t, owner, news, nil, []any{user(home.user), user(annID)}), http.StatusOK, "keep editing to the owner and ann")
	draftOn(t, owner, news, "News", "The owner's draft")
	draftOn(t, ann, news, "News", "Ann's draft")

	conn := appConn(t)
	ctx := context.Background()
	insert := `INSERT INTO page_schedule (org_id, page_id, user_id, publish_at) VALUES (current_org_id(), $1, $2, now() + interval '1 hour')`

	actAs(t, conn, home.org, annID)
	refused(t, conn, "a schedule of somebody else's draft", insert, news, home.user)
	refused(t, conn, "a schedule without a draft", insert, plain, annID)
	refused(t, conn, "a schedule for a time past", `
		INSERT INTO page_schedule (org_id, page_id, user_id, publish_at) VALUES (current_org_id(), $1, current_actor_id(), now() - interval '1 minute')`, news)
	refused(t, conn, "a schedule born failed", `
		INSERT INTO page_schedule (org_id, page_id, user_id, publish_at, failed_at, failure)
		VALUES (current_org_id(), $1, current_actor_id(), now() + interval '1 hour', now(), 'gone')`, news)
	if _, err := conn.Exec(ctx, insert, news, annID); err != nil {
		t.Fatalf("ann could not schedule her own draft: %v", err)
	}
	refused(t, conn, "a schedule marked failed by its author", `UPDATE page_schedule SET failed_at = now(), failure = 'gone' WHERE page_id = $1`, news)
	refused(t, conn, "a schedule moved into the past", `UPDATE page_schedule SET publish_at = now() - interval '1 minute' WHERE page_id = $1`, news)
	refused(t, conn, "a schedule handed to somebody else", `UPDATE page_schedule SET user_id = $2 WHERE page_id = $1`, news, home.user)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "a second schedule of one page", insert, news, home.user)
	untouched(t, conn, "an editor moving somebody else's schedule", `UPDATE page_schedule SET publish_at = now() + interval '2 hours' WHERE page_id = $1`, news)

	actAs(t, conn, home.org, readerID)
	untouched(t, conn, "a reader reading a schedule", `SELECT 1 FROM page_schedule WHERE page_id = $1`, news)
	untouched(t, conn, "a reader calling a schedule off", `DELETE FROM page_schedule WHERE page_id = $1`, news)

	// An author who lost edit still reads their own, and sets nothing.
	want(t, restrict(t, owner, news, nil, []any{user(home.user)}), http.StatusOK, "editing is the owner's alone")
	h.settle(t)
	actAs(t, conn, home.org, annID)
	var mine int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_schedule WHERE page_id = $1`, news).Scan(&mine); err != nil || mine != 1 {
		t.Errorf("an author who lost edit reads %d of her schedules, %v", mine, err)
	}
	refused(t, conn, "an author who lost edit moving her schedule", `UPDATE page_schedule SET publish_at = now() + interval '3 hours' WHERE page_id = $1`, news)
	if _, err := conn.Exec(ctx, `SELECT 1 FROM page_schedule WHERE page_id = $1 FOR UPDATE`, news); err != nil {
		t.Errorf("the worker, acting for an author who lost edit, could not lock her schedule: %v", err)
	}

	actAs(t, conn, home.org, home.user)
	if tag, err := conn.Exec(ctx, `DELETE FROM page_schedule WHERE page_id = $1`, news); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("an editor could not call ann's schedule off: %d rows, %v", tag.RowsAffected(), err)
	}

	// A draft takes its schedule with it, however it goes.
	if _, err := conn.Exec(ctx, insert, news, home.user); err != nil {
		t.Fatalf("the owner could not schedule his own draft: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM page_draft WHERE page_id = $1 AND user_id = current_actor_id()`, news); err != nil {
		t.Fatal(err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM page_schedule WHERE page_id = $1`, news); n != 0 {
		t.Errorf("%d schedules outlived their draft", n)
	}
}
