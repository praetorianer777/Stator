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

// Sketches (#312): a page keeps a sketch as its Excalidraw scene, the SVG
// drawn from it and its title, held to their shapes and limits wherever a
// body is written, and copies, search and the Markdown export carry it.

const sketchScene = `{"elements":[{"id":"box-1","type":"rectangle","x":0,"y":0,"width":180,"height":80,"strokeColor":"#1e1e1e","backgroundColor":"transparent","link":null},{"id":"words-1","type":"text","x":10,"y":27,"width":160,"height":25,"text":"Quokka\nqueue","originalText":"Quokka queue","containerId":null}],"appState":{"viewBackgroundColor":"#ffffff"}}`

const sketchDrawing = `<svg version="1.1" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 212 112" width="212" height="112"><rect x="0" y="0" width="212" height="112" fill="#ffffff"></rect><g stroke-linecap="round" transform="translate(16 16)"><path d="M0 0 L180 0 L180 80 L0 80 Z" stroke="#1e1e1e" stroke-width="2" fill="none"></path></g><g transform="translate(26 43)"><text x="80" y="17" font-family="Excalifont, Xiaolai" font-size="20px" fill="#1e1e1e" text-anchor="middle" style="white-space: pre;">Quokka queue</text></g></svg>`

func sketchNode(scene string, drawing, title any) map[string]any {
	return map[string]any{"type": document.NodeSketch, "attrs": map[string]any{"scene": scene, "drawing": drawing, "title": title}}
}

// sketchesOf are the attributes of every sketch at the top of a page's body.
func sketchesOf(t *testing.T, c *client, id string) []map[string]any {
	t.Helper()
	body := obj(t, want(t, c.get(t, pagePath(id)), http.StatusOK, "read the page"), "page")["body"].(map[string]any)
	var out []map[string]any
	for _, block := range body["content"].([]any) {
		if n := block.(map[string]any); n["type"] == document.NodeSketch {
			out = append(out, n["attrs"].(map[string]any))
		}
	}
	return out
}

func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

