//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// importFiles posts files under the paths given, as a browser sends a folder.
func (c *client) importFiles(t *testing.T, method, path string, files ...[2]string) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, f := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f[0]))
		header.Set("Content-Type", "application/octet-stream")
		part, err := form.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(f[1]))
	}
	_ = form.Close()
	return c.send(t, method, path, form.FormDataContentType(), &body)
}

func unzipped(t *testing.T, data []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is not a whole archive: %v", err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = string(content)
	}
	return out
}

func zipOf(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	_ = w.Close()
	return buf.String()
}

func TestMarkdownImportAndExport(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "markdown")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	readerID := h.addPerson(t, home.org, "member")
	reader := api.as(t, readerID, home.org, slug)
	away := h.makeMember(t, "markdown-away")
	stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
	nobody := api.anonymous()

	docs := newTree(t, owner, "MD", "Markdown")
	docs.c = member
	picture := pngOf(t, 4, 3)

	t.Run("a folder becomes pages, its pictures files and its links pages", func(t *testing.T) {
		got := obj(t, want(t, member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import"),
			[2]string{"handbook/README.md", "# Handbook\n\nRead [the setup](setup.md#install) first.\n\n![Logo](img/logo%20one.png)\n\n- [x] written\n"},
			[2]string{"handbook/setup.md", "# Setting up\n\n## Install\n\nRun `make`.\n\nSee [the notes](../notes.md) and [elsewhere](../../outside.md).\n"},
			[2]string{"handbook/img/logo one.png", string(picture)},
			[2]string{"notes.md", "Plain notes without a heading.\n"},
			[2]string{".git/config", "ignored"},
		), http.StatusCreated, "import a folder"))
		pages := got["pages"].([]any)
		if len(pages) != 3 {
			t.Fatalf("the import made %d pages: %v", len(pages), pages)
		}
		titles := map[string]map[string]any{}
		for _, p := range pages {
			titles[p.(map[string]any)["title"].(string)] = p.(map[string]any)
		}
		handbook, setup, notes := titles["Handbook"], titles["Setting up"], titles["notes"]
		if handbook == nil || setup == nil || notes == nil {
			t.Fatalf("the pages are titled %v", titles)
		}
		if setup["parentId"] != handbook["id"] || setup["depth"].(float64) != 2 || notes["parentId"] != docs.homeID {
			t.Errorf("the pages are not where their folders put them: %v", pages)
		}
		if len(got["warnings"].([]any)) == 0 {
			t.Error("a link out of the import did not warn")
		}

		read := obj(t, want(t, reader.get(t, pagePath(handbook["id"].(string))), http.StatusOK, "another member reads the handbook"), "page")
		if read["version"].(float64) != 1 || read["unpublished"] == true {
			t.Errorf("the imported page is not published: %v", read)
		}
		body := fmt.Sprint(read["body"])
		if !strings.Contains(body, "/s/MD/p/"+setup["id"].(string)+"#install") {
			t.Errorf("the link does not lead to the imported page: %s", body)
		}
		files := list(t, want(t, member.get(t, pagePath(handbook["id"].(string), "/attachments")), http.StatusOK, "the handbook's files"), "attachments")
		if len(files) != 1 || files[0].(map[string]any)["fileName"] != "logo one.png" {
			t.Fatalf("the handbook's files: %v", files)
		}
		if !strings.Contains(body, files[0].(map[string]any)["id"].(string)) || !strings.Contains(body, "taskList") {
			t.Errorf("the picture or the task list is not in the page: %s", body)
		}
		var stored []byte
		if err := h.super.QueryRow(context.Background(), `SELECT body FROM page WHERE id = $1`, setup["id"]).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(stored), `"level": 1`) || strings.Contains(string(stored), "Setting up") {
			t.Errorf("the title heading stayed in the body, or the headings did not move up: %s", stored)
		}
	})

	var guide, child, hidden, fileID string
	t.Run("an export holds the pages the caller may view, with their files", func(t *testing.T) {
		guide = docs.add(docs.homeID, "Guide")
		fileID = obj(t, want(t, member.upload(t, pagePath(guide, "/attachments"), "chart.png", picture), http.StatusCreated, "attach a chart"), "attachment")["id"].(string)
		child = docs.add(guide, "Child page")
		hidden = docs.add(guide, "Owner only")
		want(t, restrict(t, owner, hidden, []any{user(home.user)}, nil), http.StatusOK, "hide a page from the member")
		body := map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": fileID, "alt": "Chart", "width": nil}},
			map[string]any{"type": "paragraph", "content": []any{
				map[string]any{"type": "text", "text": "child", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "/s/MD/p/" + child}}}},
			}},
		}}
		guidePage := obj(t, want(t, member.get(t, pagePath(guide)), http.StatusOK, "read the guide"), "page")
		want(t, member.patch(t, pagePath(guide), map[string]any{"body": body, "version": guidePage["version"]}), http.StatusOK, "write the guide")

		resp, data := member.download(t, pagePath(guide, "/export?subtree=true"))
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" ||
			resp.Header.Get("Content-Disposition") != "attachment; filename=guide.zip" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("the export answered %d %v", resp.StatusCode, resp.Header)
		}
		files := unzipped(t, data)
		if files["guide.files/chart.png"] != string(picture) {
			t.Errorf("the chart is not in the archive: %v", keys(files))
		}
		if !strings.Contains(files["guide.md"], "# Guide") || !strings.Contains(files["guide.md"], "![Chart](guide.files/chart.png)") ||
			!strings.Contains(files["guide.md"], "[child](guide/child-page.md)") {
			t.Errorf("the guide reads\n%s", files["guide.md"])
		}
		if _, ok := files["guide/child-page.md"]; !ok {
			t.Errorf("the child is not in the archive: %v", keys(files))
		}
		if _, ok := files["guide/owner-only.md"]; ok {
			t.Error("the export holds a page the member may not view")
		}
		resp, data = owner.download(t, pagePath(guide, "/export?subtree=true"))
		if _, ok := unzipped(t, data)["guide/owner-only.md"]; !ok || resp.StatusCode != http.StatusOK {
			t.Error("the owner's export lacks the page only they may view")
		}
		resp, data = member.download(t, pagePath(guide, "/export"))
		if files := unzipped(t, data); len(files) != 2 || resp.StatusCode != http.StatusOK {
			t.Errorf("an export without its subtree holds %v", keys(files))
		}

		resp, data = member.download(t, pagePath(guide, "/markdown"))
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") || !strings.HasPrefix(string(data), "# Guide\n") {
			t.Errorf("one page as Markdown: %d %s\n%s", resp.StatusCode, resp.Header.Get("Content-Type"), data)
		}
	})

	t.Run("an exported archive imports as the pages it holds", func(t *testing.T) {
		_, data := member.download(t, pagePath(guide, "/export?subtree=true"))
		target := docs.add(docs.homeID, "Copies")
		got := obj(t, want(t, member.importFiles(t, http.MethodPost, pagePath(target, "/import"), [2]string{"guide.zip", string(data)}), http.StatusCreated, "import the archive"))
		pages := got["pages"].([]any)
		if len(pages) != 2 || pages[0].(map[string]any)["title"] != "Guide" || pages[1].(map[string]any)["title"] != "Child page" {
			t.Fatalf("the archive made %v", pages)
		}
		copyID := pages[0].(map[string]any)["id"].(string)
		files := list(t, want(t, member.get(t, pagePath(copyID, "/attachments")), http.StatusOK, "the copy's files"), "attachments")
		if len(files) != 1 || files[0].(map[string]any)["fileName"] != "chart.png" || files[0].(map[string]any)["id"] == fileID {
			t.Fatalf("the copy's files: %v", files)
		}
		body := fmt.Sprint(obj(t, member.get(t, pagePath(copyID)), "page")["body"])
		if !strings.Contains(body, files[0].(map[string]any)["id"].(string)) || !strings.Contains(body, pages[1].(map[string]any)["id"].(string)) {
			t.Errorf("the copy does not point at its own file and child: %s", body)
		}
	})

	t.Run("a page's content is replaced by one file over its version", func(t *testing.T) {
		page := obj(t, want(t, member.get(t, pagePath(child)), http.StatusOK, "read the child"), "page")
		version := int(page["version"].(float64))
		path := pagePath(child, fmt.Sprintf("/markdown?version=%d", version))
		got := obj(t, want(t, member.importFiles(t, http.MethodPut, path,
			[2]string{"child.md", "# Renamed child\n\n![Pic](pic.png)\n\n> [!WARNING]\n> Careful\n"},
			[2]string{"pic.png", string(picture)},
		), http.StatusOK, "replace the child"), "page")
		if got["title"] != "Renamed child" || int(got["version"].(float64)) != version+1 || !strings.Contains(fmt.Sprint(got["body"]), "panel") {
			t.Errorf("the replaced page is %v", got)
		}
		if got := member.importFiles(t, http.MethodPut, path, [2]string{"child.md", "stale"}); got.Status != http.StatusConflict {
			t.Errorf("a replace over an old version: %d %s", got.Status, got.Raw)
		}
		if got := member.importFiles(t, http.MethodPut, pagePath(child, "/markdown"), [2]string{"child.md", "x"}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a replace without a version: %d %s", got.Status, got.Raw)
		}
		if got := member.importFiles(t, http.MethodPut, path, [2]string{"a.md", "a"}, [2]string{"b.md", "b"}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a replace with two files: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("what is refused, and how", func(t *testing.T) {
		reading := newTree(t, owner, "MDREAD", "Read only")
		readPage := reading.add(reading.homeID, "Read me")
		want(t, owner.put(t, "/api/v1/spaces/MDREAD/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "MDREAD is read only")
		h.settle(t)
		md := [2]string{"page.md", "# Page\n"}
		if got := reader.importFiles(t, http.MethodPost, pagePath(readPage, "/import"), md); got.Status != http.StatusForbidden {
			t.Errorf("an import where the caller may only read: %d %s", got.Status, got.Raw)
		}
		if got := reader.importFiles(t, http.MethodPut, pagePath(readPage, "/markdown?version=1"), md); got.Status != http.StatusForbidden {
			t.Errorf("a replace where the caller may only read: %d %s", got.Status, got.Raw)
		}
		resp, _ := reader.download(t, pagePath(readPage, "/export"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("a reader's export: %d", resp.StatusCode)
		}

		conn := appConn(t)
		actAs(t, conn, home.org, readerID)
		denied(t, conn, "a page the import would make", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by) VALUES ($1, (SELECT space_id FROM page WHERE id = $2), $2, 'W', 'Imported', $3)`, home.org, readPage, readerID)
		denied(t, conn, "a file the import would attach", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'x.png', 1)`, home.org, readPage)
		denied(t, conn, "a version the replace would publish", `INSERT INTO page_version (org_id, page_id, number, title, body, created_by) VALUES ($1, $2, 2, 'Replaced', '{}', $3)`, home.org, readPage, readerID)
		actAs(t, conn, home.org, memberID)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE id = $1`, hidden).Scan(&n); err != nil || n != 0 {
			t.Errorf("the member reads the page the export left out through SQL: %d %v", n, err)
		}

		for what, got := range map[string]response{
			"export":   nobody.get(t, pagePath(guide, "/export")),
			"markdown": nobody.get(t, pagePath(guide, "/markdown")),
			"replace":  nobody.importFiles(t, http.MethodPut, pagePath(guide, "/markdown?version=1"), md),
			"import":   nobody.importFiles(t, http.MethodPost, pagePath(guide, "/import"), md),
		} {
			if got.Status != http.StatusUnauthorized {
				t.Errorf("%s without a session: %d", what, got.Status)
			}
		}
		for what, got := range map[string]response{
			"export":       stranger.get(t, pagePath(guide, "/export")),
			"markdown":     stranger.get(t, pagePath(guide, "/markdown")),
			"hidden":       member.get(t, pagePath(hidden, "/markdown")),
			"hidden below": member.get(t, pagePath(hidden, "/export?subtree=true")),
			"replace":      stranger.importFiles(t, http.MethodPut, pagePath(guide, "/markdown?version=1"), md),
			"import":       stranger.importFiles(t, http.MethodPost, pagePath(guide, "/import"), md),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("%s of a page the caller may not view: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.get(t, pagePath(guide, "/export?subtree=maybe")); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a subtree that is neither true nor false: %d", got.Status)
		}
		for what, files := range map[string][][2]string{
			"no Markdown":       {{"logo.png", string(picture)}},
			"a path out":        {{"../escape.md", "# Out\n"}},
			"a broken archive":  {{"docs.zip", "not a zip"}},
			"an empty picture":  {{"a.md", "![x](x.png)"}, {"x.png", ""}},
			"an archive escape": {{"evil.zip", zipOf(t, map[string]string{"../../evil.md": "# Evil\n"})}},
		} {
			got := member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import"), files...)
			if got.Status != http.StatusUnprocessableEntity || obj(t, got, "error", "fields")["file"] == nil {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.post(t, pagePath(docs.homeID, "/import"), map[string]any{"file": "x"}); got.Status != http.StatusBadRequest {
			t.Errorf("JSON instead of files: %d", got.Status)
		}
		var planted int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE title IN ('Out', 'Evil')`).Scan(&planted); err != nil || planted != 0 {
			t.Errorf("a refused import left %d pages (%v)", planted, err)
		}
	})
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
