//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Personal spaces (#35): every member makes one of their own, which nobody
// but them sees until they share it, and the database holds to that too.

func TestAPersonalSpaceIsItsOwnersAloneUntilShared(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "personal")
	slug := h.slugOf(t, home.org)
	admin := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)

	made := want(t, ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANN", "name": "Ann's notes", "personal": true}), http.StatusCreated, "a member makes her personal space")
	sp := obj(t, made, "space")
	if owner := obj(t, made, "space", "owner"); owner["id"] != annID.String() || owner["name"] != "A member" {
		t.Fatalf("the space does not name its owner: %s", made.Raw)
	}
	if can := obj(t, made, "space", "can"); can["administer"] != true || can["editPages"] != true {
		t.Fatalf("the owner may not do everything in her space: %s", made.Raw)
	}

	t.Run("only its owner holds a grant", func(t *testing.T) {
		grants := list(t, want(t, ann.get(t, "/api/v1/spaces/ANN/permissions"), http.StatusOK, "the owner reads the table"), "grants")
		if len(grants) != 1 {
			t.Fatalf("a personal space grants %v", grants)
		}
		only := grants[0].(map[string]any)
		if subject := only["subject"].(map[string]any); subject["type"] != "user" || subject["id"] != annID.String() {
			t.Errorf("the one grant is %v", only)
		}
	})

	t.Run("another member neither lists nor opens it", func(t *testing.T) {
		for _, s := range list(t, want(t, bob.get(t, "/api/v1/spaces"), http.StatusOK, "bob lists"), "spaces") {
			if s.(map[string]any)["key"] == "ANN" {
				t.Fatalf("bob lists Ann's personal space")
			}
		}
		if got := bob.get(t, "/api/v1/spaces/ANN"); got.Status != http.StatusNotFound {
			t.Errorf("bob opened Ann's personal space: %d", got.Status)
		}
		if got := bob.get(t, "/api/v1/pages/"+sp["homePageId"].(string)); got.Status != http.StatusNotFound {
			t.Errorf("bob read the home page of Ann's personal space: %d", got.Status)
		}
	})

	t.Run("an administrator of the organization still reaches it, as every space", func(t *testing.T) {
		want(t, admin.get(t, "/api/v1/spaces/ANN"), http.StatusOK, "the admin opens it")
	})

	t.Run("one each, and a member still makes no other space", func(t *testing.T) {
		again := ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANN2", "name": "More notes", "personal": true})
		if again.Status != http.StatusConflict || errorCode(t, again) != "conflict" || !strings.Contains(string(again.Raw), "already, ANN.") {
			t.Errorf("a second personal space: %d %s", again.Status, again.Raw)
		}
		if got := ann.post(t, "/api/v1/spaces", map[string]any{"key": "TEAM", "name": "Team"}); got.Status != http.StatusForbidden {
			t.Errorf("a member made an ordinary space: %d", got.Status)
		}
		want(t, bob.post(t, "/api/v1/spaces", map[string]any{"key": "BOB", "name": "Bob's notes", "personal": true}), http.StatusCreated, "bob makes his own")
	})

	t.Run("shared like any space, it shows to whom it is shared with", func(t *testing.T) {
		want(t, ann.put(t, "/api/v1/spaces/ANN/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
			map[string]any{"subject": user(bobID), "permissions": []any{"view"}},
		}}), http.StatusOK, "ann shares her space with bob")
		got := obj(t, want(t, bob.get(t, "/api/v1/spaces/ANN"), http.StatusOK, "bob opens the shared space"), "space")
		if got["owner"].(map[string]any)["id"] != annID.String() || got["can"].(map[string]any)["editPages"] != false {
			t.Errorf("bob sees %v", got)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, a
// member makes only their own personal space, only one, and cannot hand it on.
func TestPersonalSpacesAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "personal-db")
	annID := h.addPerson(t, home.org, "member")
	bobID := h.addPerson(t, home.org, "member")
	conn := appConn(t)
	actAs(t, conn, home.org, annID)
	ctx := context.Background()

	insert := `INSERT INTO space (id, org_id, key, name, created_by, owner_id) VALUES ($1, current_org_id(), $2, 'Notes', $3, $4)`
	refused(t, conn, "an ordinary space by a member", insert, uuid.New(), "PLAIN", annID, nil)
	refused(t, conn, "a personal space for somebody else", insert, uuid.New(), "BOBS", annID, bobID)
	refused(t, conn, "a personal space made in somebody else's name", insert, uuid.New(), "BOBS", bobID, bobID)

	mine := uuid.New()
	if _, err := conn.Exec(ctx, insert, mine, "ANN", annID, annID); err != nil {
		t.Fatalf("a member's own personal space was refused: %v", err)
	}
	refused(t, conn, "a second personal space", insert, uuid.New(), "ANN2", annID, annID)

	var everyoneGrants int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM space_grant WHERE space_id = $1 AND subject_type = 'everyone'`, mine).Scan(&everyoneGrants); err != nil {
		t.Fatal(err)
	}
	if everyoneGrants != 0 {
		t.Errorf("a personal space grants everyone %d permissions", everyoneGrants)
	}
	refused(t, conn, "handing the space to somebody else", `UPDATE space SET owner_id = $2 WHERE id = $1`, mine, bobID)

	actAs(t, conn, home.org, bobID)
	untouched(t, conn, "reading somebody else's personal space", `UPDATE space SET name = 'Mine now' WHERE id = $1`, mine)

	t.Run("an owner's account going leaves an ordinary space nobody owns", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, annID); err != nil {
			t.Fatalf("delete the owner: %v", err)
		}
		var owner *uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT owner_id FROM space WHERE id = $1`, mine).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if owner != nil {
			t.Errorf("the space still names its owner %v", owner)
		}
	})
}
