//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The outline walks a space as its owner and judges a page by its parent's
// answer and what it carries itself (#371). Whoever asks and wherever the walk
// starts, it must return exactly the pages the walk under row level security
// with perm_page_viewable returns.
func TestTheOutlineWalkAnswersAsRowLevelSecurity(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "outline-walk-a")
	b := h.makeOutsider(t, "outline-walk-b")
	slug := h.slugOf(t, a.org)
	owner := api.as(t, a.user, a.org, slug)
	annID, benID := h.addPerson(t, a.org, "member"), h.addPerson(t, a.org, "member")
	ann := api.as(t, annID, a.org, slug)

	docs := newTree(t, owner, "WALK", "Walk")
	h.closeSpace(t, docs.space, annID, benID)
	walled := docs.add(docs.homeID, "Walled")
	below := docs.add(walled, "Below the wall")
	inner := docs.add(below, "Inner wall")
	docs.add(inner, "Deep inside")
	want(t, restrict(t, owner, walled, []any{user(a.user), user(annID), user(benID)}, nil), http.StatusOK, "wall off Walled but for ann and ben")
	want(t, restrict(t, owner, inner, []any{user(a.user), user(annID)}, nil), http.StatusOK, "wall off Inner wall but for ann")
	open := docs.add(docs.homeID, "Open")
	docs.add(open, "Open child")
	draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": open, "title": "Ann's draft"}), http.StatusCreated, "ann drafts"), "page")["id"].(string)
	want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": draft, "title": "Below ann's draft"}), http.StatusCreated, "ann drafts below it")
	trashed := docs.add(open, "Trashed")
	docs.add(trashed, "Below the trash")
	want(t, owner.delete(t, "/api/v1/pages/"+trashed), http.StatusNoContent, "trash a page")
	archived := docs.add(open, "Archived")
	docs.add(archived, "Below the archive")
	want(t, owner.put(t, pagePath(archived, "/archive"), nil), http.StatusOK, "archive a page")

	want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
	want(t, owner.put(t, "/api/v1/spaces/WALK/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open WALK")
	shut := newTree(t, owner, "SHUTW", "Shut")
	h.closeSpace(t, shut.space, annID, benID)
	linked := shut.add(shut.homeID, "Linked")
	shut.add(linked, "Below the link")
	token := want(t, owner.post(t, pagePath(linked, "/public-links"), map[string]any{"label": "Outline"}), http.StatusCreated, "a link").Body["token"].(string)
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	ids := func(t *testing.T, sql, root string) []string {
		t.Helper()
		rows, err := conn.Query(ctx, sql, root)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	const viaFunction = `SELECT id::text FROM page_outline_rows($1)`
	const viaPolicies = `
		WITH RECURSIVE tree (id) AS (
			SELECT p.id FROM page p WHERE p.id = $1
			UNION ALL
			SELECT p.id FROM page p JOIN tree t ON p.parent_id = t.id
			WHERE p.trashed_at IS NULL AND p.archived_at IS NULL AND perm_page_viewable(p.id, current_actor_id())
		)
		SELECT id::text FROM tree`

	t.Run("it returns what row level security returns", func(t *testing.T) {
		roots := map[string]string{"WALK's home": docs.homeID, "Walled": walled, "Inner wall": inner, "Open": open,
			"ann's draft": draft, "Trashed": trashed, "SHUTW's home": shut.homeID, "Linked": linked}
		for _, who := range []struct {
			name string
			act  func()
			min  int
		}{
			{"the owner", func() { actAs(t, conn, a.org, a.user) }, 18},
			{"ann, whom both walls name", func() { actAs(t, conn, a.org, annID) }, 21},
			{"ben, whom only the outer wall names", func() { actAs(t, conn, a.org, benID) }, 14},
			{"a member both spaces are closed to", func() { actAs(t, conn, b.org, b.user) }, 0},
			{"an anonymous reader", func() { actAnonymously(t, conn, a.org) }, 5},
			{"an anonymous reader holding a link", func() { readThrough(t, conn, a.org, token) }, 6},
		} {
			who.act()
			total := 0
			for name, root := range roots {
				got, want := ids(t, viaFunction, root), ids(t, viaPolicies, root)
				if !sameSet(got, want) {
					t.Errorf("%s from %s: the walk returns %d pages, row level security %d", who.name, name, len(got), len(want))
				}
				total += len(got)
			}
			if total < who.min {
				t.Errorf("%s gets %d pages in all, want at least %d: the comparison proves nothing", who.name, total, who.min)
			}
		}
	})

	t.Run("a wall keeps its branch out of the walk through SQL", func(t *testing.T) {
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		const hidden = `SELECT count(*) FROM page_outline_rows($1) WHERE title IN ('Inner wall', 'Deep inside', 'Ann''s draft', 'Below ann''s draft')`
		actAs(t, conn, a.org, annID)
		if n := count(hidden, docs.homeID); n != 4 {
			t.Fatalf("ann's walk holds %d of the pages ben's must not, want 4: the check proves nothing", n)
		}
		actAs(t, conn, a.org, benID)
		if n := count(hidden, docs.homeID); n != 0 {
			t.Errorf("ben's walk holds %d pages walled off from him or drafted by ann", n)
		}
		if n := count(`SELECT count(*) FROM page_outline_rows($1)`, inner); n != 0 {
			t.Errorf("ben walks %d pages starting inside a wall that leaves him out", n)
		}
		actAs(t, conn, b.org, b.user)
		if n := count(`SELECT count(*) FROM page_outline_rows($1)`, docs.homeID); n != 0 {
			t.Errorf("another organization walks %d of WALK's pages", n)
		}
	})
}
