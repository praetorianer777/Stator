//go:build integration

package test

import (
	"net/http"
	"testing"
)

// Excerpts (#48): a page names parts of itself that other pages include;
// a picker lists them from the published body the reader may read.

const (
	excerptHours = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71"
	excerptEsc   = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72"
)

func excerptOf(id, name string, blocks ...any) map[string]any {
	return map[string]any{"type": "excerpt", "attrs": map[string]any{"id": id, "name": name}, "content": blocks}
}

func lineOf(text string) map[string]any {
	return map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}
}

func TestAPickerListsAPagesPublishedExcerpts(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "excerpts")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "EXC", "Excerpts")
	support := docs.add(docs.homeID, "Support", map[string]any{"body": docOf(
		lineOf("Intro"),
		excerptOf(excerptHours, "Support hours", lineOf("Nine to five on weekdays")),
		map[string]any{"type": "panel", "attrs": map[string]any{"kind": "info"}, "content": []any{excerptOf(excerptEsc, "Escalation", lineOf("Call the lead"))}},
	)})

	names := func(c *client, page string) []string {
		t.Helper()
		var out []string
		for _, each := range list(t, want(t, c.get(t, pagePath(page, "/excerpts")), http.StatusOK, "list the excerpts"), "excerpts") {
			m := each.(map[string]any)
			out = append(out, m["name"].(string)+":"+m["text"].(string))
		}
		return out
	}

	t.Run("a reader lists them in reading order, with the start of their words", func(t *testing.T) {
		got := names(member, support)
		if len(got) != 2 || got[0] != "Support hours:Nine to five on weekdays" || got[1] != "Escalation:Call the lead" {
			t.Fatalf("the excerpts: %v", got)
		}
	})

	t.Run("a draft's excerpts wait until it is published", func(t *testing.T) {
		draft := docs.add(docs.homeID, "Draft", map[string]any{"publish": false, "body": docOf(excerptOf(excerptHours, "Later", lineOf("x")))})
		if got := names(owner, draft); len(got) != 0 {
			t.Errorf("an unpublished page lists %v", got)
		}
		folder := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Folder", "kind": "folder", "publish": false}), http.StatusCreated, "add a folder"), "page")["id"].(string)
		if got := names(owner, folder); len(got) != 0 {
			t.Errorf("a folder lists %v", got)
		}
	})

	t.Run("a page the reader may not read lists nothing", func(t *testing.T) {
		want(t, restrict(t, owner, support, []any{user(home.user)}, nil), http.StatusOK, "restrict Support")
		if got := member.get(t, pagePath(support, "/excerpts")); got.Status != http.StatusNotFound {
			t.Errorf("a member who may not read the page: %d %s", got.Status, got.Raw)
		}
		want(t, restrict(t, owner, support, nil, nil), http.StatusOK, "lift the restriction")
		if got := member.get(t, pagePath("0195f000-0000-7000-8000-000000000999", "/excerpts")); got.Status != http.StatusNotFound {
			t.Errorf("no such page: %d", got.Status)
		}
	})

	t.Run("excerpts are refused nested, or sharing an id or a name", func(t *testing.T) {
		for what, body := range map[string]map[string]any{
			"nested":    docOf(excerptOf(excerptHours, "Outer", excerptOf(excerptEsc, "Inner", lineOf("x")))),
			"same id":   docOf(excerptOf(excerptHours, "One", lineOf("x")), excerptOf(excerptHours, "Two", lineOf("y"))),
			"same name": docOf(excerptOf(excerptHours, "Hours", lineOf("x")), excerptOf(excerptEsc, "hours", lineOf("y"))),
			"no name":   docOf(excerptOf(excerptHours, " ", lineOf("x"))),
		} {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": body}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		comment := docOf(excerptOf(excerptHours, "Hours", lineOf("x")))
		errorCode(t, want(t, owner.post(t, pagePath(support, "/comments"), map[string]any{"body": comment}), http.StatusUnprocessableEntity, "an excerpt in a comment"))
	})
}
