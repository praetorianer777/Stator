//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The service refusing is not proof (#530): straight through SQL as
// stator_app, a plain member changes no role, removes nobody, writes no group
// or membership and names no default theme, and an administrator still does.
func TestRolesGroupsAndTheSiteAreHeldToAdministratorsByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := h.makeMember(t, "admin-guards")
	adminID := h.addPerson(t, "admin")
	memberID := h.addPerson(t, "member")
	otherID := h.addPerson(t, "member")
	ctx := context.Background()

	var groupID, outsiderID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (name) VALUES ('Keepers') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, groupID, otherID); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, 'Outsider') RETURNING id`,
		"outsider-"+uuid.NewString()[:8]+"@example.test").Scan(&outsiderID); err != nil {
		t.Fatal(err)
	}
	shared := obj(t, want(t, api.as(t, memberID).post(t, "/api/v1/themes", map[string]any{"name": "Theirs", "shared": true}),
		http.StatusCreated, "a member's shared theme"), "theme")["id"].(string)
	h.settle(t)

	conn := appConn(t)
	t.Run("a plain member is refused", func(t *testing.T) {
		actAs(t, conn, memberID)
		untouched(t, conn, "a member making themselves an administrator", `UPDATE member SET role = 'admin' WHERE user_id = $1`, memberID)
		untouched(t, conn, "a member demoting an administrator", `UPDATE member SET role = 'member' WHERE user_id = $1`, adminID)
		untouched(t, conn, "a member handing a role to the provider", `UPDATE member SET role_source = 'oidc' WHERE user_id = $1`, otherID)
		untouched(t, conn, "a member removing somebody", `DELETE FROM member WHERE user_id = $1`, otherID)
		denied(t, conn, "a member letting somebody in", `INSERT INTO member (user_id, role) VALUES ($1, 'admin')`, outsiderID)
		denied(t, conn, "a member making a group", `INSERT INTO groups (name) VALUES ('Planted')`)
		untouched(t, conn, "a member renaming a group", `UPDATE groups SET name = 'Taken' WHERE id = $1`, groupID)
		untouched(t, conn, "a member deleting a group", `DELETE FROM groups WHERE id = $1`, groupID)
		denied(t, conn, "a member joining a group", `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, groupID, memberID)
		untouched(t, conn, "a member taking somebody out of a group", `DELETE FROM group_member WHERE group_id = $1`, groupID)
		untouched(t, conn, "a member moving somebody's membership", `UPDATE group_member SET user_id = $2 WHERE group_id = $1`, groupID, memberID)
		denied(t, conn, "a member naming the default theme", `UPDATE site SET default_theme_id = $1`, shared)
		denied(t, conn, "a member renaming the site", `UPDATE site SET name = 'Taken'`)

		var role string
		if err := h.super.QueryRow(ctx, `SELECT role FROM member WHERE user_id = $1`, memberID).Scan(&role); err != nil || role != "member" {
			t.Errorf("the member's role is %q (%v)", role, err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM group_member WHERE group_id = $1 AND user_id = $2`, groupID, otherID); n != 1 {
			t.Errorf("the group holds %d of its one member", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM site WHERE default_theme_id IS NOT NULL OR name = 'Taken'`); n != 0 {
			t.Error("a member changed the site")
		}
	})

	t.Run("an administrator still changes them", func(t *testing.T) {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, adminID.String()); err != nil {
			t.Fatal(err)
		}
		for _, try := range []struct {
			what string
			sql  string
			args []any
		}{
			{"promoting the member", `UPDATE member SET role = 'admin' WHERE user_id = $1`, []any{memberID}},
			{"letting the outsider in", `INSERT INTO member (user_id, role) VALUES ($1, 'member')`, []any{outsiderID}},
			{"renaming the group", `UPDATE groups SET name = 'Wardens' WHERE id = $1`, []any{groupID}},
			{"putting the member in it", `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, []any{groupID, memberID}},
			{"taking the other out of it", `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, []any{groupID, otherID}},
			{"removing the other", `DELETE FROM member WHERE user_id = $1`, []any{otherID}},
			{"naming the default theme", `UPDATE site SET default_theme_id = $1`, []any{shared}},
			{"deleting the group", `DELETE FROM groups WHERE id = $1`, []any{groupID}},
		} {
			tag, err := tx.Exec(ctx, try.sql, try.args...)
			if err != nil || tag.RowsAffected() != 1 {
				t.Errorf("an administrator %s reached %d rows (%v)", try.what, tag.RowsAffected(), err)
			}
		}
	})

	t.Run("a member who unshares the default theme still takes it off", func(t *testing.T) {
		want(t, api.as(t, owner.user).put(t, "/api/v1/themes/default", map[string]any{"themeId": shared}), http.StatusOK, "make it the default")
		want(t, api.as(t, memberID).patch(t, "/api/v1/themes/"+shared, map[string]any{"shared": false}), http.StatusOK, "unshare")
		if n := h.countRows(t, `SELECT count(*) FROM site WHERE default_theme_id IS NOT NULL`); n != 0 {
			t.Error("an unshared theme stayed the default")
		}
	})
}
