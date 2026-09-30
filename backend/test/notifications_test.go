//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/mail"
	"github.com/praetorianer777/stator/backend/internal/notify"
)

const (
	// deliveryWait bounds how long a test waits for the outbox to drain and
	// for Mailpit to hold a mail.
	deliveryWait = 30 * time.Second
	// testAppURL is where the suite's own worker says the pages are.
	testAppURL = "http://stator.test"
	// workerIdle keeps the suite's workers quick to notice an event.
	workerIdle = 50 * time.Millisecond
)

// testMailer is the stack's Mailpit, as the worker reaches it.
func testMailer(t *testing.T) mail.Mailer {
	t.Helper()
	addr := os.Getenv("STATOR_SMTP_ADDR")
	if addr == "" {
		t.Fatal("STATOR_SMTP_ADDR is not set; run the suite with make test-integration against the running stack")
	}
	return mail.SMTPMailer{Addr: addr, From: "Stator <no-reply@stator.test>"}
}

// runWorker drains the outbox beside the stack's own worker until the test
// ends. Either may deliver an event; what arrives must be the same.
func (h *harness) runWorker(t *testing.T, handler events.Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		events.NewWorker(h.cluster, handler, discard()).WithIdle(workerIdle).Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// drained waits until every event of an organization is done, then until
// the replica has everything the worker wrote.
func (h *harness) drained(t *testing.T, org uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND processed_at IS NULL`, org) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the outbox of %s did not drain within %s", org, deliveryWait)
		}
		time.Sleep(workerIdle)
	}
	h.settle(t)
}

func (h *harness) emailOf(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// publishDraft saves a draft of a page and publishes it.
func publishDraft(t *testing.T, c *client, page, title, text string, notifyWatchers bool, comment ...string) {
	t.Helper()
	version := number(obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["version"])
	want(t, c.put(t, pagePath(page, "/draft"), map[string]any{"title": title, "body": textDoc(text), "baseVersion": version}), http.StatusOK, "draft "+title)
	body := map[string]any{"notifyWatchers": notifyWatchers}
	if len(comment) > 0 {
		body["comment"] = comment[0]
	}
	want(t, c.post(t, pagePath(page, "/publish"), body), http.StatusOK, "publish "+title)
}

func notificationsOf(t *testing.T, c *client, query string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, want(t, c.get(t, "/api/v1/notifications"+query), http.StatusOK, "list notifications"), "notifications") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func unreadOf(t *testing.T, c *client) int {
	t.Helper()
	return number(want(t, c.get(t, "/api/v1/notifications/unread-count"), http.StatusOK, "the unread count").Body["unread"])
}

// mailpit reads what the stack's Mailpit caught.
type mailpit struct{ base string }

type caught struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
}

func newMailpit(t *testing.T) mailpit {
	t.Helper()
	base := os.Getenv("STATOR_TEST_MAILPIT_URL")
	if base == "" {
		t.Fatal("STATOR_TEST_MAILPIT_URL is not set; run the suite with make test-integration against the running stack")
	}
	return mailpit{base: strings.TrimRight(base, "/")}
}

func (m mailpit) get(t *testing.T, path string, into any) {
	t.Helper()
	resp, err := http.Get(m.base + path)
	if err != nil {
		t.Fatalf("reach Mailpit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Mailpit answered %d to %s", resp.StatusCode, path)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

// to lists the mails caught for an address, the latest first.
func (m mailpit) to(t *testing.T, address string) []caught {
	t.Helper()
	var found struct {
		Messages []caught `json:"messages"`
	}
	m.get(t, "/api/v1/search?limit=200&query="+url.QueryEscape("to:"+address), &found)
	return found.Messages
}

// await waits until an address has caught at least n mails and returns them.
func (m mailpit) await(t *testing.T, address string, n int) []caught {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		got := m.to(t, address)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s caught %d mails within %s, want %d", address, len(got), deliveryWait, n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m mailpit) text(t *testing.T, id string) string {
	t.Helper()
	var msg struct {
		Text string `json:"Text"`
	}
	m.get(t, "/api/v1/message/"+id, &msg)
	return msg.Text
}

// Publishing with notify tells the watchers once each, never the actor, and
// what they are told can be listed, counted, marked read and switched off.
func TestNotificationsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, testMailer(t), testAppURL, discard()))
	home := h.makeMember(t, "notify")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Reader"), h.namedPerson(t, home.org, "Ben Reader"), h.namedPerson(t, home.org, "Carl Reader")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)
	nobody := api.anonymous()

	docs := newTree(t, owner, "NOTE", "Notes")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("First.")})
	want(t, watchPage(t, ann, plan, false), http.StatusOK, "ann watches Plan")
	want(t, ben.put(t, "/api/v1/spaces/NOTE/watch", nil), http.StatusNoContent, "ben watches the space")
	want(t, watchPage(t, carl, plan, false), http.StatusOK, "carl watches Plan")
	h.drained(t, home.org)

	t.Run("a publish with notify tells each watcher once, and never the actor", func(t *testing.T) {
		publishDraft(t, owner, plan, "Plan", "Second.", true, "Added the budget.")
		h.drained(t, home.org)
		for name, c := range map[string]*client{"ann": ann, "ben": ben, "carl": carl} {
			got := notificationsOf(t, c, "")
			if len(got) != 1 {
				t.Fatalf("%s was told %d things", name, len(got))
			}
			n := got[0]
			page := n["page"].(map[string]any)
			if n["kind"] != "published" || n["actorName"] != "Person of notify" || n["actorId"] != home.user.String() ||
				page["title"] != "Plan" || page["spaceKey"] != "NOTE" || page["id"] != plan ||
				number(n["version"]) != 2 || n["excerpt"] != "Added the budget." || n["readAt"] != nil || n["threadId"] != nil {
				t.Errorf("%s was told %v", name, n)
			}
			if unreadOf(t, c) != 1 {
				t.Errorf("%s has %d unread", name, unreadOf(t, c))
			}
		}
		if got := notificationsOf(t, owner, ""); len(got) != 0 {
			t.Errorf("the actor was told %v", got)
		}
	})

	t.Run("without notify nobody hears, and a restore with it tells them", func(t *testing.T) {
		publishDraft(t, owner, plan, "Plan", "Third.", false)
		want(t, owner.patch(t, pagePath(plan), map[string]any{"title": "Plan", "version": 3}), http.StatusOK, "a script publishes")
		h.drained(t, home.org)
		if n := unreadOf(t, ann); n != 1 {
			t.Errorf("ann has %d unread after publishes without notify", n)
		}
		want(t, owner.post(t, pagePath(plan, "/versions/1/restore"), map[string]any{"baseVersion": 4, "notifyWatchers": true}), http.StatusOK, "restore version 1")
		h.drained(t, home.org)
		got := notificationsOf(t, ann, "")
		if len(got) != 2 || got[0]["kind"] != "published" || number(got[0]["version"]) != 5 || got[0]["excerpt"] != "Restored version 1" {
			t.Errorf("after the restore ann was told %v", got)
		}
	})

	t.Run("a page first published under a watched page or space is created", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": plan, "title": "Budget"}), http.StatusCreated, "a new page"), "page")["id"].(string)
		want(t, owner.post(t, pagePath(made, "/publish"), map[string]any{"notifyWatchers": true}), http.StatusOK, "publish it")
		h.drained(t, home.org)
		for name, c := range map[string]*client{"ann, on the page above": ann, "ben, on the space": ben} {
			got := notificationsOf(t, c, "?limit=1")
			if len(got) != 1 || got[0]["kind"] != "created" || got[0]["page"].(map[string]any)["title"] != "Budget" || number(got[0]["version"]) != 1 {
				t.Errorf("%s was told %v", name, got)
			}
		}
	})

	t.Run("marking read counts only one's own", func(t *testing.T) {
		annRows, benRows := notificationsOf(t, ann, ""), notificationsOf(t, ben, "")
		before := unreadOf(t, ben)
		want(t, ann.post(t, "/api/v1/notifications/read", map[string]any{"ids": []any{annRows[0]["id"], benRows[0]["id"]}}), http.StatusNoContent, "ann marks one of hers and one of ben's")
		if n := unreadOf(t, ann); n != len(annRows)-1 {
			t.Errorf("ann has %d unread, want %d", n, len(annRows)-1)
		}
		if n := unreadOf(t, ben); n != before {
			t.Errorf("ann's mark changed ben's count from %d to %d", before, n)
		}
		unread := notificationsOf(t, ann, "?unread=true")
		if len(unread) != len(annRows)-1 {
			t.Errorf("ann lists %d unread", len(unread))
		}
		r := want(t, ann.get(t, "/api/v1/notifications?unread=true&limit=1"), http.StatusOK, "one unread")
		if number(r.Body["total"]) != len(annRows)-1 || len(list(t, r, "notifications")) != 1 {
			t.Errorf("a page of unread is %s", r.Raw)
		}
		if read := notificationsOf(t, ann, "?unread=false"); len(read) != len(annRows) || read[0]["readAt"] == nil {
			t.Errorf("everything lists %v", read)
		}
		want(t, ann.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, "ann marks all read")
		if n := unreadOf(t, ann); n != 0 {
			t.Errorf("ann has %d unread after marking all", n)
		}
	})

	t.Run("a kind switched off in the app is not written", func(t *testing.T) {
		prefs := obj(t, want(t, ann.get(t, "/api/v1/notification-preferences"), http.StatusOK, "ann's preferences"), "preferences")
		if prefs["digest"] != "off" || prefs["autoWatch"] != true || prefs["inApp"].(map[string]any)["published"] != true {
			t.Errorf("the defaults are %v", prefs)
		}
		prefs["inApp"].(map[string]any)["published"] = false
		saved := obj(t, want(t, ann.put(t, "/api/v1/notification-preferences", prefs), http.StatusOK, "ann switches published off"), "preferences")
		if saved["inApp"].(map[string]any)["published"] != false || saved["inApp"].(map[string]any)["created"] != true {
			t.Errorf("ann saved %v", saved)
		}
		before := len(notificationsOf(t, ann, ""))
		publishDraft(t, owner, plan, "Plan", "Sixth.", true)
		h.drained(t, home.org)
		if after := len(notificationsOf(t, ann, "")); after != before {
			t.Errorf("ann was told about a publish she switched off: %d then %d", before, after)
		}
	})

	t.Run("nothing reaches anybody about a page they may no longer view", func(t *testing.T) {
		carlBefore := notificationsOf(t, carl, "")
		if len(carlBefore) == 0 {
			t.Fatal("carl was told nothing so far")
		}
		want(t, restrict(t, owner, plan, []any{user(home.user), user(annID), user(benID)}, nil), http.StatusOK, "Plan is closed to carl")
		h.settle(t)
		if got := notificationsOf(t, carl, ""); len(got) != 0 {
			t.Errorf("carl still lists %d notifications about a page he may not view", len(got))
		}
		if n := unreadOf(t, carl); n != 0 {
			t.Errorf("carl's badge still counts %d", n)
		}
		publishDraft(t, owner, plan, "Plan", "Secret.", true)
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1`, carlID); n != len(carlBefore) {
			t.Errorf("carl got a row about a page he may not view: %d rows, %d before", n, len(carlBefore))
		}
		if got := notificationsOf(t, ben, "?limit=1"); number(got[0]["version"]) != 7 {
			t.Errorf("ben, who may view it, was not told: %v", got)
		}
		want(t, restrict(t, owner, plan, nil, nil), http.StatusOK, "Plan is open again")
		h.settle(t)
		if got := notificationsOf(t, carl, ""); len(got) != len(carlBefore) {
			t.Errorf("carl lists %d once he may view the page again, want %d", len(got), len(carlBefore))
		}
	})

	t.Run("a trashed page's notifications are hidden until it comes back", func(t *testing.T) {
		before := len(notificationsOf(t, ben, ""))
		want(t, owner.delete(t, pagePath(plan)), http.StatusNoContent, "Plan goes to the trash")
		h.settle(t)
		if got := notificationsOf(t, ben, ""); len(got) != 0 {
			t.Errorf("ben lists %d about pages in the trash", len(got))
		}
		want(t, docs.restore(plan), http.StatusOK, "Plan comes back")
		h.settle(t)
		if got := notificationsOf(t, ben, ""); len(got) != before {
			t.Errorf("ben lists %d after the restore, want %d", len(got), before)
		}
	})

	t.Run("what is not so is refused in a sentence", func(t *testing.T) {
		prefs := obj(t, want(t, ben.get(t, "/api/v1/notification-preferences"), http.StatusOK, "ben's preferences"), "preferences")
		prefs["digest"] = "weekly"
		for what, r := range map[string]response{
			"no limit":                 ben.get(t, "/api/v1/notifications?limit=0"),
			"an unread that is no yes": ben.get(t, "/api/v1/notifications?unread=maybe"),
			"marking nothing":          ben.post(t, "/api/v1/notifications/read", map[string]any{}),
			"a weekly digest":          ben.put(t, "/api/v1/notification-preferences", prefs),
			"nobody's count":           nobody.get(t, "/api/v1/notifications/unread-count"),
			"nobody's preferences":     nobody.get(t, "/api/v1/notification-preferences"),
		} {
			if r.Status < 400 || r.Status >= 500 {
				t.Errorf("%s answered %d", what, r.Status)
				continue
			}
			errorCode(t, r)
		}
		r := ben.put(t, "/api/v1/notification-preferences", prefs)
		if f, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any); f["digest"] == nil {
			t.Errorf("a weekly digest is refused as %s", r.Raw)
		}
		if f, _ := ben.post(t, "/api/v1/notifications/read", map[string]any{}).Body["error"].(map[string]any)["fields"].(map[string]any); f["ids"] == nil {
			t.Errorf("marking nothing is not refused on ids")
		}
	})
}

