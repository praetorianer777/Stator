//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/share"
)

func sharePage(t *testing.T, c *client, page string, recipients []any, message string) response {
	t.Helper()
	return c.post(t, pagePath(page, "/share"), map[string]any{"recipients": recipients, "message": message})
}

func sharedOf(t *testing.T, c *client, page string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, n := range notificationsOf(t, c, "?limit=100") {
		if n["kind"] == "shared" && n["page"].(map[string]any)["id"] == page {
			out = append(out, n)
		}
	}
	return out
}

// Sharing tells people who may already view the page, in the app and by
// mail, and refuses, sending nothing, when it is closed to anybody named.
func TestSharingOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, testMailer(t), testAppURL, discard()))
	mails := newMailpit(t)
	home := h.makeMember(t, "share")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Reader"), h.namedPerson(t, home.org, "Ben Reader"), h.namedPerson(t, home.org, "Carl Outsider")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)
	team := h.makeGroup(t, home.org, "Share Team", annID, benID, carlID, home.user)
	outsiders := h.makeGroup(t, home.org, "Share Outsiders", carlID)

	docs := newTree(t, owner, "SHR", "Shared")
	runbook := docs.add(docs.homeID, "Runbook", map[string]any{"body": textDoc("Rollback steps.")})
	secret := docs.add(docs.homeID, "Secret")
	draft := docs.add(docs.homeID, "Draft", map[string]any{"publish": false})
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "Secret is for the owner and ann")
	h.drained(t, home.org)

	t.Run("a member who may view is told in the app and by mail, the sharer never", func(t *testing.T) {
		r := want(t, sharePage(t, owner, runbook, []any{user(annID)}, "  Read the rollback part before Friday.  "), http.StatusCreated, "share Runbook with ann")
		sh := obj(t, r, "share")
		if number(sh["people"]) != 1 || sh["message"] != "Read the rollback part before Friday." || sh["pageId"] != runbook {
			t.Errorf("the share is %v", sh)
		}
		if got := sh["recipients"].([]any); len(got) != 1 || got[0].(map[string]any)["name"] != "Ann Reader" {
			t.Errorf("the share names %v", got)
		}
		h.drained(t, home.org)
		got := sharedOf(t, ann, runbook)
		if len(got) != 1 || got[0]["actorName"] != "Person of share" || got[0]["excerpt"] != "Read the rollback part before Friday." ||
			got[0]["page"].(map[string]any)["title"] != "Runbook" {
			t.Fatalf("ann was told %v", got)
		}
		if got := sharedOf(t, owner, runbook); len(got) != 0 {
			t.Errorf("the sharer was told %v", got)
		}
		caught := mails.await(t, h.emailOf(t, annID), 1)
		if caught[0].Subject != `Person of share shared "Runbook" with you` {
			t.Errorf("the mail is called %q", caught[0].Subject)
		}
		text := mails.text(t, caught[0].ID)
		for _, part := range []string{"Read the rollback part before Friday.", "/s/SHR/p/" + runbook} {
			if !strings.Contains(text, part) {
				t.Errorf("the mail lacks %q:\n%s", part, text)
			}
		}
	})

	t.Run("a group tells those of its members who may view, and leaves out the sharer", func(t *testing.T) {
		r := want(t, sharePage(t, owner, secret, []any{group(team)}, ""), http.StatusCreated, "share Secret with the team")
		if n := number(obj(t, r, "share")["people"]); n != 1 {
			t.Errorf("sharing Secret with the team tells %d people, want ann alone", n)
		}
		h.drained(t, home.org)
		if got := sharedOf(t, ann, secret); len(got) != 1 || got[0]["excerpt"] != "" {
			t.Errorf("ann was told %v", got)
		}
		for name, id := range map[string]uuid.UUID{"ben": benID, "carl": carlID, "the sharer": home.user} {
			if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND page_id = $2`, id, secret); n != 0 {
				t.Errorf("%s got %d rows about a page they may not view or shared themselves", name, n)
			}
		}
	})

	t.Run("a page closed to anybody named is refused with a sentence, and nothing is delivered", func(t *testing.T) {
		sharesBefore := h.countRows(t, `SELECT count(*) FROM page_share WHERE org_id = $1`, home.org)
		eventsBefore := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, home.org, events.TopicPageShared)
		annBefore := len(sharedOf(t, ann, secret))
		for what, recipients := range map[string][]any{
			"carl and ann":      {user(annID), user(carlID)},
			"a group of carl's": {group(outsiders)},
		} {
			r := want(t, sharePage(t, owner, secret, recipients, "Have a look."), http.StatusConflict, "share Secret with "+what)
			if code := errorCode(t, r); code != "cannot_view" {
				t.Errorf("sharing with %s is refused with %s", what, code)
			}
			msg := r.Body["error"].(map[string]any)["message"].(string)
			if !strings.Contains(msg, "nothing was shared") || !strings.Contains(msg, "let them in first") {
				t.Errorf("sharing with %s says %q", what, msg)
			}
			if what == "carl and ann" && (!strings.Contains(msg, "Carl Outsider") || strings.Contains(msg, "Ann Reader")) {
				t.Errorf("the refusal names %q", msg)
			}
		}
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM page_share WHERE org_id = $1`, home.org); n != sharesBefore {
			t.Errorf("a refused share left %d shares, want %d", n, sharesBefore)
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, home.org, events.TopicPageShared); n != eventsBefore {
			t.Errorf("a refused share left an event")
		}
		if got := sharedOf(t, ann, secret); len(got) != annBefore {
			t.Errorf("ann, named beside carl, was told anyway: %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1`, carlID); n != 0 {
			t.Errorf("carl got %d rows", n)
		}
		if n := len(mails.to(t, h.emailOf(t, carlID))); n != 0 {
			t.Errorf("carl caught %d mails", n)
		}
	})

	t.Run("the picker says who may view, and the list says who already does", func(t *testing.T) {
		people := list(t, want(t, owner.get(t, pagePath(secret, "/share/recipients?q=Carl")), http.StatusOK, "find carl"), "people")
		if len(people) != 1 || people[0].(map[string]any)["canView"] != false {
			t.Errorf("carl is offered as %v", people)
		}
		people = list(t, want(t, owner.get(t, pagePath(secret, "/share/recipients?q=ann")), http.StatusOK, "find ann"), "people")
		if len(people) != 1 || people[0].(map[string]any)["canView"] != true {
			t.Errorf("ann is offered as %v", people)
		}
		groups := list(t, want(t, owner.get(t, pagePath(secret, "/share/recipients?q=Share")), http.StatusOK, "find the groups"), "groups")
		if len(groups) != 2 {
			t.Fatalf("the groups offered are %v", groups)
		}
		for _, g := range groups {
			g := g.(map[string]any)
			wantViewers := map[string]int{"Share Team": 2, "Share Outsiders": 0}[g["name"].(string)]
			if number(g["viewers"]) != wantViewers {
				t.Errorf("%s has %v viewers, want %d", g["name"], g["viewers"], wantViewers)
			}
		}
		closed := want(t, owner.get(t, pagePath(secret, "/viewers")), http.StatusOK, "who views Secret")
		names := []string{}
		for _, v := range list(t, closed, "viewers") {
			names = append(names, v.(map[string]any)["name"].(string))
		}
		if number(closed.Body["total"]) != 2 || closed.Body["everyone"] != false || strings.Join(names, ",") != "Ann Reader,Person of share" {
			t.Errorf("Secret is viewed by %v: %s", names, closed.Raw)
		}
		open := want(t, ben.get(t, pagePath(runbook, "/viewers?limit=1")), http.StatusOK, "who views Runbook")
		if number(open.Body["total"]) != 4 || open.Body["everyone"] != true || len(list(t, open, "viewers")) != 1 {
			t.Errorf("Runbook is viewed as %s", open.Raw)
		}
	})

	t.Run("what cannot be shared is refused on its field or as the page", func(t *testing.T) {
		for what, c := range map[string]struct {
			by         *client
			page       string
			recipients []any
			message    string
			status     int
		}{
			"nobody":                   {owner, runbook, []any{}, "", http.StatusUnprocessableEntity},
			"everyone":                 {owner, runbook, []any{everyone}, "", http.StatusUnprocessableEntity},
			"only oneself":             {owner, runbook, []any{user(home.user)}, "", http.StatusUnprocessableEntity},
			"a stranger":               {owner, runbook, []any{user(uuid.New())}, "", http.StatusUnprocessableEntity},
			"a note too long":          {owner, runbook, []any{user(annID)}, strings.Repeat("a", share.MaxMessageLength+1), http.StatusUnprocessableEntity},
			"an unpublished page":      {owner, draft, []any{user(annID)}, "", http.StatusConflict},
			"a page one may not view":  {carl, secret, []any{user(annID)}, "", http.StatusNotFound},
			"a page that is not there": {owner, uuid.NewString(), []any{user(annID)}, "", http.StatusNotFound},
		} {
			r := want(t, sharePage(t, c.by, c.page, c.recipients, c.message), c.status, "share "+what)
			errorCode(t, r)
		}
		want(t, owner.get(t, pagePath(secret, "/share/recipients?limit=0")), http.StatusUnprocessableEntity, "a picker of nothing")
		want(t, carl.get(t, pagePath(secret, "/share/recipients")), http.StatusNotFound, "carl asks about Secret")
		want(t, owner.get(t, pagePath(secret, "/viewers?limit=1000")), http.StatusUnprocessableEntity, "too many viewers at once")
		want(t, carl.get(t, pagePath(secret, "/viewers")), http.StatusNotFound, "carl asks who views Secret")
	})

	t.Run("one person shares at most MaxPerHour pages an hour", func(t *testing.T) {
		var limit int
		if err := h.super.QueryRow(context.Background(), `SELECT page_share_per_hour()`).Scan(&limit); err != nil || limit != share.MaxPerHour {
			t.Fatalf("the database brakes at %d (%v), the service at %d", limit, err, share.MaxPerHour)
		}
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO page_share (org_id, page_id, sharer_id, created_at)
			SELECT $1, $2, $3, now() - interval '2 hours' FROM generate_series(1, $4::int)`, home.org, runbook, benID, share.MaxPerHour); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO page_share (org_id, page_id, sharer_id, created_at)
			SELECT $1, $2, $3, now() - interval '10 minutes' FROM generate_series(1, $4::int)`, home.org, runbook, benID, share.MaxPerHour-1); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		want(t, sharePage(t, ben, runbook, []any{user(annID)}, "The last one this hour."), http.StatusCreated, "ben's last share this hour")
		r := want(t, sharePage(t, ben, runbook, []any{user(annID)}, "One too many."), http.StatusTooManyRequests, "ben shares once more")
		if code := errorCode(t, r); code != "rate_limited" {
			t.Errorf("the brake answers %s", code)
		}
		if msg := r.Body["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "Share this one again in 50 minutes.") {
			t.Errorf("the brake says %q", msg)
		}
		want(t, sharePage(t, owner, runbook, []any{user(benID)}, ""), http.StatusCreated, "somebody else still shares")
		h.drained(t, home.org)
	})
}

