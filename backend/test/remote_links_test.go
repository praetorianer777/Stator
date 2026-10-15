//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func putRemoteLink(t *testing.T, c *client, page string, body map[string]any) response {
	t.Helper()
	return c.post(t, pagePath(page, "/remote-links"), body)
}

// requirementLink is what Exciter puts on a page for a requirement, as its
// contract (docs/api-contract-m4.md in Exciter) spells it.
func requirementLink(n string) map[string]any {
	return map[string]any{
		"url":     "https://exciter.example/r/" + n,
		"title":   "REQ-" + n + " Sign in with a passkey (specifies)",
		"source":  "Exciter",
		"iconUrl": "https://exciter.example/favicon-32.png",
	}
}

func remoteLinksOf(t *testing.T, r response) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, r, "remoteLinks") {
		out = append(out, each.(map[string]any))
	}
	return out
}

// Links go on and come off a page as Armature's issues take them: a second
// put of an address replaces the first, reading needs view of the page and
// changing needs edit, and the link stays with its page in the trash.
func TestRemoteLinksOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "remote-links")
	owner := api.as(t, home.user)
	annID, benID := h.addPerson(t, "member"), h.addPerson(t, "member")
	ann, ben := api.as(t, annID), api.as(t, benID)

	docs := newTree(t, owner, "RLNK", "Requirements")
	spec := docs.add(docs.homeID, "Sign-in specification")
	locked := docs.add(docs.homeID, "Locked specification")
	want(t, restrict(t, owner, locked, nil, []any{user(annID)}), http.StatusOK, "only ann edits the locked page")
	h.settle(t)

	var first map[string]any
	t.Run("a link goes on, says who put it, and a second put of its url replaces it", func(t *testing.T) {
		first = obj(t, want(t, putRemoteLink(t, ann, spec, requirementLink("1")), http.StatusCreated, "ann links REQ-1"), "remoteLink")
		if first["url"] != "https://exciter.example/r/1" || first["source"] != "Exciter" || first["iconUrl"] != "https://exciter.example/favicon-32.png" ||
			first["createdBy"] != annID.String() || first["createdByName"] == "" {
			t.Fatalf("the new link reads %v", first)
		}
		again := requirementLink("1")
		again["title"] = "REQ-1 Sign in with a passkey (describes)"
		delete(again, "iconUrl")
		replaced := obj(t, want(t, putRemoteLink(t, ben, spec, again), http.StatusOK, "ben puts the same url"), "remoteLink")
		if replaced["id"] != first["id"] || replaced["title"] != again["title"] || replaced["iconUrl"] != nil || replaced["createdBy"] != annID.String() {
			t.Fatalf("the replaced link reads %v, want %s retitled, without an icon and still ann's", replaced, first["id"])
		}
		want(t, putRemoteLink(t, ben, spec, map[string]any{"url": "https://armature.example/issues/CP-1", "title": "CP-1", "source": "Armature"}), http.StatusCreated, "a link from another tool")
		links := remoteLinksOf(t, want(t, ben.get(t, pagePath(spec, "/remote-links")), http.StatusOK, "ben lists the links"))
		if len(links) != 2 || links[0]["source"] != "Armature" || links[1]["id"] != first["id"] {
			t.Fatalf("the page lists %v, want the Armature link and then REQ-1, by source", links)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_remote_link WHERE page_id = $1`, spec); n != 2 {
			t.Errorf("the page holds %d rows, want 2", n)
		}
	})

	t.Run("what cannot be a link is refused in a sentence on its field", func(t *testing.T) {
		for field, body := range map[string]map[string]any{
			"url":     {"url": "javascript:alert(1)", "title": "x", "source": "Exciter"},
			"title":   {"url": "https://exciter.example/r/9", "title": strings.Repeat("t", 256), "source": "Exciter"},
			"source":  {"url": "https://exciter.example/r/9", "title": "x", "source": " "},
			"iconUrl": {"url": "https://exciter.example/r/9", "title": "x", "source": "Exciter", "iconUrl": "ftp://exciter.example/i.png"},
		} {
			r := want(t, putRemoteLink(t, ann, spec, body), http.StatusUnprocessableEntity, "a bad "+field)
			fields, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any)
			if msg, _ := fields[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("a bad %s is refused with %s", field, r.Raw)
			}
		}
		want(t, ann.delete(t, pagePath(spec, "/remote-links/not-an-id")), http.StatusBadRequest, "a link id that is none")
	})

	t.Run("a reader lists the links and changes none; who may not view the page learns nothing", func(t *testing.T) {
		mine := obj(t, want(t, putRemoteLink(t, ann, locked, requirementLink("2")), http.StatusCreated, "ann links the locked page"), "remoteLink")
		want(t, ben.get(t, pagePath(locked, "/remote-links")), http.StatusOK, "ben reads the locked page's links")
		if code := errorCode(t, want(t, putRemoteLink(t, ben, locked, requirementLink("3")), http.StatusForbidden, "ben links the locked page")); code != "forbidden" {
			t.Errorf("ben's put is refused with %s", code)
		}
		want(t, ben.delete(t, pagePath(locked, "/remote-links/"+mine["id"].(string))), http.StatusForbidden, "ben takes ann's link off")
		want(t, ben.get(t, pagePath(uuid.NewString(), "/remote-links")), http.StatusNotFound, "the links of no page")
		want(t, ann.delete(t, pagePath(spec, "/remote-links/"+mine["id"].(string))), http.StatusNotFound, "a link through another page")
		want(t, restrict(t, owner, locked, []any{user(annID)}, []any{user(annID)}), http.StatusOK, "only ann views the locked page")
		h.settle(t)
		want(t, ben.get(t, pagePath(locked, "/remote-links")), http.StatusNotFound, "ben reads a page he may not view")
		want(t, putRemoteLink(t, ben, locked, requirementLink("3")), http.StatusNotFound, "ben links a page he may not view")
	})

	t.Run("a link comes off once", func(t *testing.T) {
		path := pagePath(spec, "/remote-links/"+first["id"].(string))
		want(t, ben.delete(t, path), http.StatusNoContent, "ben takes REQ-1 off")
		want(t, ben.delete(t, path), http.StatusNotFound, "taking it off again")
		for _, l := range remoteLinksOf(t, want(t, ann.get(t, pagePath(spec, "/remote-links")), http.StatusOK, "the links")) {
			if l["id"] == first["id"] {
				t.Errorf("REQ-1 is still on the page")
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = 'page.remote_link.removed' AND payload->>'linkId' = $1 AND payload->>'url' = 'https://exciter.example/r/1'`, first["id"]); n != 1 {
			t.Errorf("%d events tell of REQ-1 taken off, want 1", n)
		}
	})

	t.Run("a link stays with its page in the trash and goes when it is purged", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(spec)), http.StatusNoContent, "trash the specification")
		want(t, ann.get(t, pagePath(spec, "/remote-links")), http.StatusNotFound, "the links of a trashed page")
		want(t, putRemoteLink(t, ann, spec, requirementLink("4")), http.StatusNotFound, "a link on a trashed page")
		want(t, owner.post(t, "/api/v1/spaces/RLNK/trash/"+spec+"/restore", nil), http.StatusOK, "restore it")
		if got := remoteLinksOf(t, want(t, ann.get(t, pagePath(spec, "/remote-links")), http.StatusOK, "the restored page's links")); len(got) != 1 || got[0]["source"] != "Armature" {
			t.Errorf("the restored page carries %v", got)
		}
		want(t, owner.delete(t, pagePath(spec)), http.StatusNoContent, "trash it again")
		want(t, owner.delete(t, "/api/v1/spaces/RLNK/trash/"+spec), http.StatusNoContent, "purge it")
		if n := h.countRows(t, `SELECT count(*) FROM page_remote_link WHERE page_id = $1`, spec); n != 0 {
			t.Errorf("%d links outlived their purged page", n)
		}
	})
}

