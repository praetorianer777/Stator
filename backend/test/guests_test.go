//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// containsText reports whether an answer mentions the words anywhere.
func containsText(raw []byte, words string) bool { return bytes.Contains(raw, []byte(words)) }

// personNames lists the names a list of people answers with.
func personNames(t *testing.T, items []any) []string {
	t.Helper()
	var out []string
	for _, each := range items {
		out = append(out, each.(map[string]any)["name"].(string))
	}
	slices.Sort(out)
	return out
}

// Guests (#69): an administrator lets somebody from outside into one space,
// and they reach that space and the people in it, and nothing else of the
// organization. Every refusal has the owner reaching the same thing first.
func TestAGuestReachesOneSpaceAndThePeopleInIt(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "guests")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.namedPerson(t, home.org, "Ina Insider")
	oscarID := h.namedPerson(t, home.org, "Oscar Outsider")
	ina, oscar := api.as(t, inaID, home.org, slug), api.as(t, oscarID, home.org, slug)

	join := newTree(t, owner, "JOIN", "Joint work")
	other := newTree(t, owner, "OTHR", "Other work")
	joinPage := join.add(join.homeID, "Join zebra")
	otherPage := other.add(other.homeID, "Other zebra")
	// Ina writes in the guest's space and Oscar only elsewhere, which is what
	// makes one a person of the space and the other not.
	(&tree{t: t, c: ina, key: "JOIN", homeID: join.homeID}).add(join.homeID, "Ina's notes")
	(&tree{t: t, c: oscar, key: "OTHR", homeID: other.homeID}).add(other.homeID, "Oscar's notes")
	want(t, owner.post(t, pagePath(otherPage, "/labels"), map[string]any{"name": "zebra"}), http.StatusOK, "label Other zebra")
	want(t, owner.post(t, pagePath(otherPage, "/comments"), map[string]any{"body": commentDoc("Zebra comment.")}), http.StatusCreated, "comment on Other zebra")
	want(t, owner.post(t, calendarsPath("OTHR"), map[string]any{"name": "Other team"}), http.StatusCreated, "a calendar of OTHR")
	var groupID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO groups (org_id, name) VALUES ($1, 'Staff') RETURNING id`, home.org).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, home.org, groupID, oscarID); err != nil {
		t.Fatal(err)
	}

	email := fmt.Sprintf("gwen-%s@example.test", uuid.NewString()[:8])
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	invitations := "/api/v1/spaces/JOIN/guests"

	t.Run("an invitation is refused with a sentence beside the field", func(t *testing.T) {
		fieldError(t, want(t, owner.post(t, invitations, map[string]any{"email": "gwen", "role": "viewer"}), http.StatusUnprocessableEntity, "no address"), "email")
		fieldError(t, want(t, owner.post(t, invitations, map[string]any{"email": email, "role": "admin"}), http.StatusUnprocessableEntity, "no such role"), "role")
		var oscarEmail string
		if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, oscarID).Scan(&oscarEmail); err != nil {
			t.Fatal(err)
		}
		fieldError(t, want(t, owner.post(t, invitations, map[string]any{"email": oscarEmail, "role": "viewer"}), http.StatusUnprocessableEntity, "a member already"), "email")
		want(t, oscar.post(t, invitations, map[string]any{"email": email, "role": "viewer"}), http.StatusForbidden, "a member invites")
		if n := h.count(t, home.ctx, `SELECT count(*) FROM app_user WHERE email = $1`, email); n != 0 {
			t.Errorf("a refused invitation left %d accounts", n)
		}
	})

	made := obj(t, want(t, owner.post(t, invitations, map[string]any{"email": " " + email + " ", "role": "editor"}), http.StatusCreated, "invite gwen"), "guest")
	guestID := uuid.MustParse(made["userId"].(string))
	if made["role"] != "editor" || made["email"] != email {
		t.Fatalf("the invitation answered %v", made)
	}
	h.settle(t)
	gwen := api.as(t, guestID, home.org, slug)

	t.Run("administrators see the guest, marked as one", func(t *testing.T) {
		guests := list(t, want(t, owner.get(t, invitations), http.StatusOK, "the space's guests"), "guests")
		if len(guests) != 1 || guests[0].(map[string]any)["userId"] != guestID.String() {
			t.Errorf("the space's guests are %v", guests)
		}
		found := false
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/users"), http.StatusOK, "the members"), "members") {
			m := each.(map[string]any)
			if m["userId"] == guestID.String() {
				found = true
				if m["role"] != "guest" || m["guestSpace"].(map[string]any)["key"] != "JOIN" {
					t.Errorf("the members list shows the guest as %v", m)
				}
			} else if m["guestSpace"] != nil {
				t.Errorf("a member is shown with a guest space: %v", m)
			}
		}
		if !found {
			t.Error("the members list leaves the guest out")
		}
		var role, space string
		if err := h.super.QueryRow(context.Background(), `
			SELECT data->>'role', data->>'space' FROM audit_log WHERE org_id = $1 AND target_id = $2 AND action = 'member.guest_invited'`,
			home.org, guestID).Scan(&role, &space); err != nil || role != "editor" || space != "JOIN" {
			t.Errorf("the audit log has %q in %q (%v)", role, space, err)
		}
	})

	t.Run("the guest knows where they belong", func(t *testing.T) {
		org := obj(t, want(t, gwen.get(t, "/api/v1/auth/me"), http.StatusOK, "who am I"), "organization")
		if org["role"] != "guest" || org["guestSpace"].(map[string]any)["key"] != "JOIN" {
			t.Errorf("the guest is told %v", org)
		}
		if got := obj(t, want(t, owner.get(t, "/api/v1/auth/me"), http.StatusOK, "the owner"), "organization"); got["guestSpace"] != nil {
			t.Errorf("the owner is told of a guest space: %v", got)
		}
	})

	t.Run("they work in their space", func(t *testing.T) {
		want(t, gwen.get(t, pagePath(joinPage)), http.StatusOK, "read a page of JOIN")
		want(t, gwen.post(t, "/api/v1/pages", map[string]any{"parentId": joinPage, "title": "Gwen's zebra", "publish": true}), http.StatusCreated, "write in JOIN")
		want(t, gwen.post(t, pagePath(joinPage, "/comments"), map[string]any{"body": commentDoc("Looks right.")}), http.StatusCreated, "comment in JOIN")
		if got := spaceKeysOf(t, want(t, gwen.get(t, "/api/v1/spaces"), http.StatusOK, "the spaces")); !slices.Equal(got, []string{"JOIN"}) {
			t.Errorf("the guest lists %v", got)
		}
	})

	t.Run("anything of another space is not found", func(t *testing.T) {
		want(t, owner.get(t, "/api/v1/spaces/OTHR"), http.StatusOK, "the owner reads OTHR")
		want(t, gwen.get(t, "/api/v1/spaces/OTHR"), http.StatusNotFound, "read OTHR")
		want(t, gwen.get(t, pagePath(otherPage)), http.StatusNotFound, "read a page of OTHR")
		if got := gwen.post(t, "/api/v1/pages", map[string]any{"parentId": otherPage, "title": "Planted", "publish": true}); got.Status < 400 {
			t.Errorf("the guest wrote into OTHR: %d %s", got.Status, got.Raw)
		}
		if got := gwen.get(t, calendarsPath("OTHR")); got.Status == http.StatusOK {
			t.Errorf("the guest reads OTHR's calendars: %s", got.Raw)
		}
		if got := gwen.get(t, "/api/v1/labels/zebra/pages"); got.Status == http.StatusOK && has(titlesOf(t, got, "pages"), "Other zebra") {
			t.Error("the guest finds Other zebra by its label")
		}
	})

	t.Run("search and the home feeds keep to the space", func(t *testing.T) {
		q := url.Values{"q": {"zebra"}}
		if got := hitTitles(t, searchFor(t, owner, q)); !has(got, "Other zebra") {
			t.Errorf("the owner finds %v", got)
		}
		if got := hitTitles(t, searchFor(t, gwen, q)); has(got, "Other zebra") || !has(got, "Join zebra") {
			t.Errorf("the guest finds %v", got)
		}
		for _, path := range []string{"/api/v1/home/updates", "/api/v1/home/edited"} {
			got := want(t, gwen.get(t, path), http.StatusOK, path)
			if containsText(got.Raw, "Other zebra") {
				t.Errorf("%s names Other zebra to the guest: %s", path, got.Raw)
			}
		}
	})

	t.Run("the organization as a whole is refused", func(t *testing.T) {
		for what, got := range map[string]response{
			"make a space":       gwen.post(t, "/api/v1/spaces", map[string]any{"key": "GWEN", "name": "Gwen"}),
			"a personal space":   gwen.post(t, "/api/v1/spaces", map[string]any{"name": "Gwen", "personal": true}),
			"list members":       gwen.get(t, "/api/v1/users"),
			"list tokens":        gwen.get(t, "/api/v1/tokens"),
			"make a token":       gwen.post(t, "/api/v1/tokens", map[string]any{"name": "mine"}),
			"read the audit log": gwen.get(t, "/api/v1/audit"),
			"invite a guest":     gwen.post(t, invitations, map[string]any{"email": "x@example.test", "role": "viewer"}),
		} {
			if got.Status != http.StatusForbidden || errorCode(t, got) != "guest" {
				t.Errorf("%s = %d %s, want 403 guest", what, got.Status, got.Raw)
			}
		}
		can := obj(t, want(t, gwen.get(t, "/api/v1/access/me"), http.StatusOK, "what the guest may do"), "can")
		if can["use"] != true || can["createSpace"] != false || can["administer"] != false {
			t.Errorf("the guest may %v, want use and nothing more", can)
		}
	})

	t.Run("they see the people of their space and no directory", func(t *testing.T) {
		everybody := personNames(t, list(t, want(t, owner.get(t, "/api/v1/people"), http.StatusOK, "the owner's people"), "people"))
		if !slices.Contains(everybody, "Oscar Outsider") {
			t.Fatalf("the owner's people are %v", everybody)
		}
		people := personNames(t, list(t, want(t, gwen.get(t, "/api/v1/people"), http.StatusOK, "the guest's people"), "people"))
		if slices.Contains(people, "Oscar Outsider") || !slices.Contains(people, "Ina Insider") || !slices.Contains(people, "Person of guests") {
			t.Errorf("the guest's people are %v", people)
		}
		if got := list(t, want(t, owner.get(t, "/api/v1/groups"), http.StatusOK, "the owner's groups"), "groups"); len(got) != 1 {
			t.Errorf("the owner's groups are %v", got)
		}
		if got := list(t, want(t, gwen.get(t, "/api/v1/groups"), http.StatusOK, "the guest's groups"), "groups"); len(got) != 0 {
			t.Errorf("the guest sees groups %v", got)
		}
	})

	t.Run("mentions offer and tell only the people of the space", func(t *testing.T) {
		offered := mentionable(t, gwen, joinPage, "")
		if _, ok := offered["Oscar Outsider"]; ok || !offered["Ina Insider"] {
			t.Errorf("the guest's mention picker offers %v", offered)
		}
		if _, ok := mentionable(t, owner, joinPage, "")["Oscar Outsider"]; !ok {
			t.Error("the owner's mention picker leaves Oscar out")
		}
		mine := obj(t, want(t, gwen.post(t, "/api/v1/pages", map[string]any{"parentId": joinPage, "title": "Gwen's plan", "publish": true}), http.StatusCreated, "gwen's plan"), "page")["id"].(string)
		publishBody(t, gwen, mine, mentionDoc("Over to you.", oscarID, inaID))
		var told []string
		if err := h.super.QueryRow(context.Background(), `
			SELECT COALESCE(array_agg(m), '{}') FROM outbox_event e, jsonb_array_elements_text(e.payload -> 'mentioned') AS m
			WHERE e.org_id = $1 AND e.payload ->> 'pageId' = $2`, home.org, mine).Scan(&told); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(told, oscarID.String()) || !slices.Contains(told, inaID.String()) {
			t.Errorf("the guest's publish tells %v", told)
		}
	})

	t.Run("an assistant acting for the guest keeps to the space", func(t *testing.T) {
		m := &mcpSession{c: gwen}
		tools := m.tools(t)
		if tools["list_audit_log"] || !tools["search"] || !tools["list_people"] || !tools["create_page"] {
			t.Errorf("the guest is offered %v", tools)
		}
		res := m.call(t, "search", map[string]any{"q": "zebra"})
		if res["isError"] == true || containsText([]byte(toolText(res)), "Other zebra") {
			t.Errorf("an assistant's search for the guest answers %v", res)
		}
		res = m.call(t, "list_people", map[string]any{})
		if res["isError"] == true || containsText([]byte(toolText(res)), "Oscar Outsider") || !containsText([]byte(toolText(res)), "Ina Insider") {
			t.Errorf("an assistant lists people for the guest as %v", res)
		}
		if res := m.call(t, "get_page", map[string]any{"pageID": otherPage}); res["isError"] != true {
			t.Errorf("an assistant read OTHR for the guest: %v", res)
		}
		if res := m.call(t, "list_audit_log", map[string]any{}); res["isError"] != true {
			t.Errorf("an assistant read the audit log for the guest: %v", res)
		}
		structured(t, m.call(t, "get_page", map[string]any{"pageID": joinPage}), "page")
	})

	t.Run("a second space is refused, by the service and the database", func(t *testing.T) {
		fieldError(t, want(t, owner.post(t, "/api/v1/spaces/OTHR/guests", map[string]any{"email": email, "role": "viewer"}), http.StatusUnprocessableEntity, "gwen into OTHR"), "email")
		grants := []any{
			map[string]any{"subject": map[string]any{"type": "everyone"}, "permissions": []string{"view"}},
			map[string]any{"subject": user(home.user), "permissions": []string{"administer"}},
			map[string]any{"subject": user(guestID), "permissions": []string{"view"}},
		}
		fieldError(t, want(t, owner.put(t, "/api/v1/spaces/OTHR/permissions", map[string]any{"grants": grants}), http.StatusUnprocessableEntity, "gwen in OTHR's table"), "grants")
		administer := []any{
			map[string]any{"subject": map[string]any{"type": "everyone"}, "permissions": []string{"view", "addPages", "addComments", "delete"}},
			map[string]any{"subject": user(home.user), "permissions": []string{"administer"}},
			map[string]any{"subject": user(guestID), "permissions": []string{"administer"}},
		}
		fieldError(t, want(t, owner.put(t, "/api/v1/spaces/JOIN/permissions", map[string]any{"grants": administer}), http.StatusUnprocessableEntity, "gwen administers JOIN"), "grants")
		want(t, gwen.get(t, pagePath(joinPage)), http.StatusOK, "the refused tables left gwen as she was")
	})

	t.Run("straight through SQL the database holds the guest too", func(t *testing.T) {
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
		var strangerID uuid.UUID
		if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, 'A stranger') RETURNING id`,
			fmt.Sprintf("stranger-%s@example.test", uuid.NewString()[:8])).Scan(&strangerID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, strangerID) })
		if _, err := h.super.Exec(ctx, `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, home.org, strangerID); err != nil {
			t.Fatal(err)
		}
		otherID := other.spaceID(t)
		rows := map[string]string{
			"a page of OTHR":              `SELECT count(*) FROM page WHERE id = '` + otherPage + `'`,
			"OTHR itself":                 `SELECT count(*) FROM space WHERE key = 'OTHR'`,
			"the versions of Other zebra": `SELECT count(*) FROM page_version WHERE page_id = '` + otherPage + `'`,
			"Other zebra's comments":      `SELECT count(*) FROM comment WHERE page_id = '` + otherPage + `'`,
			"Other zebra's labels":        `SELECT count(*) FROM page_label WHERE page_id = '` + otherPage + `'`,
			"OTHR's calendars":            `SELECT count(*) FROM calendar c JOIN space s ON s.id = c.space_id WHERE s.key = 'OTHR'`,
			"Other zebra in search":       `SELECT count(*) FROM page WHERE search_vector @@ websearch_to_tsquery('simple', 'zebra') AND id = '` + otherPage + `'`,
			"Oscar":                       `SELECT count(*) FROM app_user WHERE id = '` + oscarID.String() + `'`,
			"Oscar's membership":          `SELECT count(*) FROM org_member WHERE user_id = '` + oscarID.String() + `'`,
			"the groups":                  `SELECT count(*) FROM groups`,
			"the group members":           `SELECT count(*) FROM group_member`,
			"the requests to join":        `SELECT count(*) FROM org_join_request`,
			"OTHR in the home feed":       `SELECT count(*) FROM home_edited(NULL, NULL, 100) WHERE page_id = '` + otherPage + `'`,
		}
		actAs(t, conn, home.org, home.user)
		for what, sql := range rows {
			if count(sql) == 0 {
				t.Fatalf("the owner reads no rows of %s, so the guest's zero would prove nothing", what)
			}
		}
		actAs(t, conn, home.org, guestID)
		for what, sql := range rows {
			if n := count(sql); n != 0 {
				t.Errorf("the guest reads %d rows of %s", n, what)
			}
		}
		for what, sql := range map[string]string{
			"Join zebra":      `SELECT count(*) FROM page WHERE id = '` + joinPage + `'`,
			"Ina":             `SELECT count(*) FROM app_user WHERE id = '` + inaID.String() + `'`,
			"themselves":      `SELECT count(*) FROM app_user WHERE id = '` + guestID.String() + `'`,
			"the owner":       `SELECT count(*) FROM org_member WHERE user_id = '` + home.user.String() + `'`,
			"their own space": `SELECT count(*) FROM space WHERE key = 'JOIN'`,
		} {
			if count(sql) != 1 {
				t.Errorf("the guest does not read %s", what)
			}
		}
		denied(t, conn, "a page in OTHR", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by)
			VALUES ($1, $2, $3, 'W', 'Planted', $4)`, home.org, otherID, other.homeID, guestID)
		untouched(t, conn, "retitling Other zebra", `UPDATE page SET title = 'Taken' WHERE id = $1`, otherPage)
		denied(t, conn, "a space of their own", `INSERT INTO space (org_id, key, name, created_by, owner_id) VALUES ($1, 'GWEN', 'Gwen', $2, $2)`, home.org, guestID)
		denied(t, conn, "a team space", `INSERT INTO space (org_id, key, name, created_by) VALUES ($1, 'GWENS', 'Gwens', $2)`, home.org, guestID)
		denied(t, conn, "a token", `INSERT INTO api_token (org_id, user_id, name, token_hash) VALUES ($1, $2, 'planted', $3)`, home.org, guestID, []byte(uuid.NewString()))
		denied(t, conn, "a grant of OTHR to themselves", `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
			VALUES ($1, $2, 'view', 'user', $3)`, home.org, otherID, guestID)
		denied(t, conn, "a mention of Oscar", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'page.published', $2)`,
			home.org, map[string]any{"actorId": guestID, "pageId": joinPage, "mentioned": []uuid.UUID{oscarID}})
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'page.published', $2)`,
			home.org, map[string]any{"actorId": guestID, "pageId": joinPage, "mentioned": []uuid.UUID{inaID}}); err != nil {
			t.Errorf("a mention of Ina was refused: %v", err)
		}
		_ = tx.Rollback(ctx)

		// The owner administers everything, and still cannot widen a guest.
		actAs(t, conn, home.org, home.user)
		denied(t, conn, "granting OTHR to the guest", `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
			SELECT $1, id, 'view', 'user', $2 FROM space WHERE key = 'OTHR'`, home.org, guestID)
		denied(t, conn, "the guest administering JOIN", `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
			SELECT $1, id, 'administer', 'user', $2 FROM space WHERE key = 'JOIN'`, home.org, guestID)
		denied(t, conn, "the guest in a group", `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, home.org, groupID, guestID)
		denied(t, conn, "the guest made a member", `UPDATE org_member SET org_role = 'member', guest_space_id = NULL WHERE user_id = $1`, guestID)
		denied(t, conn, "the guest moved to OTHR", `UPDATE org_member SET guest_space_id = (SELECT id FROM space WHERE key = 'OTHR') WHERE user_id = $1`, guestID)
		denied(t, conn, "a guest without a space", `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'guest')`, home.org, uuid.New())

		// A member is not an administrator, and makes or removes no guest.
		actAs(t, conn, home.org, oscarID)
		denied(t, conn, "a member making a guest", `INSERT INTO org_member (org_id, user_id, org_role, guest_space_id)
			SELECT $1, $2, 'guest', id FROM space WHERE key = 'OTHR'`, home.org, inaID)
		untouched(t, conn, "a member removing the guest", `DELETE FROM org_member WHERE user_id = $1`, guestID)
	})

	t.Run("removing the guest ends their reach at once", func(t *testing.T) {
		want(t, owner.delete(t, invitations+"/"+inaID.String()), http.StatusNotFound, "ina is no guest")
		want(t, owner.delete(t, invitations+"/"+guestID.String()), http.StatusNoContent, "remove gwen")
		if got := want(t, gwen.get(t, "/api/v1/auth/me"), http.StatusOK, "who am I now"); got.Body["organization"] != nil {
			t.Errorf("the removed guest still acts in %v", got.Body["organization"])
		}
		if got := gwen.get(t, pagePath(joinPage)); got.Status == http.StatusOK {
			t.Errorf("the removed guest still reads JOIN: %s", got.Raw)
		}
		var role string
		if err := h.super.QueryRow(context.Background(), `
			SELECT data->>'role' FROM audit_log WHERE org_id = $1 AND target_id = $2 AND action = 'member.removed'`,
			home.org, guestID).Scan(&role); err != nil || role != "guest" {
			t.Errorf("the audit log has %q (%v)", role, err)
		}
		if n := h.count(t, home.ctx, `SELECT count(*) FROM space_grant WHERE user_id = $1`, guestID); n != 0 {
			t.Errorf("the guest's grants outlived them: %d", n)
		}
	})
}

// Deleting the space a guest belongs to takes them out with it.
func TestAGuestGoesWithTheirSpace(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "guest-space")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	newTree(t, owner, "GONE", "Going")
	email := fmt.Sprintf("gus-%s@example.test", uuid.NewString()[:8])
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	made := obj(t, want(t, owner.post(t, "/api/v1/spaces/GONE/guests", map[string]any{"email": email, "role": "viewer"}), http.StatusCreated, "invite gus"), "guest")
	if made["role"] != "viewer" {
		t.Errorf("gus is %v", made)
	}
	want(t, owner.delete(t, "/api/v1/spaces/GONE"), http.StatusNoContent, "delete GONE")
	if n := h.count(t, home.ctx, `SELECT count(*) FROM org_member WHERE user_id = $1`, uuid.MustParse(made["userId"].(string))); n != 0 {
		t.Errorf("the guest outlived their space: %d memberships", n)
	}
}

// A guest signs in through the organization's provider like anybody, and
// stays a guest of their space: a group mapped to a role does not promote
// them, and no group of the provider's takes them in.
func TestAGuestSignsInAsAGuestWhateverTheirGroups(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "guest-sso")
	configureProvider(t, a, org, kc.issuer(), true)
	ctx := context.Background()
	var spaceID, userID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO space (org_id, key, name) VALUES ($1, 'VISIT', 'Visit') RETURNING id`, org.ID).Scan(&spaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'stator-administrators', 'admin')`, org.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `
		INSERT INTO app_user (email, name) VALUES ('alice@stator.test', 'alice')
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role, guest_space_id) VALUES ($1, $2, 'guest', $3)`, org.ID, userID, spaceID); err != nil {
		t.Fatal(err)
	}

	b := newBrowser(t)
	b.signIn(t, a, org.Slug, "alice", alicePassword, "/")
	status, me := b.me(t, a)
	current, _ := me["organization"].(map[string]any)
	if status != http.StatusOK || current["role"] != "guest" {
		t.Fatalf("/auth/me = %d %v, want a guest", status, me)
	}
	if space, _ := current["guestSpace"].(map[string]any); space["key"] != "VISIT" {
		t.Errorf("the guest lands in %v, want VISIT", current["guestSpace"])
	}
	if got := h.providerGroups(t, org, "alice@stator.test"); len(got) != 0 {
		t.Errorf("the guest joined the provider's groups %q", got)
	}
}
