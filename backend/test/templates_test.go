//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// hinted is a document with plain text and a hint beside it, a paragraph of
// hint alone, and a hint deep inside a list.
func hinted() map[string]any {
	hint := func(text string) map[string]any {
		return map[string]any{"type": "text", "text": text, "marks": []any{map[string]any{"type": "hint"}}}
	}
	para := func(inline ...any) map[string]any { return map[string]any{"type": "paragraph", "content": inline} }
	return map[string]any{"type": "doc", "content": []any{
		para(map[string]any{"type": "text", "text": "Owner: "}, hint("Name the owner")),
		para(hint("Describe the goal")),
		map[string]any{"type": "bulletList", "content": []any{
			map[string]any{"type": "listItem", "content": []any{para(map[string]any{"type": "text", "text": "Kept "}, hint("Say more"))}},
		}},
	}}
}

// hintsIn counts the hint marks in a stored or served body.
func hintsIn(t *testing.T, body any) int {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), `"hint"`)
}

// Templates are served to members, validate as pages do, and a page made from
// one keeps its hints until it is published, never after.
func TestTemplatesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "templates")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "TPL", "Templates")

	t.Run("members list the built-ins and read each", func(t *testing.T) {
		all := list(t, want(t, owner.get(t, "/api/v1/templates"), http.StatusOK, "list templates"), "templates")
		if len(all) != 7 {
			t.Fatalf("%d templates", len(all))
		}
		for _, each := range all {
			tpl := each.(map[string]any)
			one := obj(t, want(t, owner.get(t, "/api/v1/templates/"+tpl["key"].(string)), http.StatusOK, "read "+tpl["key"].(string)), "template")
			if one["name"] != tpl["name"] || one["builtIn"] != true || hintsIn(t, one["body"]) == 0 {
				t.Errorf("template %v", one)
			}
		}
	})

	t.Run("nobody else reads them, and an unknown key is not found", func(t *testing.T) {
		if got := api.anonymous().get(t, "/api/v1/templates"); got.Status != http.StatusUnauthorized {
			t.Errorf("without a session the list answers %d", got.Status)
		}
		got := owner.get(t, "/api/v1/templates/no-such-template")
		if got.Status != http.StatusNotFound || errorCode(t, got) != "not_found" {
			t.Errorf("an unknown template answers %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a page made from a template keeps its hints until it is published", func(t *testing.T) {
		tpl := obj(t, want(t, owner.get(t, "/api/v1/templates/meeting-notes"), http.StatusOK, "read meeting notes"), "template")
		made := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Standup", "body": tpl["body"]}), http.StatusCreated, "make a page from it"), "page")
		id := made["id"].(string)
		if made["unpublished"] != true || hintsIn(t, made["body"]) != hintsIn(t, tpl["body"]) {
			t.Fatalf("the new page is %v", made)
		}
		published := obj(t, want(t, owner.post(t, pagePath(id, "/publish"), map[string]any{}), http.StatusOK, "publish it"), "page")
		if n := hintsIn(t, published["body"]); n != 0 {
			t.Fatalf("the published page kept %d hints: %v", n, published["body"])
		}
		if !strings.Contains(mustJSON(t, published["body"]), "Participants") {
			t.Errorf("publishing took more than the hints: %v", published["body"])
		}
		version := obj(t, want(t, owner.get(t, pagePath(id, "/versions/1")), http.StatusOK, "read version 1"), "version")
		if n := hintsIn(t, version["body"]); n != 0 {
			t.Errorf("version 1 kept %d hints", n)
		}
	})

	t.Run("publishing a hinted draft keeps what was typed and drops the hints", func(t *testing.T) {
		id := docs.add(docs.homeID, "Plan")
		want(t, owner.put(t, pagePath(id, "/draft"), map[string]any{"title": "Plan", "body": hinted(), "baseVersion": 1}), http.StatusOK, "save a hinted draft")
		body := mustJSON(t, obj(t, want(t, owner.post(t, pagePath(id, "/publish"), map[string]any{}), http.StatusOK, "publish"), "page")["body"])
		if strings.Contains(body, "hint") || strings.Contains(body, "Name the owner") || !strings.Contains(body, "Owner: ") || !strings.Contains(body, "Kept ") {
			t.Errorf("the published body is %s", body)
		}
	})

	t.Run("a copy of an unpublished page loses its hints", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft plan", "body": hinted()}), http.StatusCreated, "make"), "page")
		copied := obj(t, want(t, owner.post(t, pagePath(made["id"].(string), "/copy"), map[string]any{"parentId": docs.homeID, "withChildren": false}), http.StatusCreated, "copy"), "page")
		if n := hintsIn(t, copied["body"]); n != 0 || copied["unpublished"] == true {
			t.Errorf("the copy is %v", copied)
		}
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Straight through SQL as the app role, a published version or page cannot
// keep a hint either, while an unpublished page may.
func TestTemplateHintsAreStrippedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hints-sql")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "HINT", "Hints")
	published := docs.add(docs.homeID, "Published")
	unpublished := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Mine"}), http.StatusCreated, "make"), "page")["id"].(string)
	body := mustJSON(t, hinted())

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, home.org, home.user)
	if _, err := conn.Exec(ctx, `INSERT INTO page_version (org_id, page_id, number, title, body, created_by) VALUES ($1, $2, 2, 'Raw', $3, $4)`, home.org, published, body, home.user); err != nil {
		t.Fatalf("insert a version: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE page SET body = $2, version = 2 WHERE id = $1`, published, body); err != nil {
		t.Fatalf("publish it on the page: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE page SET body = $2 WHERE id = $1`, unpublished, body); err != nil {
		t.Fatalf("write the unpublished page: %v", err)
	}
	var stored string
	for what, sql := range map[string]string{
		"the version":          `SELECT body::text FROM page_version WHERE page_id = $1 AND number = 2`,
		"the published page":   `SELECT body::text FROM page WHERE id = $1`,
		"the unpublished page": `SELECT body::text FROM page WHERE id = $1`,
	} {
		id := published
		if what == "the unpublished page" {
			id = unpublished
		}
		if err := conn.QueryRow(ctx, sql, id).Scan(&stored); err != nil {
			t.Fatalf("read %s: %v", what, err)
		}
		kept := strings.Contains(stored, `"hint"`)
		if kept != (what == "the unpublished page") || !strings.Contains(stored, "Kept ") {
			t.Errorf("%s holds %s", what, stored)
		}
	}
}
