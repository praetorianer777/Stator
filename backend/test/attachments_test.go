//go:build integration

package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// testUploadLimit is the suite's upload limit, small so a refusal costs little.
const testUploadLimit = 1 << 20

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// uploadAs sends a file with a content type of the caller's choosing, as a
// browser does for the files it knows.
func (c *client) uploadAs(t *testing.T, path, name, contentType string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	return c.send(t, http.MethodPost, path, form.FormDataContentType(), &body)
}

func objectKey(org uuid.UUID, page, id string) string {
	return fmt.Sprintf("org/%s/page/%s/%s", org, page, id)
}

// inBucket reads an object straight from the bucket, nil when it is gone.
func inBucket(t *testing.T, store objectstore.Store, key string) []byte {
	t.Helper()
	body, err := store.Get(context.Background(), key)
	if errors.Is(err, objectstore.ErrNoObject) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	defer body.Close()
	data, _ := io.ReadAll(body)
	return data
}

func (h *harness) tombstones(t *testing.T, key string) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAttachmentsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "files")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	away := h.makeMember(t, "files-away")
	stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
	nobody := api.anonymous()

	docs := newTree(t, owner, "FILES", "Files")
	docs.c = member
	pageID := docs.add(docs.homeID, "Plan")
	files := "/api/v1/pages/" + pageID + "/attachments"
	picture := pngOf(t, 3, 2)

	var pictureID string
	t.Run("an upload lands in the bucket under the organization", func(t *testing.T) {
		made := obj(t, want(t, member.upload(t, files, "plan.png", picture), http.StatusCreated, "upload"), "attachment")
		pictureID = made["id"].(string)
		if made["fileName"] != "plan.png" || made["contentType"] != "image/png" || made["size"].(float64) != float64(len(picture)) ||
			made["width"].(float64) != 3 || made["height"].(float64) != 2 || made["uploadedByName"] != "A member" || made["pageId"] != pageID {
			t.Fatalf("the upload answered %v", made)
		}
		if got := inBucket(t, api.store, objectKey(home.org, pageID, pictureID)); !bytes.Equal(got, picture) {
			t.Fatalf("the bucket holds %d bytes, want the %d uploaded", len(got), len(picture))
		}
	})

	t.Run("a download says what it is and never runs", func(t *testing.T) {
		resp, data := member.download(t, "/api/v1/attachments/"+pictureID+"?inline=1")
		if resp.StatusCode != http.StatusOK || !bytes.Equal(data, picture) {
			t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(data))
		}
		for header, value := range map[string]string{
			"Content-Type":           "image/png",
			"Content-Disposition":    "inline; filename=plan.png",
			"X-Content-Type-Options": "nosniff",
			"Cache-Control":          "private, max-age=0",
		} {
			if got := resp.Header.Get(header); got != value {
				t.Errorf("%s is %q, want %q", header, got, value)
			}
		}
		resp, _ = member.download(t, "/api/v1/attachments/"+pictureID)
		if got := resp.Header.Get("Content-Disposition"); got != "attachment; filename=plan.png" {
			t.Errorf("without inline the picture is %q", got)
		}

		for _, risky := range []struct{ name, contentType, body string }{
			{"page.html", "text/html", "<script>alert(1)</script>"},
			{"logo.svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`},
		} {
			made := obj(t, want(t, member.uploadAs(t, files, risky.name, risky.contentType, []byte(risky.body)), http.StatusCreated, "upload "+risky.name), "attachment")
			resp, _ := member.download(t, "/api/v1/attachments/"+made["id"].(string)+"?inline=1")
			if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
				t.Errorf("%s shows in place: %q", risky.name, got)
			}
			if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s may be sniffed", risky.name)
			}
		}
	})

	t.Run("the list is the latest first", func(t *testing.T) {
		got := list(t, want(t, owner.get(t, files), http.StatusOK, "list"), "attachments")
		if len(got) != 3 || got[0].(map[string]any)["fileName"] != "logo.svg" || got[2].(map[string]any)["id"] != pictureID {
			t.Fatalf("the list is %v", got)
		}
	})

	t.Run("what is refused, and how", func(t *testing.T) {
		big := make([]byte, testUploadLimit+1)
		if got := member.upload(t, files, "big.bin", big); got.Status != http.StatusRequestEntityTooLarge || errorCode(t, got) != "too_large" ||
			!strings.Contains(string(got.Raw), "1 MB") {
			t.Errorf("an oversized file: %d %s", got.Status, got.Raw)
		}
		if got := member.upload(t, files, "empty.txt", nil); got.Status != http.StatusUnprocessableEntity || obj(t, got, "error", "fields")["file"] == nil {
			t.Errorf("an empty file: %d %s", got.Status, got.Raw)
		}
		if got := member.post(t, files, map[string]any{"file": "x"}); got.Status != http.StatusBadRequest {
			t.Errorf("JSON instead of a file: %d %s", got.Status, got.Raw)
		}
		for what, got := range map[string]response{
			"list":     nobody.get(t, files),
			"upload":   nobody.upload(t, files, "a.txt", []byte("a")),
			"download": nobody.get(t, "/api/v1/attachments/"+pictureID),
			"delete":   nobody.delete(t, "/api/v1/attachments/"+pictureID),
		} {
			if got.Status != http.StatusUnauthorized {
				t.Errorf("%s without a session: %d", what, got.Status)
			}
		}
		for what, got := range map[string]response{
			"list":     stranger.get(t, files),
			"upload":   stranger.upload(t, files, "a.txt", []byte("a")),
			"download": stranger.get(t, "/api/v1/attachments/"+pictureID),
			"delete":   stranger.delete(t, "/api/v1/attachments/"+pictureID),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("%s from another organization: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := member.get(t, "/api/v1/attachments/not-an-id"); got.Status != http.StatusBadRequest {
			t.Errorf("a malformed id: %d", got.Status)
		}
	})

	t.Run("a file on a trashed page is gone until the page is back", func(t *testing.T) {
		want(t, member.delete(t, "/api/v1/pages/"+pageID), http.StatusNoContent, "trash the page")
		if got := member.get(t, "/api/v1/attachments/"+pictureID); got.Status != http.StatusNotFound {
			t.Errorf("a file on a trashed page: %d", got.Status)
		}
		if got := member.get(t, files); got.Status != http.StatusNotFound {
			t.Errorf("the files of a trashed page: %d", got.Status)
		}
		want(t, docs.restore(pageID), http.StatusOK, "restore the page")
		want(t, member.get(t, "/api/v1/attachments/"+pictureID), http.StatusOK, "the file after the restore")
	})

	t.Run("a copy brings its own files and points at them", func(t *testing.T) {
		child := docs.add(pageID, "Plan detail")
		childFile := obj(t, want(t, member.upload(t, "/api/v1/pages/"+child+"/attachments", "notes.txt", []byte("notes")), http.StatusCreated, "upload to the child"), "attachment")["id"].(string)
		body := fmt.Sprintf(`{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":%q,"alt":"plan","width":null}},`+
			`{"type":"paragraph","content":[{"type":"attachment","attrs":{"attachmentId":%q,"fileName":"notes.txt"}}]}]}`, pictureID, childFile)
		if _, err := h.super.Exec(context.Background(), `UPDATE page SET body = $2 WHERE id = $1`, pageID, body); err != nil {
			t.Fatal(err)
		}
		h.settle(t)

		copied := obj(t, want(t, member.post(t, "/api/v1/pages/"+pageID+"/copy", map[string]any{"parentId": docs.homeID, "withChildren": true}), http.StatusCreated, "copy"), "page")
		copyID := copied["id"].(string)
		mine := list(t, want(t, member.get(t, "/api/v1/pages/"+copyID+"/attachments"), http.StatusOK, "the copy's files"), "attachments")
		if len(mine) != 3 {
			t.Fatalf("the copy has %d files, want 3", len(mine))
		}
		var copiedPicture string
		for _, each := range mine {
			f := each.(map[string]any)
			if f["id"] == pictureID {
				t.Fatal("the copy shares a file with its original")
			}
			if f["fileName"] == "plan.png" {
				copiedPicture = f["id"].(string)
			}
		}
		if got := inBucket(t, api.store, objectKey(home.org, copyID, copiedPicture)); !bytes.Equal(got, picture) {
			t.Fatalf("the copy's picture holds %d bytes in the bucket", len(got))
		}
		text := string(copied["body"].(map[string]any)["content"].([]any)[0].(map[string]any)["attrs"].(map[string]any)["attachmentId"].(string))
		if text != copiedPicture {
			t.Errorf("the copy's image points at %s, want its own %s", text, copiedPicture)
		}
		var childCopy string
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM page WHERE parent_id = $1`, copyID).Scan(&childCopy); err != nil {
			t.Fatal(err)
		}
		if n := len(list(t, want(t, member.get(t, "/api/v1/pages/"+childCopy+"/attachments"), http.StatusOK, "the child copy's files"), "attachments")); n != 1 {
			t.Errorf("the child's copy has %d files", n)
		}
		var original string
		if err := h.super.QueryRow(context.Background(), `SELECT body->'content'->0->'attrs'->>'attachmentId' FROM page WHERE id = $1`, pageID).Scan(&original); err != nil || original != pictureID {
			t.Errorf("the original now points at %s (%v)", original, err)
		}
	})

	t.Run("a delete is final and takes the bytes", func(t *testing.T) {
		key := objectKey(home.org, pageID, pictureID)
		want(t, member.delete(t, "/api/v1/attachments/"+pictureID), http.StatusNoContent, "delete")
		if inBucket(t, api.store, key) != nil {
			t.Error("the bytes are still in the bucket")
		}
		if n := h.tombstones(t, key); n != 0 {
			t.Errorf("%d tombstones are left", n)
		}
		if got := member.get(t, "/api/v1/attachments/"+pictureID); got.Status != http.StatusNotFound {
			t.Errorf("a deleted file downloads: %d", got.Status)
		}
		if got := member.delete(t, "/api/v1/attachments/"+pictureID); got.Status != http.StatusNotFound {
			t.Errorf("a deleted file deletes again: %d", got.Status)
		}
	})

	t.Run("purging a page and deleting a space take the bytes", func(t *testing.T) {
		gone := docs.add(docs.homeID, "Doomed")
		id := obj(t, want(t, member.upload(t, "/api/v1/pages/"+gone+"/attachments", "a.txt", []byte("a")), http.StatusCreated, "upload"), "attachment")["id"].(string)
		key := objectKey(home.org, gone, id)
		want(t, member.delete(t, "/api/v1/pages/"+gone), http.StatusNoContent, "trash")
		if inBucket(t, api.store, key) == nil {
			t.Fatal("trashing a page took its files")
		}
		want(t, owner.delete(t, "/api/v1/spaces/FILES/trash/"+gone), http.StatusNoContent, "purge")
		if inBucket(t, api.store, key) != nil || h.tombstones(t, key) != 0 {
			t.Error("a purged page's file is still in the bucket")
		}

		keys, err := api.store.List(context.Background(), objectstore.OrgPrefix(home.org))
		if err != nil || len(keys) == 0 {
			t.Fatalf("the organization has %d objects (%v)", len(keys), err)
		}
		want(t, owner.delete(t, "/api/v1/spaces/FILES"), http.StatusNoContent, "delete the space")
		if keys, _ := api.store.List(context.Background(), objectstore.OrgPrefix(home.org)); len(keys) != 0 {
			t.Errorf("a deleted space left %v", keys)
		}
	})
}

// Deleting an organization cascades to its files, and the tombstones it
// leaves behind let the worker's reaper empty its prefix.
func TestTheReaperEmptiesADeletedOrganization(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "files-reap")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "REAP", "Reaped")
	id := obj(t, want(t, owner.upload(t, "/api/v1/pages/"+docs.homeID+"/attachments", "a.txt", []byte("a")), http.StatusCreated, "upload"), "attachment")["id"].(string)
	key := objectKey(home.org, docs.homeID, id)

	if _, err := h.super.Exec(context.Background(), `DELETE FROM org WHERE id = $1`, home.org); err != nil {
		t.Fatal(err)
	}
	if h.tombstones(t, key) != 1 {
		t.Fatal("deleting the organization left no tombstone")
	}
	reaper := attachment.NewReaper(api.attachments, discard(), 0)
	for h.tombstones(t, key) > 0 {
		n, err := reaper.Once(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 && h.tombstones(t, key) > 0 {
			t.Fatal("the reaper stopped with the tombstone still there")
		}
	}
	if inBucket(t, api.store, key) != nil {
		t.Error("the reaper left the bytes")
	}
}

// Straight through SQL as stator_app, a tenant can neither reach another's
// files nor aim the reaper at them, and a file cannot be moved off its bytes.
func TestAttachmentRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "files-wall-a")
	b := h.makeMember(t, "files-wall-b")
	ownerA := api.as(t, a.user, a.org, h.slugOf(t, a.org))
	ownerB := api.as(t, b.user, b.org, h.slugOf(t, b.org))
	homeA := newTree(t, ownerA, "WALL", "A").homeID
	newTree(t, ownerB, "WALL", "B")
	fileA := obj(t, want(t, ownerA.upload(t, "/api/v1/pages/"+homeA+"/attachments", "a.txt", []byte("a")), http.StatusCreated, "upload in A"), "attachment")["id"].(string)
	keyA := objectKey(a.org, homeA, fileA)

	conn := appConn(t)
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if got := count(`SELECT count(*) FROM attachment`); got != 0 {
		t.Errorf("an unscoped connection sees %d files", got)
	}

	actAs(t, conn, b.org)
	if got := count(`SELECT count(*) FROM attachment WHERE id = $1`, fileA); got != 0 {
		t.Error("B sees A's file")
	}
	untouched(t, conn, "deleting A's file", `DELETE FROM attachment WHERE id = $1`, fileA)
	refused(t, conn, "a file in A", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'x', 1)`, a.org, homeA)
	refused(t, conn, "B's file on A's page", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'x', 1)`, b.org, homeA)
	refused(t, conn, "a tombstone naming A's object", `INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, $2)`, keyA, b.org)
	refused(t, conn, "a tombstone in A", `INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, $2)`, keyA, a.org)

	actAs(t, conn, a.org)
	refused(t, conn, "moving a file off its bytes", `UPDATE attachment SET page_id = $2 WHERE id = $1`, fileA, homeA)
	refused(t, conn, "a key of one's choosing", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, object_key) VALUES ($1, $2, 'x', 1, 'org/elsewhere')`, a.org, homeA)
	refused(t, conn, "an empty file", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'x', 0)`, a.org, homeA)

	if inBucket(t, api.store, keyA) == nil || h.tombstones(t, keyA) != 0 {
		t.Error("A's file was touched")
	}
}
