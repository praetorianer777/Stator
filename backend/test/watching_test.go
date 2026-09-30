//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// watchingOf reads a page's watching as its reader sees it.
func watchingOf(t *testing.T, c *client, page string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page", "watching")
}

func watchPage(t *testing.T, c *client, page string, subtree bool) response {
	t.Helper()
	body := map[string]any{}
	if subtree {
		body["subtree"] = true
	}
	return c.put(t, pagePath(page, "/watch"), body)
}

// watcherNames lists a page's watchers as name=via.
func watcherNames(t *testing.T, c *client, page string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, pagePath(page, "/watchers")), http.StatusOK, "list the watchers"), "watchers") {
		w := each.(map[string]any)
		out[w["name"].(string)] = w["via"].(string)
	}
	return out
}

// namedPerson adds a member with a name of their own, so lists by name can be read.
func (h *harness) namedPerson(t *testing.T, org uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := h.addPerson(t, org, "member")
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET name = $2 WHERE id = $1`, id, name); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	return id
}

// Every watching operation, done once and refused once, with what each kind
// of watch covers and how the page tells its reader.
func TestWatchingOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "watching")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.namedPerson(t, home.org, "Ann Watcher"), h.namedPerson(t, home.org, "Ben Watcher")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	nobody := api.anonymous()

	docs := newTree(t, owner, "WAT", "Watched")
	top := docs.add(docs.homeID, "Top")
	child := docs.add(top, "Child")
	aside := docs.add(docs.homeID, "Aside")

	t.Run("creating and publishing a page makes its author watch it", func(t *testing.T) {
		if w := watchingOf(t, owner, child); w["page"] != true || w["subtree"] != false || w["inherited"] != nil {
			t.Errorf("the author's watching is %v", w)
		}
		if w := watchingOf(t, ann, child); w["page"] != false || w["inherited"] != nil {
			t.Errorf("ann's watching is %v before she watches anything", w)
		}
	})

	t.Run("a page, a subtree and a space are watched, the nearest covering first", func(t *testing.T) {
		got := obj(t, want(t, watchPage(t, ann, child, false), http.StatusOK, "ann watches Child"), "watching")
		if got["page"] != true {
			t.Errorf("watching Child answers %v", got)
		}
		want(t, watchPage(t, ann, top, true), http.StatusOK, "ann watches Top and below")
		w := watchingOf(t, ann, child)
		inherited, _ := w["inherited"].(map[string]any)
		if w["page"] != true || inherited == nil || inherited["kind"] != "subtree" || inherited["page"].(map[string]any)["title"] != "Top" {
			t.Errorf("Child's watching for ann is %v", w)
		}
		want(t, ben.put(t, "/api/v1/spaces/WAT/watch", nil), http.StatusNoContent, "ben watches the space")
		want(t, ben.put(t, "/api/v1/spaces/WAT/watch", nil), http.StatusNoContent, "watching it again")
		if sp := obj(t, want(t, ben.get(t, "/api/v1/spaces/WAT"), http.StatusOK, "the space"), "space"); sp["watching"] != true {
			t.Errorf("the space says ben watches it: %v", sp["watching"])
		}
		if w := watchingOf(t, ben, aside); w["inherited"].(map[string]any)["kind"] != "space" || w["inherited"].(map[string]any)["page"] != nil {
			t.Errorf("Aside's watching for ben is %v", w)
		}
		names := watcherNames(t, owner, child)
		if names["Ann Watcher"] != "page" || names["Ben Watcher"] != "space" || names["Person of watching"] != "page" || len(names) != 3 {
			t.Errorf("Child's watchers are %v", names)
		}
		if got := watcherNames(t, ann, aside); got["Ann Watcher"] != "" || got["Ben Watcher"] != "space" {
			t.Errorf("Aside's watchers are %v", got)
		}
		r := want(t, ann.get(t, "/api/v1/watches"), http.StatusOK, "ann's watches")
		all := list(t, r, "watches")
		if len(all) != 2 || number(r.Body["total"]) != 2 {
			t.Fatalf("ann lists %s", r.Raw)
		}
		latest := all[0].(map[string]any)
		if latest["kind"] != "subtree" || latest["spaceKey"] != "WAT" || latest["spaceName"] != "Watched" || latest["page"].(map[string]any)["title"] != "Top" {
			t.Errorf("ann's latest watch is %v", latest)
		}
		if got := list(t, want(t, ben.get(t, "/api/v1/watches?limit=1"), http.StatusOK, "ben's watches"), "watches"); len(got) != 1 || got[0].(map[string]any)["page"] != nil {
			t.Errorf("ben lists %v", got)
		}
	})

	t.Run("a moved page is covered by its new place, not its old one", func(t *testing.T) {
		want(t, docs.move(child, map[string]any{"parentId": aside}), http.StatusOK, "move Child under Aside")
		h.settle(t)
		w := watchingOf(t, ann, child)
		if w["inherited"] != nil {
			t.Errorf("Child under Aside still inherits %v for ann", w["inherited"])
		}
		want(t, docs.move(child, map[string]any{"parentId": top}), http.StatusOK, "move it back")
	})

	t.Run("unwatching sticks through one's own edits", func(t *testing.T) {
		want(t, ann.delete(t, pagePath(child, "/watch")), http.StatusNoContent, "ann stops watching Child")
		want(t, ann.delete(t, pagePath(child, "/watch")), http.StatusNoContent, "and again")
		publishDraft(t, ann, child, "Child", "Ann's edit.", false)
		if w := watchingOf(t, ann, child); w["page"] != false || w["inherited"].(map[string]any)["kind"] != "subtree" {
			t.Errorf("after her edit ann's watching is %v", w)
		}
		want(t, watchPage(t, ann, child, false), http.StatusOK, "ann watches Child explicitly")
		want(t, ann.delete(t, pagePath(child, "/watch")), http.StatusNoContent, "and stops again")
		want(t, owner.delete(t, "/api/v1/spaces/WAT/watch"), http.StatusNoContent, "the owner stops watching a space he never watched")
		want(t, ben.delete(t, "/api/v1/spaces/WAT/watch"), http.StatusNoContent, "ben stops watching the space")
		if w := watchingOf(t, ben, aside); w["inherited"] != nil {
			t.Errorf("after unwatching the space ben's watching is %v", w)
		}
	})

	t.Run("somebody who turned auto watch off watches nothing they make", func(t *testing.T) {
		prefs := obj(t, want(t, ben.get(t, "/api/v1/notification-preferences"), http.StatusOK, "ben's preferences"), "preferences")
		prefs["autoWatch"] = false
		want(t, ben.put(t, "/api/v1/notification-preferences", prefs), http.StatusOK, "ben turns auto watch off")
		made := obj(t, want(t, ben.post(t, "/api/v1/pages", map[string]any{"parentId": aside, "title": "Ben's", "publish": true}), http.StatusCreated, "ben adds a page"), "page")
		if w := made["watching"].(map[string]any); w["page"] != false {
			t.Errorf("ben watches his own page: %v", w)
		}
	})

	t.Run("nobody watches what they may not view, and a watch on it tells them nothing", func(t *testing.T) {
		want(t, watchPage(t, ben, top, true), http.StatusOK, "ben watches Top and below")
		want(t, restrict(t, owner, top, []any{user(home.user), user(annID)}, nil), http.StatusOK, "Top is for the owner and ann")
		h.settle(t)
		want(t, watchPage(t, ben, child, false), http.StatusNotFound, "ben watches a page he may not view")
		want(t, ben.delete(t, pagePath(child, "/watch")), http.StatusNotFound, "ben unwatches it")
		want(t, ben.get(t, pagePath(child, "/watchers")), http.StatusNotFound, "ben lists its watchers")
		if names := watcherNames(t, ann, child); names["Ben Watcher"] != "" {
			t.Errorf("ben is still listed as a watcher of a page he may not view: %v", names)
		}
		for _, each := range list(t, want(t, ben.get(t, "/api/v1/watches"), http.StatusOK, "ben's watches"), "watches") {
			if p, _ := each.(map[string]any)["page"].(map[string]any); p != nil && p["title"] == "Top" {
				t.Errorf("ben lists a watch on a page he may not view")
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM watch WHERE user_id = $1 AND page_id = $2`, benID, top); n != 1 {
			t.Errorf("ben's watch on Top was not kept: %d", n)
		}
		want(t, restrict(t, owner, top, nil, nil), http.StatusOK, "Top is open again")
	})

	t.Run("a trashed page's watches wait for it", func(t *testing.T) {
		want(t, watchPage(t, ann, aside, false), http.StatusOK, "ann watches Aside")
		want(t, owner.delete(t, pagePath(aside)), http.StatusNoContent, "Aside goes to the trash")
		h.settle(t)
		want(t, watchPage(t, ann, aside, false), http.StatusNotFound, "watching a trashed page")
		for _, each := range list(t, want(t, ann.get(t, "/api/v1/watches"), http.StatusOK, "ann's watches"), "watches") {
			if p, _ := each.(map[string]any)["page"].(map[string]any); p != nil && p["title"] == "Aside" {
				t.Errorf("ann lists a watch on a trashed page")
			}
		}
		want(t, docs.restore(aside), http.StatusOK, "Aside comes back")
		h.settle(t)
		if w := watchingOf(t, ann, aside); w["page"] != true {
			t.Errorf("after the restore ann's watching of Aside is %v", w)
		}
	})

	t.Run("what is not so is refused in a sentence", func(t *testing.T) {
		missing := uuid.NewString()
		for what, r := range map[string]response{
			"watch no page":         watchPage(t, ann, missing, false),
			"unwatch no page":       ann.delete(t, pagePath(missing, "/watch")),
			"no page's watchers":    ann.get(t, pagePath(missing, "/watchers")),
			"watch no space":        ann.put(t, "/api/v1/spaces/NOPE/watch", nil),
			"unwatch no space":      ann.delete(t, "/api/v1/spaces/NOPE/watch"),
			"too many watchers":     ann.get(t, pagePath(top, "/watchers?limit=0")),
			"too many watches":      ann.get(t, "/api/v1/watches?limit=101"),
			"an unknown field":      ann.put(t, pagePath(top, "/watch"), map[string]any{"everything": true}),
			"nobody lists watches":  nobody.get(t, "/api/v1/watches"),
			"nobody watches a page": watchPage(t, nobody, top, false),
		} {
			if r.Status < 400 || r.Status >= 500 {
				t.Errorf("%s answered %d", what, r.Status)
				continue
			}
			errorCode(t, r)
		}
	})
}
