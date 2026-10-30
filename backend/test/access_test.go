//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Roles, the matrix, groups and grants over the API (#585): what an
// administrator may change and the refusals around it, and a renamed and a
// new role reaching what the web app shows and what the API allows at once.
func TestRolesGroupsAndGrantsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "access")
	owner := api.as(t, home.user)
	annID, bobID := h.addPerson(t, "member"), h.addPerson(t, "member")
	ann, bob := api.as(t, annID), api.as(t, bobID)
	nobody := api.anonymous()
	docs := newTree(t, owner, "ROLE", "Roles")
	can := func(c *client) map[string]any {
		t.Helper()
		return obj(t, want(t, c.get(t, "/api/v1/spaces/ROLE"), http.StatusOK, "the space"), "space", "can")
	}

	t.Run("everybody reads the roles and the permissions they hold", func(t *testing.T) {
		roles := list(t, want(t, ann.get(t, "/api/v1/roles"), http.StatusOK, "the roles"), "roles")
		var keys []string
		for _, r := range roles {
			role := r.(map[string]any)
			keys = append(keys, role["role"].(string))
			if role["role"] == "administrator" && (role["builtin"] != true || role["orgWideOnly"] != true || len(role["permissions"].([]any)) != 8) {
				t.Errorf("the administrator is %v", role)
			}
		}
		if len(keys) < 5 || keys[0] != "administrator" || keys[1] != "space_administrator" || keys[2] != "editor" || keys[3] != "commenter" || keys[4] != "viewer" {
			t.Errorf("the roles are %v, the built-in ones first", keys)
		}
		perms := list(t, want(t, ann.get(t, "/api/v1/permissions"), http.StatusOK, "the permissions"), "permissions")
		if len(perms) != 8 || perms[0].(map[string]any)["permission"] != "view" || perms[7].(map[string]any)["scope"] != "installation" {
			t.Errorf("the permissions are %v", perms)
		}
		for _, path := range []string{"/api/v1/roles", "/api/v1/permissions", "/api/v1/role-assignments"} {
			if got := nobody.get(t, path); got.Status != http.StatusUnauthorized {
				t.Errorf("nobody reads %s: %d", path, got.Status)
			}
		}
	})

	t.Run("only an administrator adds, changes and deletes roles", func(t *testing.T) {
		if got := ann.post(t, "/api/v1/roles", map[string]any{"name": "Mine", "permissions": []string{"view"}}); got.Status != http.StatusForbidden {
			t.Errorf("a member added a role: %d", got.Status)
		}
		fieldError(t, want(t, owner.post(t, "/api/v1/roles", map[string]any{"permissions": []string{"view"}}), http.StatusUnprocessableEntity, "a role without a name"), "name")
		fieldError(t, want(t, owner.post(t, "/api/v1/roles", map[string]any{"name": "Pilot", "permissions": []string{"fly"}}), http.StatusUnprocessableEntity, "a permission nobody checks"), "permissions")
		if got := ann.patch(t, "/api/v1/roles/editor", map[string]any{"name": "Mine"}); got.Status != http.StatusForbidden {
			t.Errorf("a member renamed a role: %d", got.Status)
		}
		if got := owner.patch(t, "/api/v1/roles/nope", map[string]any{"name": "Nope"}); got.Status != http.StatusNotFound {
			t.Errorf("a role nobody defined: %d", got.Status)
		}
		if got := owner.patch(t, "/api/v1/roles/administrator", map[string]any{"permissions": []string{"view", "use"}}); got.Status != http.StatusConflict || errorCode(t, got) != "role_administrator_administers" {
			t.Errorf("the administrator stopped administering: %d %s", got.Status, got.Raw)
		}
		if got := owner.delete(t, "/api/v1/roles/viewer"); got.Status != http.StatusConflict {
			t.Errorf("a built-in role was deleted: %d", got.Status)
		}
		if got := ann.delete(t, "/api/v1/roles/space_creator"); got.Status != http.StatusForbidden {
			t.Errorf("a member deleted a role: %d", got.Status)
		}
	})

	t.Run("a renamed and a new role reach the buttons and the API at once", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/roles/editor", map[string]any{"name": "Author"}), http.StatusOK, "rename the editor")
		t.Cleanup(func() { owner.patch(t, "/api/v1/roles/editor", map[string]any{"name": "Editor"}) })
		if c := can(ann); c["editPages"] != true || c["deletePages"] != true {
			t.Errorf("Everyone, an author now, may %v", c)
		}

		made := obj(t, want(t, owner.post(t, "/api/v1/roles", map[string]any{"name": "Proofreader", "description": "Reads and comments.",
			"permissions": []string{"view", "addComments"}}), http.StatusCreated, "a new role"), "role")
		if made["role"] != "proofreader" || made["builtin"] != false || made["inUse"] != float64(0) {
			t.Errorf("the new role is %v", made)
		}
		fieldError(t, want(t, owner.post(t, "/api/v1/roles", map[string]any{"name": "Proofreader", "permissions": []string{"view"}}), http.StatusUnprocessableEntity, "the same role twice"), "key")
		h.closeSpace(t, docs.spaceID(t), home.user)
		want(t, owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "proofreader", "spaceKey": "ROLE", "userId": bobID}), http.StatusCreated, "bob proofreads")
		if c := can(bob); c["addComments"] != true || c["editPages"] != false {
			t.Errorf("a proofreader may %v", c)
		}
		if got := bob.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Not yet"}); got.Status != http.StatusForbidden {
			t.Errorf("a proofreader added a page: %d", got.Status)
		}

		ticked := obj(t, want(t, owner.patch(t, "/api/v1/roles/proofreader", map[string]any{"permissions": []string{"view", "addComments", "addPages"}}), http.StatusOK, "tick add pages"), "role")
		if ticked["inUse"] != float64(1) {
			t.Errorf("the role counts %v grants", ticked["inUse"])
		}
		if c := can(bob); c["editPages"] != true {
			t.Errorf("a proofreader who may add pages is offered %v", c)
		}
		want(t, bob.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Now", "publish": true}), http.StatusCreated, "the proofreader adds a page")

		want(t, owner.delete(t, "/api/v1/roles/proofreader"), http.StatusNoContent, "delete the role and its grants")
		if got := bob.get(t, "/api/v1/spaces/ROLE"); got.Status != http.StatusNotFound {
			t.Errorf("bob still reaches the space through a deleted role: %d", got.Status)
		}
	})

	t.Run("groups are kept by hand or filled by the provider, and Everyone by the installation", func(t *testing.T) {
		groups := list(t, want(t, ann.get(t, "/api/v1/groups"), http.StatusOK, "the groups"), "groups")
		everyone := groups[0].(map[string]any)
		if everyone["builtin"] != "everyone" || everyone["name"] != "Everyone" {
			t.Fatalf("the first group is %v", everyone)
		}
		everyoneID := everyone["id"].(string)
		if got := ann.post(t, "/api/v1/groups", map[string]any{"name": "Mine"}); got.Status != http.StatusForbidden {
			t.Errorf("a member made a group: %d", got.Status)
		}
		fieldError(t, want(t, owner.post(t, "/api/v1/groups", map[string]any{"name": " "}), http.StatusUnprocessableEntity, "a group without a name"), "name")
		group := obj(t, want(t, owner.post(t, "/api/v1/groups", map[string]any{"name": "Reviewers", "description": "Read first."}), http.StatusCreated, "a group"), "group")
		groupID := group["id"].(string)
		if group["source"] != "local" || group["memberCount"] != float64(0) {
			t.Errorf("the new group is %v", group)
		}
		fieldError(t, want(t, owner.post(t, "/api/v1/groups", map[string]any{"name": "reviewers"}), http.StatusUnprocessableEntity, "the same name"), "name")
		if got := ann.get(t, "/api/v1/groups/"+groupID); got.Status != http.StatusForbidden {
			t.Errorf("a member read a group's members: %d", got.Status)
		}
		if got := owner.get(t, "/api/v1/groups/"+uuid.NewString()); got.Status != http.StatusNotFound {
			t.Errorf("a group nobody made: %d", got.Status)
		}

		joined := obj(t, want(t, owner.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": annID}), http.StatusOK, "ann joins"), "group")
		if members := joined["members"].([]any); len(members) != 1 || members[0].(map[string]any)["id"] != annID.String() {
			t.Errorf("the group holds %v", joined["members"])
		}
		fieldError(t, want(t, owner.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": uuid.New()}), http.StatusUnprocessableEntity, "somebody not here"), "userId")
		if got := ann.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": bobID}); got.Status != http.StatusForbidden {
			t.Errorf("a member filled a group: %d", got.Status)
		}

		// A grant to the group reaches ann in a space closed to everybody else.
		want(t, owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "commenter", "spaceKey": "ROLE", "groupId": groupID}), http.StatusCreated, "Reviewers comment in ROLE")
		if c := can(ann); c["addComments"] != true || c["editPages"] != false {
			t.Errorf("a reviewer may %v", c)
		}
		read := obj(t, want(t, owner.get(t, "/api/v1/groups/"+groupID), http.StatusOK, "the group"), "group")
		if read["roleCount"] != float64(1) || read["memberCount"] != float64(1) {
			t.Errorf("the group reads %v", read)
		}

		if got := owner.patch(t, "/api/v1/groups/"+everyoneID, map[string]any{"name": "All"}); got.Status != http.StatusConflict {
			t.Errorf("Everyone was renamed: %d", got.Status)
		}
		if got := owner.post(t, "/api/v1/groups/"+everyoneID+"/members", map[string]any{"userId": annID}); got.Status != http.StatusConflict {
			t.Errorf("Everyone was filled by hand: %d", got.Status)
		}
		if got := owner.delete(t, "/api/v1/groups/"+everyoneID+"/members/"+annID.String()); got.Status != http.StatusConflict {
			t.Errorf("somebody was taken out of Everyone: %d", got.Status)
		}
		if got := owner.delete(t, "/api/v1/groups/"+everyoneID); got.Status != http.StatusConflict {
			t.Errorf("Everyone was deleted: %d", got.Status)
		}
		if got := ann.patch(t, "/api/v1/groups/"+groupID, map[string]any{"name": "Mine"}); got.Status != http.StatusForbidden {
			t.Errorf("a member renamed a group: %d", got.Status)
		}
		renamed := obj(t, want(t, owner.patch(t, "/api/v1/groups/"+groupID, map[string]any{"name": "Proofreaders"}), http.StatusOK, "rename the group"), "group")
		if renamed["name"] != "Proofreaders" || renamed["roleCount"] != float64(1) {
			t.Errorf("the renamed group is %v", renamed)
		}

		left := obj(t, want(t, owner.delete(t, "/api/v1/groups/"+groupID+"/members/"+annID.String()), http.StatusOK, "ann leaves"), "group")
		if left["memberCount"] != float64(0) {
			t.Errorf("the group still holds %v", left["members"])
		}
		fieldError(t, want(t, owner.delete(t, "/api/v1/groups/"+groupID+"/members/"+annID.String()), http.StatusUnprocessableEntity, "ann leaves twice"), "userId")
		if got := ann.get(t, "/api/v1/spaces/ROLE"); got.Status != http.StatusNotFound {
			t.Errorf("ann reaches ROLE after leaving the group: %d", got.Status)
		}

		provider := obj(t, want(t, owner.post(t, "/api/v1/groups", map[string]any{"name": "Staff", "externalRef": "staff"}), http.StatusCreated, "a group the provider fills"), "group")
		if provider["source"] != "oidc" || provider["externalRef"] != "staff" {
			t.Errorf("the provider's group is %v", provider)
		}
		if got := owner.post(t, "/api/v1/groups/"+provider["id"].(string)+"/members", map[string]any{"userId": annID}); got.Status != http.StatusConflict {
			t.Errorf("a group the provider fills was filled by hand: %d", got.Status)
		}

		if got := ann.delete(t, "/api/v1/groups/"+groupID); got.Status != http.StatusForbidden {
			t.Errorf("a member deleted a group: %d", got.Status)
		}
		want(t, owner.delete(t, "/api/v1/groups/"+groupID), http.StatusNoContent, "delete the group and its grants")
		if got := owner.delete(t, "/api/v1/groups/"+groupID); got.Status != http.StatusNotFound {
			t.Errorf("the group was deleted twice: %d", got.Status)
		}
	})

	t.Run("a grant across the installation reaches every space", func(t *testing.T) {
		if got := bob.get(t, "/api/v1/spaces/ROLE"); got.Status != http.StatusNotFound {
			t.Fatalf("bob reaches the closed space before: %d", got.Status)
		}
		granted := obj(t, want(t, owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "userId": bobID}), http.StatusCreated, "bob views every space"), "assignment")
		if granted["spaceId"] != nil || granted["roleName"] != "Viewer" {
			t.Errorf("the grant is %v", granted)
		}
		if c := can(bob); c["editPages"] != false {
			t.Errorf("a viewer of every space may %v", c)
		}
		if got := ann.delete(t, "/api/v1/role-assignments/"+granted["id"].(string)); got.Status != http.StatusForbidden {
			t.Errorf("a member took a grant across the installation away: %d", got.Status)
		}
		want(t, owner.delete(t, "/api/v1/role-assignments/"+granted["id"].(string)), http.StatusNoContent, "take it back")
		if got := owner.delete(t, "/api/v1/role-assignments/"+granted["id"].(string)); got.Status != http.StatusNotFound {
			t.Errorf("a grant taken twice: %d", got.Status)
		}
		if got := owner.get(t, "/api/v1/role-assignments?scope=everywhere"); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a scope nobody knows: %d", got.Status)
		}
		page := want(t, owner.get(t, "/api/v1/role-assignments?limit=1&q=Viewer"), http.StatusOK, "a page of grants")
		if page.Body["limit"] != float64(1) || len(list(t, page, "assignments")) > 1 {
			t.Errorf("a page of one is %s", page.Raw)
		}
	})

	t.Run("the last active administrator stays", func(t *testing.T) {
		mine := list(t, want(t, owner.get(t, "/api/v1/role-assignments?scope=installation&role=administrator"), http.StatusOK, "the administrators"), "assignments")
		ownGrant := ""
		for _, a := range mine {
			if a.(map[string]any)["userId"] == home.user.String() {
				ownGrant = a.(map[string]any)["id"].(string)
			}
		}
		if ownGrant == "" {
			t.Fatalf("the administrator's own grant is not listed: %v", mine)
		}
		var others int
		if err := h.super.QueryRow(context.Background(), `SELECT perm_active_administrators()`).Scan(&others); err != nil {
			t.Fatal(err)
		}
		if others == 1 {
			if got := owner.delete(t, "/api/v1/role-assignments/"+ownGrant); got.Status != http.StatusConflict || errorCode(t, got) != "installation_keeps_administrator" {
				t.Errorf("the last administrator's grant went: %d %s", got.Status, got.Raw)
			}
		}
	})
}

