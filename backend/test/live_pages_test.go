//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// liveSave saves a live page as its editor holds it, from a shared draft when room is set.
func liveSave(t *testing.T, c *client, id, title string, body map[string]any, room ...string) response {
	t.Helper()
	in := map[string]any{"title": title, "body": body}
	if len(room) > 0 {
		in["room"] = room[0]
	}
	return c.put(t, pagePath(id, "/live"), in)
}

func savedVersion(t *testing.T, r response, what string) (int, bool) {
	t.Helper()
	got := want(t, r, http.StatusOK, what)
	v := obj(t, got, "version")
	if v["live"] != true {
		t.Errorf("%s: the version is not marked live: %s", what, got.Raw)
	}
	return number(v["number"]), got.Body["amended"] == true
}

func notificationsOfKind(t *testing.T, c *client, kind string) int {
	t.Helper()
	n := 0
	for _, each := range notificationsOf(t, c, "") {
		if each["kind"] == kind {
			n++
		}
	}
	return n
}

// A live page is saved as it is typed and read at once; its history keeps a
// version per span of work naming everybody in it; going live first asks
// before it throws anybody's unpublished draft away.
func TestLivePagesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "live-pages")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.namedPerson(t, home.org, "Ann Live"), h.namedPerson(t, home.org, "Ben Reader")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))

	docs := newTree(t, owner, "LIVE", "Live")
	notes := docs.add(docs.homeID, "Notes")
	box := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Box", "kind": "folder"}), http.StatusCreated, "make a folder"), "page")["id"].(string)
	want(t, restrict(t, owner, notes, nil, []any{user(home.user), user(annID)}), http.StatusOK, "keep editing to the owner and ann")

	t.Run("a page goes live only once its unpublished drafts are confirmed thrown away", func(t *testing.T) {
		if mode := obj(t, want(t, ben.get(t, pagePath(notes)), http.StatusOK, "read"), "page")["mode"]; mode != "draft" {
			t.Fatalf("a new page is %v", mode)
		}
		if got := liveSave(t, owner, notes, "Notes", textDoc("Too soon")); got.Status != http.StatusConflict || errorCode(t, got) != "page_not_live" {
			t.Fatalf("a live save of a page of drafts answered %d %s", got.Status, got.Raw)
		}
		want(t, ann.put(t, pagePath(notes, "/draft"), map[string]any{"title": "Notes", "body": textDoc("Ann's idea"), "baseVersion": 1}), http.StatusOK, "ann drafts")
		// A draft that says what the page says loses nothing when it goes.
		page := obj(t, want(t, owner.get(t, pagePath(notes)), http.StatusOK, "read"), "page")
		want(t, owner.put(t, pagePath(notes, "/draft"), map[string]any{"title": page["title"], "body": page["body"], "baseVersion": 1}), http.StatusOK, "the owner drafts nothing new")

		if got := ben.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live"}); got.Status != http.StatusForbidden {
			t.Errorf("a reader changed the mode: %d %s", got.Status, got.Raw)
		}
		if got := owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "wild"}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("an unknown mode answered %d %s", got.Status, got.Raw)
		}
		got := owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live"})
		if got.Status != http.StatusConflict || errorCode(t, got) != "drafts_pending" {
			t.Fatalf("going live over a draft answered %d %s", got.Status, got.Raw)
		}
		if msg := obj(t, got, "error")["message"].(string); !strings.Contains(msg, "Ann Live has a draft") {
			t.Errorf("the refusal does not name whose draft it is: %s", msg)
		}
		if d := want(t, ann.get(t, pagePath(notes, "/draft")), http.StatusOK, "ann's draft").Body["draft"]; d == nil {
			t.Fatal("a refused switch took ann's draft")
		}

		changed := want(t, owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live", "discardDrafts": true}), http.StatusOK, "go live anyway")
		if gone := changed.Body["discardedDrafts"].([]any); changed.Body["mode"] != "live" || len(gone) != 1 || gone[0] != "Ann Live" {
			t.Fatalf("going live answered %s", changed.Raw)
		}
		for who, c := range map[string]*client{"ann": ann, "the owner": owner} {
			if d := want(t, c.get(t, pagePath(notes, "/draft")), http.StatusOK, "the draft").Body["draft"]; d != nil {
				t.Errorf("%s still has a draft of a live page: %v", who, d)
			}
		}
		if data := h.recordedOnce(t, home.org, audit.ActionPageModeChanged, &home.user, notes); !strings.Contains(data, "Ann Live") || !strings.Contains(data, `"mode": "live"`) {
			t.Errorf("the audit entry reads %s", data)
		}
		want(t, owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live"}), http.StatusOK, "choose live again")
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2`, home.org, audit.ActionPageModeChanged); n != 1 {
			t.Errorf("choosing the mode a page has is recorded too: %d entries", n)
		}
		if got := owner.put(t, pagePath(box, "/mode"), map[string]any{"mode": "live"}); got.Status != http.StatusConflict || errorCode(t, got) != "folder" {
			t.Errorf("a folder went live: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a live page has no drafts to save or publish", func(t *testing.T) {
		for what, got := range map[string]response{
			"save a draft": ann.put(t, pagePath(notes, "/draft"), map[string]any{"title": "Notes", "body": textDoc("x"), "baseVersion": 1}),
			"publish":      ann.post(t, pagePath(notes, "/publish"), map[string]any{}),
		} {
			if got.Status != http.StatusConflict || errorCode(t, got) != "page_live" {
				t.Errorf("%s on a live page answered %d %s", what, got.Status, got.Raw)
			}
		}
		if got := liveSave(t, ben, notes, "Notes", textDoc("A reader's words")); got.Status != http.StatusForbidden {
			t.Errorf("a reader saved a live page: %d %s", got.Status, got.Raw)
		}
		if got := liveSave(t, owner, notes, " ", textDoc("No title")); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a live save without a title answered %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a save is the page at once, and saves within the span amend one version", func(t *testing.T) {
		n, amended := savedVersion(t, liveSave(t, owner, notes, "Notes", textDoc("First words")), "the first save")
		if n != 2 || amended {
			t.Fatalf("the first save went into version %d, amended %v", n, amended)
		}
		read := obj(t, want(t, ben.get(t, pagePath(notes)), http.StatusOK, "a reader reads"), "page")
		if number(read["version"]) != 2 || !strings.Contains(mustJSON(t, read["body"]), "First words") {
			t.Fatalf("a reader reads %v", read)
		}
		if n, amended = savedVersion(t, liveSave(t, owner, notes, "Notes", textDoc("First words", "More words")), "the next save"); n != 2 || !amended {
			t.Fatalf("the next save went into version %d, amended %v", n, amended)
		}
		if n, amended = savedVersion(t, liveSave(t, ann, notes, "Notes", mentionDoc("Ann adds a word for", benID)), "ann's save"); n != 2 || !amended {
			t.Fatalf("ann's save went into version %d, amended %v", n, amended)
		}
		// Nothing new is no save at all.
		before := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1`, home.org)
		if n, _ = savedVersion(t, liveSave(t, ann, notes, "Notes", mentionDoc("Ann adds a word for", benID)), "the same again"); n != 2 {
			t.Fatalf("the same words again went into version %d", n)
		}
		if after := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1`, home.org); after != before {
			t.Errorf("saving nothing new emitted %d events", after-before)
		}
		ticket := uuid.NewString()
		taskBody := todoDoc("Ann adds a word for", todo{id: ticket, text: "Check the figures", assignee: benID})
		savedVersion(t, liveSave(t, ann, notes, "Notes", taskBody), "ann assigns a task")
		taskBody = todoDoc("Ann adds a word for", todo{id: ticket, text: "Check the figures twice", assignee: benID})
		savedVersion(t, liveSave(t, owner, notes, "Notes", taskBody), "the task's words change")

		history := want(t, owner.get(t, pagePath(notes, "/versions")), http.StatusOK, "history")
		sameNumbers(t, "the history", versionNumbers(t, history), 2, 1)
		latest := list(t, history, "versions")[0].(map[string]any)
		if latest["live"] != true || latest["authorName"] == "Ann Live" || mustJSON(t, latest["coEditors"]) != `["Ann Live"]` {
			t.Errorf("the live version reads %v", latest)
		}
		h.drained(t, home.org)
		if n := notificationsOfKind(t, ben, "mentioned"); n != 1 {
			t.Errorf("ben was told of his mention %d times, want once", n)
		}
		if n := notificationsOfKind(t, ben, "assigned"); n != 1 {
			t.Errorf("ben was told of his task %d times, want once", n)
		}
		contributors := want(t, ben.get(t, pagePath(notes, "/contributors?scope=page&limit=10")), http.StatusOK, "contributors")
		edits := map[string]int{}
		for _, each := range list(t, contributors, "contributors") {
			c := each.(map[string]any)
			edits[c["name"].(string)] = number(c["edits"])
		}
		if edits["Ann Live"] != 1 {
			t.Errorf("the contributors count %v", edits)
		}
	})

	t.Run("a version closes when its span ends, and the next save begins another", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `
			UPDATE page_version SET created_at = now() - live_version_span() - interval '1 minute' WHERE page_id = $1 AND number = 2`, notes); err != nil {
			t.Fatal(err)
		}
		if n, amended := savedVersion(t, liveSave(t, ann, notes, "Notes", textDoc("A new session")), "a save after the span"); n != 3 || amended {
			t.Fatalf("a save after the span went into version %d, amended %v", n, amended)
		}
		old := obj(t, want(t, owner.get(t, pagePath(notes, "/versions/2")), http.StatusOK, "version 2"), "version")
		if strings.Contains(mustJSON(t, old["body"]), "A new session") {
			t.Error("a closed version took a later save")
		}
	})

	t.Run("a write from elsewhere wins over the shared draft it overtook", func(t *testing.T) {
		var room string
		if err := h.super.QueryRow(context.Background(), `
			INSERT INTO page_collab (org_id, page_id, base_version, seeded) VALUES ($1, $2, 3, true) RETURNING id::text`, home.org, notes).Scan(&room); err != nil {
			t.Fatal(err)
		}
		if got := liveSave(t, ann, notes, "Notes", textDoc("From a lost room"), uuid.NewString()); got.Status != http.StatusConflict || errorCode(t, got) != "room_gone" {
			t.Errorf("a save from another room answered %d %s", got.Status, got.Raw)
		}
		if n, _ := savedVersion(t, liveSave(t, ann, notes, "Notes", textDoc("A new session, together"), room), "a save from the room"); n != 3 {
			t.Fatalf("the room's save went into version %d", n)
		}
		want(t, owner.post(t, pagePath(notes, "/versions/2/restore"), map[string]any{"baseVersion": 3}), http.StatusOK, "restore version 2")
		if got := liveSave(t, ann, notes, "Notes", textDoc("Typed over the restore"), room); got.Status != http.StatusConflict || errorCode(t, got) != "room_gone" {
			t.Fatalf("the overtaken room saved over the restore: %d %s", got.Status, got.Raw)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_collab WHERE page_id = $1`, notes); n != 0 {
			t.Errorf("the overtaken room is still there")
		}
		read := obj(t, want(t, ben.get(t, pagePath(notes)), http.StatusOK, "read"), "page")
		if number(read["version"]) != 4 || strings.Contains(mustJSON(t, read["body"]), "Typed over") {
			t.Errorf("after the restore a reader reads %v", read)
		}
		if n, amended := savedVersion(t, liveSave(t, ann, notes, "Notes", textDoc("After the restore")), "a save after the restore"); n != 5 || amended {
			t.Errorf("a save after a restore went into version %d, amended %v", n, amended)
		}
	})

	t.Run("back to drafts, the history stays and drafts work again", func(t *testing.T) {
		want(t, ann.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "draft"}), http.StatusOK, "back to drafts")
		if got := liveSave(t, ann, notes, "Notes", textDoc("Live again?")); got.Status != http.StatusConflict || errorCode(t, got) != "page_not_live" {
			t.Errorf("a live save of a page of drafts answered %d %s", got.Status, got.Raw)
		}
		if total := number(want(t, owner.get(t, pagePath(notes, "/versions")), http.StatusOK, "history").Body["total"]); total != 5 {
			t.Errorf("the history holds %d versions, want 5", total)
		}
		want(t, ann.put(t, pagePath(notes, "/draft"), map[string]any{"title": "Notes", "body": textDoc("A draft again"), "baseVersion": 5}), http.StatusOK, "draft again")
		want(t, ann.post(t, pagePath(notes, "/publish"), map[string]any{}), http.StatusOK, "publish again")
	})
}

