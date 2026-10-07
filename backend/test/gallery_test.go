//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func galleryOf(columns int, ids ...string) map[string]any {
	pictures := make([]any, 0, len(ids))
	for i, id := range ids {
		pictures = append(pictures, map[string]any{"type": "galleryImage", "attrs": map[string]any{"attachmentId": id, "caption": fmt.Sprintf("Picture %d", i+1)}})
	}
	return map[string]any{"type": "gallery", "attrs": map[string]any{"columns": columns}, "content": pictures}
}

// galleryIn is the files the first gallery of a document names, in its order.
func galleryIn(t *testing.T, body any) []string {
	t.Helper()
	for _, block := range body.(map[string]any)["content"].([]any) {
		b := block.(map[string]any)
		if b["type"] != "gallery" {
			continue
		}
		var out []string
		for _, p := range b["content"].([]any) {
			out = append(out, p.(map[string]any)["attrs"].(map[string]any)["attachmentId"].(string))
		}
		return out
	}
	t.Fatalf("no gallery in %v", body)
	return nil
}

// A gallery (#96) names one version of each of its pictures by id, as an
// image does, so each reader sees only the pictures they may download, a
// copy points at its own files, and an export carries the pictures.
func TestAGalleryShowsEachReaderThePicturesTheyMayDownload(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "gallery")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.addPerson(t, home.org, "member")
	ina := api.as(t, inaID, home.org, slug)
	anon := api.anonymous()
	ctx := context.Background()

	open := newTree(t, owner, "GALL", "Gallery")
	shut := newTree(t, owner, "GSHUT", "Shut gallery")
	album := open.add(open.homeID, "Album")
	secret := shut.add(shut.homeID, "Secret")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "hide the secret page from ina")
	upload := func(page, name string) string {
		t.Helper()
		return obj(t, want(t, owner.upload(t, pagePath(page, "/attachments"), name, pngOf(t, 4, 3)), http.StatusCreated, "upload "+name), "attachment")["id"].(string)
	}
	first, second := upload(album, "first.png"), upload(album, "second.png")
	hidden := upload(secret, "hidden.png")
	publishBody(t, owner, album, docOf(galleryOf(3, second, first, hidden)))

	t.Run("the page keeps the pictures in their order with their captions", func(t *testing.T) {
		body := obj(t, want(t, owner.get(t, pagePath(album)), http.StatusOK, "read the album"), "page")["body"]
		if got := galleryIn(t, body); strings.Join(got, ",") != strings.Join([]string{second, first, hidden}, ",") {
			t.Errorf("the gallery names %v", got)
		}
		if !strings.Contains(fmt.Sprint(body), "caption:Picture 2") || !strings.Contains(fmt.Sprint(body), "columns:3") {
			t.Errorf("the gallery lost its captions or columns: %v", body)
		}
	})

	t.Run("a gallery out of its bounds is refused", func(t *testing.T) {
		many := make([]string, 61)
		for i := range many {
			many[i] = first
		}
		for what, block := range map[string]map[string]any{
			"no pictures":       galleryOf(3),
			"one column":        galleryOf(1, first),
			"five columns":      galleryOf(5, first),
			"too many pictures": galleryOf(3, many...),
			"an image in it": {"type": "gallery", "attrs": map[string]any{"columns": 2}, "content": []any{
				map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": first, "alt": nil, "width": nil}},
			}},
			"a picture by address": {"type": "gallery", "attrs": map[string]any{"columns": 2}, "content": []any{
				map[string]any{"type": "galleryImage", "attrs": map[string]any{"attachmentId": first, "caption": nil, "src": "https://evil.test/x.png"}},
			}},
		} {
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": open.homeID, "title": "Bad " + what, "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a member reads the gallery but downloads no picture of a page hidden from them", func(t *testing.T) {
		h.settle(t)
		body := obj(t, want(t, ina.get(t, pagePath(album)), http.StatusOK, "ina reads the album"), "page")["body"]
		if got := galleryIn(t, body); len(got) != 3 {
			t.Fatalf("ina's gallery names %v", got)
		}
		for _, id := range []string{first, second} {
			if resp, _ := ina.download(t, "/api/v1/attachments/"+id+"?inline=1"); resp.StatusCode != http.StatusOK {
				t.Errorf("ina's picture %s: %d", id, resp.StatusCode)
			}
		}
		if resp, _ := owner.download(t, "/api/v1/attachments/"+hidden+"?inline=1"); resp.StatusCode != http.StatusOK {
			t.Fatalf("the owner's hidden picture: %d", resp.StatusCode)
		}
		want(t, ina.get(t, "/api/v1/attachments/"+hidden+"?inline=1"), http.StatusNotFound, "ina's hidden picture")

		conn := appConn(t)
		actAs(t, conn, home.org, inaID)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE id = ANY($1::uuid[])`, []string{first, second, hidden}).Scan(&n); err != nil || n != 2 {
			t.Errorf("ina reads %d of the gallery's three files through SQL (%v), want the two of the album", n, err)
		}
	})

	t.Run("an anonymous reader gets the gallery and downloads only the public pictures", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/GALL/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open the gallery's space")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
		h.settle(t)
		page := obj(t, want(t, anon.get(t, publicPath(slug, "/pages/", album)), http.StatusOK, "the public album"), "page")
		if got := galleryIn(t, page["body"]); len(got) != 3 {
			t.Fatalf("the public gallery names %v", got)
		}
		for _, id := range []string{first, second} {
			if resp, _ := anon.download(t, publicPath(slug, "/attachments/", id)); resp.StatusCode != http.StatusOK {
				t.Errorf("the public picture %s: %d", id, resp.StatusCode)
			}
		}
		want(t, anon.get(t, publicPath(slug, "/attachments/", hidden)), http.StatusNotFound, "the hidden picture, read by anybody")

		conn := appConn(t)
		actAnonymously(t, conn, home.org)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE id = ANY($1::uuid[])`, []string{first, second, hidden}).Scan(&n); err != nil || n != 2 {
			t.Errorf("an anonymous reader reads %d of the gallery's three files through SQL (%v), want the two of the album", n, err)
		}
	})

	t.Run("a copy's gallery points at the copy's own files", func(t *testing.T) {
		copied := obj(t, want(t, owner.post(t, pagePath(album, "/copy"), map[string]any{"parentId": open.homeID}), http.StatusCreated, "copy the album"), "page")
		mine := map[string]string{}
		for _, each := range list(t, want(t, owner.get(t, pagePath(copied["id"].(string), "/attachments")), http.StatusOK, "the copy's files"), "attachments") {
			f := each.(map[string]any)
			mine[f["fileName"].(string)] = f["id"].(string)
		}
		got := galleryIn(t, copied["body"])
		if want := []string{mine["second.png"], mine["first.png"], hidden}; strings.Join(got, ",") != strings.Join(want, ",") || mine["first.png"] == "" || mine["first.png"] == first {
			t.Errorf("the copy's gallery names %v, want %v", got, want)
		}
		if got := galleryIn(t, obj(t, owner.get(t, pagePath(album)), "page")["body"]); got[0] != second {
			t.Errorf("copying moved the original's gallery to %v", got)
		}
	})

	t.Run("an export carries the gallery's pictures and an import brings the gallery back", func(t *testing.T) {
		_, data := owner.download(t, pagePath(album, "/export"))
		files := unzipped(t, data)
		md := files["album.md"]
		if !strings.Contains(md, `<div data-stator="gallery" data-columns="3">`) || !strings.Contains(md, `<img src="album.files/second.png" alt="Picture 1">`) {
			t.Fatalf("the album reads\n%s", md)
		}
		if strings.Contains(md, "hidden.png") {
			t.Errorf("the export names a picture of another page:\n%s", md)
		}
		target := open.add(open.homeID, "Imported")
		got := obj(t, want(t, owner.importFiles(t, http.MethodPost, pagePath(target, "/import"), [2]string{"album.zip", string(data)}), http.StatusCreated, "import the album"))
		imported := got["pages"].([]any)[0].(map[string]any)["id"].(string)
		mine := map[string]string{}
		for _, each := range list(t, want(t, owner.get(t, pagePath(imported, "/attachments")), http.StatusOK, "the import's files"), "attachments") {
			f := each.(map[string]any)
			mine[f["fileName"].(string)] = f["id"].(string)
		}
		body := obj(t, owner.get(t, pagePath(imported)), "page")["body"]
		if got := galleryIn(t, body); strings.Join(got, ",") != mine["second.png"]+","+mine["first.png"] {
			t.Errorf("the imported gallery names %v of %v", got, mine)
		}
	})
}
