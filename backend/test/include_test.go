//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Includes (#53): a page shows another page, or one excerpt of it, as each
// reader may read it; a reader who may not sees a notice, and an include that
// leads back to a page on the way to it is refused.

func includeOf(pageID string, excerptID any) map[string]any {
	return map[string]any{"type": "include", "attrs": map[string]any{"pageId": pageID, "excerptId": excerptID}}
}

func TestIncludesShowWhatEachReaderMayRead(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "include")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "INC", "Includes")
	support := docs.add(docs.homeID, "Support", map[string]any{"body": docOf(
		lineOf("All of support"),
		excerptOf(excerptHours, "Hours", lineOf("Nine to five")),
	)})
	secret := docs.add(docs.homeID, "Secret", map[string]any{"body": docOf(lineOf("Salaries"))})
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "restrict Secret")
	draft := docs.add(docs.homeID, "Draft", map[string]any{"publish": false, "body": docOf(lineOf("Not yet"))})
	front := docs.add(docs.homeID, "Front", map[string]any{"body": docOf(includeOf(support, nil), includeOf(support, excerptHours), includeOf(secret, nil))})

	included := func(c *client, page, query string) response {
		t.Helper()
		return c.get(t, pagePath(page, "/included"+query))
	}
	body := func(r response) string {
		t.Helper()
		got := obj(t, want(t, r, http.StatusOK, "read an include"), "included")
		return fmt.Sprint(got["body"]) + fmt.Sprint(got["excerpt"]) + fmt.Sprint(got["page"])
	}

	t.Run("the whole page or one excerpt, as published", func(t *testing.T) {
		whole := body(included(member, support, "?via="+front))
		if !strings.Contains(whole, "All of support") || !strings.Contains(whole, "Nine to five") || !strings.Contains(whole, "INC") {
			t.Errorf("the whole page: %s", whole)
		}
		part := body(included(member, support, "?excerpt="+excerptHours+"&via="+front))
		if strings.Contains(part, "All of support") || !strings.Contains(part, "Nine to five") || !strings.Contains(part, "Hours") {
			t.Errorf("the excerpt: %s", part)
		}
		want(t, owner.put(t, pagePath(support, "/draft"), map[string]any{"title": "Support", "body": docOf(excerptOf(excerptHours, "Hours", lineOf("Ten to six")))}), http.StatusOK, "draft a change")
		if again := body(included(member, support, "?excerpt="+excerptHours)); !strings.Contains(again, "Nine to five") {
			t.Errorf("a draft reached the include: %s", again)
		}
	})

	t.Run("a reader who may not read the page, or of a page with nothing published, is told nothing more than that", func(t *testing.T) {
		for what, r := range map[string]response{
			"restricted":      included(member, secret, ""),
			"never published": included(owner, draft, ""),
			"no such excerpt": included(owner, support, "?excerpt=0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4aff"),
			"no such page":    included(owner, "0195f000-0000-7000-8000-000000000999", ""),
		} {
			if r.Status != http.StatusNotFound {
				t.Errorf("%s: %d %s", what, r.Status, r.Raw)
			}
		}
		if got := included(owner, secret, ""); got.Status != http.StatusOK {
			t.Errorf("the owner reads Secret's include: %d", got.Status)
		}
	})

	t.Run("an include that leads back, or goes too deep, is refused", func(t *testing.T) {
		got := included(member, front, "?via="+front+","+support)
		if got.Status != http.StatusConflict || !strings.Contains(string(got.Raw), "include_cycle") {
			t.Errorf("a cycle: %d %s", got.Status, got.Raw)
		}
		chain := []string{}
		for range 5 {
			chain = append(chain, "0195f000-0000-7000-8000-000000000999")
		}
		got = included(member, support, "?via="+strings.Join(chain, ","))
		if got.Status != http.StatusConflict || !strings.Contains(string(got.Raw), "include_depth") {
			t.Errorf("too deep: %d %s", got.Status, got.Raw)
		}
		for _, bad := range []string{"?excerpt=Hours", "?via=front"} {
			if got := included(member, support, bad); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", bad, got.Status, got.Raw)
			}
		}
	})

	t.Run("a page does not include itself, published or drafted", func(t *testing.T) {
		itself := docOf(lineOf("Loop"), includeOf(front, nil))
		if got := owner.patch(t, pagePath(front), map[string]any{"version": 1, "body": itself}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("published: %d %s", got.Status, got.Raw)
		}
		if got := owner.put(t, pagePath(front, "/draft"), map[string]any{"title": "Front", "body": itself}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("drafted: %d %s", got.Status, got.Raw)
		}
		bad := docOf(map[string]any{"type": "include", "attrs": map[string]any{"pageId": "front", "excerptId": nil}})
		if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd", "body": bad}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("an include of no page: %d %s", got.Status, got.Raw)
		}
	})
}
