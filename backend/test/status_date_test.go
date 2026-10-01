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

// Status labels and dates (#43): stored as their words and their day, found
// by search through both, and refused in any other shape.

func statusNode(label, color string) map[string]any {
	return map[string]any{"type": "status", "attrs": map[string]any{"label": label, "color": color}}
}

func dateNode(day any) map[string]any {
	return map[string]any{"type": "date", "attrs": map[string]any{"date": day}}
}

func inlineDoc(nodes ...any) map[string]any {
	return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": nodes}}}
}

func TestAPageKeepsItsStatusesAndDatesAndIsFoundByThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "status-date")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "STA", "Statuses")
	body := inlineDoc(
		map[string]any{"type": "text", "text": "Rollout "},
		statusNode("Waitingforsignoff", "warning"),
		map[string]any{"type": "text", "text": " until "},
		dateNode("2026-11-02"),
	)
	page := docs.add(docs.homeID, "Rollout plan", map[string]any{"body": body})

	t.Run("the page keeps the label, the colour and the day as they were saved", func(t *testing.T) {
		got := want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page")
		for _, part := range []string{`"type":"status","attrs":{"color":"warning","label":"Waitingforsignoff"}`, `"type":"date","attrs":{"date":"2026-11-02"}`} {
			if !bytes.Contains(got.Raw, []byte(part)) {
				t.Errorf("the stored body lacks %s: %s", part, got.Raw)
			}
		}
	})

	t.Run("search finds the page by its status and shows it in the snippet", func(t *testing.T) {
		hits := list(t, searchFor(t, owner, url.Values{"q": {"Waitingforsignoff"}}), "hits")
		if len(hits) != 1 {
			t.Fatalf("searching the status finds %d hits", len(hits))
		}
		hit := hits[0].(map[string]any)
		if hit["page"].(map[string]any)["id"] != page || !strings.Contains(joined(hit["snippet"]), "Rollout Waitingforsignoff until 2026-11-02") {
			t.Errorf("the hit is %v", hit)
		}
	})

	t.Run("a status or a date in any other shape is refused, and so is one in a comment", func(t *testing.T) {
		for name, node := range map[string]map[string]any{
			"a blank label":          statusNode("  ", "neutral"),
			"a label too long":       statusNode(strings.Repeat("a", document.MaxStatusLength+1), "neutral"),
			"a colour of its own":    statusNode("Done", "#00ff00"),
			"a status with a style":  {"type": "status", "attrs": map[string]any{"label": "Done", "color": "success", "style": "color:red"}},
			"a day that is not":      dateNode("2026-02-30"),
			"an instant":             dateNode("2026-11-02T09:00:00Z"),
			"a date as a number":     dateNode(20261102),
			"a date with no day set": dateNode(nil),
		} {
			r := want(t, owner.patch(t, pagePath(page), map[string]any{"body": inlineDoc(node)}), http.StatusUnprocessableEntity, name)
			errorCode(t, r)
		}
		for name, node := range map[string]map[string]any{"a status": statusNode("Done", "success"), "a date": dateNode("2026-11-02")} {
			errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": inlineDoc(node)}), http.StatusUnprocessableEntity, name+" in a comment"))
		}
	})

	t.Run("the database reads statuses and dates as document.PlainText does", func(t *testing.T) {
		raw := `{"type":"doc","content":[
			{"type":"paragraph","content":[{"type":"text","text":"Launch "},{"type":"status","attrs":{"label":"Blocked","color":"danger"}},{"type":"text","text":" until "},{"type":"date","attrs":{"date":"2026-11-02"}}]},
			{"type":"table","content":[{"type":"tableRow","content":[
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"status","attrs":{"label":"Done","color":"success"}}]}]},
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"date","attrs":{"date":"2026-01-31"}}]}]}]}]}]}`
		root, err := document.Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, raw).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || want != "Launch Blocked until 2026-11-02\nDone\t2026-01-31" {
			t.Errorf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