// asApp opens a transaction on the pool as stator_app acting for a person,
// which is how a request reaches the database.
func asApp(t *testing.T, pool *pgxpool.Pool, person uuid.UUID) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE stator_app`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, person.String()); err != nil {
		t.Fatal(err)
	}
	return tx
}

// refusedAt runs one statement as the person in a transaction of its own and
// wants the database to refuse it, at once or at commit, by the guard named.
func refusedAt(t *testing.T, pool *pgxpool.Pool, person uuid.UUID, guard, what, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	tx := asApp(t, pool, person)
	_, err := tx.Exec(ctx, sql, args...)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != guard {
		t.Errorf("%s: %v, want the database to refuse it by %s", what, err, guard)
	}
}

// ranAs runs statements as the person in one transaction and commits.
func ranAs(t *testing.T, pool *pgxpool.Pool, person uuid.UUID, what string, statements ...string) {
	t.Helper()
	ctx := context.Background()
	tx := asApp(t, pool, person)
	for _, sql := range statements {
		if _, err := tx.Exec(ctx, sql); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("%s: %s: %v", what, sql, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// The service refusing is not proof: straight through SQL as stator_app the
// database keeps an active administrator, keeps every space an administrator
// of its own, keeps the built-in roles and Everyone what they are, keeps a
// guest in their one space, and its row level security reads the grants.
func TestTheDatabaseKeepsTheGuardsOfAccess(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	scratch := scratchDatabase(t)
	upTo(t, scratch.db, latestMigration(t))
	db := scratch.pool
	one := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	if _, err := db.Exec(ctx, `INSERT INTO site (slug, name) VALUES ('guards', 'Guards')`); err != nil {
		t.Fatal(err)
	}
	admin := one(`INSERT INTO app_user (email, name) VALUES ('ada@guards.test', 'Ada') RETURNING id`)
	member := one(`INSERT INTO app_user (email, name) VALUES ('bob@guards.test', 'Bob') RETURNING id`)
	guest := one(`INSERT INTO app_user (email, name) VALUES ('gus@guards.test', 'Gus') RETURNING id`)
	for _, person := range []uuid.UUID{admin, member} {
		one(`INSERT INTO member (user_id, role) VALUES ($1, 'member') RETURNING user_id`, person)
	}
	adminGrant := one(`INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1) RETURNING id`, admin)
	open := one(`INSERT INTO space (key, name, created_by) VALUES ('OPEN', 'Open', $1) RETURNING id`, admin)
	owned := one(`INSERT INTO space (key, name, created_by) VALUES ('BOBS', 'Bobs', $1) RETURNING id`, member)
	shut := one(`INSERT INTO space (key, name) VALUES ('SHUT', 'Shut') RETURNING id`)
	if _, err := db.Exec(ctx, `DELETE FROM role_assignment WHERE space_id = $1`, shut); err != nil {
		t.Fatal(err)
	}
	one(`INSERT INTO member (user_id, role, guest_space_id) VALUES ($1, 'guest', $2) RETURNING user_id`, guest, open)
	everyone := one(`SELECT id FROM groups WHERE builtin = 'everyone'`)

	t.Run("every member is in Everyone, a guest is not", func(t *testing.T) {
		var members int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM group_member WHERE group_id = $1`, everyone).Scan(&members); err != nil || members != 2 {
			t.Errorf("Everyone holds %d people (%v), want the two members", members, err)
		}
	})

	t.Run("the installation keeps an active administrator", func(t *testing.T) {
		refusedAt(t, db, admin, "installation_keeps_administrator", "the last administrator's grant taken", `DELETE FROM role_assignment WHERE id = $1`, adminGrant)
		// Switching an account off is the service's, which runs it past the
		// members' policies; the guard holds there too.
		_, err := db.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, admin)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != "installation_keeps_administrator" {
			t.Errorf("the last administrator was switched off: %v", err)
		}
		refusedAt(t, db, admin, "installation_keeps_administrator", "the last administrator removed", `DELETE FROM member WHERE user_id = $1`, admin)

		// Through a group of their own somebody else administers, and then
		// the first grant may go, but not the group's last member.
		ranAs(t, db, admin, "hand administering to a group",
			`INSERT INTO groups (name) VALUES ('Keepers')`,
			`INSERT INTO group_member (group_id, user_id) SELECT id, '`+member.String()+`' FROM groups WHERE name = 'Keepers'`,
			`INSERT INTO role_assignment (role, group_id) SELECT 'administrator', id FROM groups WHERE name = 'Keepers'`,
			`DELETE FROM role_assignment WHERE id = '`+adminGrant.String()+`'`)
		refusedAt(t, db, member, "installation_keeps_administrator", "the last administrator taken out of their group",
			`DELETE FROM group_member WHERE user_id = $1 AND group_id = (SELECT id FROM groups WHERE name = 'Keepers')`, member)
		ranAs(t, db, member, "hand it back", `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', '`+admin.String()+`')`)
		// Bob is a member again for what follows, administering nothing.
		ranAs(t, db, admin, "take the group's administering", `DELETE FROM role_assignment WHERE group_id = (SELECT id FROM groups WHERE name = 'Keepers')`)
	})

	t.Run("roles and Everyone stay what they are", func(t *testing.T) {
		refusedAt(t, db, admin, "role_administrator_administers", "the administrator without administering", `UPDATE role SET permissions = ARRAY['view'] WHERE key = 'administrator'`)
		refusedAt(t, db, admin, "role_builtin_stays", "a built-in role deleted", `DELETE FROM role WHERE key = 'viewer'`)
		refusedAt(t, db, admin, "role_key_fixed", "a built-in role's key changed", `UPDATE role SET key = 'reader' WHERE key = 'viewer'`)
		refusedAt(t, db, admin, "role_assignment_everyone_administers", "Everyone made an administrator",
			`INSERT INTO role_assignment (role, group_id) VALUES ('administrator', $1)`, everyone)
		refusedAt(t, db, admin, "role_assignment_everyone_administers", "a role Everyone holds made to administer",
			`UPDATE role SET permissions = permissions || ARRAY['administerSite'] WHERE key = 'member'`)
		refusedAt(t, db, admin, "groups_builtin_fixed", "Everyone renamed", `UPDATE groups SET name = 'All' WHERE id = $1`, everyone)
		refusedAt(t, db, admin, "groups_builtin_fixed", "Everyone deleted", `DELETE FROM groups WHERE id = $1`, everyone)
		refusedAt(t, db, admin, "group_member_keeps_everyone", "a member taken out of Everyone", `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone, member)
		refusedAt(t, db, admin, "role_assignment_installation_wide", "the administrator granted in one space",
			`INSERT INTO role_assignment (role, space_id, user_id) VALUES ('administrator', $1, $2)`, open, member)
	})

	t.Run("a guest stays in their one space and administers none", func(t *testing.T) {
		refusedAt(t, db, admin, "role_assignment_guest_not_administrator", "a guest administering their space",
			`INSERT INTO role_assignment (role, space_id, user_id) VALUES ('space_administrator', $1, $2)`, open, guest)
		refusedAt(t, db, admin, "role_assignment_guest_one_space", "a guest let into another space",
			`INSERT INTO role_assignment (role, space_id, user_id) VALUES ('viewer', $1, $2)`, shut, guest)
		refusedAt(t, db, admin, "role_assignment_guest_one_space", "a guest granted the installation",
			`INSERT INTO role_assignment (role, user_id) VALUES ('member', $1)`, guest)
		ranAs(t, db, admin, "the guest comments in their space", `INSERT INTO role_assignment (role, space_id, user_id) VALUES ('commenter', '`+open.String()+`', '`+guest.String()+`')`)
		refusedAt(t, db, admin, "role_assignment_guest_not_administrator", "a role a guest holds made to administer",
			`UPDATE role SET permissions = permissions || ARRAY['administer'] WHERE key = 'commenter'`)
	})

	t.Run("a space keeps an administrator of its own", func(t *testing.T) {
		refusedAt(t, db, member, "space_keeps_administrator", "the space's last administrator's grant taken by themselves",
			`DELETE FROM role_assignment WHERE space_id = $1 AND role = 'space_administrator'`, owned)
	})

	t.Run("row level security reads the grants", func(t *testing.T) {
		sees := func(person, space uuid.UUID) bool {
			t.Helper()
			tx := asApp(t, db, person)
			defer func() { _ = tx.Rollback(ctx) }()
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM space WHERE id = $1`, space).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n == 1
		}
		if sees(member, shut) {
			t.Fatal("bob sees a space nobody granted him")
		}
		if !sees(admin, shut) {
			t.Error("the administrator does not see a space through the role granted across the installation")
		}
		ranAs(t, db, admin, "a group views the shut space",
			`INSERT INTO groups (name) VALUES ('Readers')`,
			`INSERT INTO group_member (group_id, user_id) SELECT id, '`+member.String()+`' FROM groups WHERE name = 'Readers'`,
			`INSERT INTO role_assignment (role, space_id, group_id) SELECT 'viewer', '`+shut.String()+`', id FROM groups WHERE name = 'Readers'`)
		if !sees(member, shut) {
			t.Error("bob does not see the space his group views")
		}
		refusedAt(t, db, member, "", "bob granting himself the shut space to administer",
			`INSERT INTO role_assignment (role, space_id, user_id) VALUES ('space_administrator', $1, $2)`, shut, member)
		ranAs(t, db, admin, "the group's grant goes", `DELETE FROM role_assignment WHERE space_id = '`+shut.String()+`'`)
		if sees(member, shut) {
			t.Error("bob still sees the space after his group's grant went")
		}
		ranAs(t, db, admin, "Everyone views the shut space", `INSERT INTO role_assignment (role, space_id, group_id) VALUES ('viewer', '`+shut.String()+`', '`+everyone.String()+`')`)
		if !sees(member, shut) || sees(guest, shut) {
			t.Error("Everyone's grant reaches the wrong people")
		}
	})
}