// The service refusing is not proof: straight through SQL as stator_app, a
// live page holds no drafts, only an editor switches it, only the open
// version of a live page is ever amended, and only by its editors in their own name.
func TestLivePagesAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "live-db")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	readerID := h.addPerson(t, home.org, "member")

	docs := newTree(t, owner, "LIVEDB", "Live")
	notes := docs.add(docs.homeID, "Notes")
	drafts := docs.add(docs.homeID, "Drafts")
	box := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Box", "kind": "folder"}), http.StatusCreated, "make a folder"), "page")["id"].(string)
	want(t, restrict(t, owner, notes, nil, []any{user(home.user), user(annID)}), http.StatusOK, "keep editing to the owner and ann")
	want(t, owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live"}), http.StatusOK, "go live")
	savedVersion(t, liveSave(t, owner, notes, "Notes", textDoc("Version two")), "open version 2")

	var span float64
	if err := h.super.QueryRow(context.Background(), `SELECT extract(epoch FROM live_version_span())`).Scan(&span); err != nil {
		t.Fatal(err)
	}
	if time.Duration(span)*time.Second != page.LiveVersionSpan {
		t.Errorf("the database holds a version open for %vs, the service for %s", span, page.LiveVersionSpan)
	}

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, home.org, readerID)
	refused(t, conn, "a reader taking a page off live", `UPDATE page SET mode = 'draft' WHERE id = $1`, notes)
	untouched(t, conn, "a reader amending the open version", `UPDATE page_version SET body = '{"type":"doc","content":[]}' WHERE page_id = $1 AND number = 2`, notes)
	refused(t, conn, "a reader naming themselves an editor", `INSERT INTO page_version_editor (org_id, page_id, number, user_id) VALUES (current_org_id(), $1, 2, current_actor_id())`, notes)
	var pending int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_pending_drafts($1)`, notes).Scan(&pending); err != nil {
		t.Fatal(err)
	}

	actAs(t, conn, home.org, annID)
	refused(t, conn, "a draft of a live page", `
		INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
		VALUES (current_org_id(), $1, current_actor_id(), 'Notes', '{"type":"doc","content":[]}', 2)`, notes)
	refused(t, conn, "a version saved live on a page of drafts", `
		INSERT INTO page_version (org_id, page_id, number, title, body, created_by, live)
		VALUES (current_org_id(), $1, 2, 'Drafts', '{"type":"doc","content":[]}', current_actor_id(), true)`, drafts)
	untouched(t, conn, "amending a closed version", `UPDATE page_version SET title = 'Rewritten' WHERE page_id = $1 AND number = 1`, notes)
	untouched(t, conn, "amending a version of a page of drafts", `UPDATE page_version SET title = 'Rewritten' WHERE page_id = $1 AND number = 1`, drafts)
	refused(t, conn, "rewriting a version's author", `UPDATE page_version SET created_by = current_actor_id() WHERE page_id = $1 AND number = 2`, notes)
	refused(t, conn, "rewriting a version's comment", `UPDATE page_version SET comment = 'Rewritten' WHERE page_id = $1 AND number = 2`, notes)
	refused(t, conn, "naming somebody else an editor", `INSERT INTO page_version_editor (org_id, page_id, number, user_id) VALUES (current_org_id(), $1, 2, $2)`, notes, home.user)
	refused(t, conn, "naming oneself an editor of a closed version", `INSERT INTO page_version_editor (org_id, page_id, number, user_id) VALUES (current_org_id(), $1, 1, current_actor_id())`, notes)
	refused(t, conn, "a live folder", `UPDATE page SET mode = 'live' WHERE id = $1`, box)
	refused(t, conn, "deleting an editor of a version", `DELETE FROM page_version_editor WHERE page_id = $1`, notes)
	if tag, err := conn.Exec(ctx, `UPDATE page_version SET body = '{"type":"doc","content":[]}' WHERE page_id = $1 AND number = 2`, notes); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("an editor could not amend the open version: %d rows, %v", tag.RowsAffected(), err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO page_version_editor (org_id, page_id, number, user_id) VALUES (current_org_id(), $1, 2, current_actor_id())`, notes); err != nil {
		t.Errorf("an editor could not name themselves an editor of the open version: %v", err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE page_version SET created_at = now() - live_version_span() - interval '1 minute' WHERE page_id = $1 AND number = 2`, notes); err != nil {
		t.Fatal(err)
	}
	untouched(t, conn, "amending a version past its span", `UPDATE page_version SET title = 'Late' WHERE page_id = $1 AND number = 2`, notes)

	// Drafts of a page going live go with the switch, whoever's they are.
	want(t, owner.put(t, pagePath(drafts, "/draft"), map[string]any{"title": "Drafts", "body": textDoc("Owner's"), "baseVersion": 1}), http.StatusOK, "the owner drafts")
	if _, err := conn.Exec(ctx, `
		INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
		VALUES (current_org_id(), $1, current_actor_id(), 'Drafts', '{"type":"doc","content":[]}', 1)`, drafts); err != nil {
		t.Fatalf("ann could not draft a page of drafts: %v", err)
	}
	var names []string
	rows, err := conn.Query(ctx, `SELECT name FROM page_pending_drafts($1)`, drafts)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if len(names) != 2 {
		t.Errorf("an editor reads %v as the drafts pending, want both", names)
	}
	if _, err := conn.Exec(ctx, `UPDATE page SET mode = 'live' WHERE id = $1`, drafts); err != nil {
		t.Fatalf("an editor could not make the page live: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM page_draft WHERE page_id = $1`, drafts); n != 0 {
		t.Errorf("%d drafts outlived their page going live", n)
	}
	if pending != 0 {
		t.Errorf("a reader read %d drafts pending", pending)
	}
}
