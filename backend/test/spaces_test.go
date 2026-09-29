//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// helloDoc is a document the editor could have written.
var helloDoc = map[string]any{"type": "doc", "content": []any{
	map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Hello, space."}}},
}}

// A space is made by an administrator with its home page, seen and edited by
// every member, and invisible from another organization.
func TestSpacesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	home := h.makeMember(t, "spaces")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	away := h.makeMember(t, "spaces-away")
	stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))

	made := want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": " docs ", "name": " Handbook ", "description": "How we work."}), http.StatusCreated, "make a space")
	sp := obj(t, made, "space")
	if sp["key"] != "DOCS" || sp["name"] != "Handbook" || sp["description"] != "How we work." {
		t.Fatalf("the space was not cleaned up on the way in: %s", made.Raw)
	}
	if can := obj(t, made, "space", "can"); can["administer"] != true || can["editPages"] != true || can["delete"] != true {
		t.Fatalf("the owner may not do everything: %s", made.Raw)
	}
	homeID, _ := sp["homePageId"].(string)
	if _, err := uuid.Parse(homeID); err != nil {
		t.Fatalf("the space has no home page: %s", made.Raw)
	}

	t.Run("its home page is there, titled like the space, empty and first", func(t *testing.T) {
		got := want(t, member.get(t, "/api/v1/pages/"+homeID), http.StatusOK, "read the home page")
		p := obj(t, got, "page")
		if p["title"] != "Handbook" || p["home"] != true || p["parentId"] != nil || p["spaceKey"] != "DOCS" || p["version"].(float64) != 1 {
			t.Fatalf("the home page is %s", got.Raw)
		}
		if obj(t, got, "space")["key"] != "DOCS" {
			t.Fatalf("the page does not say which space it is in: %s", got.Raw)
		}
		if body := obj(t, got, "page", "body"); body["type"] != "doc" {
			t.Fatalf("the home page body is not an empty document: %s", got.Raw)
		}
	})

	t.Run("every member sees the directory, but only administrators shape it", func(t *testing.T) {
		listed := list(t, want(t, member.get(t, "/api/v1/spaces"), http.StatusOK, "list as a member"), "spaces")
		if len(listed) != 1 || listed[0].(map[string]any)["key"] != "DOCS" {
			t.Fatalf("the member sees %v", listed)
		}
		if can := listed[0].(map[string]any)["can"].(map[string]any); can["administer"] != false || can["editPages"] != true {
			t.Fatalf("a member is offered %v", can)
		}
		got := want(t, member.get(t, "/api/v1/spaces/docs"), http.StatusOK, "a key in lower case")
		if obj(t, got, "space")["id"] != sp["id"] {
			t.Fatalf("the key did not find its space: %s", got.Raw)
		}
		for what, refused := range map[string]response{
			"create": member.post(t, "/api/v1/spaces", map[string]any{"key": "MINE", "name": "Mine"}),
			"rename": member.patch(t, "/api/v1/spaces/DOCS", map[string]any{"name": "Taken over"}),
			"delete": member.delete(t, "/api/v1/spaces/DOCS"),
		} {
			if refused.Status != http.StatusForbidden || errorCode(t, refused) != "forbidden" {
				t.Errorf("a member may %s a space: %d %s", what, refused.Status, refused.Raw)
			}
		}
	})

	t.Run("what is refused on the way in", func(t *testing.T) {
		for what, tt := range map[string]struct {
			body  map[string]any
			field string
		}{
			"a key taken":            {map[string]any{"key": "docs", "name": "Again"}, "key"},
			"a key that is a word":   {map[string]any{"key": "my docs", "name": "Words"}, "key"},
			"no key":                 {map[string]any{"name": "Keyless"}, "key"},
			"a key starting a digit": {map[string]any{"key": "1ST", "name": "First"}, "key"},
			"no name":                {map[string]any{"key": "NONAME", "name": "  "}, "name"},
		} {
			got := owner.post(t, "/api/v1/spaces", tt.body)
			if got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
				continue
			}
			fields, _ := got.Body["error"].(map[string]any)["fields"].(map[string]any)
			if msg, _ := fields[tt.field].(string); msg == "" {
				t.Errorf("%s: nothing said about %s in %s", what, tt.field, got.Raw)
			}
		}
		if got := owner.get(t, "/api/v1/spaces/NOPE"); got.Status != http.StatusNotFound || errorCode(t, got) != "not_found" {
			t.Errorf("a space that is not there: %d %s", got.Status, got.Raw)
		}
		if got := owner.get(t, "/api/v1/pages/"+uuid.NewString()); got.Status != http.StatusNotFound {
			t.Errorf("a page that is not there: %d %s", got.Status, got.Raw)
		}
		if got := api.anonymous().get(t, "/api/v1/spaces"); got.Status != http.StatusUnauthorized {
			t.Errorf("nobody signed in listed spaces: %d", got.Status)
		}
	})

	t.Run("an administrator renames and describes it, and the key stays", func(t *testing.T) {
		got := want(t, owner.patch(t, "/api/v1/spaces/DOCS", map[string]any{"name": "Team handbook", "description": ""}), http.StatusOK, "rename")
		if s := obj(t, got, "space"); s["name"] != "Team handbook" || s["description"] != "" || s["key"] != "DOCS" {
			t.Fatalf("the rename came back as %s", got.Raw)
		}
		if got := owner.patch(t, "/api/v1/spaces/DOCS", map[string]any{"key": "NEW"}); got.Status != http.StatusBadRequest {
			t.Fatalf("a key change was not refused as an unknown field: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("members save the home page over the version they opened", func(t *testing.T) {
		saved := want(t, member.patch(t, "/api/v1/pages/"+homeID, map[string]any{"title": "Welcome", "body": helloDoc, "version": 1}), http.StatusOK, "save")
		p := obj(t, saved, "page")
		if p["title"] != "Welcome" || p["version"].(float64) != 2 || p["updatedByName"] != "A member" {
			t.Fatalf("the save came back as %s", saved.Raw)
		}
		stale := owner.patch(t, "/api/v1/pages/"+homeID, map[string]any{"title": "Mine", "version": 1})
		if stale.Status != http.StatusConflict || errorCode(t, stale) != "conflict" {
			t.Fatalf("a save over a newer version: %d %s", stale.Status, stale.Raw)
		}
		bad := member.patch(t, "/api/v1/pages/"+homeID, map[string]any{"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "script"}}}, "version": 2})
		if bad.Status != http.StatusUnprocessableEntity || errorCode(t, bad) != "validation_failed" {
			t.Fatalf("a document the editor cannot have written: %d %s", bad.Status, bad.Raw)
		}
		blank := member.patch(t, "/api/v1/pages/"+homeID, map[string]any{"title": " ", "version": 2})
		if blank.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a blank title: %d %s", blank.Status, blank.Raw)
		}
		read := want(t, owner.get(t, "/api/v1/pages/"+homeID), http.StatusOK, "read back")
		text := obj(t, read, "page", "body")["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
		if text != "Hello, space." || obj(t, read, "page")["version"].(float64) != 2 {
			t.Fatalf("the refusals changed the page: %s", read.Raw)
		}
	})

	t.Run("another organization sees nothing and may use the same key", func(t *testing.T) {
		if got := stranger.get(t, "/api/v1/spaces/DOCS"); got.Status != http.StatusNotFound {
			t.Errorf("a stranger found the space: %d", got.Status)
		}
		if got := stranger.get(t, "/api/v1/pages/"+homeID); got.Status != http.StatusNotFound {
			t.Errorf("a stranger read the home page: %d", got.Status)
		}
		if got := stranger.patch(t, "/api/v1/pages/"+homeID, map[string]any{"title": "Mine", "version": 2}); got.Status != http.StatusNotFound {
			t.Errorf("a stranger saved the home page: %d", got.Status)
		}
		if got := stranger.delete(t, "/api/v1/spaces/DOCS"); got.Status != http.StatusNotFound {
			t.Errorf("a stranger deleted the space: %d", got.Status)
		}
		if listed := list(t, want(t, stranger.get(t, "/api/v1/spaces"), http.StatusOK, "list away"), "spaces"); len(listed) != 0 {
			t.Errorf("a stranger lists %v", listed)
		}
		want(t, stranger.post(t, "/api/v1/spaces", map[string]any{"key": "DOCS", "name": "Theirs"}), http.StatusCreated, "the same key elsewhere")
	})

	t.Run("deleting the space takes its pages with it", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/spaces/DOCS"), http.StatusNoContent, "delete")
		if got := owner.get(t, "/api/v1/spaces/DOCS"); got.Status != http.StatusNotFound {
			t.Errorf("the space is still there: %d", got.Status)
		}
		if got := owner.get(t, "/api/v1/pages/"+homeID); got.Status != http.StatusNotFound {
			t.Errorf("its home page is still there: %d", got.Status)
		}
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE org_id = $1`, home.org).Scan(&left); err != nil || left != 0 {
			t.Errorf("%d pages are left behind (%v)", left, err)
		}
	})

	t.Run("the audit log says who made, changed and deleted it", func(t *testing.T) {
		rows, err := h.super.Query(context.Background(), `
			SELECT action FROM audit_log
			WHERE org_id = $1 AND target_type = 'space' AND target_id = $2 AND actor_user_id = $3
			ORDER BY created_at, action`, home.org, sp["id"], home.user)
		if err != nil {
			t.Fatal(err)
		}
		actions, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if len(actions) != 3 || actions[0] != "space.created" || actions[1] != "space.updated" || actions[2] != "space.deleted" {
			t.Fatalf("the audit log has %v for the space", actions)
		}
	})
}

// appConn is a connection straight to the database as stator_app, with no Go
// code between a test and the policies.
func appConn(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// actAs scopes a raw connection to an organization, as a transaction would.
func actAs(t *testing.T, conn *pgx.Conn, org uuid.UUID) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.String()); err != nil {
		t.Fatal(err)
	}
}

// refused fails the test unless the database answered the statement with an error.
func refused(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	tag, err := conn.Exec(context.Background(), sql, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Errorf("%s was not refused (%d rows, %v)", what, tag.RowsAffected(), err)
	}
}

// untouched fails the test unless the statement ran and changed nothing.
func untouched(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	tag, err := conn.Exec(context.Background(), sql, args...)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("%s reached %d rows (%v)", what, tag.RowsAffected(), err)
	}
}

// The service refusing is not proof: straight through SQL as stator_app, a
// tenant cannot read, change or point into another tenant's spaces and pages,
// and the tree keeps its shape within one.
func TestSpaceRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "space-wall-a")
	b := h.makeMember(t, "space-wall-b")
	ownerA := api.as(t, a.user, a.org, h.slugOf(t, a.org))
	ownerB := api.as(t, b.user, b.org, h.slugOf(t, b.org))

	spA := obj(t, want(t, ownerA.post(t, "/api/v1/spaces", map[string]any{"key": "WALL", "name": "Walled"}), http.StatusCreated, "space in A"), "space")
	spA2 := obj(t, want(t, ownerA.post(t, "/api/v1/spaces", map[string]any{"key": "OTHER", "name": "Other"}), http.StatusCreated, "second space in A"), "space")
	spB := obj(t, want(t, ownerB.post(t, "/api/v1/spaces", map[string]any{"key": "WALL", "name": "Theirs"}), http.StatusCreated, "space in B"), "space")
	spaceA, homeA := spA["id"].(string), spA["homePageId"].(string)
	homeA2 := spA2["homePageId"].(string)
	spaceB, homeB := spB["id"].(string), spB["homePageId"].(string)

	conn := appConn(t)
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	for _, table := range []string{"space", "page"} {
		if got := count(`SELECT count(*) FROM ` + table); got != 0 {
			t.Errorf("an unscoped connection sees %d rows in %s", got, table)
		}
	}

	actAs(t, conn, b.org)
	for _, table := range []string{"space", "page"} {
		if got := count(`SELECT count(*) FROM `+table+` WHERE org_id = $1`, a.org); got != 0 {
			t.Errorf("organization B sees %d of A's rows in %s", got, table)
		}
	}
	untouched(t, conn, "renaming A's space", `UPDATE space SET name = 'Taken' WHERE id = $1`, spaceA)
	untouched(t, conn, "retitling A's home page", `UPDATE page SET title = 'Taken' WHERE id = $1`, homeA)
	untouched(t, conn, "deleting A's space", `DELETE FROM space WHERE id = $1`, spaceA)
	refused(t, conn, "planting a page in A", `INSERT INTO page (org_id, space_id, parent_id, rank, title) VALUES ($1, $2, $3, 'W', 'Planted')`, a.org, spaceA, homeA)
	refused(t, conn, "hanging B's page under A's home", `INSERT INTO page (org_id, space_id, parent_id, rank, title) VALUES ($1, $2, $3, 'W', 'Planted')`, b.org, spaceB, homeA)
	refused(t, conn, "putting B's page in A's space", `INSERT INTO page (org_id, space_id, parent_id, rank, title) VALUES ($1, $2, $3, 'W', 'Planted')`, b.org, spaceA, homeB)
	refused(t, conn, "making A's page B's home", `UPDATE space SET home_page_id = $2 WHERE id = $1`, spaceB, homeA)

	actAs(t, conn, a.org)
	refused(t, conn, "a second root in a space", `INSERT INTO page (org_id, space_id, rank, title) VALUES ($1, $2, 'W', 'Second root')`, a.org, spaceA)
	refused(t, conn, "a parent in another space", `INSERT INTO page (org_id, space_id, parent_id, rank, title) VALUES ($1, $2, $3, 'W', 'Astray')`, a.org, spaceA, homeA2)
	refused(t, conn, "another space's page as the home", `UPDATE space SET home_page_id = $2 WHERE id = $1`, spaceA, homeA2)
	refused(t, conn, "deleting a home page", `DELETE FROM page WHERE id = $1`, homeA)
	refused(t, conn, "a key that is not one", `INSERT INTO space (org_id, key, name) VALUES ($1, 'bad key', 'Bad')`, a.org)

	var title string
	if err := h.super.QueryRow(context.Background(), `SELECT title FROM page WHERE id = $1`, homeA).Scan(&title); err != nil || title != "Walled" {
		t.Errorf("A's home page is %q (%v), want it untouched", title, err)
	}
}
