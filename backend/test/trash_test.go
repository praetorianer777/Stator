//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

// trash lists the items of a space's trash.
func (tr *tree) trash() []map[string]any {
	tr.t.Helper()
	var out []map[string]any
	for _, each := range list(tr.t, want(tr.t, tr.c.get(tr.t, "/api/v1/spaces/"+tr.key+"/trash"), http.StatusOK, "list the trash"), "items") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func (tr *tree) restore(id string) response {
	tr.t.Helper()
	return tr.c.post(tr.t, "/api/v1/spaces/"+tr.key+"/trash/"+id+"/restore", nil)
}

// Deleting a page takes what is below it to the trash, a restore puts it all
// back where it was or under the home page, and only administrators purge.
func TestTrashAndRestoreOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "trash")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "TRASH", "Bin")
	docs.c = member
	a := docs.add(docs.homeID, "A")
	a1 := docs.add(a, "A1")
	a2 := docs.add(a1, "A2")
	docs.add(docs.homeID, "B")

	t.Run("a deleted page leaves the tree with everything below it", func(t *testing.T) {
		want(t, member.delete(t, "/api/v1/pages/"+a), http.StatusNoContent, "delete A")
		sameTitles(t, "top", docs.titles(""), "B")
		for _, id := range []string{a, a1, a2} {
			if got := member.get(t, "/api/v1/pages/"+id); got.Status != http.StatusNotFound {
				t.Errorf("a trashed page is still read: %d", got.Status)
			}
		}
		items := docs.trash()
		if len(items) != 1 || items[0]["title"] != "A" || items[0]["pages"].(float64) != 3 || items[0]["parentTitle"] != "Bin" || items[0]["parentInTree"] != true || items[0]["trashedByName"] != "A member" {
			t.Fatalf("the trash holds %v", items)
		}
		for what, got := range map[string]response{
			"add under it":  member.post(t, "/api/v1/pages", map[string]any{"parentId": a1, "title": "Orphan"}),
			"move under it": member.post(t, "/api/v1/pages/"+a2+"/move", map[string]any{"parentId": docs.homeID}),
			"list under it": member.get(t, "/api/v1/spaces/TRASH/pages?parent="+a),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("%s a trashed page: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.delete(t, "/api/v1/pages/"+docs.homeID); got.Status != http.StatusConflict {
			t.Errorf("the home page went to the trash: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a restore puts it back where it was", func(t *testing.T) {
		restored := obj(t, want(t, docs.restore(a), http.StatusOK, "restore A"), "page")
		if restored["parentId"] != docs.homeID {
			t.Fatalf("A came back under %v", restored["parentId"])
		}
		sameTitles(t, "top", docs.titles(""), "A", "B")
		sameTitles(t, "under A1", docs.titles(a1), "A2")
		if items := docs.trash(); len(items) != 0 {
			t.Fatalf("the trash still holds %v", items)
		}
	})

	t.Run("a page whose parent went to the trash comes back under the home page", func(t *testing.T) {
		want(t, member.delete(t, "/api/v1/pages/"+a1), http.StatusNoContent, "delete A1")
		want(t, member.delete(t, "/api/v1/pages/"+a), http.StatusNoContent, "delete A")
		items := docs.trash()
		if len(items) != 2 || items[0]["title"] != "A" || items[0]["pages"].(float64) != 1 || items[1]["title"] != "A1" || items[1]["parentInTree"] != false {
			t.Fatalf("the trash holds %v", items)
		}
		want(t, docs.restore(a1), http.StatusOK, "restore A1")
		sameTitles(t, "top", docs.titles(""), "B", "A1")
		sameTitles(t, "under A1", docs.titles(a1), "A2")
		want(t, docs.restore(a), http.StatusOK, "restore A")
		sameTitles(t, "top", docs.titles(""), "A", "B", "A1")
		if got := docs.restore(a); got.Status != http.StatusNotFound {
			t.Errorf("a page not in the trash was restored: %d", got.Status)
		}
	})

	t.Run("only an administrator purges, and what was below stays in the trash", func(t *testing.T) {
		c := docs.add(a, "C")
		want(t, member.delete(t, "/api/v1/pages/"+c), http.StatusNoContent, "delete C")
		want(t, member.delete(t, "/api/v1/pages/"+a), http.StatusNoContent, "delete A")
		if got := member.delete(t, "/api/v1/spaces/TRASH/trash/"+a); got.Status != http.StatusForbidden || errorCode(t, got) != "forbidden" {
			t.Fatalf("a member purged: %d %s", got.Status, got.Raw)
		}
		if got := member.delete(t, "/api/v1/spaces/TRASH/trash"); got.Status != http.StatusForbidden {
			t.Fatalf("a member emptied the trash: %d %s", got.Status, got.Raw)
		}
		want(t, owner.delete(t, "/api/v1/spaces/TRASH/trash/"+a), http.StatusNoContent, "purge A")
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE id = $1`, a).Scan(&left); err != nil || left != 0 {
			t.Fatalf("A is still stored (%d, %v)", left, err)
		}
		items := docs.trash()
		if len(items) != 1 || items[0]["title"] != "C" || items[0]["parentTitle"] != "Bin" {
			t.Fatalf("C did not stay in the trash under the home page: %v", items)
		}
		want(t, docs.restore(c), http.StatusOK, "restore C")
		sameTitles(t, "top", docs.titles(""), "B", "A1", "C")
	})

	t.Run("emptying the trash deletes everything in it, and the audit log says so", func(t *testing.T) {
		b := docs.titles("")
		if len(b) != 3 {
			t.Fatalf("top is %v", b)
		}
		want(t, member.delete(t, "/api/v1/pages/"+a1), http.StatusNoContent, "delete A1")
		want(t, owner.delete(t, "/api/v1/spaces/TRASH/trash"), http.StatusNoContent, "empty")
		if items := docs.trash(); len(items) != 0 {
			t.Fatalf("the trash still holds %v", items)
		}
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE id = ANY($1::uuid[])`, []string{a1, a2}).Scan(&left); err != nil || left != 0 {
			t.Fatalf("%d emptied pages are still stored (%v)", left, err)
		}
		rows, err := h.super.Query(context.Background(), `SELECT action FROM audit_log WHERE org_id = $1 AND action IN ('page.purged', 'trash.emptied') ORDER BY created_at`, home.org)
		if err != nil {
			t.Fatal(err)
		}
		actions, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(actions) != 2 || actions[0] != "page.purged" || actions[1] != "trash.emptied" {
			t.Fatalf("the audit log has %v (%v)", actions, err)
		}
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		d := docs.add(docs.homeID, "D")
		want(t, member.delete(t, "/api/v1/pages/"+d), http.StatusNoContent, "delete D")
		away := h.makeMember(t, "trash-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		for what, got := range map[string]response{
			"list":    stranger.get(t, "/api/v1/spaces/TRASH/trash"),
			"restore": stranger.post(t, "/api/v1/spaces/TRASH/trash/"+d+"/restore", nil),
			"purge":   stranger.delete(t, "/api/v1/spaces/TRASH/trash/"+d),
			"delete":  stranger.delete(t, "/api/v1/pages/"+docs.homeID),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("a stranger could %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}

// Straight through SQL as stator_app another tenant's trash is out of reach,
// the home page cannot be trashed, and an item cannot name another tenant's page.
func TestTheTrashIsWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "trash-wall-a")
	b := h.makeMember(t, "trash-wall-b")
	ownerA := api.as(t, a.user, a.org, h.slugOf(t, a.org))
	docsA := newTree(t, ownerA, "WALL", "Walled")
	docsB := newTree(t, api.as(t, b.user, b.org, h.slugOf(t, b.org)), "WALL", "Theirs")
	gone := docsA.add(docsA.homeID, "Gone")
	want(t, ownerA.delete(t, "/api/v1/pages/"+gone), http.StatusNoContent, "delete")
	theirs := docsB.add(docsB.homeID, "Theirs")

	conn := appConn(t)
	actAs(t, conn, b.org, b.user)
	untouched(t, conn, "restoring A's page from B", `UPDATE page SET trashed_at = NULL, trash_id = NULL WHERE id = $1`, gone)
	untouched(t, conn, "purging A's page from B", `DELETE FROM page WHERE id = $1`, gone)
	refused(t, conn, "an item named after A's page", `UPDATE page SET trashed_at = now(), trash_id = $2 WHERE id = $1`, theirs, gone)

	actAs(t, conn, a.org, a.user)
	refused(t, conn, "trashing the home page", `UPDATE page SET trashed_at = now(), trash_id = id WHERE id = $1`, docsA.homeID)
	refused(t, conn, "half a trash mark", `UPDATE page SET trashed_at = now() WHERE id = $1`, docsA.add(docsA.homeID, "Half"))

	var trashed bool
	if err := h.super.QueryRow(context.Background(), `SELECT trashed_at IS NOT NULL FROM page WHERE id = $1`, gone).Scan(&trashed); err != nil || !trashed {
		t.Errorf("A's trashed page is %v (%v), want it still in the trash", trashed, err)
	}
}
