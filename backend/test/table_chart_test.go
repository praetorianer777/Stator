//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

// A chart from a table (#59): the page stores the table and what to draw of
// it, so the chart is always the table's.

func chartTable(rows ...[]string) map[string]any {
	var out []any
	for ri, row := range rows {
		var cells []any
		for _, text := range row {
			kind := "tableCell"
			if ri == 0 {
				kind = "tableHeader"
			}
			cells = append(cells, map[string]any{"type": kind, "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}})
		}
		out = append(out, map[string]any{"type": "tableRow", "content": cells})
	}
	return map[string]any{"type": "table", "content": out}
}

func TestAChartFromATableIsKeptWithItsTable(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "table-chart")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "CHRT", "Charts")

	chart := map[string]any{"type": "tableChart", "attrs": map[string]any{"chart": "bar", "showTable": true}, "content": []any{
		chartTable([]string{"Region", "Sales"}, []string{"Quokkaland", "12"}, []string{"Wombatia", "7"}),
	}}
	page := docs.add(docs.homeID, "Sales", map[string]any{"body": docOf(chart)})

	t.Run("the page keeps the chart and its table as stored", func(t *testing.T) {
		body := obj(t, want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["body"].(map[string]any)
		stored := body["content"].([]any)[0].(map[string]any)
		if stored["type"] != "tableChart" || stored["attrs"].(map[string]any)["chart"] != "bar" || len(stored["content"].([]any)) != 1 {
			t.Errorf("the page holds %v", stored)
		}
	})

	t.Run("search finds the page by a word in the chart's table", func(t *testing.T) {
		h.settle(t)
		if got := hitTitles(t, want(t, owner.get(t, "/api/v1/search?q=quokkaland"), http.StatusOK, "search")); len(got) != 1 || got[0] != "Sales" {
			t.Errorf("a search for a region finds %v", got)
		}
	})

	t.Run("an export keeps the table and what to draw of it", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(page, "/markdown"))
		md := string(data)
		if resp.StatusCode != http.StatusOK || !strings.Contains(md, `data-stator="table-chart" data-chart="bar" data-show-table="true"`) || !strings.Contains(md, "| Quokkaland |") {
			t.Errorf("the export: %d\n%s", resp.StatusCode, md)
		}
	})

	t.Run("a chart of no table, two tables or an unknown kind is refused", func(t *testing.T) {
		for what, block := range map[string]map[string]any{
			"no table":   {"type": "tableChart", "attrs": map[string]any{"chart": "bar", "showTable": true}},
			"two tables": {"type": "tableChart", "attrs": map[string]any{"chart": "bar", "showTable": true}, "content": []any{chartTable([]string{"a"}), chartTable([]string{"b"})}},
			"a donut":    {"type": "tableChart", "attrs": map[string]any{"chart": "donut", "showTable": true}, "content": []any{chartTable([]string{"a"})}},
			"a paragraph": {"type": "tableChart", "attrs": map[string]any{"chart": "pie", "showTable": false}, "content": []any{
				map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "12"}}},
			}},
		} {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Bad " + what, "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}
