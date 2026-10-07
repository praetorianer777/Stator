//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/shortcut"
)

func shortcutsPath(key string, rest ...string) string {
	path := "/api/v1/spaces/" + key + "/shortcuts"
	for _, r := range rest {
		path += "/" + r
	}
	return path
}

// shortcutNames lists a space's shortcuts as the caller sees them, each by its
// label or, for a page without one, its page's title.
func shortcutNames(t *testing.T, c *client, key string) []string {
	t.Helper()
	out := []string{}
	for _, each := range list(t, want(t, c.get(t, shortcutsPath(key)), http.StatusOK, "list the shortcuts of "+key), "shortcuts") {
		sc := each.(map[string]any)
		name, _ := sc["label"].(string)
		if p, ok := sc["page"].(map[string]any); ok && name == "" {
			name = p["title"].(string)
		}
		out = append(out, name)
	}
	return out
}

func shortcutIDs(t *testing.T, c *client, key string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, shortcutsPath(key)), http.StatusOK, "list the shortcuts of "+key), "shortcuts") {
		sc := each.(map[string]any)
		name, _ := sc["label"].(string)
		if p, ok := sc["page"].(map[string]any); ok && name == "" {
			name = p["title"].(string)
		}
		out[name] = sc["id"].(string)
	}
	return out
}