// The move to roles (#585) keeps everybody's access: an installation seeded
// as the release before wrote it, with owners, administrators by hand and by
// a provider group, groups, guests, grids of every shape, permissions across
// it and a space open to anybody, answers every question of the permission
// functions the same before and after the migration.
func TestTheMoveToRolesKeepsEveryonesAccess(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	roles, before := migrationsAround(t, "_roles_and_grants.sql")
	scratch := scratchDatabase(t)
	upTo(t, scratch.db, before)
	db := scratch.pool
	for _, sql := range []string{
		`INSERT INTO site (slug, name, anonymous_access) VALUES ('acme', 'Acme', true)`,
		`INSERT INTO app_user (email, name) VALUES
		 ('owner@x.test', 'Owner'), ('admin@x.test', 'Admin'), ('mem@x.test', 'Mem'), ('com@x.test', 'Com'), ('ed@x.test', 'Ed'),
		 ('guest@x.test', 'Guest'), ('oidcadmin@x.test', 'Oidc admin'), ('crt@x.test', 'Creator'), ('lone@x.test', 'Lone')`,
		`INSERT INTO member (user_id, role) SELECT id, 'owner' FROM app_user WHERE email = 'owner@x.test'`,
		`INSERT INTO member (user_id, role) SELECT id, 'admin' FROM app_user WHERE email = 'admin@x.test'`,
		`INSERT INTO member (user_id, role) SELECT id, 'member' FROM app_user WHERE email IN ('mem@x.test', 'com@x.test', 'ed@x.test', 'crt@x.test')`,
		`INSERT INTO oidc_provider (issuer, client_id) VALUES ('https://id.test', 'stator')`,
		`INSERT INTO member (user_id, role, role_source) SELECT id, 'admin', 'oidc' FROM app_user WHERE email IN ('oidcadmin@x.test', 'lone@x.test')`,
		`INSERT INTO groups (name) VALUES ('Writers'), ('everyone')`,
		`INSERT INTO groups (name, source, external_ref) VALUES ('ops', 'oidc', 'ops')`,
		`INSERT INTO group_member (group_id, user_id) SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'Writers' AND u.email IN ('ed@x.test', 'com@x.test')`,
		`INSERT INTO group_member (group_id, user_id) SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'ops' AND u.email = 'oidcadmin@x.test'`,
		`INSERT INTO group_member (group_id, user_id) SELECT g.id, u.id FROM groups g, app_user u WHERE g.name = 'everyone' AND u.email = 'mem@x.test'`,
		`INSERT INTO oidc_group_role (group_ref, role) VALUES ('ops', 'admin'), ('staff', 'member')`,
		`INSERT INTO global_grant (permission, subject_type, user_id) SELECT 'createSpace', 'user', id FROM app_user WHERE email = 'crt@x.test'`,
		`INSERT INTO global_grant (permission, subject_type, group_id) SELECT 'createSpace', 'group', id FROM groups WHERE name = 'Writers'`,
		`INSERT INTO space (key, name, created_by) SELECT 'DOCS', 'Docs', id FROM app_user WHERE email = 'owner@x.test'`,
		`INSERT INTO space (key, name, created_by) SELECT 'TEAM', 'Team', id FROM app_user WHERE email = 'mem@x.test'`,
		`INSERT INTO space (key, name, created_by, owner_id) SELECT 'PRIV', 'Priv', id, id FROM app_user WHERE email = 'mem@x.test'`,
		`DELETE FROM space_grant WHERE space_id = (SELECT id FROM space WHERE key = 'TEAM') AND subject_type = 'everyone'`,
		`INSERT INTO space_grant (space_id, permission, subject_type) SELECT id, 'view', 'everyone' FROM space WHERE key = 'TEAM'`,
		`INSERT INTO space_grant (space_id, permission, subject_type, group_id) SELECT s.id, 'addPages', 'group', g.id FROM space s, groups g WHERE s.key = 'TEAM' AND g.name = 'Writers'`,
		`INSERT INTO space_grant (space_id, permission, subject_type, user_id) SELECT s.id, 'addComments', 'user', u.id FROM space s, app_user u WHERE s.key = 'TEAM' AND u.email = 'com@x.test'`,
		`INSERT INTO space_grant (space_id, permission, subject_type, user_id) SELECT s.id, p, 'user', u.id FROM space s, app_user u, unnest(ARRAY['view', 'delete']) p WHERE s.key = 'TEAM' AND u.email = 'ed@x.test'`,
		`INSERT INTO space_grant (space_id, permission, subject_type, group_id) SELECT s.id, 'view', 'group', g.id FROM space s, groups g WHERE s.key = 'DOCS' AND g.name = 'everyone'`,
		`INSERT INTO member (user_id, role, guest_space_id) SELECT u.id, 'guest', s.id FROM app_user u, space s WHERE u.email = 'guest@x.test' AND s.key = 'TEAM'`,
		`INSERT INTO space_grant (space_id, permission, subject_type, user_id) SELECT s.id, p, 'user', u.id FROM space s, app_user u, unnest(ARRAY['view', 'addComments']) p WHERE s.key = 'TEAM' AND u.email = 'guest@x.test'`,
		`INSERT INTO space_grant (space_id, permission, subject_type) SELECT id, 'view', 'anonymous' FROM space WHERE key = 'DOCS'`,
	} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatalf("seed: %s: %v", sql, err)
		}
	}
	// Every question the policies ask, for everybody and every space.
	const answers = `
		SELECT u.email || ' ' || s.key || ' ' || p || ' ' || perm_space_holds(u.id, s.id, p)
		FROM app_user u, space s, unnest(ARRAY['view', 'addPages', 'addComments', 'delete', 'administer']) p
		UNION ALL
		SELECT u.email || ' * ' || p || ' ' || perm_global_holds(u.id, p) FROM app_user u, unnest(ARRAY['use', 'createSpace', 'administer']) p
		UNION ALL
		SELECT u.email || ' * admin ' || perm_is_admin(u.id) FROM app_user u
		UNION ALL
		SELECT 'anybody ' || s.key || ' ' || perm_space_anonymous(s.id) FROM space s
		ORDER BY 1`
	read := func() []string {
		t.Helper()
		rows, err := db.Query(ctx, answers)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	was := read()
	upTo(t, scratch.db, roles)
	now := read()
	if len(now) != len(was) {
		t.Fatalf("%d answers before, %d after", len(was), len(now))
	}
	for i := range was {
		if was[i] != now[i] {
			t.Errorf("before: %s, after: %s", was[i], now[i])
		}
	}

	grants := map[string]string{}
	rows, err := db.Query(ctx, `
		SELECT COALESCE(s.key, '*') || ' ' || COALESCE(u.email, g.name), a.role
		FROM role_assignment a LEFT JOIN space s ON s.id = a.space_id
		LEFT JOIN app_user u ON u.id = a.user_id LEFT JOIN groups g ON g.id = a.group_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var who, role string
		if err := rows.Scan(&who, &role); err != nil {
			t.Fatal(err)
		}
		grants[who] += role
	}
	for who, role := range map[string]string{
		"* owner@x.test": "administrator", "* admin@x.test": "administrator", "* lone@x.test": "administrator",
		"* ops": "administrator", "* staff": "member", "* Everyone": "member", "* crt@x.test": "space_creator", "* Writers": "space_creator",
		"DOCS Everyone": "editor", "DOCS owner@x.test": "space_administrator", "DOCS everyone (made by hand)": "viewer",
		"TEAM Everyone": "viewer", "TEAM com@x.test": "commenter", "TEAM guest@x.test": "commenter", "TEAM mem@x.test": "space_administrator",
		"TEAM ed@x.test": "view_and_delete", "TEAM Writers": "view_and_add_pages",
	} {
		if grants[who] != role {
			t.Errorf("%s holds %q, want %s", who, grants[who], role)
		}
	}
	if grants["* oidcadmin@x.test"] != "" {
		t.Errorf("the administrator through ops holds %q by name too", grants["* oidcadmin@x.test"])
	}
	var standings int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM member WHERE role NOT IN ('member', 'guest')`).Scan(&standings); err != nil || standings != 0 {
		t.Errorf("%d owners and admins of before are left (%v)", standings, err)
	}
}

// latestMigration is the newest migration's version.
func latestMigration(t *testing.T) int64 {
	t.Helper()
	newest, _ := migrationsAround(t, "_roles_and_grants.sql")
	return newest + 1<<20
}
