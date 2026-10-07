//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// textDoc is a document of one paragraph per line.
func textDoc(lines ...string) map[string]any {
	var blocks []any
	for _, line := range lines {
		blocks = append(blocks, map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": line}}})
	}
	return map[string]any{"type": "doc", "content": blocks}
}

func pagePath(id string, rest ...string) string {
	return "/api/v1/pages/" + id + strings.Join(rest, "")
}

func number(v any) int {
	f, _ := v.(float64)
	return int(f)
}

// versionNumbers lists the numbers of a page of history, in the order given.
func versionNumbers(t *testing.T, r response) []int {
	t.Helper()
	var out []int
	for _, each := range list(t, r, "versions") {
		out = append(out, number(each.(map[string]any)["number"]))
	}
	return out
}

func sameNumbers(t *testing.T, what string, got []int, want ...int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: %v, want %v", what, got, want)
		}
	}
}

// A page begins as its creator's alone, drafts are private, a publish that
// raced another is refused, and every publish is a numbered version.
func TestDraftsAndPublishingOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "drafts")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	ann := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	ben := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "DRAFT", "Drafts")

	var pageID, childID string

	t.Run("a new page is its creator's alone until published", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Plan", "body": textDoc("Idea")}), http.StatusCreated, "make a page"), "page")
		pageID = made["id"].(string)
		if number(made["version"]) != 0 || made["unpublished"] != true {
			t.Fatalf("a new page is %v", made)
		}
		childID = obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": pageID, "title": "Detail", "publish": true}), http.StatusCreated, "publish a page under it"), "page")["id"].(string)
		for what, got := range map[string]response{
			"read it":         ben.get(t, pagePath(pageID)),
			"read below it":   ben.get(t, pagePath(childID)),
			"list under it":   ben.get(t, "/api/v1/spaces/DRAFT/pages?parent="+pageID),
			"add under it":    ben.post(t, "/api/v1/pages", map[string]any{"parentId": pageID, "title": "Mine"}),
			"draft it":        ben.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": textDoc("x"), "baseVersion": 0}),
			"read its drafts": ben.get(t, pagePath(pageID, "/draft")),
			"publish it":      ben.post(t, pagePath(pageID, "/publish"), map[string]any{}),
			"read history":    ben.get(t, pagePath(pageID, "/versions")),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("somebody else could %s: %d %s", what, got.Status, got.Raw)
			}
		}
		docs.c = ben
		if titles := docs.titles(""); len(titles) != 0 {
			t.Fatalf("somebody else's tree shows %v", titles)
		}
		var outline []string
		for _, each := range list(t, want(t, ben.get(t, "/api/v1/spaces/DRAFT/outline"), http.StatusOK, "outline"), "pages") {
			outline = append(outline, each.(map[string]any)["title"].(string))
		}
		sameTitles(t, "somebody else's outline", outline, "Drafts")
		mine := list(t, want(t, ann.get(t, "/api/v1/spaces/DRAFT/pages"), http.StatusOK, "the creator's tree"), "pages")
		if len(mine) != 1 || mine[0].(map[string]any)["unpublished"] != true || mine[0].(map[string]any)["hasChildren"] != true {
			t.Fatalf("the creator's tree shows %v", mine)
		}
		scratch := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Scratch"}), http.StatusCreated, "make scratch"), "page")["id"].(string)
		want(t, ann.delete(t, pagePath(scratch)), http.StatusNoContent, "trash scratch")
		if items := docs.trash(); len(items) != 0 {
			t.Fatalf("somebody else's trash shows %v", items)
		}
		docs.c = ann
		if items := docs.trash(); len(items) != 1 {
			t.Fatalf("the creator's trash shows %v", items)
		}
		if got := number(obj(t, want(t, ann.get(t, pagePath(pageID, "/versions")), http.StatusOK, "history"))["total"]); got != 0 {
			t.Fatalf("an unpublished page has %d versions", got)
		}
	})

	t.Run("publishing an unpublished page without a draft publishes what it was made with", func(t *testing.T) {
		got := want(t, ann.post(t, pagePath(pageID, "/publish"), map[string]any{"comment": "  First cut  ", "notifyWatchers": true}), http.StatusOK, "publish")
		p, v := obj(t, got, "page"), obj(t, got, "version")
		if number(p["version"]) != 1 || p["unpublished"] != false || number(v["number"]) != 1 || v["comment"] != "First cut" || v["authorName"] != "A member" {
			t.Fatalf("the publish answered %s", got.Raw)
		}
		want(t, ben.get(t, pagePath(pageID)), http.StatusOK, "now others read it")
		want(t, ben.get(t, pagePath(childID)), http.StatusOK, "and what is below it")
	})

	t.Run("a draft is private and publishing it makes the next version", func(t *testing.T) {
		if d := want(t, ann.get(t, pagePath(pageID, "/draft")), http.StatusOK, "no draft yet"); d.Body["draft"] != nil {
			t.Fatalf("a draft before any edit: %s", d.Raw)
		}
		saved := obj(t, want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": textDoc("Idea", "More"), "baseVersion": 1}), http.StatusOK, "autosave"), "draft")
		if number(saved["baseVersion"]) != 1 || saved["title"] != "Plan" {
			t.Fatalf("the draft is %v", saved)
		}
		want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan B", "body": textDoc("Idea", "More"), "baseVersion": 1}), http.StatusOK, "autosave again")
		if ref := obj(t, want(t, ann.get(t, pagePath(pageID)), http.StatusOK, "read"), "page", "draft"); number(ref["baseVersion"]) != 1 {
			t.Fatalf("the page does not tell its author of the draft: %v", ref)
		}
		theirs := want(t, ben.get(t, pagePath(pageID)), http.StatusOK, "another reads")
		if obj(t, theirs, "page")["draft"] != nil || obj(t, theirs, "page")["title"] != "Plan" {
			t.Fatalf("somebody else sees the draft: %s", theirs.Raw)
		}
		if d := want(t, ben.get(t, pagePath(pageID, "/draft")), http.StatusOK, "another's draft"); d.Body["draft"] != nil {
			t.Fatalf("somebody else read the draft: %s", d.Raw)
		}
		for what, got := range map[string]response{
			"a blank title":        ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": " ", "body": textDoc("x"), "baseVersion": 1}),
			"no body":              ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "baseVersion": 1}),
			"a body out of bounds": ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "script"}}}, "baseVersion": 1}),
			"a version to come":    ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": textDoc("x"), "baseVersion": 7}),
			"a long comment":       ann.post(t, pagePath(pageID, "/publish"), map[string]any{"comment": strings.Repeat("c", 501)}),
		} {
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		got := want(t, ann.post(t, pagePath(pageID, "/publish"), map[string]any{"comment": "More detail"}), http.StatusOK, "publish the draft")
		if p := obj(t, got, "page"); number(p["version"]) != 2 || p["title"] != "Plan B" || p["draft"] != nil {
			t.Fatalf("the publish answered %s", got.Raw)
		}
		if d := want(t, ann.get(t, pagePath(pageID, "/draft")), http.StatusOK, "the draft after"); d.Body["draft"] != nil {
			t.Fatalf("the published draft is still there: %s", d.Raw)
		}
		again := ann.post(t, pagePath(pageID, "/publish"), map[string]any{})
		if again.Status != http.StatusConflict || errorCode(t, again) != "no_draft" {
			t.Fatalf("publishing nothing: %d %s", again.Status, again.Raw)
		}
	})

	t.Run("a draft begun before somebody else's publish conflicts until it takes that as its base", func(t *testing.T) {
		want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan B", "body": textDoc("Idea", "Ann's"), "baseVersion": 2}), http.StatusOK, "ann drafts")
		want(t, ben.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan B", "body": textDoc("Idea", "Ben's"), "baseVersion": 2}), http.StatusOK, "ben drafts")
		want(t, ben.post(t, pagePath(pageID, "/publish"), map[string]any{}), http.StatusOK, "ben publishes first")
		late := ann.post(t, pagePath(pageID, "/publish"), map[string]any{"comment": "Mine"})
		if late.Status != http.StatusConflict || errorCode(t, late) != "publish_conflict" {
			t.Fatalf("a publish over a newer version: %d %s", late.Status, late.Raw)
		}
		cmp := obj(t, want(t, ann.get(t, pagePath(pageID, "/compare?from=3&to=draft")), http.StatusOK, "compare with the draft"), "comparison")
		if number(obj(t, response{Body: cmp}, "from")["number"]) != 3 || obj(t, response{Body: cmp}, "to")["draft"] != true {
			t.Fatalf("the comparison is %v", cmp)
		}
		want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan B", "body": textDoc("Idea", "Ann's"), "baseVersion": 3}), http.StatusOK, "save over theirs")
		if p := obj(t, want(t, ann.post(t, pagePath(pageID, "/publish"), map[string]any{"comment": "Mine"}), http.StatusOK, "publish again"), "page"); number(p["version"]) != 4 {
			t.Fatalf("the second try made %v", p["version"])
		}
	})

	t.Run("a save through PATCH publishes a version with no comment and leaves drafts alone", func(t *testing.T) {
		want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan C", "body": textDoc("Later"), "baseVersion": 4}), http.StatusOK, "ann drafts")
		saved := obj(t, want(t, ben.patch(t, pagePath(pageID), map[string]any{"body": textDoc("Idea", "Patched"), "version": 4}), http.StatusOK, "patch"), "page")
		if number(saved["version"]) != 5 {
			t.Fatalf("a patch made version %v", saved["version"])
		}
		stale := ben.patch(t, pagePath(pageID), map[string]any{"title": "Old", "version": 4})
		if stale.Status != http.StatusConflict || errorCode(t, stale) != "conflict" {
			t.Fatalf("a patch over a newer version: %d %s", stale.Status, stale.Raw)
		}
		if d := obj(t, want(t, ann.get(t, pagePath(pageID, "/draft")), http.StatusOK, "ann's draft"), "draft"); d["title"] != "Plan C" {
			t.Fatalf("the patch touched a draft: %v", d)
		}
		if v := list(t, want(t, ann.get(t, pagePath(pageID, "/versions?limit=1")), http.StatusOK, "latest"), "versions")[0].(map[string]any); v["comment"] != "" {
			t.Fatalf("a patch left the comment %v", v["comment"])
		}
	})

	t.Run("discarding a draft leaves the page as published", func(t *testing.T) {
		want(t, ann.delete(t, pagePath(pageID, "/draft")), http.StatusNoContent, "discard")
		want(t, ann.delete(t, pagePath(pageID, "/draft")), http.StatusNoContent, "discard nothing")
		if d := want(t, ann.get(t, pagePath(pageID, "/draft")), http.StatusOK, "after"); d.Body["draft"] != nil {
			t.Fatalf("the draft survived: %s", d.Raw)
		}
		if p := obj(t, want(t, ann.get(t, pagePath(pageID)), http.StatusOK, "read"), "page"); number(p["version"]) != 5 || p["draft"] != nil {
			t.Fatalf("the page is %v", p)
		}
		if got := ann.delete(t, pagePath(uuid.NewString(), "/draft")); got.Status != http.StatusNotFound {
			t.Errorf("discarding on no page: %d", got.Status)
		}
	})

	t.Run("new pages made published, spaces' home pages and copies start at version 1", func(t *testing.T) {
		for what, id := range map[string]string{
			"published at once": obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Now", "publish": true}), http.StatusCreated, "publish at once"), "page")["id"].(string),
			"a home page":       docs.homeID,
			"a copy":            obj(t, want(t, ann.post(t, pagePath(pageID, "/copy"), map[string]any{"parentId": docs.homeID, "withChildren": true}), http.StatusCreated, "copy"), "page")["id"].(string),
		} {
			got := want(t, ben.get(t, pagePath(id, "/versions")), http.StatusOK, what)
			if number(got.Body["total"]) != 1 {
				t.Errorf("%s has %s", what, got.Raw)
			}
		}
	})

	t.Run("a trashed page's drafts and history are gone with it", func(t *testing.T) {
		want(t, ann.put(t, pagePath(childID, "/draft"), map[string]any{"title": "Detail", "body": textDoc("x"), "baseVersion": 1}), http.StatusOK, "draft the child")
		want(t, ann.delete(t, pagePath(childID)), http.StatusNoContent, "trash the child")
		for what, got := range map[string]response{
			"draft":   ann.get(t, pagePath(childID, "/draft")),
			"history": ann.get(t, pagePath(childID, "/versions")),
			"version": ann.get(t, pagePath(childID, "/versions/1")),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("the trashed page's %s: %d", what, got.Status)
			}
		}
		want(t, owner.delete(t, "/api/v1/spaces/DRAFT/trash/"+childID), http.StatusNoContent, "purge")
		for _, table := range []string{"page_version", "page_draft"} {
			if n := h.count(t, home.ctx, `SELECT count(*) FROM `+table+` WHERE page_id = $1`, childID); n != 0 {
				t.Errorf("%d rows of %s outlived the purge", n, table)
			}
		}
	})
}

