//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/notify"
)

// Straight through SQL as stator_app, a person reaches only their own watching
// and notification rows, about what they may view, and adds events as themselves.
func TestWatchesAndNotificationsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, testMailer(t), testAppURL, discard()))
	home := h.makeMember(t, "wn-rls")
	other := h.makeMember(t, "wn-rls-other")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)

	docs := newTree(t, owner, "WNR", "Walls")
	open := docs.add(docs.homeID, "Open")
	closed := docs.add(docs.homeID, "Closed")
	want(t, watchPage(t, ann, open, false), http.StatusOK, "ann watches Open")
	want(t, watchPage(t, ben, open, false), http.StatusOK, "ben watches Open")
	want(t, ben.delete(t, pagePath(closed, "/watch")), http.StatusNoContent, "ben opts out of Closed")
	want(t, ben.put(t, "/api/v1/spaces/WNR/watch", nil), http.StatusNoContent, "ben watches the space")
	want(t, ann.put(t, "/api/v1/spaces/WNR/watch", nil), http.StatusNoContent, "ann watches the space")
	want(t, ben.put(t, "/api/v1/notification-preferences", map[string]any{
		"inApp": allKinds(true), "email": allKinds(true), "digest": "daily", "autoWatch": true,
	}), http.StatusOK, "ben saves preferences")
	want(t, restrict(t, owner, closed, []any{user(home.user), user(benID)}, nil), http.StatusOK, "Closed is not ann's")
	publishDraft(t, owner, open, "Open", "News.", true)
	publishDraft(t, owner, closed, "Closed", "Secret.", true)
	h.drained(t, home.org)
	var benRow, benClosedRow uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM notification WHERE user_id = $1 AND page_id = $2`, benID, open).Scan(&benRow); err != nil {
		t.Fatalf("ben was not told about Open: %v", err)
	}
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM notification WHERE user_id = $1 AND page_id = $2`, benID, closed).Scan(&benClosedRow); err != nil {
		t.Fatalf("ben was not told about Closed: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND page_id = $2`, annID, closed); n != 0 {
		t.Fatalf("ann was told about a page she may not view")
	}

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

	t.Run("somebody reads and changes only their own watches", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		if n := count(`SELECT count(*) FROM watch`); n != 2 {
			t.Errorf("ann reads %d watches, want her own two", n)
		}
		if n := count(`SELECT count(*) FROM watch_optout`); n != 0 {
			t.Errorf("ann reads %d opt outs", n)
		}
		if n := count(`SELECT count(*) FROM page_watchers($1)`, closed); n != 0 {
			t.Errorf("ann lists %d watchers of a page she may not view", n)
		}
		denied(t, conn, "watching for ben", `INSERT INTO watch (org_id, user_id, kind, page_id) VALUES ($1, $2, 'page', $3)`, home.org, benID, open)
		denied(t, conn, "watching a page she may not view", `INSERT INTO watch (org_id, user_id, kind, page_id) VALUES ($1, $2, 'page', $3)`, home.org, annID, closed)
		denied(t, conn, "opting ben out", `INSERT INTO watch_optout (org_id, user_id, page_id) VALUES ($1, $2, $3)`, home.org, benID, open)
		untouched(t, conn, "turning ben's watch into a subtree", `UPDATE watch SET kind = 'subtree' WHERE user_id = $1`, benID)
		untouched(t, conn, "removing ben's watches", `DELETE FROM watch WHERE user_id = $1`, benID)
		untouched(t, conn, "clearing ben's opt out", `DELETE FROM watch_optout WHERE user_id = $1`, benID)
		refused(t, conn, "reading who covers a page, which is the worker's", `SELECT * FROM page_watch_coverage($1, false)`, open)
	})

	t.Run("somebody reads and marks only their own notifications", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		if n := count(`SELECT count(*) FROM notification WHERE user_id <> $1`, annID); n != 0 {
			t.Errorf("ann reads %d of other people's notifications", n)
		}
		untouched(t, conn, "marking ben's read", `UPDATE notification SET read_at = now() WHERE id = $1`, benRow)
		denied(t, conn, "telling ben something", `INSERT INTO notification (org_id, user_id, event_id, kind, page_id) VALUES ($1, $2, $3, 'published', $4)`, home.org, benID, uuid.New(), open)
		denied(t, conn, "telling herself about a page she may not view", `INSERT INTO notification (org_id, user_id, event_id, kind, page_id) VALUES ($1, $2, $3, 'published', $4)`, home.org, annID, uuid.New(), closed)
		denied(t, conn, "rewriting her own row", `UPDATE notification SET page_id = $2 WHERE user_id = $1`, annID, closed)
		denied(t, conn, "deleting her own rows", `DELETE FROM notification WHERE user_id = $1`, annID)
		if n := count(`SELECT count(*) FROM notification_preference`); n != 0 {
			t.Errorf("ann reads %d preferences, none of them hers", n)
		}
		untouched(t, conn, "changing ben's preferences", `UPDATE notification_preference SET digest = 'off' WHERE user_id = $1`, benID)
		denied(t, conn, "saving preferences for ben", `INSERT INTO notification_preference (org_id, user_id) VALUES ($1, $2)`, home.org, benID)
		denied(t, conn, "queueing a digest for ben", `INSERT INTO notification_digest (org_id, user_id, notification_id) VALUES ($1, $2, $3)`, home.org, benID, benRow)
		untouched(t, conn, "emptying ben's digest", `DELETE FROM notification_digest WHERE user_id = $1`, benID)
	})

	t.Run("somebody who may no longer view a page reads nothing about it", func(t *testing.T) {
		want(t, restrict(t, owner, closed, []any{user(home.user)}, nil), http.StatusOK, "Closed is not ben's either")
		h.settle(t)
		actAs(t, conn, home.org, benID)
		if n := count(`SELECT count(*) FROM notification WHERE id = $1`, benClosedRow); n != 0 {
			t.Errorf("ben reads his row about a page he may no longer view")
		}
		if n := count(`SELECT count(*) FROM notification WHERE id = $1`, benRow); n != 1 {
			t.Errorf("ben does not read his own row about an open page")
		}
		if n := count(`SELECT count(*) FROM watch_optout WHERE page_id = $1`, closed); n != 0 {
			t.Errorf("ben reads his opt out of a page he may no longer view")
		}
	})

	t.Run("an event is added only in one's own name and never read", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		denied(t, conn, "an event in ben's name", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'page.published', jsonb_build_object('actorId', $2::text))`, home.org, benID)
		refused(t, conn, "reading the outbox", `SELECT count(*) FROM outbox_event`)
		refused(t, conn, "marking an event done", `UPDATE outbox_event SET processed_at = now()`)
		if _, err := conn.Exec(ctx, `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'test.nothing', jsonb_build_object('actorId', $2::text))`, home.org, annID); err != nil {
			t.Errorf("ann cannot add an event in her own name: %v", err)
		}
	})

	t.Run("another organization reads and writes nothing of this one", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		for _, table := range []string{"watch", "watch_optout", "notification", "notification_preference", "notification_digest"} {
			if n := count(`SELECT count(*) FROM ` + table); n != 0 {
				t.Errorf("another organization reads %d rows of %s", n, table)
			}
		}
		denied(t, conn, "a watch in the other organization", `INSERT INTO watch (org_id, user_id, kind, page_id) VALUES ($1, $2, 'page', $3)`, home.org, other.user, open)
		denied(t, conn, "a notification in the other organization", `INSERT INTO notification (org_id, user_id, event_id, kind, page_id) VALUES ($1, $2, $3, 'published', $4)`, home.org, other.user, uuid.New(), open)
		denied(t, conn, "an event in the other organization", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'page.published', jsonb_build_object('actorId', $2::text))`, home.org, other.user)
		untouched(t, conn, "marking the other organization's rows read", `UPDATE notification SET read_at = now() WHERE org_id = $1`, home.org)
	})
}

func allKinds(on bool) map[string]any {
	return map[string]any{"mentioned": on, "shared": on, "replied": on, "commented": on, "resolved": on, "published": on, "created": on}
}
