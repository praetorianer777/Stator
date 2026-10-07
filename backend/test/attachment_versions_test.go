//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
)

// Versions of a file (#58): an upload under a name the page already has is
// that file's next version, numbered by the database.

func filesOf(t *testing.T, c *client, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, want(t, c.get(t, path), http.StatusOK, "list "+path), "attachments") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func TestAFileUploadedAgainIsItsNextVersion(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "file-versions")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	docs := newTree(t, owner, "FVER", "Versions")
	pageID := docs.add(docs.homeID, "Plan")
	other := docs.add(docs.homeID, "Other")
	files := "/api/v1/pages/" + pageID + "/attachments"

	upload := func(page, name, body string) map[string]any {
		t.Helper()
		return obj(t, want(t, owner.upload(t, "/api/v1/pages/"+page+"/attachments", name, []byte(body)), http.StatusCreated, "upload "+name), "attachment")
	}
	first := upload(pageID, "budget.xlsx", "one")
	second := upload(pageID, "Budget.XLSX", "two")
	notes := upload(pageID, "notes.txt", "notes")
	elsewhere := upload(other, "budget.xlsx", "elsewhere")
	third := upload(pageID, "budget.xlsx", "three")

	t.Run("each upload of a name counts on, whatever its case, on its own page", func(t *testing.T) {
		for _, c := range []struct {
			what    string
			file    map[string]any
			version float64
			count   float64
		}{
			{"the first budget", first, 1, 1},
			{"the second, in capitals", second, 2, 2},
			{"other notes", notes, 1, 1},
			{"a budget on another page", elsewhere, 1, 1},
			{"the third budget", third, 3, 3},
		} {
			if c.file["version"] != c.version || c.file["versions"] != c.count {
				t.Errorf("%s reads version %v of %v", c.what, c.file["version"], c.file["versions"])
			}
		}
		all := filesOf(t, owner, files)
		if len(all) != 4 || all[0]["id"] != third["id"] || all[0]["versions"] != float64(3) || all[3]["id"] != first["id"] || all[3]["versions"] != float64(3) {
			t.Errorf("every version reads %v", all)
		}
		current := filesOf(t, owner, files+"?current=true")
		if len(current) != 2 || current[0]["id"] != third["id"] || current[1]["id"] != notes["id"] {
			t.Errorf("the latest of each name reads %v", current)
		}
		want(t, owner.get(t, files+"?current=maybe"), http.StatusUnprocessableEntity, "current that is no yes or no")
	})

	t.Run("an older version keeps its own bytes", func(t *testing.T) {
		resp, data := owner.download(t, "/api/v1/attachments/"+first["id"].(string))
		if resp.StatusCode != http.StatusOK || string(data) != "one" {
			t.Errorf("the first version downloads %d %q", resp.StatusCode, data)
		}
	})

	t.Run("deleting a version leaves the others their numbers", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/attachments/"+second["id"].(string)), http.StatusNoContent, "delete the second budget")
		current := filesOf(t, owner, files+"?current=true")
		if current[0]["id"] != third["id"] || current[0]["version"] != float64(3) || current[0]["versions"] != float64(2) {
			t.Errorf("after a delete the budget reads %v", current[0])
		}
		if fourth := upload(pageID, "budget.xlsx", "four"); fourth["version"] != float64(4) {
			t.Errorf("the next upload is version %v", fourth["version"])
		}
	})

	// A copy is a new page, so its versions count from 1 again, in the order uploaded.
	t.Run("a copy of the page keeps the versions in order", func(t *testing.T) {
		r := want(t, owner.post(t, "/api/v1/pages/"+pageID+"/copy", map[string]any{"parentId": docs.homeID, "title": "Plan copy"}), http.StatusCreated, "copy the page")
		copied := obj(t, r, "page")["id"].(string)
		current := filesOf(t, owner, "/api/v1/pages/"+copied+"/attachments?current=true")
		var budget map[string]any
		for _, f := range current {
			if f["fileName"] == "budget.xlsx" {
				budget = f
			}
		}
		if budget == nil || budget["version"] != float64(3) || budget["versions"] != float64(3) {
			t.Errorf("the copy's budget reads %v of %v", budget, current)
		}
	})

	t.Run("straight through SQL the number is the database's", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, home.user)
		var version int
		if err := conn.QueryRow(context.Background(),
			`INSERT INTO attachment (org_id, page_id, file_name, size_bytes, version) VALUES ($1, $2, 'BUDGET.xlsx', 1, 99) RETURNING version`,
			home.org, pageID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 5 {
			t.Errorf("a row claiming version 99 got %d", version)
		}
		refused(t, conn, "renumbering a version", `UPDATE attachment SET version = 1 WHERE page_id = $1`, pageID)
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1 AND lower(file_name) = 'budget.xlsx' AND version = 1`, pageID); n != 1 {
			t.Errorf("%d rows are the budget's first version", n)
		}
	})
}

// Restoring a version (#95) uploads its bytes again as the name's next
// version: nothing is moved or overwritten, and the history only grows.
func TestAnEarlierVersionIsRestoredAsTheNextOne(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "file-restore")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	readerID := h.addPerson(t, home.org, "member")
	reader := api.as(t, readerID, home.org, slug)
	docs := newTree(t, owner, "FRES", "Restore")
	want(t, owner.put(t, "/api/v1/spaces/FRES/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": user(home.user), "permissions": []any{"view", "addPages", "administer"}},
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
	}}), http.StatusOK, "everybody else reads FRES")
	pageID := docs.add(docs.homeID, "Plan")
	files := "/api/v1/pages/" + pageID + "/attachments"

	upload := func(name, body string) map[string]any {
		t.Helper()
		return obj(t, want(t, owner.upload(t, files, name, []byte(body)), http.StatusCreated, "upload "+name), "attachment")
	}
	restore := func(c *client, file map[string]any) response {
		t.Helper()
		return c.post(t, "/api/v1/attachments/"+file["id"].(string)+"/restore", nil)
	}
	first := upload("budget.csv", "q1")
	second := upload("Budget.csv", "q1,q2")
	h.settle(t)

	var restored map[string]any
	t.Run("an earlier version comes back as the next, with its bytes, by whoever restores it", func(t *testing.T) {
		restored = obj(t, want(t, restore(owner, first), http.StatusCreated, "restore the first budget"), "attachment")
		if restored["version"] != float64(3) || restored["versions"] != float64(3) || restored["restoredFrom"] != float64(1) || restored["fileName"] != "budget.csv" {
			t.Errorf("the restore reads %v", restored)
		}
		if second["restoredFrom"] != nil {
			t.Errorf("an upload reads as restored from %v", second["restoredFrom"])
		}
		if restored["id"] == first["id"] {
			t.Error("the restore is the first version's own row")
		}
		_, data := owner.download(t, "/api/v1/attachments/"+restored["id"].(string))
		if string(data) != "q1" {
			t.Errorf("the restored version holds %q", data)
		}
		current := filesOf(t, owner, files+"?current=true")
		if len(current) != 1 || current[0]["id"] != restored["id"] {
			t.Errorf("the latest reads %v", current)
		}
		for _, older := range []map[string]any{first, second} {
			if resp, _ := owner.download(t, "/api/v1/attachments/"+older["id"].(string)); resp.StatusCode != http.StatusOK {
				t.Errorf("version %v went: %d", older["version"], resp.StatusCode)
			}
		}
	})

	t.Run("the latest version is not restored", func(t *testing.T) {
		r := want(t, restore(owner, restored), http.StatusConflict, "restore the latest")
		if code := errorCode(t, r); code != "already_latest" {
			t.Errorf("refused with %s", code)
		}
	})

	t.Run("who may only read the page may not restore", func(t *testing.T) {
		want(t, restore(reader, second), http.StatusForbidden, "a reader restores")
		want(t, reader.delete(t, "/api/v1/attachments/"+second["id"].(string)+"?versions=all"), http.StatusForbidden, "a reader deletes every version")
		want(t, owner.post(t, "/api/v1/attachments/00000000-0000-7000-8000-000000000000/restore", nil), http.StatusNotFound, "restore no file")
	})

	t.Run("straight through SQL a restore names an earlier version of the same file, by an editor", func(t *testing.T) {
		upload("notes.txt", "notes")
		conn := appConn(t)
		actAs(t, conn, home.org, readerID)
		denied(t, conn, "a reader restoring", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, restored_from) VALUES ($1, $2, 'budget.csv', 2, 1)`, home.org, pageID)
		actAs(t, conn, home.org, home.user)
		for what, sql := range map[string]string{
			"restoring the latest version":   `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, restored_from) VALUES ($1, $2, 'BUDGET.csv', 2, 3)`,
			"restoring a version never made": `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, restored_from) VALUES ($1, $2, 'budget.csv', 2, 9)`,
			"restoring another file":         `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, restored_from) VALUES ($1, $2, 'notes.txt', 2, 2)`,
		} {
			refused(t, conn, what, sql, home.org, pageID)
		}
		refused(t, conn, "restoring version 0", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes, restored_from) VALUES ($1, $2, 'budget.csv', 2, 0)`, home.org, pageID)
		refused(t, conn, "marking an upload restored", `UPDATE attachment SET restored_from = 1 WHERE page_id = $1`, pageID)
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1`, pageID); n != 4 {
			t.Errorf("the page holds %d files, want the 4 the service made", n)
		}
	})

	t.Run("a file is deleted a version at a time, or every version at once", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/attachments/"+first["id"].(string)+"?versions=some"), http.StatusUnprocessableEntity, "versions that is not all")
		want(t, owner.delete(t, "/api/v1/attachments/"+first["id"].(string)), http.StatusNoContent, "delete the first budget")
		if all := filesOf(t, owner, files); len(all) != 3 {
			t.Errorf("after one version went the page lists %v", all)
		}
		keys := []string{objectKey(home.org, pageID, second["id"].(string)), objectKey(home.org, pageID, restored["id"].(string))}
		want(t, owner.delete(t, "/api/v1/attachments/"+second["id"].(string)+"?versions=all"), http.StatusNoContent, "delete every budget")
		all := filesOf(t, owner, files)
		if len(all) != 1 || all[0]["fileName"] != "notes.txt" {
			t.Errorf("after every budget went the page lists %v", all)
		}
		for _, key := range keys {
			if got := inBucket(t, api.store, key); got != nil {
				t.Errorf("%s is still in the bucket", key)
			}
		}
	})
}
