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

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// The service refusing is not proof. Straight through SQL as stator_app, for
// a member the transaction names, the database refuses what the rules refuse.
func TestPermissionsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "perm-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, bobID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)

	docs := newTree(t, owner, "RAW", "Raw")
	top := docs.add(docs.homeID, "Top")
	low := docs.add(top, "Low")
	open := docs.add(docs.homeID, "Open")
	want(t, restrict(t, ann, top, []any{user(annID), user(bobID)}, []any{user(annID)}), http.StatusOK, "ann restricts Top")
	want(t, ann.put(t, pagePath(low, "/draft"), map[string]any{"title": "Draft", "body": textDoc("secret"), "baseVersion": 1}), http.StatusOK, "ann drafts Low")
	want(t, ann.upload(t, pagePath(low, "/attachments"), "low.txt", []byte("secret")), http.StatusCreated, "ann attaches to Low")
	reading := newTree(t, owner, "READ", "Read only")
	readPage := reading.add(reading.homeID, "Read me")
	want(t, owner.put(t, "/api/v1/spaces/READ/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
	}}), http.StatusOK, "READ is read only")
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

	t.Run("a transaction that names nobody sees no page", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false), set_config($3, '', false)`, tenant.PostgresVar, home.org.String(), db.UserVar); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"space", "page", "page_version", "page_restriction", "space_grant", "global_grant"} {
			if n := count(`SELECT count(*) FROM ` + table); n != 0 {
				t.Errorf("nobody sees %d rows of %s", n, table)
			}
		}
	})

	t.Run("somebody off a view list reads nothing below it", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		for table, column := range map[string]string{"page": "id", "page_version": "page_id", "page_restriction": "page_id", "page_draft": "page_id", "attachment": "page_id"} {
			if n := count(`SELECT count(*) FROM `+table+` WHERE `+column+` = ANY ($1)`, []string{top, low}); n != 0 {
				t.Errorf("carl reads %d rows of %s", n, table)
			}
		}
		if n := count(`SELECT count(*) FROM page WHERE id = $1`, open); n != 1 {
			t.Errorf("carl does not read an open page: %d", n)
		}
		untouched(t, conn, "retitling a hidden page", `UPDATE page SET title = 'Taken' WHERE id = $1`, low)
		untouched(t, conn, "lifting a restriction he cannot see", `DELETE FROM page_restriction WHERE page_id = $1`, top)
		denied(t, conn, "putting himself on the list", `INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id) VALUES ($1, $2, 'view', 'user', $3)`, home.org, top, carlID)
		denied(t, conn, "a page under a hidden one", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by) VALUES ($1, (SELECT space_id FROM page WHERE id = $2), $3, 'W', 'Planted', $4)`, home.org, open, low, carlID)
		denied(t, conn, "trashing through the database's own function", `SELECT page_trash($1)`, low)
		denied(t, conn, "moving through the database's own function", `SELECT page_place(ARRAY[$1::uuid], $2, ARRAY['W'])`, open, low)
	})

	t.Run("somebody off an edit list reads but cannot change", func(t *testing.T) {
		actAs(t, conn, home.org, bobID)
		if n := count(`SELECT count(*) FROM page WHERE id = $1`, low); n != 1 {
			t.Fatalf("bob does not read Low: %d", n)
		}
		denied(t, conn, "retitling it", `UPDATE page SET title = 'Bob''s' WHERE id = $1`, low)
		denied(t, conn, "a file on it", `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'x', 1)`, home.org, low)
		untouched(t, conn, "its files", `DELETE FROM attachment WHERE page_id = $1`, low)
		denied(t, conn, "trashing it", `UPDATE page SET trashed_at = now(), trash_id = id WHERE id = $1`, low)
		denied(t, conn, "publishing a version of it", `INSERT INTO page_version (org_id, page_id, number, title, body, created_by) VALUES ($1, $2, 2, 'Bob''s', '{}', $3)`, home.org, low, bobID)
		denied(t, conn, "a draft of it", `INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version) VALUES ($1, $2, $3, 'Bob''s', '{}', 1)`, home.org, low, bobID)
		untouched(t, conn, "lifting its restriction", `DELETE FROM page_restriction WHERE page_id = $1`, top)
		untouched(t, conn, "ann's draft", `DELETE FROM page_draft WHERE page_id = $1`, low)
		if n := count(`SELECT count(*) FROM page_restriction WHERE page_id = $1`, top); n != 3 {
			t.Errorf("bob reads %d of Top's names, want the 3 of both lists", n)
		}
	})

	t.Run("a reader cannot write, a member cannot administer", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		denied(t, conn, "a page in a read only space", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by) VALUES ($1, (SELECT space_id FROM page WHERE id = $2), $2, 'W', 'Planted', $3)`, home.org, readPage, annID)
		denied(t, conn, "retitling in a read only space", `UPDATE page SET title = 'Taken' WHERE id = $1`, readPage)
		denied(t, conn, "renaming a space", `UPDATE space SET name = 'Taken' WHERE key = 'RAW'`)
		untouched(t, conn, "deleting a space", `DELETE FROM space WHERE key = 'RAW'`)
		denied(t, conn, "granting herself a space", `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id) VALUES ($1, (SELECT id FROM space WHERE key = 'READ'), 'administer', 'user', $2)`, home.org, annID)
		denied(t, conn, "granting herself spaces", `INSERT INTO global_grant (org_id, permission, subject_type, user_id) VALUES ($1, 'createSpace', 'user', $2)`, home.org, annID)
		untouched(t, conn, "taking use from everyone", `DELETE FROM global_grant`)
		denied(t, conn, "making a space", `INSERT INTO space (org_id, key, name, created_by) VALUES ($1, 'MINE', 'Mine', $2)`, home.org, annID)
		denied(t, conn, "purging", `SELECT page_purge($1)`, top)
		denied(t, conn, "emptying the trash", `SELECT space_empty_trash((SELECT id FROM space WHERE key = 'RAW'))`)
		denied(t, conn, "a view list on the home page", `INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id) VALUES ($1, $2, 'view', 'user', $3)`, home.org, docs.homeID, annID)
		if n := count(`SELECT count(*) FROM page WHERE id = ANY ($1)`, []string{top, low}); n != 2 {
			t.Errorf("ann does not read the pages she restricted: %d", n)
		}
	})

	t.Run("an administrator passes every list", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		if n := count(`SELECT count(*) FROM page WHERE id = ANY ($1)`, []string{top, low}); n != 2 {
			t.Errorf("the owner reads %d of the restricted pages", n)
		}
		if n := count(`SELECT count(*) FROM page_draft WHERE page_id = $1`, low); n != 0 {
			t.Errorf("the owner reads ann's draft")
		}
		if _, err := conn.Exec(ctx, `UPDATE page SET rank = rank WHERE id = $1`, low); err != nil {
			t.Errorf("the owner may not touch Low: %v", err)
		}
	})

	t.Run("without use nothing is seen", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `DELETE FROM global_grant WHERE org_id = $1 AND permission = 'use'`, home.org); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			h.cleanupExec(t, h.super, `INSERT INTO global_grant (org_id, permission, subject_type) VALUES ($1, 'use', 'everyone')`, home.org)
		})
		actAs(t, conn, home.org, annID)
		for _, table := range []string{"space", "page"} {
			if n := count(`SELECT count(*) FROM ` + table); n != 0 {
				t.Errorf("a member without use reads %d rows of %s", n, table)
			}
		}
		actAs(t, conn, home.org, home.user)
		if n := count(`SELECT count(*) FROM space`); n != 2 {
			t.Errorf("the owner without a use grant reads %d spaces", n)
		}
	})
}

// The service's rules and the database's are the same rules: for every
// person and page the reader answers exactly what perm_page_viewable says.
func TestTheServiceAndTheDatabaseAgree(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "perm-agree")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	people := map[string]uuid.UUID{"owner": home.user}
	for _, name := range []string{"ann", "bob", "carl"} {
		people[name] = h.addPerson(t, home.org, "member")
	}
	team := h.makeGroup(t, home.org, "Team", people["bob"])
	ann := api.as(t, people["ann"], home.org, slug)

	docs := newTree(t, owner, "AGREE", "Agree")
	a := docs.add(docs.homeID, "A")
	b := docs.add(a, "B")
	c := docs.add(b, "C")
	d := docs.add(docs.homeID, "D")
	want(t, restrict(t, ann, a, []any{user(people["ann"]), group(team)}, nil), http.StatusOK, "restrict A")
	want(t, restrict(t, ann, c, []any{user(people["ann"])}, []any{user(people["ann"])}), http.StatusOK, "restrict C")
	unpublished := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": d, "title": "Ann's own"}), http.StatusCreated, "ann's unpublished page"), "page")["id"].(string)
	h.settle(t)

	titles := map[string]string{docs.homeID: "Home", a: "A", b: "B", c: "C", d: "D", unpublished: "Ann's own"}
	expected := map[string][]string{
		"owner": {"Home", "A", "B", "C", "D"},
		"ann":   {"Home", "A", "B", "C", "D", "Ann's own"},
		"bob":   {"Home", "A", "B", "D"},
		"carl":  {"Home", "D"},
	}
	for name, id := range people {
		caller := api.as(t, id, home.org, slug)
		seen := map[string]bool{}
		for page, title := range titles {
			got := caller.get(t, "/api/v1/pages/"+page).Status == http.StatusOK
			var viewable bool
			err := h.cluster.ReadPrimary(db.WithUser(home.ctx, id), func(ctx context.Context, tx db.DBTX) error {
				return tx.QueryRow(ctx, `SELECT perm_page_viewable($1, $2)`, page, id).Scan(&viewable)
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != viewable {
				t.Errorf("%s: the service answers %v for %s, the database %v", name, got, title, viewable)
			}
			seen[title] = got
		}
		for _, title := range expected[name] {
			if !seen[title] {
				t.Errorf("%s does not see %s", name, title)
			}
			delete(seen, title)
		}
		for title, ok := range seen {
			if ok {
				t.Errorf("%s sees %s", name, title)
			}
		}
	}
}

// denied is refused for the right reason: a policy, a privilege the rules
// withhold, or a check, never a statement that is merely wrong.
func denied(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	_, err := conn.Exec(context.Background(), sql, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Errorf("%s was not refused (%v)", what, err)
		return
	}
	if pgErr.Code != "42501" && pgErr.Code != "23514" {
		t.Errorf("%s was refused for the wrong reason: %s %s", what, pgErr.Code, pgErr.Message)
	}
}