// Mail is a copy of the row: sent at once, bundled on a schedule, or not at
// all when switched off; every one lands in Mailpit.
func TestNotificationsAreMailed(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	mailer := testMailer(t)
	h.runWorker(t, notify.NewFanOut(h.cluster, mailer, testAppURL, discard()))
	pit := newMailpit(t)
	home := h.makeMember(t, "mailed")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.namedPerson(t, home.org, "Ann Mailed"), h.namedPerson(t, home.org, "Ben Mailed")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	annMail, benMail := h.emailOf(t, annID), h.emailOf(t, benID)

	docs := newTree(t, owner, "MAIL", "Mailed")
	page := docs.add(docs.homeID, "Launch plan")
	want(t, watchPage(t, ann, page, false), http.StatusOK, "ann watches")
	want(t, watchPage(t, ben, page, false), http.StatusOK, "ben watches")
	benPrefs := obj(t, want(t, ben.get(t, "/api/v1/notification-preferences"), http.StatusOK, "ben's preferences"), "preferences")
	benPrefs["digest"] = "daily"
	want(t, ben.put(t, "/api/v1/notification-preferences", benPrefs), http.StatusOK, "ben wants a daily digest")

	t.Run("a watcher with no digest is mailed at once, with the page's link", func(t *testing.T) {
		publishDraft(t, owner, page, "Launch plan", "Go.", true, "Dates fixed.")
		h.drained(t, home.org)
		got := pit.await(t, annMail, 1)
		if len(got) != 1 || got[0].Subject != `Person of mailed published a new version of "Launch plan"` {
			t.Fatalf("ann caught %v", got)
		}
		text := pit.text(t, got[0].ID)
		for _, want := range []string{"Dates fixed.", "/s/MAIL/p/" + page, "/settings/notifications"} {
			if !strings.Contains(text, want) {
				t.Errorf("the mail lacks %q:\n%s", want, text)
			}
		}
		if n := len(pit.to(t, benMail)); n != 0 {
			t.Errorf("ben, on a daily digest, was mailed %d at once", n)
		}
	})

	t.Run("a daily digest bundles what is unread at eight", func(t *testing.T) {
		publishDraft(t, owner, page, "Launch plan", "Go again.", true)
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, benID); n != 2 {
			t.Fatalf("ben's queue holds %d", n)
		}
		first := notificationsOf(t, ben, "")
		want(t, ben.post(t, "/api/v1/notifications/read", map[string]any{"ids": []any{first[len(first)-1]["id"]}}), http.StatusNoContent, "ben reads the older one in the app")
		h.settle(t)
		digester := notify.NewDigester(h.cluster, mailer, testAppURL, discard())
		if _, err := digester.WithClock(func() time.Time { return time.Now().Add(-48 * time.Hour) }).Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, benID); n != 2 {
			t.Errorf("a bundle that is not due yet went: %d left", n)
		}
		tomorrow := time.Now().UTC().Add(24 * time.Hour)
		eight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), notify.DailyDigestHour, 0, 0, 0, time.UTC)
		if _, err := digester.WithClock(func() time.Time { return eight }).Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := pit.await(t, benMail, 1)
		if len(got) != 1 || got[0].Subject != "1 update in Stator" {
			t.Fatalf("ben caught %v", got)
		}
		if text := pit.text(t, got[0].ID); !strings.Contains(text, `published a new version of "Launch plan"`) || strings.Count(text, "\n- ") != 1 {
			t.Errorf("the bundle reads:\n%s", text)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, benID); n != 0 {
			t.Errorf("the queue keeps %d after the bundle", n)
		}
		if _, err := digester.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := len(pit.to(t, benMail)); n != 1 {
			t.Errorf("an empty queue sent another bundle: %d", n)
		}
	})

	t.Run("a kind switched off by mail is shown in the app only", func(t *testing.T) {
		prefs := obj(t, want(t, ann.get(t, "/api/v1/notification-preferences"), http.StatusOK, "ann's preferences"), "preferences")
		prefs["email"].(map[string]any)["published"] = false
		want(t, ann.put(t, "/api/v1/notification-preferences", prefs), http.StatusOK, "ann stops mail about publishes")
		before, rows := len(pit.to(t, annMail)), len(notificationsOf(t, ann, ""))
		publishDraft(t, owner, page, "Launch plan", "Quietly.", true)
		h.drained(t, home.org)
		if got := len(notificationsOf(t, ann, "")); got != rows+1 {
			t.Errorf("ann lists %d, want %d", got, rows+1)
		}
		time.Sleep(time.Second)
		if n := len(pit.to(t, annMail)); n != before {
			t.Errorf("ann was mailed about a kind she switched off: %d then %d", before, n)
		}
	})
}

