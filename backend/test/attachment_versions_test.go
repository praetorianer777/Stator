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