// A tool such as Exciter keeps a page's links with a person's own token, as
// its worker does: put, put again, list and take off, nothing but the token.
func TestAToolKeepsRemoteLinksWithAPersonalToken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "remote-link-tool")
	owner := api.as(t, home.user)
	docs := newTree(t, owner, "RTOOL", "Tooling")
	spec := docs.add(docs.homeID, "Payment specification")

	if !strings.Contains(string(want(t, api.anonymous().get(t, "/api/v1/meta"), http.StatusOK, "meta").Raw), `"page-remote-links@1"`) {
		t.Fatal("/meta does not offer page-remote-links@1")
	}
	_, secret := makeToken(t, owner, map[string]any{"name": "exciter"})
	exciter := api.withToken(secret)
	made := obj(t, want(t, putRemoteLink(t, exciter, spec, requirementLink("7")), http.StatusCreated, "the tool links REQ-7"), "remoteLink")
	want(t, putRemoteLink(t, exciter, spec, requirementLink("7")), http.StatusOK, "the tool syncs again")
	links := remoteLinksOf(t, want(t, exciter.get(t, pagePath(spec, "/remote-links")), http.StatusOK, "the tool lists"))
	if len(links) != 1 || links[0]["id"] != made["id"] || links[0]["createdBy"] != home.user.String() {
		t.Fatalf("after two syncs the page carries %v", links)
	}
	want(t, exciter.delete(t, pagePath(spec, "/remote-links/"+made["id"].(string))), http.StatusNoContent, "the tool takes it off")

	_, readSecret := makeToken(t, owner, map[string]any{"name": "reader", "scopes": []string{"read"}})
	reader := api.withToken(readSecret)
	want(t, reader.get(t, pagePath(spec, "/remote-links")), http.StatusOK, "a read token lists")
	if code := errorCode(t, want(t, putRemoteLink(t, reader, spec, requirementLink("8")), http.StatusForbidden, "a read token puts")); code != "read_only_token" {
		t.Errorf("a read token's put is refused with %s", code)
	}
}

