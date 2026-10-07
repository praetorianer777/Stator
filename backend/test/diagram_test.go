//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Diagrams (#46): a page keeps a diagram as its Mermaid text, and search, the
// Markdown export and import read it so.

func diagramNode(source string) map[string]any {
	return map[string]any{"type": "diagram", "attrs": map[string]any{"source": source}}
}

func TestAPageKeepsDiagramsAsTheirText(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "diagram")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "ARCH", "Architecture")
	source := "flowchart LR\n  api[Wombatgateway] --> db[(Postgres)]\n\n  api -.-> cache"
	page := docs.add(docs.homeID, "Overview", map[string]any{"body": docOf(diagramNode(source))})

	t.Run("the page reads back with its diagram as written, its lines kept", func(t *testing.T) {
		got, _ := json.Marshal(obj(t, want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["body"])
		wanted, _ := json.Marshal(source)
		if !strings.Contains(string(got), `"type":"diagram"`) || !strings.Contains(string(got), string(wanted)) {
			t.Errorf("the body: %s", got)
		}
	})

	t.Run("search finds a page by the labels in its diagrams", func(t *testing.T) {
		if got := hitTitles(t, searchFor(t, owner, url.Values{"q": {"wombatgateway"}})); len(got) != 1 || got[0] != "Overview" {
			t.Errorf("searching for a diagram's label finds %v", got)
		}
	})

	t.Run("the Markdown export writes a mermaid fence, and its import reads it back as the diagram", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(page, "/markdown"))
		md := string(data)
		if resp.StatusCode != http.StatusOK || !strings.Contains(md, "```mermaid\n"+source+"\n```") {
			t.Fatalf("the export: %d\n%s", resp.StatusCode, md)
		}
		copied := obj(t, want(t, owner.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import"), [2]string{"overview.md", md}), http.StatusCreated, "import the export"))
		id := copied["pages"].([]any)[0].(map[string]any)["id"].(string)
		body := obj(t, owner.get(t, pagePath(id)), "page")["body"].(map[string]any)
		blocks := body["content"].([]any)
		if len(blocks) != 1 || blocks[0].(map[string]any)["attrs"].(map[string]any)["source"] != source {
			t.Errorf("the imported copy: %v", body)
		}
	})

	t.Run("a diagram without text, or too long, or in a line or a comment, is refused", func(t *testing.T) {
		for what, body := range map[string]map[string]any{
			"no text":   docOf(diagramNode(" \n")),
			"too long":  docOf(diagramNode(strings.Repeat("x", document.MaxDiagramLength+1))),
			"in a line": docOf(map[string]any{"type": "paragraph", "content": []any{diagramNode("graph TD")}}),
		} {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": body}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": docOf(diagramNode("graph TD"))}), http.StatusUnprocessableEntity, "a diagram in a comment"))
	})

	t.Run("the database reads a diagram as document.PlainText does", func(t *testing.T) {
		raw, _ := json.Marshal(docOf(
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "before"}}},
			diagramNode("graph TD\n  a --> b"),
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
		if want := document.PlainText(root); got != want || got != "before\ngraph TD\n  a --> b\nafter" {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
