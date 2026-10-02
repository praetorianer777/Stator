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

// Column layouts (#42): a page stores two or three columns with their shares
// of the row, and search reads every column.

func columnsDoc(columns ...map[string]any) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Before the columns"}}},
		map[string]any{"type": "columns", "content": columns},
	}}
}

func columnOf(width any, words string) map[string]any {
	return map[string]any{"type": "column", "attrs": map[string]any{"width": width}, "content": textDoc(words)["content"]}
}

func TestColumnsKeepTheirSharesAndSearchReadsEveryColumn(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "columns")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "COL", "Columns")
	page := docs.add(docs.homeID, "Compare", map[string]any{"body": columnsDoc(columnOf(33, "The narrow quokka"), columnOf(67, "The wide wombat"))})

	t.Run("the page stores the columns as written", func(t *testing.T) {
		got := want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page")
		for _, part := range []string{`"type":"column","attrs":{"width":33}`, `"type":"column","attrs":{"width":67}`, "The wide wombat"} {
			if !bytes.Contains(got.Raw, []byte(part)) {
				t.Errorf("the stored body lacks %s: %s", part, got.Raw)
			}
		}
	})

	t.Run("three even columns are taken too", func(t *testing.T) {
		want(t, owner.patch(t, pagePath(page), map[string]any{"version": 1, "body": columnsDoc(columnOf(nil, "The narrow quokka"), columnOf(nil, "The wide wombat"), columnOf(nil, "A third"))}), http.StatusOK, "three columns")
	})

	t.Run("search finds the page by the words in any column", func(t *testing.T) {
		for _, q := range []string{"quokka", "wombat"} {
			if got := hitTitles(t, searchFor(t, owner, url.Values{"q": {q}})); len(got) != 1 || got[0] != "Compare" {
				t.Errorf("searching %q finds %v", q, got)
			}
		}
	})

	t.Run("one column, four, a share out of range or columns in a comment are refused", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"one column":        columnsDoc(columnOf(nil, "x")),
			"four columns":      columnsDoc(columnOf(nil, "a"), columnOf(nil, "b"), columnOf(nil, "c"), columnOf(nil, "d")),
			"a share too small": columnsDoc(columnOf(document.MinColumnShare-1, "a"), columnOf(50, "b")),
			"a share too large": columnsDoc(columnOf(document.MaxColumnShare+1, "a"), columnOf(10, "b")),
			"a share as text":   columnsDoc(columnOf("50%", "a"), columnOf(50, "b")),
		} {
			errorCode(t, want(t, owner.patch(t, pagePath(page), map[string]any{"body": body}), http.StatusUnprocessableEntity, name))
		}
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": columnsDoc(columnOf(50, "a"), columnOf(50, "b"))}), http.StatusUnprocessableEntity, "columns in a comment"))
	})

	t.Run("the database reads the columns as document.PlainText does", func(t *testing.T) {
		raw := `{"type":"doc","content":[
			{"type":"columns","content":[
				{"type":"column","attrs":{"width":33},"content":[{"type":"paragraph","content":[{"type":"text","text":"left"}]}]},
				{"type":"column","attrs":{"width":67},"content":[
					{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"right"}]},
					{"type":"columns","content":[
						{"type":"column","content":[{"type":"paragraph","content":[{"type":"text","text":"inner one"}]}]},
						{"type":"column","content":[{"type":"paragraph"}]}]}]}]},
			{"type":"paragraph","content":[{"type":"text","text":"after"}]}]}`
		root, err := document.Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, raw).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || !strings.Contains(got, "left\nright\ninner one\nafter") {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
