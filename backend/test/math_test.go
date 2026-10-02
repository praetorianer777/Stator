//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Math formulas (#45): a page keeps a formula as its TeX source, inline or on
// a line of its own, and search, the Markdown export and import read it so.

func mathInline(latex string) map[string]any {
	return map[string]any{"type": "mathInline", "attrs": map[string]any{"latex": latex}}
}

func mathBlock(latex string) map[string]any {
	return map[string]any{"type": "mathBlock", "attrs": map[string]any{"latex": latex}}
}

func TestAPageKeepsFormulasAsTheirSource(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "math")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "MATH", "Maths")
	body := docOf(
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Energy is "}, mathInline(`E = mc^2`)}},
		mathBlock(`\int_0^1 \operatorname{quokkarate}(x)\,dx`),
	)
	page := docs.add(docs.homeID, "Physics", map[string]any{"body": body})

	t.Run("the page reads back with its formulas as written", func(t *testing.T) {
		got, _ := json.Marshal(obj(t, want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["body"])
		for _, wanted := range []string{`"latex":"E = mc^2"`, `"type":"mathBlock"`, `quokkarate`} {
			if !strings.Contains(string(got), wanted) {
				t.Errorf("the body lacks %s: %s", wanted, got)
			}
		}
	})

	t.Run("search finds a page by what its formulas say", func(t *testing.T) {
		if got := hitTitles(t, searchFor(t, owner, url.Values{"q": {"quokkarate"}})); len(got) != 1 || got[0] != "Physics" {
			t.Errorf("searching for a formula's word finds %v", got)
		}
	})

	t.Run("the Markdown export writes them as Markdown that typesets them, and its import reads the block back", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(page, "/markdown"))
		md := string(data)
		if resp.StatusCode != http.StatusOK || !strings.Contains(md, "Energy is $E = mc^2$") || !strings.Contains(md, "```math\n\\int_0^1") {
			t.Fatalf("the export: %d\n%s", resp.StatusCode, md)
		}
		copied := obj(t, want(t, owner.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import"), [2]string{"physics.md", md}), http.StatusCreated, "import the export"))
		id := copied["pages"].([]any)[0].(map[string]any)["id"].(string)
		got, _ := json.Marshal(obj(t, owner.get(t, pagePath(id)), "page")["body"])
		if !strings.Contains(string(got), `"type":"mathBlock"`) || !strings.Contains(string(got), `quokkarate`) {
			t.Errorf("the imported copy: %s", got)
		}
	})

	t.Run("a formula without source, or too long, or in a comment, is refused", func(t *testing.T) {
		for what, bad := range map[string]any{
			"no source":    mathBlock("  "),
			"too long":     mathBlock(strings.Repeat("x", document.MaxMathLength+1)),
			"not a string": map[string]any{"type": "mathBlock", "attrs": map[string]any{"latex": 2}},
			"inline alone": mathInline("x"),
		} {
			got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": docOf(bad)})
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		comment := docOf(map[string]any{"type": "paragraph", "content": []any{mathInline("x")}})
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": comment}), http.StatusUnprocessableEntity, "a formula in a comment"))
	})

	t.Run("the database reads a formula as document.PlainText does", func(t *testing.T) {
		raw, _ := json.Marshal(docOf(
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "so "}, mathInline(`a^2`)}},
			mathBlock(`\sqrt{2}`),
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "after"}}},
		))
		root, err := document.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, string(raw)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || got != fmt.Sprintf("so a^2\n%s\nafter", `\sqrt{2}`) {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
