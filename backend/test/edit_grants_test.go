//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// restrictWith replaces a page's own lists, who else may edit included.
func restrictWith(t *testing.T, c *client, page string, view, edit, grant []any) response {
	t.Helper()
	body := map[string]any{"view": nonNilList(view), "edit": nonNilList(edit), "editGrant": nonNilList(grant)}
	return c.put(t, pagePath(page, "/restrictions"), body)
}

func nonNilList(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// invite lets somebody new into a space as a guest and answers their id.
func invite(t *testing.T, h *harness, owner *client, spaceKey, role string) uuid.UUID {
	t.Helper()
	email := fmt.Sprintf("guest-%s@example.test", uuid.NewString()[:8])
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	g := obj(t, want(t, owner.post(t, "/api/v1/spaces/"+spaceKey+"/guests", map[string]any{"email": email, "role": role}), http.StatusCreated, "invite a guest to "+spaceKey), "guest")
	return uuid.MustParse(g["userId"].(string))
}

// A page's grant list lets people who may only view the space edit it and
// the pages below it, and nothing more: the service and the database agree.
func TestAGrantListLetsAReaderEditOnePage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "edit-grants")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, bobID, carlID, daveID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, bob, carl, dave := api.as(t, annID, home.org, slug), api.as(t, bobID, home.org, slug), api.as(t, carlID, home.org, slug), api.as(t, daveID, home.org, slug)
	team := h.makeGroup(t, home.org, "Readers team", carlID)

	docs := newTree(t, owner, "GRANT", "Grants")
	top := docs.add(docs.homeID, "Top")
	low := docs.add(top, "Low")
	sibling := docs.add(docs.homeID, "Sibling")
	locked := docs.add(docs.homeID, "Locked")
	lockedLow := docs.add(locked, "Locked low")
	hidden := docs.add(docs.homeID, "Hidden")
	want(t, owner.put(t, "/api/v1/spaces/GRANT/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addComments"}},
		map[string]any{"subject": user(daveID), "permissions": []any{"addPages"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}}), http.StatusOK, "GRANT is read only but for dave and ann")
	other := newTree(t, owner, "ELSEWHERE", "Elsewhere")
	elsewhere := other.add(other.homeID, "Elsewhere")
	gwenID := invite(t, h, owner, "GRANT", "viewer")
	halID := invite(t, h, owner, "ELSEWHERE", "editor")
	gwen, hal := api.as(t, gwenID, home.org, slug), api.as(t, halID, home.org, slug)

	want(t, restrictWith(t, ann, top, nil, nil, []any{user(bobID), user(gwenID)}), http.StatusOK, "ann lets bob and gwen edit Top")
	want(t, restrictWith(t, ann, locked, nil, []any{user(annID)}, nil), http.StatusOK, "ann locks Locked")
	want(t, restrictWith(t, ann, lockedLow, nil, nil, []any{user(bobID)}), http.StatusOK, "ann lets bob edit Locked low")
	want(t, restrictWith(t, ann, hidden, []any{user(annID)}, nil, []any{user(bobID)}), http.StatusOK, "ann hides Hidden and names bob")
	h.settle(t)

	t.Run("bob edits the page and the pages below it, and adds nothing", func(t *testing.T) {
		can := obj(t, want(t, bob.get(t, "/api/v1/pages/"+low), http.StatusOK, "bob reads Low"), "page")["can"].(map[string]any)
		if can["edit"] != true || can["add"] != false || can["grantEdit"] != false || can["restrict"] != true {
			t.Errorf("bob may %v on Low", can)
		}
		want(t, bob.put(t, pagePath(top, "/draft"), map[string]any{"title": "Top by bob", "body": textDoc("bob was here"), "baseVersion": 1}), http.StatusOK, "bob drafts Top")
		want(t, bob.post(t, pagePath(top, "/publish"), map[string]any{}), http.StatusOK, "bob publishes Top")
		want(t, bob.put(t, pagePath(low, "/draft"), map[string]any{"title": "Low by bob", "body": textDoc("below"), "baseVersion": 1}), http.StatusOK, "bob drafts Low")
		want(t, bob.post(t, pagePath(low, "/publish"), map[string]any{}), http.StatusOK, "bob publishes Low")
		want(t, bob.upload(t, pagePath(top, "/attachments"), "bob.txt", []byte("bob's file")), http.StatusCreated, "bob attaches a file to Top")
		want(t, bob.get(t, pagePath(top, "/readers")), http.StatusOK, "bob sees who read Top")

		want(t, bob.put(t, pagePath(sibling, "/draft"), map[string]any{"title": "Sibling by bob", "body": textDoc("no"), "baseVersion": 1}), http.StatusForbidden, "bob drafts Sibling")
		want(t, bob.post(t, "/api/v1/pages", map[string]any{"parentId": top, "title": "Bob's child", "publish": true}), http.StatusForbidden, "bob adds a page under Top")
		want(t, bob.post(t, pagePath(low, "/move"), map[string]any{"parentId": docs.homeID}), http.StatusForbidden, "bob moves Low")
		want(t, bob.post(t, pagePath(low, "/copy"), map[string]any{"parentId": top}), http.StatusForbidden, "bob copies Low")
		want(t, bob.delete(t, "/api/v1/pages/"+low), http.StatusForbidden, "bob trashes Low")
		want(t, bob.put(t, pagePath(lockedLow, "/draft"), map[string]any{"title": "No", "body": textDoc("no"), "baseVersion": 1}), http.StatusForbidden, "bob drafts below Locked")
		want(t, bob.get(t, "/api/v1/pages/"+hidden), http.StatusNotFound, "bob reads Hidden")
		want(t, carl.put(t, pagePath(top, "/draft"), map[string]any{"title": "No", "body": textDoc("no"), "baseVersion": 1}), http.StatusForbidden, "carl drafts Top")
	})

	t.Run("bob changes the lists as an editor, but not who else may edit", func(t *testing.T) {
		want(t, restrict(t, bob, low, nil, []any{user(annID)}), http.StatusConflict, "bob narrows editing of Low to ann, which Top's grant does not pass")
		want(t, restrict(t, bob, low, nil, []any{user(annID), user(bobID)}), http.StatusOK, "bob narrows editing of Low to ann and himself")
		want(t, restrict(t, bob, low, nil, nil), http.StatusOK, "bob lifts it again")
		want(t, restrictWith(t, bob, top, nil, nil, []any{user(bobID), user(gwenID), user(carlID)}), http.StatusForbidden, "bob lets carl edit Top")
		want(t, restrictWith(t, dave, sibling, nil, nil, []any{user(carlID)}), http.StatusForbidden, "dave lets carl edit Sibling")
		want(t, restrictWith(t, dave, top, nil, nil, []any{user(bobID), user(gwenID)}), http.StatusOK, "dave saves Top's grant unchanged")
		want(t, restrict(t, dave, top, nil, []any{user(daveID)}), http.StatusOK, "dave narrows Top, leaving its grant alone")
		got := obj(t, want(t, bob.get(t, pagePath(top, "/restrictions")), http.StatusOK, "Top's lists"), "restrictions")
		if len(got["editGrant"].([]any)) != 2 || len(got["edit"].([]any)) != 1 {
			t.Errorf("Top's lists are %v", got)
		}
		want(t, bob.put(t, pagePath(top, "/draft"), map[string]any{"title": "Still bob's", "body": textDoc("still"), "baseVersion": 2}), http.StatusOK, "bob still drafts Top, whose grant passes its edit list")
		want(t, restrict(t, dave, top, nil, nil), http.StatusOK, "dave lifts Top's edit list")
		inherited := obj(t, want(t, bob.get(t, pagePath(low, "/restrictions")), http.StatusOK, "Low's lists"), "restrictions")["inherited"].([]any)
		if len(inherited) != 1 || len(inherited[0].(map[string]any)["editGrant"].([]any)) != 2 {
			t.Errorf("Low inherits %v", inherited)
		}
	})

	t.Run("guests are granted in their own space only", func(t *testing.T) {
		want(t, gwen.put(t, pagePath(top, "/draft"), map[string]any{"title": "Gwen's", "body": textDoc("gwen"), "baseVersion": 2}), http.StatusOK, "gwen drafts Top")
		r := want(t, restrictWith(t, owner, sibling, nil, nil, []any{user(halID)}), http.StatusUnprocessableEntity, "the owner lets a guest of Elsewhere edit Sibling")
		if fields := obj(t, r, "error")["fields"]; fields == nil {
			t.Errorf("the refusal names no field: %s", r.Raw)
		}
		want(t, hal.get(t, "/api/v1/pages/"+top), http.StatusNotFound, "hal reads Top")
	})

	t.Run("the edit list is checked before it is saved", func(t *testing.T) {
		check := func(c *client, page string, edit, grant []any) []any {
			t.Helper()
			r := want(t, c.post(t, pagePath(page, "/restrictions/check"), map[string]any{"view": []any{}, "edit": edit, "editGrant": grant}), http.StatusOK, "check")
			return obj(t, r, "check")["cannotEdit"].([]any)
		}
		got := check(ann, sibling, []any{user(bobID), user(daveID), group(team), user(annID)}, []any{})
		if len(got) != 2 {
			t.Fatalf("cannot edit: %v", got)
		}
		first, second := got[0].(map[string]any), got[1].(map[string]any)
		if first["subject"].(map[string]any)["id"] != bobID.String() || first["members"] != float64(0) {
			t.Errorf("first is %v", first)
		}
		if second["subject"].(map[string]any)["id"] != team.String() || second["members"] != float64(1) {
			t.Errorf("second is %v", second)
		}
		if got := check(ann, sibling, []any{user(bobID), group(team)}, []any{user(bobID), group(team)}); len(got) != 0 {
			t.Errorf("a grant list naming them still leaves %v", got)
		}
		if got := check(ann, low, []any{user(bobID)}, []any{}); len(got) != 0 {
			t.Errorf("a grant above still leaves %v", got)
		}
		want(t, carl.post(t, pagePath(sibling, "/restrictions/check"), map[string]any{"view": []any{}, "edit": []any{user(bobID)}}), http.StatusForbidden, "carl checks Sibling")
	})

	t.Run("the inspector names the grant", func(t *testing.T) {
		r := want(t, owner.get(t, pagePath(low, "/access/", bobID.String())), http.StatusOK, "inspect bob on Low")
		var out struct{ Access perm.AccessReport }
		if err := json.Unmarshal(r.Raw, &out); err != nil {
			t.Fatal(err)
		}
		edit := out.Access.Rights[1]
		if edit.Right != perm.RightEdit || !edit.Allowed {
			t.Fatalf("bob's edit is %+v", edit)
		}
		named := false
		for _, s := range edit.Steps {
			if s.Kind == perm.StepGrant && s.Page != nil && s.Page.ID.String() == top && s.Passed {
				named = true
			}
		}
		if !named {
			t.Errorf("the grant on Top is not named: %+v", edit.Steps)
		}
	})

	t.Run("copies and moves to another space leave the grant behind", func(t *testing.T) {
		copied := obj(t, want(t, ann.post(t, pagePath(top, "/copy"), map[string]any{"parentId": docs.homeID, "withChildren": false}), http.StatusCreated, "ann copies Top"), "page")["id"].(string)
		if got := obj(t, want(t, ann.get(t, pagePath(copied, "/restrictions")), http.StatusOK, "the copy's lists"), "restrictions"); len(got["editGrant"].([]any)) != 0 {
			t.Errorf("the copy lets %v edit", got["editGrant"])
		}
		want(t, owner.post(t, pagePath(top, "/move"), map[string]any{"parentId": elsewhere}), http.StatusOK, "the owner moves Top to Elsewhere")
		if got := obj(t, want(t, owner.get(t, pagePath(top, "/restrictions")), http.StatusOK, "the moved page's lists"), "restrictions"); len(got["editGrant"].([]any)) != 0 {
			t.Errorf("Top still lets %v edit in Elsewhere", got["editGrant"])
		}
		want(t, owner.post(t, pagePath(top, "/move"), map[string]any{"parentId": docs.homeID}), http.StatusOK, "the owner moves Top back")
		want(t, restrictWith(t, ann, top, nil, nil, []any{user(bobID), user(gwenID)}), http.StatusOK, "ann grants Top again")
	})

	t.Run("every change of who else may edit is audited", func(t *testing.T) {
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'page.restrictions_set' AND target_id = $2
			  AND jsonb_array_length(data -> 'editGrant') = 2`, home.org, top).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Error("the audit log does not say whom Top's grant names")
		}
	})

	// Straight through SQL as stator_app, the database grants and refuses the same.
	conn := appConn(t)
	ctx := context.Background()
	lets := func(person uuid.UUID, sql string, args ...any) bool {
		t.Helper()
		actAs(t, conn, home.org, person)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		tag, err := tx.Exec(ctx, sql, args...)
		return err == nil && tag.RowsAffected() == 1
	}
	retitle := `UPDATE page SET title = title || ' (sql)' WHERE id = $1`

	t.Run("the database lets somebody on the list edit, and nobody else", func(t *testing.T) {
		for _, tt := range []struct {
			who    string
			person uuid.UUID
			page   string
			want   bool
		}{
			{"bob on Top", bobID, top, true},
			{"bob below Top", bobID, low, true},
			{"gwen, a guest of the space, on Top", gwenID, top, true},
			{"bob on Sibling", bobID, sibling, false},
			{"carl on Top", carlID, top, false},
			{"bob past a view list", bobID, hidden, false},
			{"bob under an edit list he is not on", bobID, lockedLow, false},
			{"hal, a guest of another space", halID, top, false},
		} {
			if got := lets(tt.person, retitle, tt.page); got != tt.want {
				t.Errorf("%s: retitling let through %v, want %v", tt.who, got, tt.want)
			}
		}
		if !lets(bobID, `INSERT INTO page_version (org_id, page_id, number, title, body, created_by) SELECT org_id, id, version + 1, title, body, $2 FROM page WHERE id = $1`, low, bobID) {
			t.Error("bob may not publish a version of Low")
		}
		if !lets(bobID, `INSERT INTO attachment (org_id, page_id, file_name, size_bytes) VALUES ($1, $2, 'sql.txt', 1)`, home.org, low) {
			t.Error("bob may not attach to Low")
		}
		actAs(t, conn, home.org, bobID)
		denied(t, conn, "bob adds a page under Top", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by) VALUES ($1, (SELECT space_id FROM page WHERE id = $2), $2, 'W', 'Planted', $3)`, home.org, top, bobID)
		denied(t, conn, "bob moves Low with the database's own function", `SELECT page_place(ARRAY[$1::uuid], $2, ARRAY['W'])`, low, top)
		denied(t, conn, "bob reorders Low", `UPDATE page SET rank = 'zz' WHERE id = $1`, low)
		denied(t, conn, "bob trashes Low", `SELECT page_trash($1)`, low)
		denied(t, conn, "bob publishes Sibling", `INSERT INTO page_version (org_id, page_id, number, title, body, created_by) VALUES ($1, $2, 9, 'Bob''s', '{}', $3)`, home.org, sibling, bobID)
		untouched(t, conn, "bob retitles Hidden", retitle, hidden)
		var editable bool
		if err := conn.QueryRow(ctx, `SELECT perm_page_editable($1, $2)`, hidden, bobID).Scan(&editable); err != nil || editable {
			t.Errorf("the grant lets bob past Hidden's view list: %v %v", editable, err)
		}
	})

	t.Run("only the space's administrators change who else may edit", func(t *testing.T) {
		grantRow := `INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id) VALUES ($1, $2, 'editGrant', 'user', $3)`
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "carl, a reader, puts himself on Sibling's list", grantRow, home.org, sibling, carlID)
		actAs(t, conn, home.org, bobID)
		denied(t, conn, "bob, who edits Top through it, puts carl on its list", grantRow, home.org, top, carlID)
		untouched(t, conn, "bob takes gwen off Top's list", `DELETE FROM page_restriction WHERE page_id = $1 AND kind = 'editGrant' AND user_id = $2`, top, gwenID)
		actAs(t, conn, home.org, daveID)
		denied(t, conn, "dave, who adds pages, puts himself on Sibling's list", grantRow, home.org, sibling, carlID)
		untouched(t, conn, "dave lifts Top's list", `DELETE FROM page_restriction WHERE page_id = $1 AND kind = 'editGrant'`, top)
		if _, err := h.super.Exec(ctx, `INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id) VALUES ($1, $2, 'edit', 'user', $3)`, home.org, sibling, daveID); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		t.Cleanup(func() {
			h.cleanupExec(t, h.super, `DELETE FROM page_restriction WHERE page_id = $1 AND kind = 'edit'`, sibling)
		})
		denied(t, conn, "dave turns his edit entry into a grant", `UPDATE page_restriction SET kind = 'editGrant' WHERE page_id = $1 AND kind = 'edit'`, sibling)
		if !lets(annID, grantRow, home.org, sibling, carlID) {
			t.Error("ann, who administers the space, may not let carl edit Sibling")
		}
		actAs(t, conn, home.org, annID)
		denied(t, conn, "ann lets hal, a guest of another space, edit Sibling", grantRow, home.org, sibling, halID)
	})

	t.Run("the service and the database agree", func(t *testing.T) {
		people := map[string]uuid.UUID{"ann": annID, "bob": bobID, "carl": carlID, "dave": daveID, "gwen": gwenID, "hal": halID}
		pages := map[string]string{"Top": top, "Low": low, "Sibling": sibling, "Locked": locked, "Locked low": lockedLow, "Hidden": hidden}
		for name, id := range people {
			caller := api.as(t, id, home.org, slug)
			for title, page := range pages {
				r := caller.get(t, "/api/v1/pages/"+page)
				service := r.Status == http.StatusOK && obj(t, r, "page")["can"].(map[string]any)["edit"] == true
				var database bool
				actAs(t, conn, home.org, id)
				if err := conn.QueryRow(ctx, `SELECT perm_page_editable($1, $2)`, page, id).Scan(&database); err != nil {
					t.Fatal(err)
				}
				if service != database {
					t.Errorf("%s on %s: the service answers %v, the database %v", name, title, service, database)
				}
			}
		}
	})
}