// A publish conflict tells its caller of somebody else's newer version, so
// their next reads show it, even when a replica has not replayed it.
func TestAPublishConflictShowsItsCallerTheNewerVersion(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "conflict-read")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)
	ben := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "RACE", "Race")

	made := want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Plan", "body": textDoc("Idea"), "publish": true}), http.StatusCreated, "make a page")
	pageID := obj(t, made, "page")["id"].(string)
	want(t, ann.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": textDoc("Idea", "Ann's"), "baseVersion": 1}), http.StatusOK, "ann drafts")
	want(t, ben.put(t, pagePath(pageID, "/draft"), map[string]any{"title": "Plan", "body": textDoc("Idea", "Ben's"), "baseVersion": 1}), http.StatusOK, "ben drafts")
	h.settle(t)
	h.pauseReplay(t)
	ann.eager, ben.eager = true, true

	want(t, ben.post(t, pagePath(pageID, "/publish"), map[string]any{}), http.StatusOK, "ben publishes first")
	late := ann.post(t, pagePath(pageID, "/publish"), map[string]any{})
	if late.Status != http.StatusConflict || errorCode(t, late) != "publish_conflict" {
		t.Fatalf("a publish over a newer version: %d %s", late.Status, late.Raw)
	}

	stranger := api.as(t, annID, home.org, slug)
	stranger.eager = true
	if p := obj(t, want(t, stranger.get(t, pagePath(pageID)), http.StatusOK, "a fresh browser reads"), "page"); number(p["version"]) != 1 {
		t.Fatalf("a fresh browser read version %v, so the replica was not behind", p["version"])
	}
	if p := obj(t, want(t, ann.get(t, pagePath(pageID)), http.StatusOK, "ann reads after the refusal"), "page"); number(p["version"]) != 2 {
		t.Fatalf("after a conflict with version 2 its caller read version %v", p["version"])
	}
	cmp := obj(t, want(t, ann.get(t, pagePath(pageID, "/compare?from=2&to=draft")), http.StatusOK, "compare the newer version with the draft"), "comparison")
	if number(obj(t, response{Body: cmp}, "from")["number"]) != 2 {
		t.Fatalf("the comparison is %v", cmp)
	}
}