func shortcutActions(t *testing.T, h *harness, org uuid.UUID, space string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT action FROM audit_log WHERE org_id = $1 AND target_type = 'space' AND target_id = $2 AND action LIKE 'space.shortcut%'
		ORDER BY created_at, id`, org, space)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// Administrators of a space pin pages and addresses above its tree, order and
// remove them; members read them and change nothing; a shortcut to a page is
// shown only to whoever may view that page; every change is in the audit log.
func TestSpaceShortcutsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "shortcuts")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.namedPerson(t, org.org, "Ann Member")
	ann := api.as(t, annID, org.org, slug)

	docs := newTree(t, owner, "SHC", "Shortcuts")
	runbook := docs.add(docs.homeID, "Runbook")
	secret := docs.add(docs.homeID, "Salaries")
	old := docs.add(docs.homeID, "Old plans")
	want(t, restrict(t, owner, secret, []any{user(org.user)}, nil), http.StatusOK, "restrict Salaries to the owner")

	t.Run("an administrator pins pages and addresses, last each time", func(t *testing.T) {
		sc := obj(t, want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": runbook}), http.StatusCreated, "pin Runbook"), "shortcut")
		p, _ := sc["page"].(map[string]any)
		if sc["kind"] != "page" || sc["label"] != "" || sc["url"] != nil || p == nil || p["title"] != "Runbook" || p["spaceKey"] != "SHC" || p["home"] != false {
			t.Fatalf("the page shortcut reads %v", sc)
		}
		link := obj(t, want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"url": "https://status.example.com/now"}), http.StatusCreated, "pin a status page"), "shortcut")
		if link["kind"] != "link" || link["label"] != "status.example.com" || link["url"] != "https://status.example.com/now" || link["page"] != nil {
			t.Fatalf("the link reads %v", link)
		}
		want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": docs.homeID, "label": "Start here"}), http.StatusCreated, "pin the home page")
		want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": secret}), http.StatusCreated, "pin Salaries")
		want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"url": "HTTPS://Chat.example.com/#ops", "label": " Ops chat "}), http.StatusCreated, "pin the chat")
		sameList(t, "the owner's shortcuts", shortcutNames(t, owner, "SHC"), "Runbook", "status.example.com", "Start here", "Salaries", "Ops chat")
		ids := shortcutIDs(t, owner, "SHC")
		for _, each := range list(t, want(t, owner.get(t, shortcutsPath("SHC")), http.StatusOK, "list"), "shortcuts") {
			sc := each.(map[string]any)
			if sc["id"] == ids["Start here"] && sc["page"].(map[string]any)["home"] != true {
				t.Errorf("the home page's shortcut is not marked home: %v", sc)
			}
			if sc["id"] == ids["Ops chat"] && sc["url"] != "https://Chat.example.com/#ops" {
				t.Errorf("the chat's address is kept as %v", sc["url"])
			}
		}
	})

	t.Run("a shortcut to a page is shown only to whoever may view it", func(t *testing.T) {
		h.settle(t)
		sameList(t, "ann's shortcuts", shortcutNames(t, ann, "SHC"), "Runbook", "status.example.com", "Start here", "Ops chat")
		want(t, restrict(t, owner, secret, nil, nil), http.StatusOK, "open Salaries")
		h.settle(t)
		sameList(t, "ann's shortcuts once Salaries is open", shortcutNames(t, ann, "SHC"), "Runbook", "status.example.com", "Start here", "Salaries", "Ops chat")
		want(t, restrict(t, owner, secret, []any{user(org.user)}, nil), http.StatusOK, "restrict Salaries again")
	})

	t.Run("addresses that are not the web's are refused with a sentence", func(t *testing.T) {
		for _, bad := range []string{
			"javascript:alert(document.cookie)", "JavaScript:alert(1)", "data:text/html,<b>x</b>", "vbscript:x",
			"ftp://files.example.com", "//example.com", "example.com", "https://user:pw@example.com", "https://exa mple.com",
		} {
			r := want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"url": bad, "label": "Bad"}), http.StatusUnprocessableEntity, "pin "+bad)
			fieldError(t, r, "url")
		}
		fieldError(t, want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{}), http.StatusUnprocessableEntity, "pin nothing"), "url")
		fieldError(t, want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": runbook, "url": "https://example.com"}), http.StatusUnprocessableEntity, "pin both"), "url")
		fieldError(t, want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": uuid.NewString()}), http.StatusUnprocessableEntity, "pin no page"), "pageId")
		want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": runbook, "colour": "red"}), http.StatusBadRequest, "pin with a field nobody knows")
		if got := len(shortcutNames(t, owner, "SHC")); got != 5 {
			t.Errorf("a refused shortcut was kept: %d", got)
		}
	})

	t.Run("members read the shortcuts and change nothing", func(t *testing.T) {
		ids := shortcutIDs(t, owner, "SHC")
		for what, r := range map[string]response{
			"pin":    ann.post(t, shortcutsPath("SHC"), map[string]any{"url": "https://example.com"}),
			"move":   ann.post(t, shortcutsPath("SHC", ids["Ops chat"], "move"), map[string]any{"after": nil}),
			"remove": ann.delete(t, shortcutsPath("SHC", ids["Runbook"])),
		} {
			if r.Status != http.StatusForbidden || errorCode(t, r) != "forbidden" {
				t.Errorf("ann could %s: %d %s", what, r.Status, r.Raw)
			}
		}
		away := h.makeMember(t, "shortcuts-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		for what, r := range map[string]response{
			"list":   stranger.get(t, shortcutsPath("SHC")),
			"pin":    stranger.post(t, shortcutsPath("SHC"), map[string]any{"url": "https://example.com"}),
			"move":   stranger.post(t, shortcutsPath("SHC", ids["Ops chat"], "move"), map[string]any{"after": nil}),
			"remove": stranger.delete(t, shortcutsPath("SHC", ids["Runbook"])),
		} {
			if r.Status != http.StatusNotFound {
				t.Errorf("a stranger could %s: %d %s", what, r.Status, r.Raw)
			}
		}
		want(t, api.anonymous().get(t, shortcutsPath("SHC")), http.StatusUnauthorized, "nobody lists")
		sameList(t, "the shortcuts after the refusals", shortcutNames(t, owner, "SHC"), "Runbook", "status.example.com", "Start here", "Salaries", "Ops chat")
	})

	t.Run("an administrator orders them", func(t *testing.T) {
		ids := shortcutIDs(t, owner, "SHC")
		moved := list(t, want(t, owner.post(t, shortcutsPath("SHC", ids["Ops chat"], "move"), map[string]any{"after": nil}), http.StatusOK, "move the chat first"), "shortcuts")
		if len(moved) != 5 || moved[0].(map[string]any)["id"] != ids["Ops chat"] {
			t.Errorf("the move answered %v", moved)
		}
		want(t, owner.post(t, shortcutsPath("SHC", ids["Runbook"], "move"), map[string]any{"after": ids["Salaries"]}), http.StatusOK, "move Runbook last")
		want(t, owner.post(t, shortcutsPath("SHC", ids["Start here"], "move"), map[string]any{"after": ids["Ops chat"]}), http.StatusOK, "move the home page second")
		sameList(t, "the owner's order", shortcutNames(t, owner, "SHC"), "Ops chat", "Start here", "status.example.com", "Salaries", "Runbook")
		h.settle(t)
		sameList(t, "ann's order", shortcutNames(t, ann, "SHC"), "Ops chat", "Start here", "status.example.com", "Runbook")

		fieldError(t, want(t, owner.post(t, shortcutsPath("SHC", ids["Runbook"], "move"), map[string]any{"after": ids["Runbook"]}), http.StatusUnprocessableEntity, "after itself"), "after")
		fieldError(t, want(t, owner.post(t, shortcutsPath("SHC", ids["Runbook"], "move"), map[string]any{"after": uuid.NewString()}), http.StatusUnprocessableEntity, "after nothing"), "after")
		want(t, owner.post(t, shortcutsPath("SHC", uuid.NewString(), "move"), map[string]any{"after": nil}), http.StatusNotFound, "move nothing")
		want(t, owner.post(t, shortcutsPath("SHC", "nope", "move"), map[string]any{"after": nil}), http.StatusBadRequest, "move a bad id")
	})

	t.Run("a page in the trash hides its shortcut until it is back, and an archived one is marked", func(t *testing.T) {
		want(t, owner.post(t, shortcutsPath("SHC"), map[string]any{"pageId": old}), http.StatusCreated, "pin Old plans")
		want(t, owner.delete(t, pagePath(runbook)), http.StatusNoContent, "trash Runbook")
		h.settle(t)
		if names := shortcutNames(t, owner, "SHC"); has(names, "Runbook") {
			t.Errorf("a trashed page's shortcut shows: %v", names)
		}
		want(t, docs.restore(runbook), http.StatusOK, "restore Runbook")
		want(t, owner.put(t, pagePath(old, "/archive"), nil), http.StatusOK, "archive Old plans")
		h.settle(t)
		found := false
		for _, each := range list(t, want(t, ann.get(t, shortcutsPath("SHC")), http.StatusOK, "ann lists"), "shortcuts") {
			sc := each.(map[string]any)
			p, _ := sc["page"].(map[string]any)
			if p != nil && p["title"] == "Old plans" {
				found = true
				if p["archived"] != true {
					t.Errorf("the archived page is not marked: %v", p)
				}
			}
		}
		if !found || !has(shortcutNames(t, ann, "SHC"), "Runbook") {
			t.Errorf("ann's shortcuts are %v", shortcutNames(t, ann, "SHC"))
		}
		want(t, owner.delete(t, pagePath(old, "/archive")), http.StatusOK, "unarchive Old plans")
		want(t, owner.delete(t, pagePath(old)), http.StatusNoContent, "trash Old plans")
		want(t, owner.delete(t, "/api/v1/spaces/SHC/trash/"+old), http.StatusNoContent, "purge Old plans")
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM space_shortcut WHERE page_id = $1`, old).Scan(&left); err != nil || left != 0 {
			t.Errorf("a purged page left %d shortcuts (%v)", left, err)
		}
	})

	t.Run("an administrator removes them", func(t *testing.T) {
		ids := shortcutIDs(t, owner, "SHC")
		want(t, owner.delete(t, shortcutsPath("SHC", ids["status.example.com"])), http.StatusNoContent, "remove the status page")
		want(t, owner.delete(t, shortcutsPath("SHC", ids["status.example.com"])), http.StatusNotFound, "remove it again")
		if names := shortcutNames(t, owner, "SHC"); has(names, "status.example.com") {
			t.Errorf("the removed shortcut shows: %v", names)
		}
	})

	t.Run("every change is in the audit log", func(t *testing.T) {
		sameList(t, "the space's shortcut acts", shortcutActions(t, h, org.org, docs.spaceID(t)),
			audit.ActionShortcutAdded, audit.ActionShortcutAdded, audit.ActionShortcutAdded, audit.ActionShortcutAdded, audit.ActionShortcutAdded,
			audit.ActionShortcutMoved, audit.ActionShortcutMoved, audit.ActionShortcutMoved,
			audit.ActionShortcutAdded, audit.ActionShortcutRemoved)
	})
}

