//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// labelsOf reads the names in a {labels} answer.
func labelsOf(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "labels") {
		switch v := each.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			out = append(out, v["name"].(string))
		}
	}
	return out
}

func addLabel(t *testing.T, c *client, page, name string) response {
	t.Helper()
	return c.post(t, pagePath(page, "/labels"), map[string]any{"name": name})
}

// Labels go on and come off pages, are offered as they are typed, list their
// pages and filter search, all among what the caller may see.
func TestLabelsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "labels")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)

	ops := newTree(t, owner, "LOPS", "Operations")
	dev := newTree(t, owner, "LDEV", "Development")
	runbook := ops.add(ops.homeID, "Deploy runbook", map[string]any{"body": textDoc("How we ship.")})
	rollback := ops.add(runbook, "Rollback", map[string]any{"body": textDoc("Roll back first.")})
	notes := dev.add(dev.homeID, "Release notes", map[string]any{"body": textDoc("What shipped.")})

	t.Run("a label goes on normalized, once, and comes off", func(t *testing.T) {
		got := labelsOf(t, want(t, addLabel(t, ann, runbook, "  Release Process "), http.StatusOK, "ann labels the runbook"))
		if !sameSet(got, []string{"release-process"}) {
			t.Fatalf("the runbook carries %v", got)
		}
		want(t, addLabel(t, ann, runbook, "release-process"), http.StatusOK, "the same label again")
		want(t, addLabel(t, ann, runbook, "ops"), http.StatusOK, "a second label")
		got = labelsOf(t, want(t, ben.get(t, pagePath(runbook, "/labels")), http.StatusOK, "ben reads the labels"))
		if len(got) != 2 || got[0] != "ops" || got[1] != "release-process" {
			t.Fatalf("the runbook carries %v, want ops and release-process in order", got)
		}
		want(t, ann.delete(t, pagePath(runbook, "/labels/ops")), http.StatusNoContent, "ann takes ops off")
		want(t, ann.delete(t, pagePath(runbook, "/labels/ops")), http.StatusNoContent, "taking it off again")
		if got := labelsOf(t, want(t, ann.get(t, pagePath(runbook, "/labels")), http.StatusOK, "labels")); !sameSet(got, []string{"release-process"}) {
			t.Fatalf("after removing ops the runbook carries %v", got)
		}
	})

	t.Run("a name that is no label is refused in a sentence", func(t *testing.T) {
		for _, bad := range []string{"", "a/b", "-lead", "semi;colon", "an-overly-long-label-that-goes-on-and-on-forever"} {
			r := want(t, addLabel(t, ann, runbook, bad), http.StatusUnprocessableEntity, "label "+bad)
			if code := errorCode(t, r); code != "validation_failed" {
				t.Errorf("%q is refused with %s", bad, code)
			}
		}
		want(t, ann.delete(t, pagePath(runbook, "/labels/a;b")), http.StatusUnprocessableEntity, "removing a name that is no label")
		want(t, ann.get(t, "/api/v1/labels/a;b/pages"), http.StatusUnprocessableEntity, "listing a name that is no label")
		want(t, ann.get(t, "/api/v1/labels?limit=0"), http.StatusUnprocessableEntity, "suggesting none")
		want(t, ann.get(t, "/api/v1/labels/ops/pages?limit=101"), http.StatusUnprocessableEntity, "listing too many")
	})

	t.Run("a reader may not label, and nobody labels what they cannot see", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/LDEV/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "LDEV is read only")
		h.settle(t)
		r := want(t, addLabel(t, ben, notes, "mine"), http.StatusForbidden, "ben labels in a read only space")
		if code := errorCode(t, r); code != "forbidden" {
			t.Errorf("the refusal is %s", code)
		}
		want(t, addLabel(t, owner, notes, "release"), http.StatusOK, "the owner labels the notes")
		want(t, ben.delete(t, pagePath(notes, "/labels/release")), http.StatusForbidden, "ben takes a label off")
		want(t, ben.get(t, pagePath(notes, "/labels")), http.StatusOK, "ben still reads the labels")
		want(t, ben.get(t, pagePath("00000000-0000-0000-0000-000000000000", "/labels")), http.StatusNotFound, "labels of no page")
		want(t, ben.get(t, "/api/v1/labels/release/pages?space=NOPE"), http.StatusNotFound, "a label's pages in no space")
	})

	t.Run("a label lists its pages, per space and across the organization, with paging", func(t *testing.T) {
		want(t, addLabel(t, ann, rollback, "release"), http.StatusOK, "label the rollback")
		want(t, addLabel(t, ann, runbook, "release"), http.StatusOK, "label the runbook")
		r := want(t, ben.get(t, "/api/v1/labels/release/pages"), http.StatusOK, "the release pages")
		if got := pageTitles(t, r); len(got) != 3 || got[0] != "Deploy runbook" || got[1] != "Release notes" || got[2] != "Rollback" {
			t.Fatalf("release lists %v, want the three by title", got)
		}
		first := list(t, r, "pages")[2].(map[string]any)
		if first["spaceKey"] != "LOPS" || first["spaceName"] != "Operations" || first["updatedByName"] == "" {
			t.Errorf("the rollback is listed as %v", first)
		}
		if path := first["path"].([]any); len(path) != 2 || path[1] != "Deploy runbook" {
			t.Errorf("the rollback's path is %v", path)
		}
		if labels := first["labels"].([]any); len(labels) != 1 || labels[0] != "release" {
			t.Errorf("the rollback carries %v", labels)
		}
		if got := pageTitles(t, want(t, ben.get(t, "/api/v1/labels/Release/pages?space=lops"), http.StatusOK, "in one space")); !sameSet(got, []string{"Deploy runbook", "Rollback"}) {
			t.Errorf("release in LOPS lists %v", got)
		}
		paged := want(t, ben.get(t, "/api/v1/labels/release/pages?limit=2&offset=2"), http.StatusOK, "the second page")
		if got := pageTitles(t, paged); len(got) != 1 || got[0] != "Rollback" || number(paged.Body["total"]) != 3 {
			t.Errorf("the second page of two is %v of %v", got, paged.Body["total"])
		}
	})

	t.Run("labels are offered as they are typed, the most used first", func(t *testing.T) {
		got := labelsOf(t, want(t, ben.get(t, "/api/v1/labels?q=rel"), http.StatusOK, "suggest rel"))
		if len(got) != 2 || got[0] != "release" || got[1] != "release-process" {
			t.Fatalf("rel offers %v", got)
		}
		r := want(t, ben.get(t, "/api/v1/labels?q=release"), http.StatusOK, "suggest release")
		if n := number(list(t, r, "labels")[0].(map[string]any)["pages"]); n != 3 {
			t.Errorf("release is on %d pages, want 3", n)
		}
		if got := labelsOf(t, want(t, ben.get(t, "/api/v1/labels?q=release%20p"), http.StatusOK, "suggest with a space")); !sameSet(got, []string{"release-process"}) {
			t.Errorf("release p offers %v", got)
		}
		if got := labelsOf(t, want(t, ben.get(t, "/api/v1/labels?q=rel&space=LDEV"), http.StatusOK, "suggest in LDEV")); !sameSet(got, []string{"release"}) {
			t.Errorf("rel in LDEV offers %v", got)
		}
		if got := labelsOf(t, want(t, ben.get(t, "/api/v1/labels?q=r_l"), http.StatusOK, "an underscore is literal")); len(got) != 0 {
			t.Errorf("r_l offers %v", got)
		}
	})

	t.Run("search filters by label, and a hit carries its labels", func(t *testing.T) {
		want(t, owner.upload(t, pagePath(runbook, "/attachments"), "release-plan.txt", []byte("x")), http.StatusCreated, "a file on the runbook")
		for what, c := range map[string]struct {
			params url.Values
			want   []string
		}{
			"one label":          {url.Values{"label": {"release"}}, []string{"Deploy runbook", "Rollback", "Release notes"}},
			"typed as written":   {url.Values{"label": {"Release Process"}}, []string{"Deploy runbook"}},
			"either of two":      {url.Values{"label": {"release-process", "nope"}}, []string{"Deploy runbook"}},
			"with words":         {url.Values{"q": {"roll"}, "label": {"release"}}, []string{"Rollback"}},
			"with a space":       {url.Values{"label": {"release"}, "space": {"LDEV"}}, []string{"Release notes"}},
			"an unknown label":   {url.Values{"label": {"nope"}}, nil},
			"a name that is not": {url.Values{"label": {"a/b"}}, nil},
			"files carry none":   {url.Values{"q": {"release"}, "label": {"release"}, "type": {"attachment"}}, nil},
		} {
			if got := hitTitles(t, searchFor(t, ben, c.params)); !sameSet(got, c.want) {
				t.Errorf("%s: %v, want %v", what, got, c.want)
			}
		}
		r := searchFor(t, ben, url.Values{"q": {"runbook"}, "type": {"page"}})
		hit := list(t, r, "hits")[0].(map[string]any)
		if labels := hit["labels"].([]any); len(labels) != 2 || labels[0] != "release" || labels[1] != "release-process" {
			t.Errorf("the runbook's hit carries %v", labels)
		}
	})

	t.Run("a copy takes its original's labels", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, pagePath(runbook, "/copy"), map[string]any{"parentId": ops.homeID, "withChildren": true, "title": "Runbook copy"}), http.StatusCreated, "copy the runbook"), "page")["id"].(string)
		if got := labelsOf(t, want(t, ann.get(t, pagePath(made, "/labels")), http.StatusOK, "the copy's labels")); !sameSet(got, []string{"release", "release-process"}) {
			t.Errorf("the copy carries %v", got)
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/labels/release/pages?space=LOPS"), http.StatusOK, "release in LOPS")); !sameSet(got, []string{"Deploy runbook", "Rollback", "Runbook copy", "Rollback"}) {
			t.Errorf("after the copy release in LOPS lists %v", got)
		}
	})

	t.Run("a trashed page leaves the lists and keeps its labels for its return", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(rollback)), http.StatusNoContent, "trash the rollback")
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/labels/release/pages?space=LOPS"), http.StatusOK, "release in LOPS")); !sameSet(got, []string{"Deploy runbook", "Runbook copy", "Rollback"}) {
			t.Errorf("with the rollback trashed release lists %v", got)
		}
		want(t, addLabel(t, owner, rollback, "gone"), http.StatusNotFound, "labelling a trashed page")
		want(t, owner.post(t, "/api/v1/spaces/LOPS/trash/"+rollback+"/restore", nil), http.StatusOK, "restore it")
		if got := labelsOf(t, want(t, owner.get(t, pagePath(rollback, "/labels")), http.StatusOK, "its labels")); !sameSet(got, []string{"release"}) {
			t.Errorf("the restored rollback carries %v", got)
		}
	})

	t.Run("a page carries a bounded number of labels", func(t *testing.T) {
		scratch := ops.add(ops.homeID, "Scratch")
		for i := range 50 {
			want(t, addLabel(t, owner, scratch, "tag"+string(rune('a'+i/26))+string(rune('a'+i%26))), http.StatusOK, "a label")
		}
		want(t, addLabel(t, owner, scratch, "one-too-many"), http.StatusUnprocessableEntity, "the fifty-first label")
		want(t, addLabel(t, owner, scratch, "tagaa"), http.StatusOK, "one it has already")
	})
}