// History lists every version, any two compare, and a restore publishes an
// old version again as the newest.
func TestHistoryCompareAndRestoreOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "history")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "HIST", "History")

	made := obj(t, want(t, member.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Guide", "body": textDoc("Install it", "Run it"), "publish": true}), http.StatusCreated, "v1"), "page")
	id := made["id"].(string)
	want(t, member.patch(t, pagePath(id), map[string]any{"title": "Guide", "body": textDoc("Install it quickly", "Run it", "Enjoy"), "version": 1}), http.StatusOK, "v2")
	want(t, member.put(t, pagePath(id, "/draft"), map[string]any{"title": "The guide", "body": textDoc("Install it quickly", "Enjoy"), "baseVersion": 2}), http.StatusOK, "draft")
	want(t, member.post(t, pagePath(id, "/publish"), map[string]any{"comment": "Shorter"}), http.StatusOK, "v3")

	t.Run("history lists every version, the latest first, a page at a time", func(t *testing.T) {
		all := want(t, owner.get(t, pagePath(id, "/versions")), http.StatusOK, "history")
		sameNumbers(t, "all", versionNumbers(t, all), 3, 2, 1)
		first := list(t, all, "versions")[0].(map[string]any)
		if first["comment"] != "Shorter" || first["title"] != "The guide" || first["authorName"] != "A member" || first["restoredFrom"] != nil || number(all.Body["total"]) != 3 || number(all.Body["limit"]) != 20 {
			t.Fatalf("the history is %s", all.Raw)
		}
		sameNumbers(t, "a page", versionNumbers(t, want(t, owner.get(t, pagePath(id, "/versions?limit=1&offset=1")), http.StatusOK, "paged")), 2)
		for _, q := range []string{"limit=0", "limit=101", "offset=-1", "limit=x"} {
			if got := owner.get(t, pagePath(id, "/versions?"+q)); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", q, got.Status, got.Raw)
			}
		}
	})

	t.Run("any version reads as it was", func(t *testing.T) {
		v := obj(t, want(t, owner.get(t, pagePath(id, "/versions/1")), http.StatusOK, "v1"), "version")
		if number(v["number"]) != 1 || v["title"] != "Guide" || len(v["body"].(map[string]any)["content"].([]any)) != 2 {
			t.Fatalf("version 1 is %v", v)
		}
		if got := owner.get(t, pagePath(id, "/versions/4")); got.Status != http.StatusNotFound {
			t.Errorf("a version to come: %d", got.Status)
		}
		if got := owner.get(t, pagePath(id, "/versions/first")); got.Status != http.StatusBadRequest {
			t.Errorf("a version that is not a number: %d", got.Status)
		}
	})

	t.Run("two versions compare block by block with inline marks", func(t *testing.T) {
		c := want(t, owner.get(t, pagePath(id, "/compare?from=1&to=2")), http.StatusOK, "1 to 2")
		var got []string
		for _, b := range list(t, response{Body: obj(t, c, "comparison")}, "blocks") {
			got = append(got, b.(map[string]any)["change"].(string))
		}
		sameTitles(t, "changes", got, "modified", "equal", "inserted")
		if !strings.Contains(string(c.Raw), `"diffInsert"`) {
			t.Fatalf("the modified block carries no marks: %s", c.Raw)
		}
		latest := obj(t, want(t, owner.get(t, pagePath(id, "/compare")), http.StatusOK, "defaults"), "comparison")
		from, to := latest["from"].(map[string]any), latest["to"].(map[string]any)
		if number(from["number"]) != 2 || number(to["number"]) != 3 || from["title"] != "Guide" || to["title"] != "The guide" {
			t.Fatalf("the default comparison is %v to %v", from, to)
		}
		first := obj(t, want(t, owner.get(t, pagePath(id, "/compare?to=1")), http.StatusOK, "the first"), "comparison")
		if f := first["from"].(map[string]any); number(f["number"]) != 0 || f["draft"] != false {
			t.Fatalf("the first version compares with %v", f)
		}
		backwards := want(t, owner.get(t, pagePath(id, "/compare?from=3&to=1")), http.StatusOK, "either order")
		if len(list(t, response{Body: obj(t, backwards, "comparison")}, "blocks")) == 0 {
			t.Fatalf("a backwards comparison is empty: %s", backwards.Raw)
		}
		for what, got := range map[string]response{
			"a side that is no version": owner.get(t, pagePath(id, "/compare?from=latest")),
			"a negative side":           owner.get(t, pagePath(id, "/compare?to=-1")),
		} {
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		for what, got := range map[string]response{
			"a draft the caller lacks": owner.get(t, pagePath(id, "/compare?to=draft")),
			"a version to come":        owner.get(t, pagePath(id, "/compare?to=9")),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a restore publishes the old version again and conflicts with anything newer", func(t *testing.T) {
		want(t, owner.put(t, pagePath(id, "/draft"), map[string]any{"title": "Mine", "body": textDoc("x"), "baseVersion": 3}), http.StatusOK, "a draft on 3")
		got := want(t, member.post(t, pagePath(id, "/versions/1/restore"), map[string]any{"baseVersion": 3}), http.StatusOK, "restore 1")
		p, v := obj(t, got, "page"), obj(t, got, "version")
		if number(p["version"]) != 4 || p["title"] != "Guide" || number(v["number"]) != 4 || number(v["restoredFrom"]) != 1 || v["comment"] != "Restored version 1" {
			t.Fatalf("the restore answered %s", got.Raw)
		}
		if c := obj(t, want(t, owner.get(t, pagePath(id, "/compare?from=1&to=4")), http.StatusOK, "1 and 4"), "comparison"); len(c["blocks"].([]any)) != 2 {
			t.Fatalf("version 4 is not version 1: %v", c)
		}
		for what, got := range map[string]response{
			"from an old history": member.post(t, pagePath(id, "/versions/2/restore"), map[string]any{"baseVersion": 3}),
			"of the latest":       member.post(t, pagePath(id, "/versions/4/restore"), map[string]any{"baseVersion": 4}),
		} {
			if got.Status != http.StatusConflict || errorCode(t, got) != "conflict" {
				t.Errorf("a restore %s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.post(t, pagePath(id, "/versions/9/restore"), map[string]any{"baseVersion": 4}); got.Status != http.StatusNotFound {
			t.Errorf("a restore of a version to come: %d", got.Status)
		}
		named := obj(t, want(t, member.post(t, pagePath(id, "/versions/3/restore"), map[string]any{"baseVersion": 4, "comment": "Back to short"}), http.StatusOK, "restore 3"), "version")
		if named["comment"] != "Back to short" || number(named["restoredFrom"]) != 3 {
			t.Fatalf("the named restore is %v", named)
		}
		late := owner.post(t, pagePath(id, "/publish"), map[string]any{})
		if late.Status != http.StatusConflict || errorCode(t, late) != "publish_conflict" {
			t.Fatalf("a draft from before the restore: %d %s", late.Status, late.Raw)
		}
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		away := h.makeMember(t, "history-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		for what, got := range map[string]response{
			"history": stranger.get(t, pagePath(id, "/versions")),
			"version": stranger.get(t, pagePath(id, "/versions/1")),
			"compare": stranger.get(t, pagePath(id, "/compare")),
			"restore": stranger.post(t, pagePath(id, "/versions/1/restore"), map[string]any{"baseVersion": 5}),
			"draft":   stranger.put(t, pagePath(id, "/draft"), map[string]any{"title": "x", "body": textDoc("x"), "baseVersion": 5}),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("a stranger reached the %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}

// Straight through SQL as stator_app, versions and drafts stay inside their
// organization, history cannot be rewritten, and numbers cannot skip.
func TestVersionsAndDraftsAreGuardedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "version-wall-a")
	b := h.makeMember(t, "version-wall-b")
	ownerA := api.as(t, a.user, a.org, h.slugOf(t, a.org))
	docsA := newTree(t, ownerA, "WALL", "Walled")
	docsB := newTree(t, api.as(t, b.user, b.org, h.slugOf(t, b.org)), "WALL", "Theirs")
	pageA := docsA.add(docsA.homeID, "Secret")
	pageB := docsB.add(docsB.homeID, "Theirs")
	want(t, ownerA.put(t, pagePath(pageA, "/draft"), map[string]any{"title": "Secret", "body": textDoc("draft"), "baseVersion": 1}), http.StatusOK, "a draft in A")

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, b.org, b.user)
	for _, table := range []string{"page_version", "page_draft"} {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE page_id = $1`, pageA).Scan(&n); err != nil || n != 0 {
			t.Errorf("B reads %d rows of A's %s (%v)", n, table, err)
		}
	}
	refused(t, conn, "a version of A's page, as A", `INSERT INTO page_version (org_id, page_id, number, title, body) VALUES ($1, $2, 2, 'Planted', '{}')`, a.org, pageA)
	refused(t, conn, "a version of A's page, as B", `INSERT INTO page_version (org_id, page_id, number, title, body) VALUES ($1, $2, 2, 'Planted', '{}')`, b.org, pageA)
	refused(t, conn, "a draft of A's page", `INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version) VALUES ($1, $2, $3, 'Planted', '{}', 1)`, b.org, pageA, b.user)
	untouched(t, conn, "A's drafts from B", `UPDATE page_draft SET title = 'Taken' WHERE page_id = $1`, pageA)
	untouched(t, conn, "deleting A's drafts from B", `DELETE FROM page_draft WHERE page_id = $1`, pageA)

	// Only a live page's open version takes a save; a published one, none.
	untouched(t, conn, "rewriting B's own history", `UPDATE page_version SET title = 'Rewritten' WHERE page_id = $1`, pageB)
	refused(t, conn, "rewriting B's own history's comment", `UPDATE page_version SET comment = 'Rewritten' WHERE page_id = $1`, pageB)
	refused(t, conn, "deleting B's own history", `DELETE FROM page_version WHERE page_id = $1`, pageB)
	refused(t, conn, "skipping a version number", `INSERT INTO page_version (org_id, page_id, number, title, body) VALUES ($1, $2, 3, 'Skipped', '{}')`, b.org, pageB)
	refused(t, conn, "reusing a version number", `INSERT INTO page_version (org_id, page_id, number, title, body) VALUES ($1, $2, 1, 'Again', '{}')`, b.org, pageB)
	refused(t, conn, "a draft of somebody not in the organization", `INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version) VALUES ($1, $2, $3, 'Planted', '{}', 1)`, b.org, pageB, a.user)

	if n := h.count(t, a.ctx, `SELECT count(*) FROM page_draft WHERE page_id = $1 AND title = 'Secret'`, pageA); n != 1 {
		t.Errorf("A's draft is gone or changed: %d", n)
	}
}