// A space holds at most shortcut.MaxPerSpace shortcuts, and the refusal says
// what to do.
func TestASpaceHoldsAFewShortcuts(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "shortcuts-full")
	owner := api.as(t, org.user, org.org, h.slugOf(t, org.org))
	newTree(t, owner, "FUL", "Full")
	for i := range shortcut.MaxPerSpace {
		want(t, owner.post(t, shortcutsPath("FUL"), map[string]any{"url": fmt.Sprintf("https://example.com/%d", i)}), http.StatusCreated, "pin one more")
	}
	r := want(t, owner.post(t, shortcutsPath("FUL"), map[string]any{"url": "https://example.com/last"}), http.StatusConflict, "pin one too many")
	if msg := obj(t, r, "error")["message"].(string); msg != (&shortcut.FullError{}).Error() {
		t.Errorf("the refusal reads %q", msg)
	}
	var max int
	if err := h.super.QueryRow(context.Background(), `SELECT space_shortcut_max()`).Scan(&max); err != nil || max != shortcut.MaxPerSpace {
		t.Errorf("the database holds a space to %d shortcuts, the service to %d (%v)", max, shortcut.MaxPerSpace, err)
	}
}

// Straight through SQL as stator_app: members change no shortcut, nobody
// reads one to a page they may not view, nobody points one at a script or a
// trashed page, and nothing moves one to another space or target.
func TestShortcutsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "shortcut-wall")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.addPerson(t, org.org, "member")

	docs := newTree(t, owner, "SWL", "Walled shortcuts")
	open := docs.add(docs.homeID, "Open")
	secret := docs.add(docs.homeID, "Secret")
	gone := docs.add(docs.homeID, "Gone")
	other := newTree(t, owner, "SWO", "Other")
	want(t, restrict(t, owner, secret, []any{user(org.user)}, nil), http.StatusOK, "restrict Secret")
	want(t, owner.delete(t, pagePath(gone)), http.StatusNoContent, "trash Gone")
	want(t, owner.post(t, shortcutsPath("SWL"), map[string]any{"pageId": open}), http.StatusCreated, "pin Open")
	want(t, owner.post(t, shortcutsPath("SWL"), map[string]any{"pageId": secret}), http.StatusCreated, "pin Secret")
	want(t, owner.post(t, shortcutsPath("SWL"), map[string]any{"url": "https://example.com"}), http.StatusCreated, "pin a link")
	h.settle(t)
	spaceID, otherID := docs.spaceID(t), other.spaceID(t)

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

	t.Run("a member reads what they may view and changes nothing", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		if n := count(`SELECT count(*) FROM space_shortcut WHERE space_id = $1`, spaceID); n != 2 {
			t.Errorf("ann reads %d shortcuts, want the two that do not name Secret", n)
		}
		if n := count(`SELECT count(*) FROM space_shortcut WHERE page_id = $1`, secret); n != 0 {
			t.Errorf("ann reads the shortcut to Secret")
		}
		denied(t, conn, "ann pins a link", `INSERT INTO space_shortcut (org_id, space_id, url, label, rank) VALUES ($1, $2, 'https://example.org', 'Mine', 'z')`, org.org, spaceID)
		denied(t, conn, "ann pins a page", `INSERT INTO space_shortcut (org_id, space_id, page_id, rank) VALUES ($1, $2, $3, 'z')`, org.org, spaceID, open)
		untouched(t, conn, "ann reorders", `UPDATE space_shortcut SET rank = 'a' WHERE space_id = $1`, spaceID)
		untouched(t, conn, "ann relabels", `UPDATE space_shortcut SET label = 'Mine' WHERE space_id = $1`, spaceID)
		untouched(t, conn, "ann removes", `DELETE FROM space_shortcut WHERE space_id = $1`, spaceID)
	})

	t.Run("an administrator keeps to the web, to pages they may view, and to the row's space and target", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		for _, bad := range []string{
			"javascript:alert(1)", "JAVASCRIPT:alert(1)", "data:text/html,x", "ftp://example.com", "//example.com",
			"https://user:pw@example.com", "https://exa mple.com", "https://example.com/\nx", "",
		} {
			denied(t, conn, "a link to "+bad, `INSERT INTO space_shortcut (org_id, space_id, url, label, rank) VALUES ($1, $2, $3, 'Bad', 'z')`, org.org, spaceID, bad)
		}
		denied(t, conn, "a link without a label", `INSERT INTO space_shortcut (org_id, space_id, url, rank) VALUES ($1, $2, 'https://example.org', 'z')`, org.org, spaceID)
		denied(t, conn, "a page and a link at once", `INSERT INTO space_shortcut (org_id, space_id, page_id, url, label, rank) VALUES ($1, $2, $3, 'https://example.org', 'Both', 'z')`, org.org, spaceID, open)
		denied(t, conn, "a shortcut to a trashed page", `INSERT INTO space_shortcut (org_id, space_id, page_id, rank) VALUES ($1, $2, $3, 'z')`, org.org, spaceID, gone)
		denied(t, conn, "a shortcut moved to another space", `UPDATE space_shortcut SET space_id = $2 WHERE space_id = $1`, spaceID, otherID)
		denied(t, conn, "a link pointed elsewhere", `UPDATE space_shortcut SET url = 'javascript:alert(1)' WHERE url IS NOT NULL AND space_id = $1`, spaceID)
		denied(t, conn, "a page shortcut pointed elsewhere", `UPDATE space_shortcut SET page_id = $2 WHERE page_id = $1`, open, secret)
		if _, err := conn.Exec(ctx, `UPDATE space_shortcut SET rank = 'b' WHERE page_id = $1`, open); err != nil {
			t.Errorf("the owner cannot reorder: %v", err)
		}
		if n := count(`SELECT count(*) FROM space_shortcut WHERE space_id = $1`, spaceID); n != 3 {
			t.Errorf("the owner reads %d shortcuts", n)
		}
	})

	t.Run("the limit holds whatever path adds them", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		refused(t, conn, "a space filled past its limit", `
			INSERT INTO space_shortcut (org_id, space_id, url, label, rank)
			SELECT $1, $2, 'https://example.com/' || n, 'Link ' || n, 'y' || lpad(n::text, 3, '0')
			FROM generate_series(1, space_shortcut_max() + 1) AS n`, org.org, otherID)
		if n := count(`SELECT count(*) FROM space_shortcut WHERE space_id = $1`, otherID); n != 0 {
			t.Errorf("a refused statement left %d shortcuts", n)
		}
	})
}
