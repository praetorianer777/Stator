//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Page appearance (#50): an emoji before the title and in the tree, a width,
// and one of the page's own pictures as its cover with a focus point.

func TestAPagesAppearanceIsItsEditorsToChoose(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "appearance")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "LOOK", "Looks")
	plans := docs.add(docs.homeID, "Plans")
	other := docs.add(docs.homeID, "Other")
	upload := func(page, name string, data []byte) string {
		t.Helper()
		return obj(t, want(t, owner.upload(t, pagePath(page, "/attachments"), name, data), http.StatusCreated, "upload "+name), "attachment")["id"].(string)
	}
	picture := upload(plans, "harbour.png", pngOf(t, 16, 9))
	notes := upload(plans, "notes.txt", []byte("notes"))
	elsewhere := upload(other, "elsewhere.png", pngOf(t, 4, 4))

	set := func(c *client, body map[string]any) response {
		t.Helper()
		return c.put(t, pagePath(plans, "/appearance"), body)
	}

	t.Run("an editor chooses an emoji, a width and a cover, which the page and the tree show", func(t *testing.T) {
		got := obj(t, want(t, set(owner, map[string]any{"icon": "🚀", "width": "full", "cover": map[string]any{"attachmentId": picture, "focusX": 30, "focusY": 70}}), http.StatusOK, "choose"), "appearance")
		if got["icon"] != "🚀" || got["width"] != "full" {
			t.Fatalf("the appearance: %v", got)
		}
		look := obj(t, member.get(t, pagePath(plans)), "page")["appearance"].(map[string]any)
		cover, _ := look["cover"].(map[string]any)
		if look["icon"] != "🚀" || look["width"] != "full" || cover["attachmentId"] != picture || cover["focusX"] != float64(30) || cover["focusY"] != float64(70) {
			t.Errorf("the member reads %v", look)
		}
		var icon any
		for _, n := range list(t, want(t, member.get(t, "/api/v1/spaces/LOOK/pages"), http.StatusOK, "the tree"), "pages") {
			if m := n.(map[string]any); m["id"] == plans {
				icon = m["icon"]
			}
		}
		if icon != "🚀" {
			t.Errorf("the tree shows %v", icon)
		}
	})

	t.Run("words, a file of another page, a file that is no picture and a focus outside it are refused", func(t *testing.T) {
		for what, body := range map[string]map[string]any{
			"words":          {"icon": "Plans", "width": "fixed", "cover": nil},
			"an odd width":   {"icon": nil, "width": "huge", "cover": nil},
			"another page's": {"icon": nil, "width": "fixed", "cover": map[string]any{"attachmentId": elsewhere, "focusX": 50, "focusY": 50}},
			"no picture":     {"icon": nil, "width": "fixed", "cover": map[string]any{"attachmentId": notes, "focusX": 50, "focusY": 50}},
			"outside":        {"icon": nil, "width": "fixed", "cover": map[string]any{"attachmentId": picture, "focusX": 101, "focusY": 50}},
		} {
			if got := set(owner, body); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a reader who may not edit the page does not change how it looks", func(t *testing.T) {
		want(t, restrict(t, owner, plans, nil, []any{user(home.user)}), http.StatusOK, "keep editing to the owner")
		if got := set(member, map[string]any{"icon": nil, "width": "fixed", "cover": nil}); got.Status != http.StatusForbidden {
			t.Errorf("a reader changed it: %d %s", got.Status, got.Raw)
		}
		want(t, restrict(t, owner, plans, nil, nil), http.StatusOK, "lift the restriction")
	})

	t.Run("an emoji and a cover are taken away with null", func(t *testing.T) {
		got := obj(t, want(t, set(owner, map[string]any{"icon": nil, "width": "fixed", "cover": nil}), http.StatusOK, "clear"), "appearance")
		if got["icon"] != nil || got["cover"] != nil || got["width"] != "fixed" {
			t.Errorf("cleared: %v", got)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, only
// an editor changes how a page looks, only to a picture of its own, and a
// cover goes with its file.
func TestAPagesAppearanceIsHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "appearance-db")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")

	docs := newTree(t, owner, "LOOKDB", "Looks")
	plans := docs.add(docs.homeID, "Plans")
	other := docs.add(docs.homeID, "Other")
	picture := obj(t, want(t, owner.upload(t, pagePath(plans, "/attachments"), "p.png", pngOf(t, 4, 4)), http.StatusCreated, "upload"), "attachment")["id"].(string)
	elsewhere := obj(t, want(t, owner.upload(t, pagePath(other, "/attachments"), "o.png", pngOf(t, 4, 4)), http.StatusCreated, "upload"), "attachment")["id"].(string)
	want(t, restrict(t, owner, plans, nil, []any{user(home.user)}), http.StatusOK, "keep editing to the owner")

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a reader setting the emoji", `UPDATE page SET icon = '🚀' WHERE id = $1`, plans)
	refused(t, conn, "a reader setting the cover", `UPDATE page SET cover_attachment_id = $2 WHERE id = $1`, plans, picture)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "another page's file as the cover", `UPDATE page SET cover_attachment_id = $2 WHERE id = $1`, plans, elsewhere)
	refused(t, conn, "words as the emoji", `UPDATE page SET icon = 'two words' WHERE id = $1`, plans)
	refused(t, conn, "an odd width", `UPDATE page SET width = 'huge' WHERE id = $1`, plans)
	refused(t, conn, "a focus outside the picture", `UPDATE page SET cover_focus_x = 120 WHERE id = $1`, plans)
	if _, err := conn.Exec(ctx, `UPDATE page SET icon = '🚀', cover_attachment_id = $2 WHERE id = $1`, plans, picture); err != nil {
		t.Fatalf("an editor could not set the cover: %v", err)
	}

	want(t, owner.delete(t, fmt.Sprintf("/api/v1/attachments/%s", picture)), http.StatusNoContent, "delete the cover's file")
	var cover *string
	if err := h.super.QueryRow(ctx, `SELECT cover_attachment_id::text FROM page WHERE id = $1`, plans).Scan(&cover); err != nil {
		t.Fatal(err)
	}
	if cover != nil {
		t.Errorf("after its file went, the cover is %v", *cover)
	}
}
