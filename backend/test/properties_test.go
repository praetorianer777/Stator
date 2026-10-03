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

// Page properties (#54): a page names its metadata in a properties block, and
// a properties report tabulates it across the pages carrying some labels,
// from the published pages each reader may read.

func propertiesOf(rows ...[2]string) map[string]any {
	var content []any
	for _, r := range rows {
		row := map[string]any{"type": "propertyRow", "attrs": map[string]any{"key": r[0]}}
		if r[1] != "" {
			row["content"] = []any{map[string]any{"type": "text", "text": r[1]}}
		}
		content = append(content, row)
	}
	return map[string]any{"type": "properties", "content": content}
}

func reportPath(labels []string, space string, columns ...string) string {
	v := url.Values{"label": labels, "column": columns}
	if space != "" {
		v.Set("space", space)
	}
	return "/api/v1/properties-report?" + v.Encode()
}

// register writes a report as Title:value|value per row, "-" for no value.
func register(t *testing.T, c *client, path string) (string, string) {
	t.Helper()
	r := want(t, c.get(t, path), http.StatusOK, "read the report "+path)
	var columns []string
	for _, col := range list(t, r, "columns") {
		columns = append(columns, col.(string))
	}
	var rows []string
	for _, each := range list(t, r, "rows") {
		row := each.(map[string]any)
		var cells []string
		for _, v := range row["values"].([]any) {
			if v == nil {
				cells = append(cells, "-")
			} else {
				cells = append(cells, v.(map[string]any)["text"].(string))
			}
		}
		rows = append(rows, row["title"].(string)+":"+strings.Join(cells, "|"))
	}
	return strings.Join(columns, ","), strings.Join(rows, " ")
}

func TestAPropertiesReportTabulatesWhatEachReaderMayRead(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "props")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "ADR", "Decisions")
	other := newTree(t, owner, "OPS", "Operations")
	labelled := func(tree *tree, parent, title string, body map[string]any, labels ...string) string {
		id := tree.add(parent, title, map[string]any{"body": body})
		for _, name := range labels {
			want(t, owner.post(t, pagePath(id, "/labels"), map[string]any{"name": name}), http.StatusOK, "label "+title)
		}
		return id
	}
	labelled(docs, docs.homeID, "Use Postgres", docOf(propertiesOf([2]string{"Status", "Accepted"}, [2]string{"Owner", "Ada"}, [2]string{"Deciders", "quokkateam"})), "adr")
	labelled(docs, docs.homeID, "Drop the cache", docOf(
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Why we drop it."}}},
		propertiesOf([2]string{"status", "Proposed"}, [2]string{"Review date", "2026-11-01"}),
	), "adr", "backend")
	labelled(docs, docs.homeID, "No metadata", docOf(map[string]any{"type": "paragraph"}), "adr")
	secret := labelled(docs, docs.homeID, "Hire two", docOf(propertiesOf([2]string{"Status", "Accepted"})), "adr")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "restrict Hire two")
	labelled(other, other.homeID, "Rotate keys", docOf(propertiesOf([2]string{"Owner", "Bob"})), "adr", "backend")
	draft := docs.add(docs.homeID, "Unpublished", map[string]any{"publish": false, "body": docOf(propertiesOf([2]string{"Status", "Draft"}))})
	want(t, owner.post(t, pagePath(draft, "/labels"), map[string]any{"name": "adr"}), http.StatusOK, "label the draft")

	t.Run("every name found becomes a column, first seen first, and a page lacking one is blank there", func(t *testing.T) {
		columns, rows := register(t, owner, reportPath([]string{"adr"}, ""))
		if columns != "status,Review date,Owner,Deciders" {
			t.Errorf("the columns: %s", columns)
		}
		if rows != "Drop the cache:Proposed|2026-11-01|-|- Hire two:Accepted|-|-|- No metadata:-|-|-|- Rotate keys:-|-|Bob|- Use Postgres:Accepted|-|Ada|quokkateam" {
			t.Errorf("the owner's rows: %s", rows)
		}
	})

	t.Run("a reader sees only the pages they may read, and the columns asked for", func(t *testing.T) {
		columns, rows := register(t, member, reportPath([]string{"ADR"}, "", "owner", "Status"))
		if columns != "owner,Status" || rows != "Drop the cache:-|Proposed No metadata:-|- Rotate keys:Bob|- Use Postgres:Ada|Accepted" {
			t.Errorf("the member's report: %s / %s", columns, rows)
		}
	})

	t.Run("every label must be on a page, and a space keeps the report inside it", func(t *testing.T) {
		if _, rows := register(t, member, reportPath([]string{"adr", "backend"}, "")); rows != "Drop the cache:Proposed|2026-11-01|- Rotate keys:-|-|Bob" {
			t.Errorf("two labels: %s", rows)
		}
		if _, rows := register(t, member, reportPath([]string{"adr", "backend"}, "OPS")); rows != "Rotate keys:Bob" {
			t.Errorf("in OPS: %s", rows)
		}
	})

	t.Run("a report without labels, of a label that cannot be, or of a space nobody has, is refused", func(t *testing.T) {
		for what, path := range map[string]string{
			"no label":       reportPath(nil, ""),
			"a slash":        reportPath([]string{"a/b"}, ""),
			"six labels":     reportPath([]string{"a", "b", "c", "d", "e", "f"}, ""),
			"a blank column": reportPath([]string{"adr"}, "", " "),
			"eleven columns": reportPath([]string{"adr"}, "", strings.Split("a b c d e f g h i j k", " ")...),
		} {
			if got := member.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.get(t, reportPath([]string{"adr"}, "NOPE")); got.Status != http.StatusNotFound {
			t.Errorf("a space nobody has: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("search finds a page by its properties", func(t *testing.T) {
		if got := hitTitles(t, searchFor(t, member, url.Values{"q": {"quokkateam"}})); len(got) != 1 || got[0] != "Use Postgres" {
			t.Errorf("searching for a property's value finds %v", got)
		}
	})

	t.Run("a page keeps what a report gathers, never the rows, and refuses a malformed block", func(t *testing.T) {
		report := map[string]any{"type": "propertiesReport", "attrs": map[string]any{"labels": []any{"adr"}, "space": nil, "columns": []any{}}}
		docs.add(docs.homeID, "Register", map[string]any{"body": docOf(report)})
		for what, bad := range map[string]any{
			"rows in a report":  map[string]any{"type": "propertiesReport", "attrs": map[string]any{"labels": []any{"adr"}, "space": nil, "columns": []any{}, "rows": []any{}}},
			"an empty block":    map[string]any{"type": "properties", "content": []any{}},
			"a long name":       propertiesOf([2]string{strings.Repeat("x", document.MaxPropertyKeyLength+1), "v"}),
			"a paragraph in it": map[string]any{"type": "properties", "content": []any{map[string]any{"type": "paragraph"}}},
		} {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": docOf(bad)}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("the database reads properties as document.PlainText does", func(t *testing.T) {
		raw, _ := json.Marshal(docOf(propertiesOf([2]string{"Owner", "Ada"}, [2]string{"", "half typed"}, [2]string{"Due", ""}), map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "after"}}}))
		root, err := document.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, string(raw)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || got != "Owner\tAda\n\thalf typed\nDue\nafter" {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