// countingHandler notes which events it handled, to show no two workers
// ever held the same one.
type countingHandler struct {
	next events.Handler
	mu   *sync.Mutex
	seen map[uuid.UUID]int
}

func (c countingHandler) Handle(ctx context.Context, e events.Event) error {
	c.mu.Lock()
	c.seen[e.ID]++
	c.mu.Unlock()
	return c.next.Handle(ctx, e)
}

// Workers running side by side, the stack's own among them, share the
// outbox: each event is handled by one, and each person is told once.
func TestTwoWorkersNeverDeliverTwice(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	mailer := testMailer(t)
	pit := newMailpit(t)
	home := h.makeMember(t, "workers")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.namedPerson(t, home.org, "Ann Twice")
	ann := api.as(t, annID, home.org, slug)
	annMail := h.emailOf(t, annID)
	docs := newTree(t, owner, "TWICE", "Twice")
	want(t, ann.put(t, "/api/v1/spaces/TWICE/watch", nil), http.StatusNoContent, "ann watches the space")

	const pages = 12
	var ids []string
	for i := range pages {
		id := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": fmt.Sprintf("Page %d", i)}), http.StatusCreated, "a page"), "page")["id"].(string)
		ids = append(ids, id)
	}

	var mu sync.Mutex
	handled := []map[uuid.UUID]int{{}, {}, {}}
	for i := range handled {
		h.runWorker(t, countingHandler{next: notify.NewFanOut(h.cluster, mailer, testAppURL, discard()), mu: &mu, seen: handled[i]})
	}
	for _, id := range ids {
		want(t, owner.post(t, pagePath(id, "/publish"), map[string]any{"notifyWatchers": true}), http.StatusOK, "publish a page")
	}
	h.drained(t, home.org)

	mu.Lock()
	for id := range handled[0] {
		for _, other := range handled[1:] {
			if other[id] > 0 {
				t.Errorf("event %s was handled by two workers", id)
			}
		}
	}
	for i, seen := range handled {
		for id, n := range seen {
			if n > 1 {
				t.Errorf("worker %d handled %s %d times", i, id, n)
			}
		}
	}
	mu.Unlock()
	if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND kind = 'created'`, annID); n != pages {
		t.Errorf("ann has %d rows, want %d", n, pages)
	}
	got := pit.await(t, annMail, pages)
	time.Sleep(time.Second)
	if got = pit.to(t, annMail); len(got) != pages {
		t.Errorf("ann caught %d mails, want %d", len(got), pages)
	}
	subjects := map[string]int{}
	for _, m := range got {
		subjects[m.Subject]++
	}
	for subject, n := range subjects {
		if n > 1 {
			t.Errorf("%q was mailed %d times", subject, n)
		}
	}
}
