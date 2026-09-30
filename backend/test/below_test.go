//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

// A child pages block lists the pages below its page that the reader may
// view: out of the trash, published or their own, and past every view list.
func TestPagesBelowFollowWhatTheReaderMayView(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "below")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)

	docs := newTree(t, owner, "BELOW", "Handbook")
	guide := docs.add(docs.homeID, "Guide")
	setup := docs.add(guide, "Setup")
	install := docs.add(setup, "Install")
	docs.add(install, "Linux")
	closed := docs.add(guide, "Closed")
	docs.add(closed, "Under closed")
	binned := docs.add(guide, "Binned")
	docs.add(binned, "Under binned")
	docs.add(guide, "appendix")
	draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": guide, "title": "Ann's draft"}), http.StatusCreated, "ann's unpublished page"), "page")["id"].(string)
	want(t, restrict(t, owner, closed, []any{user(annID)}, nil), http.StatusOK, "only ann views the closed page")
	want(t, owner.delete(t, pagePath(binned)), http.StatusNoContent, "trash a page")
	h.settle(t)

	below := func(c *client, id, query string) []string {
		t.Helper()
		return pageTitles(t, want(t, c.get(t, pagePath(id, "/below")+query), http.StatusOK, "below "+query))
	}

	t.Run("direct children or the subtree, down to a depth, in each order", func(t *testing.T) {
		for query, wanted := range map[string][]string{
			"":                                   {"Setup", "appendix"},
			"?scope=children&depth=3":            {"Setup", "appendix"},
			"?scope=subtree":                     {"Setup", "Install", "Linux", "appendix"},
			"?scope=subtree&depth=2":             {"Setup", "Install", "appendix"},
			"?scope=subtree&depth=1":             {"Setup", "appendix"},
			"?scope=children&sort=title":         {"appendix", "Setup"},
			"?scope=children&sort=updated":       {"appendix", "Setup"},
			"?scope=subtree&sort=title&depth=10": {"appendix", "Setup", "Install", "Linux"},
		} {
			if got := below(ben, guide, query); !slices.Equal(got, wanted) {
				t.Errorf("ben lists %v for %q, want %v", got, query, wanted)
			}
		}
		r := want(t, ben.get(t, pagePath(guide, "/below?scope=subtree")), http.StatusOK, "the subtree")
		linux := list(t, r, "pages")[2].(map[string]any)
		if number(linux["depth"]) != 3 || linux["parentId"] != install || linux["unpublished"] != false || linux["updatedAt"] == "" {
			t.Errorf("Linux is listed as %v", linux)
		}
		if r.Body["truncated"] != false {
			t.Errorf("a short list says truncated: %v", r.Body["truncated"])
		}
	})

	t.Run("a view list, an unpublished page and the trash hide pages and all below them", func(t *testing.T) {
		if got := below(ann, guide, "?scope=subtree"); !slices.Equal(got, []string{"Setup", "Install", "Linux", "Closed", "Under closed", "appendix", "Ann's draft"}) {
			t.Errorf("ann lists %v", got)
		}
		r := want(t, ann.get(t, pagePath(guide, "/below")), http.StatusOK, "ann's children")
		last := list(t, r, "pages")[3].(map[string]any)
		if last["title"] != "Ann's draft" || last["unpublished"] != true {
			t.Errorf("ann's own unpublished page is listed as %v", last)
		}
		if got := below(owner, guide, "?scope=subtree"); !slices.Equal(got, []string{"Setup", "Install", "Linux", "Closed", "Under closed", "appendix"}) {
			t.Errorf("an administrator lists %v, somebody else's unpublished page or the trash among them", got)
		}
		want(t, ben.get(t, pagePath(closed, "/below")), http.StatusNotFound, "ben lists below a page he may not view")
		want(t, owner.get(t, pagePath(draft, "/below")), http.StatusNotFound, "the owner lists below ann's unpublished page")
		want(t, owner.get(t, pagePath(binned, "/below")), http.StatusNotFound, "listing below a trashed page")
		if got := below(ann, closed, ""); !slices.Equal(got, []string{"Under closed"}) {
			t.Errorf("ann lists %v below the closed page", got)
		}
	})

	t.Run("what a block cannot hold is refused in a sentence", func(t *testing.T) {
		for _, query := range []string{"?scope=space", "?sort=created", "?scope=subtree&depth=0", "?scope=subtree&depth=11", "?depth=two"} {
			r := want(t, ben.get(t, pagePath(guide, "/below")+query), http.StatusUnprocessableEntity, query)
			if code := errorCode(t, r); code != "validation_failed" {
				t.Errorf("%s is refused with %s", query, code)
			}
		}
		want(t, ben.get(t, "/api/v1/pages/not-a-page/below"), http.StatusBadRequest, "a malformed id")
	})

	t.Run("the database hides the same pages from a raw read", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, benID)
		var n int
		if err := conn.QueryRow(context.Background(), `
			WITH RECURSIVE below (id) AS (
				SELECT id FROM page WHERE parent_id = $1
				UNION ALL
				SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
			)
			SELECT count(*) FROM page WHERE id IN (SELECT id FROM below) AND title IN ('Closed', 'Under closed', 'Ann''s draft')`, guide).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("ben reads %d pages below the guide that he may not view", n)
		}
	})
}