// Straight through SQL as stator_app, a person shares only as themselves,
// only pages they may view, only with people who may view them, and reads
// and changes nobody's shares; the worker tells nobody the page is closed to.
func TestSharesAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	home := h.makeMember(t, "share-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	docs := newTree(t, owner, "SHW", "Walled")
	open := docs.add(docs.homeID, "Open")
	secret := docs.add(docs.homeID, "Secret")
	draft := docs.add(docs.homeID, "Draft", map[string]any{"publish": false})
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "Secret is for the owner and ann")
	r := want(t, sharePage(t, api.as(t, benID, home.org, slug), open, []any{user(annID)}, "From ben."), http.StatusCreated, "ben shares Open")
	bensShare := obj(t, r, "share")["id"].(string)
	h.drained(t, home.org)

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

	t.Run("a share is made only as oneself, of a page one may share", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		denied(t, conn, "sharing as ben", `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3)`, home.org, open, benID)
		denied(t, conn, "sharing a page nobody else can read yet", `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3)`, home.org, draft, annID)
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "sharing a page one may not view", `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3)`, home.org, secret, carlID)
	})

	t.Run("a share names only people who may view its page", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var mine uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3) RETURNING id`, home.org, secret, annID).Scan(&mine); err != nil {
			t.Fatalf("ann may share Secret: %v", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO page_share_recipient (org_id, share_id, user_id) VALUES ($1, $2, $3)`, home.org, mine, home.user); err != nil {
			t.Fatalf("ann may name the owner: %v", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, $2, $3)`, home.org, events.TopicPageShared,
			map[string]any{"shareId": mine, "pageId": secret, "actorId": annID}); err != nil {
			t.Fatalf("ann may send her share: %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var kept uuid.UUID
		if err := conn.QueryRow(ctx, `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3) RETURNING id`, home.org, secret, annID).Scan(&kept); err != nil {
			t.Fatalf("ann may share Secret: %v", err)
		}
		denied(t, conn, "naming somebody the page is closed to", `INSERT INTO page_share_recipient (org_id, share_id, user_id) VALUES ($1, $2, $3)`, home.org, kept, carlID)
		denied(t, conn, "naming herself", `INSERT INTO page_share_recipient (org_id, share_id, user_id) VALUES ($1, $2, $3)`, home.org, kept, annID)
		denied(t, conn, "naming somebody in ben's share", `INSERT INTO page_share_recipient (org_id, share_id, user_id) VALUES ($1, $2, $3)`, home.org, bensShare, home.user)
		denied(t, conn, "sending an old share again", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, $2, $3)`, home.org, events.TopicPageShared,
			map[string]any{"shareId": kept, "pageId": secret, "actorId": annID})
		denied(t, conn, "sending ben's share as herself", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, $2, $3)`, home.org, events.TopicPageShared,
			map[string]any{"shareId": bensShare, "pageId": open, "actorId": annID})
	})

	t.Run("a share is read only by its sharer and never changed", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		if n := count(`SELECT count(*) FROM page_share WHERE sharer_id <> $1`, annID); n != 0 {
			t.Errorf("ann reads %d of other people's shares", n)
		}
		if n := count(`SELECT count(*) FROM page_share_recipient WHERE share_id = $1`, bensShare); n != 0 {
			t.Errorf("ann reads whom ben's share told")
		}
		denied(t, conn, "rewriting a note", `UPDATE page_share SET message = 'changed' WHERE sharer_id = $1`, annID)
		denied(t, conn, "taking a share back", `DELETE FROM page_share WHERE sharer_id = $1`, annID)
		denied(t, conn, "dropping a recipient", `DELETE FROM page_share_recipient WHERE share_id = $1`, bensShare)
	})

	t.Run("the brake holds in the database", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `
			INSERT INTO page_share (org_id, page_id, sharer_id) SELECT $1, $2, $3 FROM generate_series(1, $4::int)`,
			home.org, open, carlID, share.MaxPerHour); err != nil {
			t.Fatal(err)
		}
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "sharing past the hourly limit", `INSERT INTO page_share (org_id, page_id, sharer_id) VALUES ($1, $2, $3)`, home.org, open, carlID)
	})

	t.Run("a share naming somebody the page is closed to tells them nothing", func(t *testing.T) {
		var forged uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO page_share (org_id, page_id, sharer_id, message) VALUES ($1, $2, $3, 'behind the back') RETURNING id`,
			home.org, secret, home.user).Scan(&forged); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO page_share_recipient (org_id, share_id, user_id) VALUES ($1, $2, $3), ($1, $2, $4)`, home.org, forged, carlID, annID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, $2, $3)`, home.org, events.TopicPageShared,
			map[string]any{"shareId": forged, "pageId": secret, "actorId": home.user}); err != nil {
			t.Fatal(err)
		}
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1`, carlID); n != 0 {
			t.Errorf("carl was told %d times about a page he may not view", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND page_id = $2 AND kind = 'shared'`, annID, secret); n != 1 {
			t.Errorf("ann, who may view it, got %d rows", n)
		}
	})
}
