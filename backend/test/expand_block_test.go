//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Expand blocks (#41): a page stores the title and the blocks inside, never
// whether it is open, and search reads both.

func expandDoc(attrs map[string]any, inner ...string) map[string]any {
	blocks := textDoc(inner...)["content"]
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Before the details"}}},
		map[string]any{"type": "expand", "attrs": attrs, "content": blocks},
	}}
}

func TestAnExpandBlockKeepsItsTitleAndContentAndSearchReadsBoth(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "expand")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "EXP", "Expand")
	page := docs.add(docs.homeID, "Runbook", map[string]any{"body": expandDoc(map[string]any{"title": "Rollback quokka"}, "Revert the wombat release")})

	t.Run("the page stores the block as written", func(t *testing.T) {
		got := want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page")
		if !bytes.Contains(got.Raw, []byte(`"type":"expand","attrs":{"title":"Rollback quokka"}`)) || !bytes.Contains(got.Raw, []byte("Revert the wombat release")) {
			t.Errorf("the stored body is %s", got.Raw)
		}
	})

	t.Run("search finds the page by the title and by the words folded away", func(t *testing.T) {
		for _, q := range []string{"quokka", "wombat"} {
			if got := hitTitles(t, searchFor(t, owner, url.Values{"q": {q}})); len(got) != 1 || got[0] != "Runbook" {
				t.Errorf("searching %q finds %v", q, got)
			}
		}
	})

	t.Run("a block with a long title, a stored open state or in a comment is refused", func(t *testing.T) {
		for name, attrs := range map[string]map[string]any{
			"a title too long": {"title": strings.Repeat("a", document.MaxExpandTitleLength+1)},
			"a null title":     {"title": nil},
			"an open state":    {"title": "More", "open": true},
		} {
			errorCode(t, want(t, owner.patch(t, pagePath(page), map[string]any{"body": expandDoc(attrs, "x")}), http.StatusUnprocessableEntity, name))
		}
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": expandDoc(map[string]any{"title": "More"}, "x")}), http.StatusUnprocessableEntity, "a block in a comment"))
	})

	t.Run("the database reads the block as document.PlainText does", func(t *testing.T) {
		raw := `{"type":"doc","content":[
			{"type":"expand","attrs":{"title":"Outer"},"content":[
				{"type":"paragraph","content":[{"type":"text","text":"inside"}]},
				{"type":"expand","attrs":{"title":""},"content":[{"type":"paragraph","content":[{"type":"text","text":"deeper"}]}]},
				{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"expand","attrs":{"title":"In a cell"},"content":[{"type":"paragraph","content":[{"type":"text","text":"cell words"}]}]}]}]}]}]},
			{"type":"expand","content":[{"type":"paragraph","content":[{"type":"text","text":"untitled"}]}]}]}`
		root, err := document.Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, raw).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || !strings.Contains(got, "Outer\ninside\ndeeper") {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
