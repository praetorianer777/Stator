//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
)

// copyPath is the copy of another space's permissions onto key's, previewed
// with a GET and applied with a POST.
func copyPath(key, from, mode string) string {
	path := "/api/v1/spaces/" + key + "/permissions/copy"
	if from == "" && mode == "" {
		return path
	}
	return path + "?" + url.Values{"from": {from}, "mode": {mode}}.Encode()
}

// changesByName maps each subject a preview names to how it changes.
func changesByName(t *testing.T, preview map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range preview["changes"].([]any) {
		c := each.(map[string]any)
		out[c["subject"].(map[string]any)["name"].(string)] = c["kind"].(string)
	}
	return out
}

// grantRows lists a space's grants straight from the database, as "subject
// role" lines in order, reading without signing in as Anybody's view.
func (h *harness) grantRows(t *testing.T, ctx context.Context, space string) []string {
	t.Helper()
	rows, err := h.super.Query(ctx, `
		SELECT COALESCE(u.name, gr.name, '') || ' ' || a.role
		FROM role_assignment a LEFT JOIN app_user u ON u.id = a.user_id LEFT JOIN groups gr ON gr.id = a.group_id
		WHERE a.space_id = $1
		UNION ALL
		SELECT 'Anybody view' FROM space WHERE id = $1 AND anonymous_view`, space)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		out = append(out, line)
	}
	slices.Sort(out)
	return out
}

