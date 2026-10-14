//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The organization's hub (#40): a page its administrators choose, which
// everybody who may read it finds, and may land on.

func TestTheHubIsChosenByAdministratorsAndReadByWhoeverMayReadIt(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hub")
	admin := api.as(t, home.user)
	member := api.as(t, h.addPerson(t, "member"))

	news := newTree(t, admin, "NEWS", "News")
	front := news.add(news.homeID, "Front page")

	t.Run("there is none to begin with", func(t *testing.T) {
		got := obj(t, want(t, member.get(t, "/api/v1/site/hub"), http.StatusOK, "read the hub"), "hub")
		if got["page"] != nil || got["landing"] != false {
			t.Fatalf("a new organization has the hub %v", got)
		}
	})

	t.Run("an administrator chooses it, and everybody lands on it", func(t *testing.T) {
		want(t, admin.put(t, "/api/v1/site/hub", map[string]any{"pageId": front, "landing": true}), http.StatusOK, "choose the hub")
		got := obj(t, want(t, member.get(t, "/api/v1/site/hub"), http.StatusOK, "the member reads it"), "hub")
		page, _ := got["page"].(map[string]any)
		if page["id"] != front || page["title"] != "Front page" || page["spaceKey"] != "NEWS" || got["landing"] != true {
			t.Fatalf("the member reads the hub %v", got)
		}
	})

	t.Run("a member does not choose it", func(t *testing.T) {
		got := member.put(t, "/api/v1/site/hub", map[string]any{"pageId": nil, "landing": false})
		if got.Status != http.StatusForbidden {
			t.Fatalf("a member chose the hub: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("landing needs a hub, and the hub a page that is there", func(t *testing.T) {
		gone := news.add(news.homeID, "Gone")
		want(t, admin.delete(t, pagePath(gone)), http.StatusNoContent, "trash a page")
		for what, body := range map[string]map[string]any{
			"landing on nothing": {"pageId": nil, "landing": true},
			"a trashed page":     {"pageId": gone, "landing": false},
			"no such page":       {"pageId": "0195f000-0000-7000-8000-000000000999", "landing": false},
		} {
			if got := admin.put(t, "/api/v1/site/hub", body); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("whoever may not read the page has no hub, and lands nowhere", func(t *testing.T) {
		want(t, restrict(t, admin, front, []any{user(home.user)}, nil), http.StatusOK, "restrict the front page")
		got := obj(t, want(t, member.get(t, "/api/v1/site/hub"), http.StatusOK, "the member reads it"), "hub")
		if got["page"] != nil || got["landing"] != false {
			t.Fatalf("a member who may not read the page has the hub %v", got)
		}
		want(t, restrict(t, admin, front, nil, nil), http.StatusOK, "lift the restriction")
	})

	t.Run("the choice is in the audit log", func(t *testing.T) {
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'org.hub_set'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("the audit log holds %d hub choices", n)
		}
	})

	t.Run("an administrator clears it", func(t *testing.T) {
		got := obj(t, want(t, admin.put(t, "/api/v1/site/hub", map[string]any{"pageId": nil, "landing": false}), http.StatusOK, "clear the hub"), "hub")
		if got["page"] != nil || got["landing"] != false {
			t.Fatalf("the cleared hub reads %v", got)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, only
// an administrator chooses the hub, only one of the organization's pages, and
// a page deleted for good stops being it.
func TestTheHubIsHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hub-db")
	admin := api.as(t, home.user)
	memberID := h.addPerson(t, "member")

	news := newTree(t, admin, "NEWS", "News")
	front := news.add(news.homeID, "Front page")

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, memberID)
	refused(t, conn, "a member choosing the hub", `UPDATE site SET hub_page_id = $1`, front)

	actAs(t, conn, home.user)
	refused(t, conn, "a page that is not there as the hub", `UPDATE site SET hub_page_id = $1`, uuid.New())
	refused(t, conn, "landing on no hub", `UPDATE site SET hub_landing = true`)
	if _, err := conn.Exec(ctx, `UPDATE site SET hub_page_id = $1, hub_landing = true`, front); err != nil {
		t.Fatalf("an administrator could not choose the hub: %v", err)
	}

	if _, err := h.super.Exec(ctx, `DELETE FROM page WHERE id = $1`, front); err != nil {
		t.Fatalf("delete the hub page for good: %v", err)
	}
	var (
		hub     *string
		landing bool
	)
	if err := h.super.QueryRow(ctx, `SELECT hub_page_id::text, hub_landing FROM site`).Scan(&hub, &landing); err != nil {
		t.Fatal(err)
	}
	if hub != nil || landing {
		t.Errorf("after its page went, the hub is %v and landing %v", hub, landing)
	}
}
