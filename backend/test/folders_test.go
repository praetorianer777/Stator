//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Folders (#36): rows of the page tree that hold pages and folders and have no
// content of their own, seen at once by everybody who sees where they are.

func folder(tr *tree, parent, title string) string {
	tr.t.Helper()
	return tr.add(parent, title, map[string]any{"kind": "folder", "publish": false})
}

func TestFoldersHoldPagesAndNothingElse(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "folders")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "FOLD", "Folders")
	guides := folder(docs, docs.homeID, "Guides")
	setup := folder(docs, guides, "Setup")
	install := docs.add(setup, "Install")
	docs.add(guides, "Style")

	t.Run("a folder is seen at once, says what it is, and holds pages and folders", func(t *testing.T) {
		got := obj(t, want(t, member.get(t, pagePath(guides)), http.StatusOK, "a member opens the folder"), "page")
		if got["kind"] != "folder" || got["unpublished"] != false || got["version"].(float64) != 1 {
			t.Fatalf("the folder reads %v", got)
		}
		var kinds []string
		for _, n := range list(t, want(t, member.get(t, "/api/v1/spaces/FOLD/pages?parent="+guides), http.StatusOK, "list the folder"), "pages") {
			kinds = append(kinds, n.(map[string]any)["title"].(string)+":"+n.(map[string]any)["kind"].(string))
		}
		sameTitles(t, "under Guides", kinds, "Setup:folder", "Style:page")
		sameTitles(t, "under Setup", docs.titles(setup), "Install")
	})

	t.Run("renaming a folder changes its title alone", func(t *testing.T) {
		got := obj(t, want(t, owner.patch(t, pagePath(guides), map[string]any{"title": "How-tos", "version": 1}), http.StatusOK, "rename"), "page")
		if got["title"] != "How-tos" || got["version"].(float64) != 1 {
			t.Fatalf("the renamed folder reads %v", got)
		}
		if n := len(list(t, want(t, owner.get(t, pagePath(guides, "/versions")), http.StatusOK, "its history"), "versions")); n != 0 {
			t.Errorf("a folder has %d versions", n)
		}
	})

	t.Run("a folder moves with what it holds, and pages move into it", func(t *testing.T) {
		want(t, docs.move(setup, map[string]any{"parentId": docs.homeID}), http.StatusOK, "move Setup up")
		sameTitles(t, "under Setup", docs.titles(setup), "Install")
		other := docs.add(docs.homeID, "Loose page")
		want(t, docs.move(other, map[string]any{"parentId": setup}), http.StatusOK, "move a page into Setup")
		sameTitles(t, "under Setup", docs.titles(setup), "Install", "Loose page")
		if got := docs.move(setup, map[string]any{"parentId": install}); got.Status != http.StatusConflict {
			t.Errorf("a folder moved under its own page: %d", got.Status)
		}
	})

	t.Run("deleting a folder takes what it holds to the trash, and a restore brings it back", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(setup)), http.StatusNoContent, "trash Setup")
		if got := member.get(t, pagePath(install)); got.Status != http.StatusNotFound {
			t.Errorf("a page of a trashed folder still reads: %d", got.Status)
		}
		want(t, docs.restore(setup), http.StatusOK, "restore Setup")
		want(t, member.get(t, pagePath(install)), http.StatusOK, "the page is back")
	})

	t.Run("a copied folder stays a folder, and its pages stay published", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, pagePath(setup, "/copy"), map[string]any{"parentId": docs.homeID, "withChildren": true, "title": "Setup again"}), http.StatusCreated, "copy Setup"), "page")
		if made["kind"] != "folder" {
			t.Fatalf("the copy reads %v", made)
		}
		under := list(t, want(t, member.get(t, "/api/v1/spaces/FOLD/pages?parent="+made["id"].(string)), http.StatusOK, "list the copy"), "pages")
		if len(under) != 2 || under[0].(map[string]any)["unpublished"] != false {
			t.Errorf("the copy holds %v", under)
		}
	})

	t.Run("nothing that is a page's own is taken by a folder", func(t *testing.T) {
		for what, got := range map[string]response{
			"a body":            owner.patch(t, pagePath(guides), map[string]any{"body": textDoc("words"), "version": 1}),
			"a draft":           owner.put(t, pagePath(guides, "/draft"), map[string]any{"title": "x", "body": textDoc("x"), "baseVersion": 1}),
			"a comment":         owner.post(t, pagePath(guides, "/comments"), map[string]any{"body": commentDoc("x")}),
			"a label":           owner.post(t, pagePath(guides, "/labels"), map[string]any{"name": "howto"}),
			"a reaction":        owner.post(t, reactionsPath("pages", guides), map[string]any{"emoji": "👍"}),
			"a body on the way": owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Full", "kind": "folder", "body": textDoc("x")}),
		} {
			if got.Status != http.StatusConflict || errorCode(t, got) != "folder" {
				t.Errorf("a folder took %s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd", "kind": "box"}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("an unknown kind: %d", got.Status)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, a
// folder keeps its kind and its empty body, and takes no draft or reaction.
func TestFoldersAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "folders-db")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "FDB", "Folders")
	box := folder(docs, docs.homeID, "Box")
	inside := docs.add(box, "Inside")

	conn := appConn(t)
	actAs(t, conn, home.org, home.user)
	refused(t, conn, "turning a folder into a page", `UPDATE page SET kind = 'page' WHERE id = $1`, box)
	refused(t, conn, "turning a page into a folder", `UPDATE page SET kind = 'folder' WHERE id = $1`, inside)
	refused(t, conn, "writing in a folder", `UPDATE page SET body = '{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}' WHERE id = $1`, box)
	refused(t, conn, "a draft of a folder", `INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version)
		VALUES (current_org_id(), $1, $2, 'Box', '{"type":"doc"}', 1)`, box, home.user)
	refused(t, conn, "a reaction to a folder", `INSERT INTO reaction (org_id, page_id, user_id, emoji) VALUES (current_org_id(), $1, $2, '👍')`, box, home.user)
	refused(t, conn, "a version of a folder", `INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
		VALUES (current_org_id(), $1, 2, 'Box', '{"type":"doc"}', $2)`, box, home.user)
	refused(t, conn, "a folder for a home page", `INSERT INTO page (id, org_id, space_id, parent_id, rank, title, kind, created_by, updated_by)
		SELECT $1, org_id, space_id, NULL, 'a', 'Root', 'folder', $2, $2 FROM page WHERE id = $3`, uuid.New(), home.user, box)

	var published *string
	if err := conn.QueryRow(context.Background(), `SELECT published_at::text FROM page WHERE id = $1`, box).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != nil {
		t.Errorf("a folder was stamped published at %s, which would put it in the feeds", *published)
	}
}