// A label is its page's: on a page somebody may not view, it is in no answer
// they get, and the database refuses it to them straight through SQL.
func TestLabelsFollowTheirPage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "label-wall")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	other := h.makeMember(t, "label-wall-b")

	docs := newTree(t, owner, "LWALL", "Walled")
	open := docs.add(docs.homeID, "Open plan", map[string]any{"body": textDoc("plans")})
	closed := docs.add(docs.homeID, "Closed plan", map[string]any{"body": textDoc("plans")})
	below := docs.add(closed, "Below the closed plan")
	locked := docs.add(docs.homeID, "Locked plan")
	want(t, addLabel(t, owner, open, "shared"), http.StatusOK, "label the open page")
	for _, id := range []string{closed, below} {
		want(t, addLabel(t, owner, id, "shared"), http.StatusOK, "label a page to be closed")
		want(t, addLabel(t, owner, id, "secret-merger"), http.StatusOK, "a word only its readers may know")
	}
	want(t, restrict(t, owner, closed, []any{user(annID)}, nil), http.StatusOK, "restrict the closed plan to ann")
	want(t, restrict(t, owner, locked, nil, []any{user(annID)}), http.StatusOK, "only ann edits the locked plan")
	unpublished := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Ann's draft"}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
	want(t, addLabel(t, ann, unpublished, "draft-idea"), http.StatusOK, "ann labels her unpublished page")
	h.settle(t)

	t.Run("nothing leaks to somebody off the view list", func(t *testing.T) {
		if got := pageTitles(t, want(t, ben.get(t, "/api/v1/labels/shared/pages"), http.StatusOK, "shared")); !sameSet(got, []string{"Open plan"}) {
			t.Errorf("ben's shared lists %v", got)
		}
		r := want(t, ben.get(t, "/api/v1/labels/secret-merger/pages"), http.StatusOK, "the secret label")
		if number(r.Body["total"]) != 0 {
			t.Errorf("ben lists the secret label's pages: %s", r.Raw)
		}
		for _, q := range []string{"", "s", "secret", "d"} {
			for _, name := range labelsOf(t, want(t, ben.get(t, "/api/v1/labels?q="+q), http.StatusOK, "suggest "+q)) {
				if name != "shared" {
					t.Errorf("ben is offered %q for %q", name, q)
				}
			}
		}
		r = want(t, ben.get(t, "/api/v1/labels?q=shared"), http.StatusOK, "suggest shared")
		if n := number(list(t, r, "labels")[0].(map[string]any)["pages"]); n != 1 {
			t.Errorf("ben counts %d pages for shared", n)
		}
		if got := hitTitles(t, searchFor(t, ben, url.Values{"label": {"secret-merger"}})); len(got) != 0 {
			t.Errorf("ben finds %v by the secret label", got)
		}
		if got := hitTitles(t, searchFor(t, ben, url.Values{"label": {"shared"}})); !sameSet(got, []string{"Open plan"}) {
			t.Errorf("ben finds %v by shared", got)
		}
		want(t, ben.get(t, pagePath(closed, "/labels")), http.StatusNotFound, "ben reads the closed plan's labels")
		want(t, addLabel(t, ben, closed, "mine"), http.StatusNotFound, "ben labels the closed plan")
		want(t, ben.delete(t, pagePath(below, "/labels/shared")), http.StatusNotFound, "ben unlabels below it")
		want(t, addLabel(t, ben, locked, "mine"), http.StatusForbidden, "ben labels the locked plan")
	})

	t.Run("somebody on the list sees them all", func(t *testing.T) {
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/labels/shared/pages"), http.StatusOK, "shared")); !sameSet(got, []string{"Open plan", "Closed plan", "Below the closed plan"}) {
			t.Errorf("ann's shared lists %v", got)
		}
		if got := labelsOf(t, want(t, ann.get(t, "/api/v1/labels?q=s"), http.StatusOK, "suggest s")); len(got) != 2 || got[0] != "shared" || got[1] != "secret-merger" {
			t.Errorf("ann is offered %v", got)
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/labels/draft-idea/pages"), http.StatusOK, "her own draft")); !sameSet(got, []string{"Ann's draft"}) {
			t.Errorf("ann's draft-idea lists %v", got)
		}
		if got := pageTitles(t, want(t, owner.get(t, "/api/v1/labels/draft-idea/pages"), http.StatusOK, "somebody else's draft")); len(got) != 0 {
			t.Errorf("the owner lists ann's unpublished page: %v", got)
		}
	})

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

	t.Run("the database refuses what the service refuses", func(t *testing.T) {
		actAs(t, conn, home.org, benID)
		if n := count(`SELECT count(*) FROM page_label WHERE page_id = ANY ($1)`, []string{closed, below, unpublished}); n != 0 {
			t.Errorf("ben reads %d labels of pages he may not view", n)
		}
		if n := count(`SELECT count(*) FROM page_label WHERE name = 'secret-merger'`); n != 0 {
			t.Errorf("ben reads the secret label %d times", n)
		}
		if n := count(`SELECT count(*) FROM page_label WHERE page_id = $1`, open); n != 1 {
			t.Errorf("ben reads %d labels of the open page", n)
		}
		denied(t, conn, "labelling a page he may not view", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'mine', $3)`, home.org, closed, benID)
		denied(t, conn, "labelling a page he may not edit", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'mine', $3)`, home.org, locked, benID)
		denied(t, conn, "labelling in somebody else's name", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'mine', $3)`, home.org, open, annID)
		untouched(t, conn, "unlabelling a page he may not view", `DELETE FROM page_label WHERE page_id = $1`, closed)
		refused(t, conn, "renaming a label", `UPDATE page_label SET name = 'renamed' WHERE page_id = $1`, open)
		denied(t, conn, "a name that is no label", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'Not/Normal', $3)`, home.org, open, benID)
		if _, err := conn.Exec(ctx, `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'bens', $3)`, home.org, open, benID); err != nil {
			t.Errorf("ben may not label a page he edits: %v", err)
		}
		if _, err := conn.Exec(ctx, `DELETE FROM page_label WHERE page_id = $1 AND name = 'bens'`, open); err != nil {
			t.Errorf("ben may not take his label off: %v", err)
		}
	})

	t.Run("another organization reads and writes none of them", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM page_label`); n != 0 {
			t.Errorf("another organization reads %d labels", n)
		}
		refused(t, conn, "a label on another organization's page", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'planted', $3)`, other.org, open, other.user)
		refused(t, conn, "a label in another organization's name", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'planted', $3)`, home.org, open, other.user)
		untouched(t, conn, "deleting another organization's labels", `DELETE FROM page_label`)
	})
}