func TestAPageKeepsSketchesAsTheirScene(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "sketch")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	docs := newTree(t, owner, "SKT", "Sketches")
	page := docs.add(docs.homeID, "Flows", map[string]any{"body": docOf(sketchNode(sketchScene, sketchDrawing, "Wallaby routing"))})

	t.Run("the page reads back with its sketch as saved", func(t *testing.T) {
		got := sketchesOf(t, owner, page)
		if len(got) != 1 || got[0]["scene"] != sketchScene || got[0]["drawing"] != sketchDrawing || got[0]["title"] != "Wallaby routing" {
			t.Errorf("the sketch: %v", got)
		}
	})

	t.Run("search finds a page by a sketch's title and by the words drawn in it", func(t *testing.T) {
		for _, q := range []string{"wallaby", "quokka"} {
			if got := hitTitles(t, searchFor(t, owner, url.Values{"q": {q}})); len(got) != 1 || got[0] != "Flows" {
				t.Errorf("searching for %q finds %v", q, got)
			}
		}
	})

	t.Run("a copy of the page carries the sketch whole", func(t *testing.T) {
		copied := obj(t, want(t, owner.post(t, pagePath(page, "/copy"), map[string]any{"parentId": docs.homeID, "title": "Flows again"}), http.StatusCreated, "copy the page"), "page")
		got := sketchesOf(t, owner, copied["id"].(string))
		if len(got) != 1 || got[0]["scene"] != sketchScene || got[0]["drawing"] != sketchDrawing || got[0]["title"] != "Wallaby routing" {
			t.Errorf("the copy's sketch: %v", got)
		}
	})

	t.Run("the Markdown export writes an Excalidraw file, and its import reads it back as the sketch", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(page, "/markdown"))
		md := string(data)
		if resp.StatusCode != http.StatusOK || !strings.Contains(md, "```excalidraw \"Wallaby routing\"\n{\n  \"type\": \"excalidraw\",") || strings.Contains(md, "<svg") {
			t.Fatalf("the export: %d\n%s", resp.StatusCode, md)
		}
		copied := obj(t, want(t, owner.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import"), [2]string{"flows.md", md}), http.StatusCreated, "import the export"))
		id := copied["pages"].([]any)[0].(map[string]any)["id"].(string)
		got := sketchesOf(t, owner, id)
		if len(got) != 1 || !sameJSON(t, got[0]["scene"].(string), sketchScene) || got[0]["title"] != "Wallaby routing" || got[0]["drawing"] != nil {
			t.Errorf("the imported sketch: %v", got)
		}
	})

	t.Run("only well-formed scenes and clean drawings within their limits are kept, wherever a body is written", func(t *testing.T) {
		image := `{"elements":[{"id":"pic","type":"image","fileId":"f1"}]}`
		hostile := strings.Replace(sketchDrawing, "<rect", `<script>alert(1)</script><rect onload="alert(2)"`, 1)
		long := `{"elements":[{"id":"t","type":"text","text":"` + strings.Repeat("x", document.MaxSketchSceneLength) + `"}]}`
		bodies := map[string]map[string]any{
			"a scene that is no JSON":       docOf(sketchNode(`{"elements":[`, nil, nil)),
			"a scene with a picture":        docOf(sketchNode(image, nil, nil)),
			"a scene with a script link":    docOf(sketchNode(`{"elements":[{"id":"a","type":"rectangle","link":"javascript:alert(1)"}]}`, nil, nil)),
			"a scene too long":              docOf(sketchNode(long, nil, nil)),
			"a drawing that runs a script":  docOf(sketchNode(sketchScene, hostile, nil)),
			"a drawing that fetches":        docOf(sketchNode(sketchScene, strings.Replace(sketchDrawing, `fill="#ffffff"`, `fill="url(https://elsewhere.test/p.svg#a)"`, 1), nil)),
			"a drawing that is no SVG":      docOf(sketchNode(sketchScene, "<html></html>", nil)),
			"a drawing too long":            docOf(sketchNode(sketchScene, strings.Replace(sketchDrawing, "Quokka queue", strings.Repeat("x", document.MaxSketchDrawingLength), 1), nil)),
			"a title too long":              docOf(sketchNode(sketchScene, nil, strings.Repeat("x", document.MaxAltLength+1))),
			"a sketch in a line":            docOf(map[string]any{"type": "paragraph", "content": []any{sketchNode(sketchScene, nil, nil)}}),
			"a sketch with a field of more": docOf(map[string]any{"type": document.NodeSketch, "attrs": map[string]any{"scene": sketchScene, "svg": sketchDrawing}}),
		}
		for what, body := range bodies {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": body}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("a new page with %s: %d %s", what, got.Status, got.Raw)
			}
			if got := owner.put(t, pagePath(page, "/draft"), map[string]any{"title": "Flows", "body": body, "baseVersion": 1}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("a draft with %s: %d %s", what, got.Status, got.Raw)
			}
		}
		got := owner.post(t, "/api/v1/templates", map[string]any{"name": "Sketched", "spaceKey": "SKT", "title": "Sketched", "body": docOf(sketchNode(sketchScene, hostile, nil)), "variables": []any{}})
		if got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a template with a drawing that runs a script: %d %s", got.Status, got.Raw)
		}
		refused := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Pictured", "body": docOf(sketchNode(image, nil, nil))})
		if !strings.Contains(string(refused.Raw), "put the picture on the page beside the sketch") {
			t.Errorf("a sketch with a picture is refused as %s", refused.Raw)
		}
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": docOf(sketchNode(sketchScene, nil, nil))}), http.StatusUnprocessableEntity, "a sketch in a comment"))
	})

	t.Run("a template carries a sketch into the page made from it", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, "/api/v1/templates", map[string]any{"name": "Sketched", "spaceKey": "SKT", "title": "Sketched", "body": docOf(sketchNode(sketchScene, sketchDrawing, "Wallaby routing")), "variables": []any{}}), http.StatusCreated, "make a template"), "template")
		created := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "From the template", "template": made["key"]}), http.StatusCreated, "make a page from it"), "page")
		got := sketchesOf(t, owner, created["id"].(string))
		if len(got) != 1 || got[0]["scene"] != sketchScene || got[0]["drawing"] != sketchDrawing {
			t.Errorf("the page from the template: %v", got)
		}
	})

	t.Run("the database reads a sketch as document.PlainText does, and passes over a scene that is no JSON", func(t *testing.T) {
		raw, _ := json.Marshal(docOf(
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "before"}}},
			sketchNode(sketchScene, nil, "Wallaby routing"),
			sketchNode(`{"elements":[{"id":"gone","type":"text","text":"Gone","isDeleted":true}]}`, nil, nil),
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
		if want := document.PlainText(root); got != want || got != "before\nWallaby routing\nQuokka queue\nafter" {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
		broken, _ := json.Marshal(docOf(sketchNode("not json", nil, "Kept"), sketchNode(`{"elements":3}`, nil, nil)))
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, string(broken)).Scan(&got); err != nil || got != "Kept" {
			t.Fatalf("the database reads a broken scene as %q, %v", got, err)
		}
	})
}