// A link is its page's: straight through SQL as the app role, nobody reads one
// on a page they may not view, puts or takes one on a page they may not edit,
// puts one in another's name, or moves one to another page or address.
func TestRemoteLinksFollowTheirPageInTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "remote-link-wall")
	owner := api.as(t, home.user)
	annID, benID := h.addPerson(t, "member"), h.addPerson(t, "member")
	other := h.makeOutsider(t, "remote-link-wall-b")

	docs := newTree(t, owner, "RWALL", "Walled")
	h.closeSpace(t, docs.space, annID, benID)
	open := docs.add(docs.homeID, "Open specification")
	closed := docs.add(docs.homeID, "Closed specification")
	locked := docs.add(docs.homeID, "Locked specification")
	for _, id := range []string{open, closed, locked} {
		want(t, putRemoteLink(t, owner, id, requirementLink("1")), http.StatusCreated, "link a page")
	}
	want(t, restrict(t, owner, closed, []any{user(annID)}, nil), http.StatusOK, "only ann views the closed page")
	want(t, restrict(t, owner, locked, nil, []any{user(annID)}), http.StatusOK, "only ann edits the locked page")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	const insert = `INSERT INTO page_remote_link (page_id, url, title, source, created_by) VALUES ($1, $2, 'REQ', 'Exciter', $3)`

	t.Run("the database refuses what the service refuses", func(t *testing.T) {
		actAs(t, conn, benID)
		if n := count(`SELECT count(*) FROM page_remote_link WHERE page_id = $1`, closed); n != 0 {
			t.Errorf("ben reads %d links of a page he may not view", n)
		}
		if n := count(`SELECT count(*) FROM page_remote_link WHERE page_id = ANY ($1)`, []string{open, locked}); n != 2 {
			t.Errorf("ben reads %d links of the pages he may view, want 2", n)
		}
		denied(t, conn, "a link on a page he may not view", insert, closed, "https://exciter.example/r/2", benID)
		denied(t, conn, "a link on a page he may not edit", insert, locked, "https://exciter.example/r/2", benID)
		denied(t, conn, "a link in ann's name", insert, open, "https://exciter.example/r/2", annID)
		denied(t, conn, "a link to no web address", insert, open, "javascript:alert(1)", benID)
		denied(t, conn, "a link with a space in its address", insert, open, "https://exciter.example/r 2", benID)
		denied(t, conn, "a title past 255 characters", `INSERT INTO page_remote_link (page_id, url, title, source, created_by) VALUES ($1, 'https://exciter.example/r/3', repeat('t', 256), 'Exciter', $2)`, open, benID)
		denied(t, conn, "a blank source", `INSERT INTO page_remote_link (page_id, url, title, source, created_by) VALUES ($1, 'https://exciter.example/r/3', 'REQ', ' ', $2)`, open, benID)
		denied(t, conn, "an icon that is no web address", `INSERT INTO page_remote_link (page_id, url, title, source, icon_url, created_by) VALUES ($1, 'https://exciter.example/r/3', 'REQ', 'Exciter', 'data:x', $2)`, open, benID)
		refused(t, conn, "a second link at the same address", insert, open, "https://exciter.example/r/1", benID)
		untouched(t, conn, "retitling a link on a page he may not edit", `UPDATE page_remote_link SET title = 'mine' WHERE page_id = $1`, locked)
		untouched(t, conn, "removing a link on a page he may not edit", `DELETE FROM page_remote_link WHERE page_id = $1`, locked)
		untouched(t, conn, "removing a link on a page he may not view", `DELETE FROM page_remote_link WHERE page_id = $1`, closed)
		refused(t, conn, "moving a link to another address", `UPDATE page_remote_link SET url = 'https://elsewhere.example/' WHERE page_id = $1`, open)
		refused(t, conn, "moving a link to another page", `UPDATE page_remote_link SET page_id = $2 WHERE page_id = $1`, open, locked)
		refused(t, conn, "naming another as who put it", `UPDATE page_remote_link SET created_by = $2 WHERE page_id = $1`, open, benID)
		if _, err := conn.Exec(ctx, insert, open, "https://exciter.example/r/5", benID); err != nil {
			t.Errorf("ben may not link a page he edits: %v", err)
		}
		if _, err := conn.Exec(ctx, `UPDATE page_remote_link SET title = 'REQ-5 retitled', updated_at = now() WHERE page_id = $1 AND url = 'https://exciter.example/r/5'`, open); err != nil {
			t.Errorf("ben may not retitle a link on a page he edits: %v", err)
		}
		if _, err := conn.Exec(ctx, `DELETE FROM page_remote_link WHERE page_id = $1 AND url = 'https://exciter.example/r/5'`, open); err != nil {
			t.Errorf("ben may not take a link off a page he edits: %v", err)
		}
	})

	t.Run("a member the space is closed to reads and writes none of them", func(t *testing.T) {
		actAs(t, conn, other.user)
		if n := count(`SELECT count(*) FROM page_remote_link`); n != 0 {
			t.Errorf("a member the space is closed to reads %d links", n)
		}
		refused(t, conn, "a link on a page of the closed space", insert, open, "https://exciter.example/r/6", other.user)
		untouched(t, conn, "deleting the links of the closed space", `DELETE FROM page_remote_link`)
	})
}
