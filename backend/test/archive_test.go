//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// archived lists the items of a space's archive by title.
func (tr *tree) archived() []map[string]any {
	tr.t.Helper()
	var out []map[string]any
	for _, each := range list(tr.t, want(tr.t, tr.c.get(tr.t, "/api/v1/spaces/"+tr.key+"/archived-pages"), http.StatusOK, "list the archive"), "items") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func spaceKeys(t *testing.T, c *client, query string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/spaces"+query), http.StatusOK, "list the spaces"+query), "spaces") {
		out = append(out, each.(map[string]any)["key"].(string))
	}
	return out
}

func auditActions(t *testing.T, h *harness, org any, targetType string, target string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT action FROM audit_log WHERE org_id = $1 AND target_type = $2 AND target_id = $3 AND action LIKE '%archived'
		ORDER BY created_at, id`, org, targetType, target)
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

// An archived page and everything below it stays readable, leaves the tree,
// search and the home page, changes for nobody, and comes back whole; only the
// space's administrators archive, and every act is in the audit log.
func TestArchivePagesAndSpacesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "archive")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.namedPerson(t, org.org, "Ann Editor"), h.namedPerson(t, org.org, "Ben Reader")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)

	docs := newTree(t, owner, "ARC", "Archive")
	plans := docs.add(docs.homeID, "Plans of yesteryear")
	old := docs.add(plans, "Old roadmap")
	older := docs.add(old, "Older roadmap")
	live := docs.add(docs.homeID, "Live roadmap")
	other := newTree(t, owner, "ARX", "Other")
	other.add(other.homeID, "Other roadmap")
	h.settle(t)

	t.Run("only an administrator of the space archives", func(t *testing.T) {
		r := want(t, ann.put(t, pagePath(plans, "/archive"), nil), http.StatusForbidden, "ann archives")
		if errorCode(t, r) != "forbidden" {
			t.Errorf("ann is refused with %s", r.Raw)
		}
		if p := obj(t, want(t, ann.get(t, pagePath(plans)), http.StatusOK, "ann reads Plans"), "page"); p["can"].(map[string]any)["archive"] != false {
			t.Errorf("ann is offered archiving: %v", p["can"])
		}
		if p := obj(t, want(t, owner.get(t, pagePath(docs.homeID)), http.StatusOK, "the home page"), "page"); p["can"].(map[string]any)["archive"] != false {
			t.Errorf("the home page offers archiving: %v", p["can"])
		}
		want(t, owner.put(t, pagePath(docs.homeID, "/archive"), nil), http.StatusConflict, "archive the home page")
		want(t, api.anonymous().put(t, pagePath(plans, "/archive"), nil), http.StatusUnauthorized, "nobody archives")
	})

	t.Run("an archived page keeps everything below it, readable", func(t *testing.T) {
		p := obj(t, want(t, owner.put(t, pagePath(plans, "/archive"), nil), http.StatusOK, "archive Plans"), "page")
		a, _ := p["archived"].(map[string]any)
		if a == nil || a["page"].(map[string]any)["id"] != plans || a["space"] != false || a["archivedByName"] != "Person of archive" {
			t.Fatalf("Plans reads archived %v", p["archived"])
		}
		want(t, owner.put(t, pagePath(plans, "/archive"), nil), http.StatusOK, "archive Plans again")
		h.settle(t)

		for _, id := range []string{old, older} {
			p := obj(t, want(t, ann.get(t, pagePath(id)), http.StatusOK, "ann reads a page below"), "page")
			a, _ := p["archived"].(map[string]any)
			can := p["can"].(map[string]any)
			if a == nil || a["page"].(map[string]any)["title"] != "Plans of yesteryear" || can["edit"] != false || can["comment"] != false || can["delete"] != false {
				t.Errorf("a page below reads archived %v, can %v", p["archived"], can)
			}
		}
		items := docs.archived()
		if len(items) != 1 || items[0]["id"] != plans || number(items[0]["pages"]) != 3 || items[0]["parentTitle"] != "Archive" {
			t.Errorf("the archive holds %v", items)
		}
		sameTitles(t, "the tree", docs.titles(""), "Live roadmap")
		sameTitles(t, "under an archived page", docs.titles(plans), "Old roadmap")
		below := list(t, want(t, ann.get(t, pagePath(docs.homeID, "/below?scope=subtree")), http.StatusOK, "below the home page"), "pages")
		if len(below) != 1 {
			t.Errorf("the home page's child pages are %v", below)
		}
		if below := list(t, want(t, ann.get(t, pagePath(plans, "/below?scope=subtree")), http.StatusOK, "below Plans"), "pages"); len(below) != 2 {
			t.Errorf("Plans's child pages are %v", below)
		}
	})

	t.Run("nobody changes an archived page, and the refusal says why", func(t *testing.T) {
		for what, r := range map[string]response{
			"edit it":          ann.patch(t, pagePath(old), map[string]any{"title": "New", "version": 1}),
			"draft it":         ann.put(t, pagePath(old, "/draft"), map[string]any{"title": "New", "body": textDoc("x"), "baseVersion": 1}),
			"add a page":       ann.post(t, "/api/v1/pages", map[string]any{"parentId": old, "title": "Newer", "publish": true}),
			"comment":          ann.post(t, pagePath(old, "/comments"), map[string]any{"body": commentDoc("Still true?")}),
			"react":            react(t, ann, "pages", old, "👍"),
			"label it":         ann.post(t, pagePath(old, "/labels"), map[string]any{"name": "stale"}),
			"move it":          ann.post(t, pagePath(old, "/move"), map[string]any{"parentId": docs.homeID}),
			"move a page in":   ann.post(t, pagePath(live, "/move"), map[string]any{"parentId": old}),
			"delete it":        ann.delete(t, pagePath(old)),
			"restrict it":      restrict(t, owner, old, nil, []any{user(org.user)}),
			"verify it":        owner.put(t, pagePath(old, "/verification"), map[string]any{"days": 30}),
			"the owner edits":  owner.patch(t, pagePath(plans), map[string]any{"title": "New", "version": 1}),
			"unarchive within": owner.delete(t, pagePath(old, "/archive")),
		} {
			if r.Status != http.StatusConflict || errorCode(t, r) != "archived" {
				t.Errorf("%s: %d %s", what, r.Status, r.Raw)
			}
		}
		if p := obj(t, want(t, ann.get(t, pagePath(old)), http.StatusOK, "ann reads Old"), "page"); p["title"] != "Old roadmap" {
			t.Errorf("Old changed to %v", p["title"])
		}
		want(t, owner.post(t, pagePath(old, "/copy"), map[string]any{"parentId": docs.homeID, "title": "Roadmap revived"}), http.StatusCreated, "copy Old out")
		h.settle(t)
		revived := docs.titles("")
		if len(revived) != 2 || revived[1] != "Roadmap revived" {
			t.Errorf("the copy is not a live page: %v", revived)
		}
	})

	t.Run("search, quick search and the home page leave it out unless asked", func(t *testing.T) {
		if got := hitTitles(t, searchFor(t, ann, url.Values{"q": {"roadmap"}})); !sameSet(got, []string{"Live roadmap", "Roadmap revived", "Other roadmap"}) {
			t.Errorf("a search finds %v", got)
		}
		hits := list(t, searchFor(t, ann, url.Values{"q": {"roadmap"}, "archived": {"true"}}), "hits")
		archivedHits := 0
		for _, each := range hits {
			if each.(map[string]any)["archived"] == true {
				archivedHits++
			}
		}
		if len(hits) != 5 || archivedHits != 2 {
			t.Errorf("a search with archived finds %d hits, %d archived", len(hits), archivedHits)
		}
		want(t, ann.get(t, "/api/v1/search?q=roadmap&archived=maybe"), http.StatusUnprocessableEntity, "archived maybe")
		quick := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q=old"), http.StatusOK, "quick search"))
		if len(quick) != 0 {
			t.Errorf("quick search offers %v", quick)
		}
		updates := titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates"), "updates")
		if has(updates, "Old roadmap") || has(updates, "Plans of yesteryear") || !has(updates, "Live roadmap") {
			t.Errorf("ann's updates are %v", updates)
		}
		if edited := titlesOf(t, want(t, owner.get(t, "/api/v1/home/edited"), http.StatusOK, "the owner's edits"), "pages"); has(edited, "Old roadmap") || !has(edited, "Live roadmap") {
			t.Errorf("the owner's edits are %v", edited)
		}
	})

	t.Run("the inspector says the page is archived", func(t *testing.T) {
		r := want(t, owner.get(t, pagePath(old, "/access/", annID.String())), http.StatusOK, "inspect ann on Old")
		var out struct{ Access perm.AccessReport }
		if err := json.Unmarshal(r.Raw, &out); err != nil {
			t.Fatal(err)
		}
		if !out.Access.Rights[0].Allowed {
			t.Errorf("ann may not read Old")
		}
		for _, right := range out.Access.Rights[1:] {
			d := decider(right)
			if right.Allowed || d == nil || d.Kind != perm.StepArchived || d.Page == nil || d.Page.Title != "Plans of yesteryear" {
				t.Errorf("%s of Old is decided by %+v", right.Right, d)
			}
		}
	})

	t.Run("unarchiving brings the item back whole, and only from its top", func(t *testing.T) {
		want(t, owner.put(t, pagePath(old, "/archive"), nil), http.StatusOK, "archiving within is no change")
		want(t, ann.delete(t, pagePath(plans, "/archive")), http.StatusForbidden, "ann unarchives")
		p := obj(t, want(t, owner.delete(t, pagePath(plans, "/archive")), http.StatusOK, "unarchive Plans"), "page")
		if p["archived"] != nil || p["can"].(map[string]any)["edit"] != true {
			t.Fatalf("Plans reads %v, can %v", p["archived"], p["can"])
		}
		want(t, owner.delete(t, pagePath(plans, "/archive")), http.StatusOK, "unarchive Plans again")
		h.settle(t)
		sameTitles(t, "the tree", docs.titles(""), "Plans of yesteryear", "Live roadmap", "Roadmap revived")
		want(t, ann.patch(t, pagePath(older), map[string]any{"title": "Older roadmap, kept", "version": 1}), http.StatusOK, "ann edits Older")
		if items := docs.archived(); len(items) != 0 {
			t.Errorf("the archive still holds %v", items)
		}
	})

	t.Run("an item inside another stays its own, and a deletion above takes it along", func(t *testing.T) {
		want(t, owner.put(t, pagePath(old, "/archive"), nil), http.StatusOK, "archive Old")
		want(t, owner.put(t, pagePath(plans, "/archive"), nil), http.StatusOK, "archive Plans over it")
		h.settle(t)
		if items := docs.archived(); len(items) != 2 {
			t.Fatalf("the archive holds %v", items)
		}
		want(t, owner.delete(t, pagePath(plans, "/archive")), http.StatusOK, "unarchive Plans")
		h.settle(t)
		if p := obj(t, want(t, ann.get(t, pagePath(older)), http.StatusOK, "ann reads Older"), "page"); p["archived"] == nil {
			t.Errorf("Older came out of Old's item with Plans")
		}
		want(t, ann.delete(t, pagePath(plans)), http.StatusNoContent, "ann deletes Plans with archived Old below")
		want(t, docs.restore(plans), http.StatusOK, "restore Plans")
		h.settle(t)
		if p := obj(t, want(t, ann.get(t, pagePath(old)), http.StatusOK, "ann reads Old"), "page"); p["archived"] == nil {
			t.Errorf("Old came back from the trash unarchived")
		}
		want(t, owner.delete(t, pagePath(old, "/archive")), http.StatusOK, "unarchive Old")
	})

	t.Run("an archived space stays readable and leaves the lists", func(t *testing.T) {
		want(t, ann.put(t, "/api/v1/spaces/ARC/archive", nil), http.StatusForbidden, "ann archives the space")
		sp := obj(t, want(t, owner.put(t, "/api/v1/spaces/ARC/archive", nil), http.StatusOK, "archive the space"), "space")
		if sp["archivedAt"] == nil || sp["archivedByName"] != "Person of archive" || sp["can"].(map[string]any)["editPages"] != false {
			t.Fatalf("the space reads %v", sp)
		}
		want(t, owner.put(t, "/api/v1/spaces/ARC/archive", nil), http.StatusOK, "archive the space again")
		h.settle(t)
		if keys := spaceKeys(t, ann, ""); has(keys, "ARC") || !has(keys, "ARX") {
			t.Errorf("the spaces are %v", keys)
		}
		if keys := spaceKeys(t, ann, "?archived=true"); !has(keys, "ARC") {
			t.Errorf("the spaces with archived ones are %v", keys)
		}
		want(t, ann.get(t, "/api/v1/spaces?archived=maybe"), http.StatusUnprocessableEntity, "archived maybe")
		p := obj(t, want(t, ben.get(t, pagePath(live)), http.StatusOK, "ben reads Live"), "page")
		if a, _ := p["archived"].(map[string]any); a == nil || a["space"] != true || a["page"] != nil {
			t.Errorf("Live reads archived %v", p["archived"])
		}
		r := ann.patch(t, pagePath(live), map[string]any{"title": "New", "version": 1})
		if r.Status != http.StatusConflict || errorCode(t, r) != "archived" {
			t.Errorf("ann edits in an archived space: %d %s", r.Status, r.Raw)
		}
		want(t, owner.put(t, pagePath(live, "/archive"), nil), http.StatusConflict, "archive a page of an archived space")
		if got := hitTitles(t, searchFor(t, ann, url.Values{"q": {"roadmap"}})); !sameSet(got, []string{"Other roadmap"}) {
			t.Errorf("a search finds %v", got)
		}
		if got := hitTitles(t, searchFor(t, ann, url.Values{"q": {"roadmap"}, "space": {"ARC"}, "archived": {"true"}})); len(got) != 4 {
			t.Errorf("a search of the archived space finds %v", got)
		}
		want(t, ann.delete(t, "/api/v1/spaces/ARC/archive"), http.StatusForbidden, "ann unarchives the space")
		if sp := obj(t, want(t, owner.delete(t, "/api/v1/spaces/ARC/archive"), http.StatusOK, "unarchive the space"), "space"); sp["archivedAt"] != nil {
			t.Errorf("the space reads %v", sp)
		}
		want(t, owner.delete(t, "/api/v1/spaces/ARC/archive"), http.StatusOK, "unarchive the space again")
		h.settle(t)
		want(t, ann.patch(t, pagePath(live), map[string]any{"title": "Live roadmap, kept", "version": 1}), http.StatusOK, "ann edits Live")
	})

	t.Run("each act is in the audit log once, and strangers reach nothing", func(t *testing.T) {
		sameList(t, "Plans's archive acts", auditActions(t, h, org.org, "page", plans),
			audit.ActionPageArchived, audit.ActionPageUnarchived, audit.ActionPageArchived, audit.ActionPageUnarchived)
		sameList(t, "the space's archive acts", auditActions(t, h, org.org, "space", docs.spaceID(t)),
			audit.ActionSpaceArchived, audit.ActionSpaceUnarchived)
		away := h.makeMember(t, "archive-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		for what, got := range map[string]response{
			"list":      stranger.get(t, "/api/v1/spaces/ARC/archived-pages"),
			"archive":   stranger.put(t, pagePath(live, "/archive"), nil),
			"unarchive": stranger.delete(t, pagePath(live, "/archive")),
			"space":     stranger.put(t, "/api/v1/spaces/ARC/archive", nil),
			"unspace":   stranger.delete(t, "/api/v1/spaces/ARC/archive"),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("a stranger could %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}

// spaceID reads the tree's space's id.
func (tr *tree) spaceID(t *testing.T) string {
	t.Helper()
	return obj(t, want(t, tr.c.get(t, "/api/v1/spaces/"+tr.key), http.StatusOK, "the space"), "space")["id"].(string)
}

// Straight through SQL as stator_app: nothing of an archived page changes for
// anybody, its marks are written only by the functions that check who asks,
// and an archived space holds every page in it the same way.
func TestArchivingIsEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "archive-wall")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.addPerson(t, org.org, "member")

	docs := newTree(t, owner, "AWL", "Walled archive")
	plans := docs.add(docs.homeID, "Plans")
	old := docs.add(plans, "Old")
	live := docs.add(docs.homeID, "Live")
	want(t, owner.put(t, pagePath(plans, "/archive"), nil), http.StatusOK, "archive Plans")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	for name, person := range map[string]uuid.UUID{"ann": annID, "the owner": org.user} {
		t.Run(name+" changes nothing archived", func(t *testing.T) {
			actAs(t, conn, org.org, person)
			var seen int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM page WHERE id = ANY($1::uuid[])`, []string{plans, old}).Scan(&seen); err != nil || seen != 2 {
				t.Errorf("%s reads %d archived pages (%v)", name, seen, err)
			}
			denied(t, conn, "a new title", `UPDATE page SET title = 'Changed' WHERE id = $1`, old)
			denied(t, conn, "a move out", `UPDATE page SET parent_id = $2 WHERE id = $1`, old, docs.homeID)
			denied(t, conn, "a move in", `UPDATE page SET parent_id = $2 WHERE id = $1`, live, old)
			denied(t, conn, "a page added below", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, updated_by)
				SELECT org_id, space_id, id, 'm', 'Newer', $2, $2 FROM page WHERE id = $1`, old, person)
			denied(t, conn, "a version", `INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
				SELECT org_id, id, version + 1, title, body, $2 FROM page WHERE id = $1`, old, person)
			denied(t, conn, "a draft", `INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
				SELECT org_id, id, $2, title, body, version FROM page WHERE id = $1`, old, person)
			denied(t, conn, "a label", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'stale', $3)`, org.org, old, person)
			denied(t, conn, "a comment", `INSERT INTO comment_thread (org_id, page_id, kind, created_by) VALUES ($1, $2, 'page', $3)`, org.org, old, person)
			denied(t, conn, "a reaction", `INSERT INTO reaction (org_id, page_id, user_id, emoji) VALUES ($1, $2, $3, '👍')`, org.org, old, person)
			denied(t, conn, "a restriction", `INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id) VALUES ($1, $2, 'edit', 'user', $3)`, org.org, old, person)
			denied(t, conn, "the trash", `SELECT page_trash($1)`, old)
			denied(t, conn, "a place among the pages below", `SELECT page_place(ARRAY[$1::uuid], $2, ARRAY['m'])`, live, old)
			denied(t, conn, "the marks taken off", `UPDATE page SET archived_at = NULL, archived_by = NULL, archive_id = NULL WHERE id = $1`, old)
			denied(t, conn, "the marks put on", `UPDATE page SET archived_at = now(), archive_id = id WHERE id = $1`, live)
			denied(t, conn, "a page born archived", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, updated_by, archived_at, archive_id)
				SELECT org_id, space_id, id, 'z', 'Born old', $2, $2, now(), id FROM page WHERE id = $1`, live, person)
			denied(t, conn, "unarchiving a page inside the item", `SELECT page_unarchive($1)`, old)
		})
	}

	t.Run("only an administrator of the space archives, through the functions", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		denied(t, conn, "ann archives", `SELECT page_archive($1)`, live)
		denied(t, conn, "ann unarchives", `SELECT page_unarchive($1)`, plans)
		actAs(t, conn, org.org, org.user)
		denied(t, conn, "the home page", `SELECT page_archive($1)`, docs.homeID)
		var marked int
		if err := conn.QueryRow(ctx, `SELECT page_archive($1)`, live).Scan(&marked); err != nil || marked != 1 {
			t.Fatalf("the owner archives Live: %d (%v)", marked, err)
		}
		if err := conn.QueryRow(ctx, `SELECT page_unarchive($1)`, live).Scan(&marked); err != nil || marked != 1 {
			t.Fatalf("the owner unarchives Live: %d (%v)", marked, err)
		}
	})

	t.Run("an archived space holds every page in it, and stamps who archived it", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		denied(t, conn, "ann archives the space", `UPDATE space SET archived_at = now() WHERE key = 'AWL'`)
		actAs(t, conn, org.org, org.user)
		if _, err := conn.Exec(ctx, `UPDATE space SET archived_at = now() - interval '9 days', archived_by = $1 WHERE key = 'AWL'`, annID); err != nil {
			t.Fatalf("the owner archives the space: %v", err)
		}
		var (
			fresh  bool
			byWhom string
		)
		if err := conn.QueryRow(ctx, `SELECT archived_at > now() - interval '1 minute', archived_by::text FROM space WHERE key = 'AWL'`).Scan(&fresh, &byWhom); err != nil || !fresh || byWhom != org.user.String() {
			t.Errorf("the space was archived now %v, by %s (%v)", fresh, byWhom, err)
		}
		for name, person := range map[string]uuid.UUID{"ann": annID, "the owner": org.user} {
			actAs(t, conn, org.org, person)
			denied(t, conn, name+" edits Live", `UPDATE page SET title = 'Changed' WHERE id = $1`, live)
			denied(t, conn, name+" adds a page", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, updated_by)
				SELECT org_id, space_id, id, 'm', 'Newer', $2, $2 FROM page WHERE id = $1`, live, person)
			denied(t, conn, name+" archives Live", `SELECT page_archive($1)`, live)
		}
		actAs(t, conn, org.org, org.user)
		if _, err := conn.Exec(ctx, `UPDATE space SET archived_at = NULL WHERE key = 'AWL'`); err != nil {
			t.Fatalf("the owner unarchives the space: %v", err)
		}
		if _, err := conn.Exec(ctx, `UPDATE page SET title = 'Live again' WHERE id = $1`, live); err != nil {
			t.Errorf("the owner cannot edit Live after: %v", err)
		}
	})
}
