//go:build integration

package test

import (
	"bytes"
	"context"
	"image"
	"image/gif"
	"image/jpeg"
	"net/http"
	"testing"
)

// An edited picture (#94), cropped or drawn on in the browser, is saved as
// the next version of the file it was drawn on, which stays as it was.
func TestAnEditedPictureIsItsFilesNextVersion(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "picture-edit")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	readerID := h.addPerson(t, home.org, "member")
	reader := api.as(t, readerID, home.org, slug)
	docs := newTree(t, owner, "PEDT", "Edits")
	want(t, owner.put(t, "/api/v1/spaces/PEDT/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": user(home.user), "permissions": []any{"view", "addPages", "administer"}},
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
	}}), http.StatusOK, "everybody else reads PEDT")
	pageID := docs.add(docs.homeID, "Screens")
	files := "/api/v1/pages/" + pageID + "/attachments"

	upload := func(name, contentType string, data []byte) map[string]any {
		t.Helper()
		return obj(t, want(t, owner.uploadAs(t, files, name, contentType, data), http.StatusCreated, "upload "+name), "attachment")
	}
	edit := func(c *client, file map[string]any, contentType string, data []byte) response {
		t.Helper()
		return c.uploadAs(t, "/api/v1/attachments/"+file["id"].(string)+"/edit", "edited", contentType, data)
	}
	original := pngOf(t, 40, 30)
	shot := upload("shot.png", "image/png", original)
	notes := upload("notes.txt", "text/plain", []byte("notes"))
	var still bytes.Buffer
	if err := gif.Encode(&still, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	moving := upload("loop.gif", "image/gif", still.Bytes())
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	h.settle(t)

	var edited map[string]any
	cropped := pngOf(t, 20, 10)
	t.Run("the edit is the next version, with its own bytes and size, and the original stays", func(t *testing.T) {
		edited = obj(t, want(t, edit(owner, shot, "image/png", cropped), http.StatusCreated, "save the edited shot"), "attachment")
		if edited["version"] != float64(2) || edited["editedFrom"] != float64(1) || edited["restoredFrom"] != nil || edited["fileName"] != "shot.png" ||
			edited["contentType"] != "image/png" || edited["width"] != float64(20) || edited["height"] != float64(10) {
			t.Errorf("the edit reads %v", edited)
		}
		if shot["editedFrom"] != nil {
			t.Errorf("an upload reads as edited from %v", shot["editedFrom"])
		}
		if _, data := owner.download(t, "/api/v1/attachments/"+edited["id"].(string)); !bytes.Equal(data, cropped) {
			t.Error("the edited version does not hold the bytes sent")
		}
		if _, data := owner.download(t, "/api/v1/attachments/"+shot["id"].(string)); !bytes.Equal(data, original) {
			t.Error("the version drawn on changed")
		}
		current := filesOf(t, owner, files+"?current=true")
		if len(current) != 3 || current[0]["id"] != edited["id"] {
			t.Errorf("the latest reads %v", current)
		}
	})

	t.Run("an earlier version is edited into the next one too", func(t *testing.T) {
		again := obj(t, want(t, edit(owner, shot, "image/png", pngOf(t, 5, 5)), http.StatusCreated, "edit the first shot again"), "attachment")
		if again["version"] != float64(3) || again["editedFrom"] != float64(1) {
			t.Errorf("the second edit reads %v", again)
		}
	})

	t.Run("an edit keeps its file's type, whatever the part claims", func(t *testing.T) {
		for what, c := range map[string]struct {
			contentType string
			data        []byte
		}{
			"a JPEG for a PNG":        {"image/jpeg", photo.Bytes()},
			"a JPEG claiming PNG":     {"image/png", photo.Bytes()},
			"text claiming a picture": {"image/png", []byte("not a picture")},
		} {
			r := want(t, edit(owner, edited, c.contentType, c.data), http.StatusUnsupportedMediaType, what)
			if code := errorCode(t, r); code != "wrong_type" {
				t.Errorf("%s refused with %s", what, code)
			}
		}
		for what, file := range map[string]map[string]any{"a text file": notes, "a GIF, which may move": moving} {
			r := want(t, edit(owner, file, "image/png", cropped), http.StatusUnsupportedMediaType, "edit "+what)
			if code := errorCode(t, r); code != "not_editable" {
				t.Errorf("editing %s refused with %s", what, code)
			}
		}
		big := append(append([]byte{}, cropped...), make([]byte, testUploadLimit)...)
		if r := edit(owner, edited, "image/png", big); r.Status != http.StatusRequestEntityTooLarge || errorCode(t, r) != "too_large" {
			t.Errorf("an edit over the limit answered %d", r.Status)
		}
		want(t, edit(owner, edited, "image/png", nil), http.StatusUnprocessableEntity, "an empty edit")
	})

	t.Run("who may only read the page may not save an edit", func(t *testing.T) {
		want(t, edit(reader, edited, "image/png", cropped), http.StatusForbidden, "a reader edits")
		want(t, owner.uploadAs(t, "/api/v1/attachments/00000000-0000-7000-8000-000000000000/edit", "x.png", "image/png", cropped), http.StatusNotFound, "edit no file")
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1 AND lower(file_name) = 'shot.png'`, pageID); n != 3 {
			t.Errorf("the shot has %d versions after the refusals, want 3", n)
		}
	})

	t.Run("straight through SQL an edit names a picture's version of the same file and keeps its type, by an editor", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, readerID)
		denied(t, conn, "a reader saving an edit", `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'shot.png', 'image/png', 2, 1)`, home.org, pageID)
		actAs(t, conn, home.org, home.user)
		for what, sql := range map[string]string{
			"editing a version never made":  `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'shot.png', 'image/png', 2, 9)`,
			"an edit of another type":       `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'SHOT.png', 'image/jpeg', 2, 1)`,
			"editing a text file":           `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'notes.txt', 'text/plain', 2, 1)`,
			"editing a GIF":                 `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'loop.gif', 'image/gif', 2, 1)`,
			"an edit that is a restore too": `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from, restored_from) VALUES ($1, $2, 'shot.png', 'image/png', 2, 1, 1)`,
			"editing version 0":             `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'shot.png', 'image/png', 2, 0)`,
		} {
			refused(t, conn, what, sql, home.org, pageID)
		}
		refused(t, conn, "an edit of another page's file", `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'shot.png', 'image/png', 2, 1)`, home.org, docs.homeID)
		refused(t, conn, "marking an upload edited", `UPDATE attachment SET edited_from = 1 WHERE page_id = $1`, pageID)
		// What the rule allows goes through, so the refusals above are the rule's.
		if _, err := conn.Exec(context.Background(), `INSERT INTO attachment (org_id, page_id, file_name, content_type, size_bytes, edited_from) VALUES ($1, $2, 'Shot.PNG', 'image/png', 2, 2)`, home.org, pageID); err != nil {
			t.Errorf("an editor's edit of version 2 was refused: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1 AND lower(file_name) = 'shot.png'`, pageID); n != 4 {
			t.Errorf("the shot has %d versions, want the 3 the service made and the one allowed", n)
		}
	})
}
