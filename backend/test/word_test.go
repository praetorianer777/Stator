//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/convert"
	"github.com/praetorianer777/stator/backend/internal/docx"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// wordFile fails unless the answer is a Word document, and returns its parts by name.
func wordFile(t *testing.T, resp *http.Response, data []byte, what string) map[string]string {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %s", what, resp.StatusCode, data)
	}
	if got := resp.Header.Get("Content-Type"); got != docx.ContentType {
		t.Errorf("%s: Content-Type %q", what, got)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("%s is no zip: %v", what, err)
	}
	parts := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
	}
	if parts["word/document.xml"] == "" {
		t.Fatalf("%s has no document.xml", what)
	}
	return parts
}

// Word export (#87): the published page as a .docx as its reader may read it;
// anybody exports what anybody may read, and what nobody may is refused.
func TestAPageIsExportedToWordAsItsReaderReadsIt(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h, func(s *httpapi.Server) { s.AppBaseURL = "https://stator.example" })
	home := h.makeMember(t, "word-export")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.addPerson(t, home.org, "member")
	ina := api.as(t, inaID, home.org, slug)
	ctx := context.Background()

	docs := newTree(t, owner, "WRD", "Word handbook")
	guide := docs.add(docs.homeID, "Word guide")
	secret := docs.add(docs.homeID, "Word secret")
	publishBody(t, owner, secret, map[string]any{"type": "doc", "content": []any{plainPara("Only the owner reads this.")}})
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "keep the secret to the owner")
	picture := obj(t, want(t, owner.upload(t, pagePath(guide, "/attachments"), "chart.png", pngOf(t, 40, 20)), http.StatusCreated, "upload a picture"), "attachment")["id"].(string)
	cell := func(kind, words string) map[string]any {
		return map[string]any{"type": kind, "content": []any{plainPara(words)}}
	}
	item := func(content ...any) map[string]any { return map[string]any{"type": "listItem", "content": content} }
	publishBody(t, owner, guide, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": 1}, "content": []any{map[string]any{"type": "text", "text": "Setting up"}}},
		map[string]any{"type": "bulletList", "content": []any{
			item(plainPara("Outer step"), map[string]any{"type": "orderedList", "content": []any{item(plainPara("Inner step"))}}),
		}},
		map[string]any{"type": "table", "content": []any{
			map[string]any{"type": "tableRow", "content": []any{cell("tableHeader", "Setting"), cell("tableHeader", "Value")}},
			map[string]any{"type": "tableRow", "content": []any{cell("tableCell", "Port"), cell("tableCell", "8080")}},
		}},
		map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": picture, "alt": "The chart"}},
		map[string]any{"type": "codeBlock", "attrs": map[string]any{"language": "go"}, "content": []any{map[string]any{"type": "text", "text": "fmt.Println(\"ready\")"}}},
		map[string]any{"type": "include", "attrs": map[string]any{"pageId": secret}},
	}})
	box := folder(docs, docs.homeID, "Word folder")
	draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Word draft"}), http.StatusCreated, "a draft"), "page")["id"].(string)
	h.settle(t)

	t.Run("a member downloads the published page with its blocks and picture, and the export is audited", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(guide, "/docx"))
		parts := wordFile(t, resp, data, "the owner's document")
		body := parts["word/document.xml"]
		for _, want := range []string{
			`<w:pStyle w:val="Heading1"/>`, "Setting up",
			`<w:ilvl w:val="1"/>`, "Inner step",
			`<w:tblHeader/>`, "Setting", "8080",
			`<w:drawing>`, `descr="The chart"`,
			`<w:pStyle w:val="Code"/>`, `fmt.Println(&#34;ready&#34;)`,
			"Only the owner reads this.", "Included from Word secret",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the owner's document lacks %s", want)
			}
		}
		if parts["word/media/picture1.png"] == "" {
			t.Error("the picture is not inside the document")
		}
		core := parts["docProps/core.xml"]
		if !strings.Contains(core, "<dc:title>Word guide</dc:title>") || !strings.Contains(core, "<dc:creator>") || !strings.Contains(core, "<dcterms:modified") {
			t.Errorf("the properties read %s", core)
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "WRD-word-guide-"+time.Now().Format("2006-01-02")+".docx") {
			t.Errorf("Content-Disposition = %q, want a download named after the space, the page and the day", got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		if data := h.recordedOnce(t, home.org, "page.exported", &home.user, guide); !strings.Contains(data, `"scope": "docx"`) {
			t.Errorf("the export's record reads %s", data)
		}
		// The stack's office suite opening it is the proof an editor of
		// documents reads what was written, beyond its being well formed.
		pdf, err := convert.New(h.cfg.ConverterURL, attachment.MaxPreviewSize).ToPDF(ctx, "guide.docx", data)
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Errorf("the office suite could not open the document: %v", err)
		}
	})

	t.Run("a reader who may not read the included page gets a sentence in their language, never its words", func(t *testing.T) {
		want(t, ina.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "ina reads German")
		resp, data := ina.download(t, pagePath(guide, "/docx"))
		parts := wordFile(t, resp, data, "ina's document")
		body := parts["word/document.xml"]
		if strings.Contains(body, "Only the owner reads this.") || strings.Contains(body, "Word secret") {
			t.Error("ina's document carries the page kept from her")
		}
		if !strings.Contains(body, "Hier steht eine andere Seite") {
			t.Error("ina's document does not say an included page is left out")
		}
		if !strings.Contains(parts["word/styles.xml"], `w:val="de-DE"`) {
			t.Error("ina's document is not in German")
		}
		// The service leaving it out is not the proof: the database itself
		// shows ina no such page.
		conn := appConn(t)
		actAs(t, conn, home.org, inaID)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page WHERE id = $1`, secret).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("the database shows ina the secret page %d times", n)
		}
	})

	t.Run("a reader without access, a folder and a draft are refused", func(t *testing.T) {
		want(t, ina.get(t, pagePath(secret, "/docx")), http.StatusNotFound, "ina exports the secret")
		want(t, ina.get(t, pagePath(draft, "/docx")), http.StatusNotFound, "ina exports the owner's draft")
		if got := want(t, owner.get(t, pagePath(box, "/docx")), http.StatusConflict, "export a folder"); errorCode(t, got) != "not_exportable" {
			t.Errorf("a folder is refused with %s", got.Raw)
		}
		got := want(t, owner.get(t, pagePath(draft, "/docx")), http.StatusConflict, "export a page never published")
		if errorCode(t, got) != "not_exportable" || !strings.Contains(got.Body["error"].(map[string]any)["message"].(string), "Publish it") {
			t.Errorf("a draft is refused with %s", got.Raw)
		}
		want(t, api.anonymous().get(t, pagePath(guide, "/docx")), http.StatusUnauthorized, "nobody exports a page")
	})

	t.Run("anybody exports what anybody may read, without what only members read", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/WRD/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open WRD")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
		h.settle(t)
		anon := api.anonymous()
		resp, data := anon.download(t, publicPath(slug, "/pages/", guide, "/docx"))
		parts := wordFile(t, resp, data, "the public document")
		body := parts["word/document.xml"]
		if strings.Contains(body, "Only the owner reads this.") || !strings.Contains(body, "Setting up") {
			t.Error("the public document is not the page as anybody reads it")
		}
		if parts["word/media/picture1.png"] == "" {
			t.Error("the public document lacks the page's picture")
		}
		if core := parts["docProps/core.xml"]; strings.Contains(core, "creator") || strings.Contains(core, "lastModifiedBy") {
			t.Errorf("the public document names who wrote it: %s", core)
		}
		want(t, anon.get(t, publicPath(slug, "/pages/", secret, "/docx")), http.StatusNotFound, "export a page nobody may read")
	})

	t.Run("a public link exports its page for whoever holds it, until it is revoked", func(t *testing.T) {
		made := want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"label": "Word"}), http.StatusCreated, "make a link")
		token := made.Body["token"].(string)
		linkID := obj(t, made, "link")["id"].(string)
		h.settle(t)
		anon := api.anonymous()
		resp, data := anon.download(t, linkPath(slug, token, "/docx"))
		parts := wordFile(t, resp, data, "the link's document")
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, `filename=word-guide-`) {
			t.Errorf("the link's document is named %q, which should say nothing of its space", got)
		}
		for name, part := range parts {
			if strings.Contains(part, token) {
				t.Errorf("the link's document carries its token in %s", name)
			}
		}
		if parts["word/media/picture1.png"] == "" {
			t.Error("the link's document lacks the page's picture")
		}
		want(t, owner.delete(t, pagePath(guide, "/public-links/", linkID)), http.StatusNoContent, "revoke the link")
		h.settle(t)
		if got := anon.get(t, linkPath(slug, token, "/docx")); got.Status != http.StatusNotFound || errorCode(t, got) != "link_gone" {
			t.Errorf("the revoked link exported %d %s", got.Status, got.Raw)
		}
	})

	// Last, since the brake is per page and would refuse the exports above.
	t.Run("one address is braked on one public page", func(t *testing.T) {
		anon := api.anonymous()
		throttled := false
		for range httpapi.PublicWordExportsPerMinute + 1 {
			if anon.get(t, publicPath(slug, "/pages/", guide, "/docx")).Status == http.StatusTooManyRequests {
				throttled = true
				break
			}
		}
		if !throttled {
			t.Errorf("one address exported one public page more than %d times a minute", httpapi.PublicWordExportsPerMinute)
		}
	})
}

func plainPara(words string) map[string]any {
	return map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": words}}}
}
