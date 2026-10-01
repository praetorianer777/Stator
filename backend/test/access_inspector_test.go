//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// The inspector explains what the database enforces: for every person and
// page its verdicts are what acting as that person straight through SQL
// gets, and the step it names as the reason is the grant or list that decides.
func TestTheInspectorAgreesWithTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "inspect")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, bobID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, bob, carl := api.as(t, annID, home.org, slug), api.as(t, bobID, home.org, slug), api.as(t, carlID, home.org, slug)
	writers := h.makeGroup(t, home.org, "Writers", bobID)

	docs := newTree(t, owner, "INSP", "Inspect")
	top := docs.add(docs.homeID, "Top")
	low := docs.add(top, "Low")
	open := docs.add(docs.homeID, "Open")
	want(t, owner.put(t, "/api/v1/spaces/INSP/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addComments"}},
		map[string]any{"subject": group(writers), "permissions": []any{"addPages", "delete"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}}), http.StatusOK, "INSP's grants")
	want(t, restrict(t, ann, top, []any{group(writers), user(annID)}, []any{user(annID)}), http.StatusOK, "ann restricts Top")
	draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": open, "title": "Draft"}), http.StatusCreated, "ann's unpublished page"), "page")["id"].(string)
	h.settle(t)

	inspect := func(c *client, page string, person uuid.UUID) perm.AccessReport {
		t.Helper()
		r := want(t, c.get(t, pagePath(page, "/access/", person.String())), http.StatusOK, "inspect")
		var out struct{ Access perm.AccessReport }
		if err := json.Unmarshal(r.Raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.Access
	}

	conn := appConn(t)
	ctx := context.Background()
	// does runs a statement as the person and rolls it back, answering
	// whether the database let it through and touch one row.
	does := func(person uuid.UUID, sql string, args ...any) bool {
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
	database := func(person uuid.UUID, page string) map[perm.Right]bool {
		var seen int
		actAs(t, conn, home.org, person)
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page WHERE id = $1`, page).Scan(&seen); err != nil {
			t.Fatal(err)
		}
		return map[perm.Right]bool{
			perm.RightView:    seen == 1,
			perm.RightEdit:    does(person, `UPDATE page SET title = title || ' (checked)' WHERE id = $1`, page),
			perm.RightDelete:  does(person, `SELECT page_trash($1)`, page),
			perm.RightComment: does(person, `INSERT INTO comment_thread (org_id, page_id, kind, created_by) VALUES ($1, $2, 'page', $3)`, home.org, page, person),
		}
	}
	people := map[string]uuid.UUID{"owner": home.user, "ann": annID, "bob": bobID, "carl": carlID}
	pages := map[string]string{"Home": docs.homeID, "Top": top, "Low": low, "Open": open, "Draft": draft}
	agree := func(t *testing.T) {
		for name, person := range people {
			for title, page := range pages {
				report := inspect(ann, page, person)
				got := database(person, page)
				for _, r := range report.Rights {
					// A check keeps the home page out of the trash whoever asks, so trashing it
					// says nothing about the right; the space goes as a whole instead.
					if r.Right == perm.RightDelete && page == docs.homeID {
						continue
					}
					if r.Allowed != got[r.Right] {
						t.Errorf("%s may %s %s: the inspector says %v, the database %v", name, r.Right, title, r.Allowed, got[r.Right])
					}
					if d := decider(r); (d == nil) != r.Allowed {
						t.Errorf("%s may %s %s is %v, but its steps say %+v", name, r.Right, title, r.Allowed, d)
					}
				}
			}
		}
	}

	t.Run("every verdict is the database's", agree)

	t.Run("each refusal names what decides it", func(t *testing.T) {
		lowCarl := inspect(ann, low, carlID)
		if d := decider(lowCarl.Rights[0]); d == nil || d.Kind != perm.StepList || d.Page == nil || d.Page.Title != "Top" || *d.List != perm.ListView || len(d.Listed) != 2 {
			t.Errorf("carl's view of Low is decided by %+v", d)
		}
		lowBob := inspect(ann, low, bobID)
		var through *perm.AccessStep
		for i, s := range lowBob.Rights[0].Steps {
			if s.Kind == perm.StepList {
				through = &lowBob.Rights[0].Steps[i]
			}
		}
		if through == nil || !through.Passed || through.Bypassed || len(through.Via) != 1 || through.Via[0].Type != perm.SubjectGroup || *through.Via[0].ID != writers {
			t.Errorf("bob's way through Top's view list is %+v", through)
		}
		if d := decider(lowBob.Rights[1]); d == nil || d.Kind != perm.StepList || *d.List != perm.ListEdit || d.Page.Title != "Top" {
			t.Errorf("bob's edit of Low is decided by %+v", d)
		}
		openCarl := inspect(ann, open, carlID)
		if d := decider(openCarl.Rights[1]); d == nil || d.Kind != perm.StepSpace || *d.Permission != perm.SpaceAddPages || len(d.Grants) != 0 {
			t.Errorf("carl's edit of Open is decided by %+v", d)
		}
		if s := openCarl.Rights[0].Steps[1]; s.Kind != perm.StepSpace || len(s.Grants) != 1 || s.Grants[0].Subject.Type != perm.SubjectEveryone {
			t.Errorf("carl views Open through %+v", s)
		}
		if d := decider(inspect(ann, draft, bobID).Rights[0]); d == nil || d.Kind != perm.StepUnpublished || d.Page.Title != "Draft" {
			t.Errorf("bob's view of ann's unpublished page is decided by %+v", d)
		}
		lowOwner := inspect(ann, low, home.user)
		if lowOwner.Role != "owner" || lowOwner.Rights[1].Steps[1].Kind != perm.StepOrgAdmin || !lowOwner.Rights[1].Steps[2].Bypassed {
			t.Errorf("the owner edits Low through %+v", lowOwner.Rights[1].Steps)
		}
		if again := inspect(owner, low, carlID); again.Rights[0].Allowed {
			t.Errorf("an organization administrator is told carl may view Low")
		}
	})

	t.Run("use granted to a group decides who uses the organization", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/org/permissions/use", map[string]any{"subjects": []any{group(writers), user(annID)}}), http.StatusOK, "use for Writers and ann")
		// Through the API rather than a cleanup, so the next reader waits for the replica to have it.
		defer func() {
			want(t, owner.put(t, "/api/v1/org/permissions/use", map[string]any{"subjects": []any{everyone}}), http.StatusOK, "use for everyone again")
		}()
		h.settle(t)
		if d := decider(inspect(ann, open, carlID).Rights[0]); d == nil || d.Kind != perm.StepUse {
			t.Errorf("carl's view of Open without use is decided by %+v", d)
		}
		use := inspect(ann, open, bobID).Rights[0].Steps[0]
		if use.Kind != perm.StepUse || !use.Passed || len(use.Via) != 1 || use.Via[0].Name != "Writers" {
			t.Errorf("bob uses the organization through %+v", use)
		}
		agree(t)
	})

	t.Run("only an administrator of the space inspects", func(t *testing.T) {
		if got := carl.get(t, pagePath(open, "/access/", bobID.String())); got.Status != http.StatusForbidden || errorCode(t, got) != "forbidden" {
			t.Errorf("a member inspects somebody: %d %s", got.Status, got.Raw)
		}
		if got := bob.get(t, pagePath(top, "/access/", bobID.String())); got.Status != http.StatusForbidden {
			t.Errorf("a member inspects himself: %d", got.Status)
		}
		if got := carl.get(t, pagePath(top, "/access/", bobID.String())); got.Status != http.StatusNotFound {
			t.Errorf("a page carl may not view is %d, not 404", got.Status)
		}
		if got := ann.get(t, pagePath(open, "/access/", uuid.NewString())); got.Status != http.StatusNotFound {
			t.Errorf("somebody who is not a member is %d, not 404", got.Status)
		}
		if got := api.anonymous().get(t, pagePath(open, "/access/", bobID.String())); got.Status != http.StatusUnauthorized {
			t.Errorf("nobody inspects: %d", got.Status)
		}
	})

	t.Run("the database names grants only to those who may inspect", func(t *testing.T) {
		sources := func(caller, person uuid.UUID) int {
			t.Helper()
			var n int
			actAs(t, conn, home.org, caller)
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM perm_global_grant_sources($1, 'use')`, person).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		if n := sources(carlID, bobID); n != 0 {
			t.Errorf("carl reads %d of the grants that reach bob", n)
		}
		if n := sources(carlID, carlID); n != 1 {
			t.Errorf("carl reads %d of the grants that reach himself", n)
		}
		if n := sources(annID, bobID); n != 1 {
			t.Errorf("ann, who administers a space, reads %d of the grants that reach bob", n)
		}
	})
}

// decider is the first step of a right not passed, the reason for a no.
func decider(r perm.AccessRight) *perm.AccessStep {
	for i := range r.Steps {
		if !r.Steps[i].Passed {
			return &r.Steps[i]
		}
	}
	return nil
}