// Copying space permissions (#82): an administrator of both spaces previews
// what a copy would change, and applies exactly that preview in one step.
func TestSpacePermissionsAreCopiedAsPreviewed(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "permcopy")
	ctx := home.ctx
	owner := api.as(t, home.user)
	annID, bobID, carlID := h.namedPerson(t, "Ann Admin"), h.namedPerson(t, "Bob Writer"), h.namedPerson(t, "Carl Keeper")
	ann, bob, carl := api.as(t, annID), api.as(t, bobID), api.as(t, carlID)
	writers := h.makeGroup(t, "Writers", bobID)

	source := newTree(t, owner, "SRC", "Source")
	target := newTree(t, owner, "TGT", "Target")
	want(t, owner.setTable(t, "SRC", []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": group(writers), "permissions": []any{"view", "addPages", "addComments"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}), http.StatusOK, "SRC's table")
	want(t, owner.setTable(t, "TGT", []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments", "delete"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
		map[string]any{"subject": user(carlID), "permissions": []any{"administer"}},
		map[string]any{"subject": user(bobID), "permissions": []any{"view"}},
	}), http.StatusOK, "TGT's table")
	want(t, owner.put(t, "/api/v1/site/anonymous-access", map[string]any{"enabled": true, "indexable": false}), http.StatusOK, "let anybody read")
	want(t, owner.put(t, "/api/v1/spaces/SRC/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open SRC to anybody")
	var guests []uuid.UUID
	for _, invite := range []struct{ key, name string }{{"SRC", "Gwen"}, {"TGT", "Hank"}} {
		email := fmt.Sprintf("%s-%s@example.test", strings.ToLower(invite.name), uuid.NewString()[:8])
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
		made := obj(t, want(t, owner.post(t, "/api/v1/spaces/"+invite.key+"/guests", map[string]any{"email": email, "role": "viewer"}), http.StatusCreated, "invite "+invite.name), "guest")
		id := uuid.MustParse(made["userId"].(string))
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET name = $2 WHERE id = $1`, id, invite.name+" Guest"); err != nil {
			t.Fatal(err)
		}
		guests = append(guests, id)
	}
	gwenID := guests[0]
	personal := obj(t, want(t, ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANNP", "name": "Ann's own", "personal": true}), http.StatusCreated, "ann's personal space"), "space")
	h.settle(t)
	sourceID, targetID := source.spaceID(t), target.spaceID(t)

	t.Run("a replace preview lists every change and what is left behind", func(t *testing.T) {
		before := h.grantRows(t, ctx, targetID)
		preview := obj(t, want(t, ann.get(t, copyPath("TGT", "src", "replace")), http.StatusOK, "preview a replace"), "preview")
		if got := changesByName(t, preview); !sameMap(got, map[string]string{
			"Everyone": "narrowed", "Writers": "added", "Anybody": "added", "Carl Keeper": "removed", "Bob Writer": "removed",
		}) {
			t.Errorf("the replace changes %v", got)
		}
		skipped := preview["skipped"].([]any)
		if len(skipped) != 1 || skipped[0].(map[string]any)["reason"] != "guest" || skipped[0].(map[string]any)["subject"].(map[string]any)["name"] != "Gwen Guest" {
			t.Errorf("the replace skips %v", skipped)
		}
		kept := preview["kept"].([]any)
		if len(kept) != 1 || kept[0].(map[string]any)["subject"].(map[string]any)["name"] != "Hank Guest" {
			t.Errorf("the replace keeps %v", kept)
		}
		counts := preview["counts"].(map[string]any)
		if number(counts["added"]) != 2 || number(counts["narrowed"]) != 1 || number(counts["removed"]) != 2 || number(counts["skipped"]) != 1 || number(counts["unchanged"]) != 2 {
			t.Errorf("the counts are %v", counts)
		}
		if preview["leavesNoAdministrator"] != false || preview["fingerprint"] == "" || preview["source"].(map[string]any)["key"] != "SRC" {
			t.Errorf("the preview reads %v", preview)
		}
		if after := h.grantRows(t, ctx, targetID); !slices.Equal(before, after) {
			t.Errorf("a preview changed TGT from %v to %v", before, after)
		}
	})

	t.Run("a merge only adds and widens", func(t *testing.T) {
		preview := obj(t, want(t, ann.get(t, copyPath("TGT", "SRC", "merge")), http.StatusOK, "preview a merge"), "preview")
		if got := changesByName(t, preview); !sameMap(got, map[string]string{"Writers": "added", "Anybody": "added"}) {
			t.Errorf("the merge changes %v", got)
		}
		if len(preview["kept"].([]any)) != 0 {
			t.Errorf("a merge reports kept guests: %v", preview["kept"])
		}
	})

	t.Run("a preview or a copy is refused with a sentence", func(t *testing.T) {
		code := func(r response, status int, code, what string) {
			t.Helper()
			if got := errorCode(t, want(t, r, status, what)); got != code {
				t.Errorf("%s answers %s, want %s", what, got, code)
			}
		}
		code(carl.get(t, copyPath("TGT", "SRC", "replace")), http.StatusForbidden, "forbidden", "carl, who does not administer SRC")
		code(bob.get(t, copyPath("TGT", "SRC", "replace")), http.StatusForbidden, "forbidden", "bob, who administers neither")
		code(bob.post(t, copyPath("TGT", "", ""), map[string]any{"from": "SRC", "mode": "merge", "fingerprint": "x"}), http.StatusForbidden, "forbidden", "bob copies")
		fieldError(t, want(t, ann.get(t, copyPath("TGT", "", "replace")), http.StatusUnprocessableEntity, "no source"), "from")
		fieldError(t, want(t, ann.get(t, copyPath("TGT", "SRC", "swap")), http.StatusUnprocessableEntity, "no such mode"), "mode")
		fieldError(t, want(t, ann.get(t, copyPath("TGT", "TGT", "merge")), http.StatusUnprocessableEntity, "from itself"), "from")
		fieldError(t, want(t, ann.get(t, copyPath("TGT", "NOPE", "merge")), http.StatusUnprocessableEntity, "from nowhere"), "from")
		fieldError(t, want(t, ann.get(t, copyPath("TGT", "ANNP", "merge")), http.StatusUnprocessableEntity, "from a personal space"), "from")
		code(ann.get(t, copyPath("ANNP", "SRC", "merge")), http.StatusConflict, "personal_space", "into a personal space")
		fieldError(t, want(t, ann.post(t, copyPath("TGT", "", ""), map[string]any{"from": "SRC", "mode": "replace"}), http.StatusUnprocessableEntity, "no fingerprint"), "fingerprint")
		want(t, ann.get(t, copyPath("NOPE", "SRC", "merge")), http.StatusNotFound, "into nowhere")
		if personal["key"] != "ANNP" {
			t.Fatalf("ann's space is %v", personal)
		}
	})

	t.Run("a preview gone stale is refused and changes nothing", func(t *testing.T) {
		preview := obj(t, want(t, ann.get(t, copyPath("TGT", "SRC", "replace")), http.StatusOK, "preview"), "preview")
		want(t, owner.setTable(t, "SRC", []any{
			map[string]any{"subject": everyone, "permissions": []any{"view", "addComments"}},
			map[string]any{"subject": group(writers), "permissions": []any{"view", "addPages", "addComments"}},
			map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
			map[string]any{"subject": user(gwenID), "permissions": []any{"view"}},
		}), http.StatusOK, "SRC changes meanwhile")
		h.settle(t)
		before := h.grantRows(t, ctx, targetID)
		got := ann.post(t, copyPath("TGT", "", ""), map[string]any{"from": "SRC", "mode": "replace", "fingerprint": preview["fingerprint"]})
		if code := errorCode(t, want(t, got, http.StatusConflict, "apply the old preview")); code != "copy_changed" {
			t.Errorf("a stale preview answers %s", code)
		}
		if after := h.grantRows(t, ctx, targetID); !slices.Equal(before, after) {
			t.Errorf("a refused copy changed TGT from %v to %v", before, after)
		}
	})

	t.Run("a copy that leaves the space without an administrator is refused", func(t *testing.T) {
		newTree(t, owner, "BARE", "Bare")
		want(t, owner.setTable(t, "BARE", []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}), http.StatusOK, "BARE has no administrator of its own")
		h.settle(t)
		want(t, ann.get(t, copyPath("TGT", "BARE", "replace")), http.StatusForbidden, "ann does not administer BARE")
		preview := obj(t, want(t, owner.get(t, copyPath("TGT", "BARE", "replace")), http.StatusOK, "the owner previews BARE onto TGT"), "preview")
		if preview["leavesNoAdministrator"] != true {
			t.Fatalf("the preview does not warn: %v", preview)
		}
		got := owner.post(t, copyPath("TGT", "", ""), map[string]any{"from": "BARE", "mode": "replace", "fingerprint": preview["fingerprint"]})
		if code := errorCode(t, want(t, got, http.StatusConflict, "copy BARE onto TGT")); code != "no_administrator" {
			t.Errorf("leaving no administrator answers %s", code)
		}
	})

	t.Run("applying the preview copies it in one audited step", func(t *testing.T) {
		preview := obj(t, want(t, ann.get(t, copyPath("TGT", "SRC", "replace")), http.StatusOK, "preview again"), "preview")
		applied := want(t, ann.post(t, copyPath("TGT", "", ""), map[string]any{"from": "SRC", "mode": "replace", "fingerprint": preview["fingerprint"]}), http.StatusOK, "apply the replace")
		var names []string
		for _, each := range list(t, applied, "grants") {
			a := each.(map[string]any)
			name, _ := a["userName"].(string)
			if group, _ := a["groupName"].(string); group != "" {
				name = group
			}
			names = append(names, name+" "+a["role"].(string))
		}
		if !slices.Equal(names, []string{"Ann Admin space_administrator", "Everyone commenter", "Hank Guest viewer", "Writers view_add_pages_and_comment"}) {
			t.Errorf("the grants answered are %v", names)
		}
		wantRows := []string{"Ann Admin space_administrator", "Anybody view", "Everyone commenter", "Hank Guest viewer", "Writers view_add_pages_and_comment"}
		if got := h.grantRows(t, ctx, targetID); !slices.Equal(got, wantRows) {
			t.Errorf("TGT holds %v, want %v", got, wantRows)
		}
		data := h.recordedOnce(t, audit.ActionSpacePermissionsCopied, &annID, targetID)
		for _, part := range []string{`"mode": "replace"`, `"key": "SRC"`, sourceID, `"added": 2`, `"removed": 2`, `"skipped": 1`} {
			if !strings.Contains(data, part) {
				t.Errorf("the record %s lacks %s", data, part)
			}
		}
		h.settle(t)
		again := obj(t, want(t, ann.get(t, copyPath("TGT", "SRC", "replace")), http.StatusOK, "preview once more"), "preview")
		if len(again["changes"].([]any)) != 0 {
			t.Errorf("copying twice changes %v", again["changes"])
		}
		want(t, ann.post(t, copyPath("TGT", "", ""), map[string]any{"from": "SRC", "mode": "replace", "fingerprint": again["fingerprint"]}), http.StatusOK, "apply a copy that changes nothing")
		h.recordedOnce(t, audit.ActionSpacePermissionsCopied, &annID, targetID)
	})

	t.Run("straight through SQL the database holds a copy to the same rules", func(t *testing.T) {
		conn := appConn(t)
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		readSource := `SELECT count(*) FROM role_assignment WHERE space_id = $1`
		actAs(t, conn, annID)
		if count(readSource, sourceID) == 0 {
			t.Fatal("ann reads none of SRC's grants, so carl's zero would prove nothing")
		}
		actAs(t, conn, carlID)
		if n := count(readSource, sourceID); n != 0 {
			t.Errorf("carl, who does not administer SRC, reads %d of its grants", n)
		}
		actAs(t, conn, bobID)
		denied(t, conn, "bob granting in TGT", `INSERT INTO role_assignment (role, space_id, group_id) VALUES ('editor', $1, $2)`, targetID, writers)
		actAs(t, conn, annID)
		denied(t, conn, "SRC's guest copied into TGT", `INSERT INTO role_assignment (role, space_id, user_id) VALUES ('viewer', $1, $2)`, targetID, gwenID)
		denied(t, conn, "SRC's guest copied into ann's own space", `INSERT INTO role_assignment (role, space_id, user_id) VALUES ('viewer', $1, $2)`, personal["id"], gwenID)
		denied(t, conn, "anybody let into ann's own space", `UPDATE space SET anonymous_view = true WHERE id = $1`, personal["id"])
		denied(t, conn, "ann taking the last administrator from TGT", `DELETE FROM role_assignment WHERE space_id = $1 AND role = 'space_administrator'`, targetID)
		if n := h.count(t, ctx, `SELECT count(*) FROM role_assignment WHERE space_id = $1 AND role = 'space_administrator'`, targetID); n != 1 {
			t.Errorf("TGT has %d administrator grants after the refusal", n)
		}
		if _, err := conn.Exec(ctx, `
			WITH handed AS (INSERT INTO role_assignment (role, space_id, group_id) VALUES ('space_administrator', $1, $2))
			DELETE FROM role_assignment WHERE space_id = $1 AND role = 'space_administrator' AND user_id = $3`, targetID, writers, annID); err != nil {
			t.Errorf("ann may hand administering TGT on to the writers: %v", err)
		}
		// The installation's administrators hold every space through their
		// own role anyway, and may close one down to themselves.
		actAs(t, conn, home.user)
		if _, err := conn.Exec(ctx, `DELETE FROM role_assignment WHERE space_id = $1 AND role = 'space_administrator'`, targetID); err != nil {
			t.Errorf("the administrator may not take the last space administrator: %v", err)
		}
	})
}

// sameMap reports whether two string maps hold the same pairs.
func sameMap(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
