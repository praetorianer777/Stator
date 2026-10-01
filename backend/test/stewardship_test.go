//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// lapse makes a page's verification run out a minute ago, as the superuser
// with triggers off, since the database stamps every verification with now.
func (h *harness) lapse(t *testing.T, pageID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE page_verification SET verified_at = now() - interval '31 days', expires_at = now() - interval '1 minute'
		WHERE page_id = $1`, pageID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("make the verification of %s run out: %d rows, %v", pageID, tag.RowsAffected(), err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

func pageActions(t *testing.T, h *harness, org uuid.UUID, pageID string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT action FROM audit_log WHERE org_id = $1 AND target_type = 'page' AND target_id = $2 ORDER BY created_at, id`, org, pageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// Every owner and verification operation, done once and refused once: who may
// name an owner and verify, who may be named, and where the badge shows.
func TestOwnersAndVerificationOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stewards")
	slug := h.slugOf(t, org.org)
	admin := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Reader"), h.namedPerson(t, org.org, "Ben Editor"), h.namedPerson(t, org.org, "Carl Outsider")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)
	nobody := api.anonymous()

	docs := newTree(t, admin, "STW", "Stewards")
	runbook := docs.add(docs.homeID, "Runbook stewarded")
	secret := docs.add(docs.homeID, "Secret plans")
	want(t, restrict(t, admin, runbook, nil, []any{user(org.user), user(benID)}), http.StatusOK, "only ben and the admin edit the runbook")
	want(t, restrict(t, admin, secret, []any{user(org.user), user(benID)}, nil), http.StatusOK, "only ben and the admin see the plans")
	h.settle(t)

	t.Run("an editor names an owner who may view the page", func(t *testing.T) {
		got := obj(t, want(t, ben.put(t, pagePath(runbook, "/owner"), map[string]any{"userId": annID}), http.StatusOK, "ben names ann"), "owner")
		if got["id"] != annID.String() || got["name"] != "Ann Reader" || got["canView"] != true {
			t.Errorf("the owner reads %v", got)
		}
		p := obj(t, want(t, ann.get(t, pagePath(runbook)), http.StatusOK, "ann reads the runbook"), "page")
		if o, _ := p["owner"].(map[string]any); o == nil || o["id"] != annID.String() || o["canView"] != true {
			t.Errorf("the page shows the owner %v", p["owner"])
		}
		want(t, ben.put(t, pagePath(runbook, "/owner"), map[string]any{"userId": benID}), http.StatusOK, "ben takes it over")
		want(t, ben.put(t, pagePath(runbook, "/owner"), map[string]any{"userId": annID}), http.StatusOK, "and hands it back")

		r := want(t, ben.put(t, pagePath(secret, "/owner"), map[string]any{"userId": carlID}), http.StatusUnprocessableEntity, "carl may not view the plans")
		if fields, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any); fields["userId"] == nil {
			t.Errorf("the refusal does not name the field: %v", r.Body)
		}
		want(t, ben.put(t, pagePath(secret, "/owner"), map[string]any{"userId": uuid.New()}), http.StatusUnprocessableEntity, "a stranger")
	})

	t.Run("an editor verifies for a term, and the badge shows everywhere", func(t *testing.T) {
		before := time.Now()
		v := obj(t, want(t, ben.put(t, pagePath(runbook, "/verification"), map[string]any{"days": 30}), http.StatusOK, "ben verifies"), "verification")
		expires, err := time.Parse(time.RFC3339Nano, v["expiresAt"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if v["status"] != "verified" || v["verifiedByName"] != "Ben Editor" || number(v["version"]) != 1 ||
			expires.Before(before.Add(29*24*time.Hour)) || expires.After(time.Now().Add(31*24*time.Hour)) {
			t.Errorf("the verification reads %v", v)
		}
		v = obj(t, want(t, ben.put(t, pagePath(runbook, "/verification"), map[string]any{}), http.StatusOK, "ben verifies with no term"), "verification")
		if expires, _ := time.Parse(time.RFC3339Nano, v["expiresAt"].(string)); expires.Before(time.Now().Add((page.DefaultVerifyDays - 1) * 24 * time.Hour)) {
			t.Errorf("a verification with no term runs to %v", v["expiresAt"])
		}
		want(t, ben.put(t, pagePath(runbook, "/verification"), map[string]any{"days": page.MaxVerifyDays + 1}), http.StatusUnprocessableEntity, "too long a term")
		want(t, ben.put(t, pagePath(runbook, "/verification"), map[string]any{"days": -3}), http.StatusUnprocessableEntity, "a term in the past")
		h.settle(t)

		p := obj(t, want(t, ann.get(t, pagePath(runbook)), http.StatusOK, "ann reads the runbook"), "page")
		if v, _ := p["verification"].(map[string]any); v == nil || v["status"] != "verified" {
			t.Errorf("the page shows the verification %v", p["verification"])
		}
		hits := list(t, searchFor(t, ann, url.Values{"q": {"stewarded"}}), "hits")
		if len(hits) != 1 || hits[0].(map[string]any)["verified"] != true {
			t.Errorf("search shows %v", hits)
		}
		if hits := list(t, searchFor(t, ann, url.Values{"q": {"plans"}}), "hits"); len(hits) != 0 {
			t.Errorf("ann finds the plans: %v", hits)
		}
		for _, each := range list(t, want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates"), "updates") {
			u := each.(map[string]any)
			if wantVerified := u["id"] == runbook; u["verified"] != wantVerified {
				t.Errorf("the update of %v says verified %v", u["title"], u["verified"])
			}
		}
	})

	t.Run("an edit keeps the verification, which names the version it checked", func(t *testing.T) {
		publishDraft(t, ben, runbook, "Runbook stewarded", "Changed after the check.", false)
		h.settle(t)
		p := obj(t, want(t, ann.get(t, pagePath(runbook)), http.StatusOK, "ann reads the runbook"), "page")
		v, _ := p["verification"].(map[string]any)
		if v == nil || v["status"] != "verified" || number(v["version"]) != 1 || number(p["version"]) != 2 {
			t.Errorf("after an edit the page is version %v with the verification %v", p["version"], v)
		}
		v = obj(t, want(t, ben.put(t, pagePath(runbook, "/verification"), map[string]any{"days": 7}), http.StatusOK, "ben checks it again"), "verification")
		if number(v["version"]) != 2 {
			t.Errorf("a new check is of version %v", v["version"])
		}
	})

	t.Run("a reader may neither name an owner nor verify", func(t *testing.T) {
		for what, r := range map[string]response{
			"name an owner":  ann.put(t, pagePath(runbook, "/owner"), map[string]any{"userId": annID}),
			"remove it":      ann.delete(t, pagePath(runbook, "/owner")),
			"verify":         ann.put(t, pagePath(runbook, "/verification"), map[string]any{"days": 30}),
			"take it away":   ann.delete(t, pagePath(runbook, "/verification")),
			"verify unseen":  ann.put(t, pagePath(secret, "/verification"), map[string]any{}),
			"owner unseen":   ann.put(t, pagePath(secret, "/owner"), map[string]any{"userId": annID}),
			"nobody":         nobody.put(t, pagePath(runbook, "/verification"), map[string]any{}),
			"nobody removes": nobody.delete(t, pagePath(runbook, "/owner")),
		} {
			if r.Status < 400 || r.Status >= 500 {
				t.Errorf("ann may %s: %d", what, r.Status)
				continue
			}
			errorCode(t, r)
		}
		want(t, ann.put(t, pagePath(runbook, "/owner"), map[string]any{"userId": annID}), http.StatusForbidden, "ann names herself")
		want(t, ann.put(t, pagePath(secret, "/verification"), map[string]any{}), http.StatusNotFound, "ann verifies what she may not view")
		missing := uuid.NewString()
		want(t, ben.put(t, pagePath(missing, "/verification"), map[string]any{}), http.StatusNotFound, "no such page")
		want(t, ben.delete(t, pagePath(missing, "/owner")), http.StatusNotFound, "no such page to leave without an owner")
	})

	t.Run("a page nobody else has read takes neither", func(t *testing.T) {
		sketch := obj(t, want(t, ben.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Sketch"}), http.StatusCreated, "ben's unpublished page"), "page")["id"].(string)
		want(t, ben.put(t, pagePath(sketch, "/verification"), map[string]any{}), http.StatusConflict, "verifying an unpublished page")
		want(t, ben.put(t, pagePath(sketch, "/owner"), map[string]any{"userId": benID}), http.StatusConflict, "an owner of an unpublished page")
	})

	t.Run("taking them away, twice, and the audit log keeps each act", func(t *testing.T) {
		want(t, ben.delete(t, pagePath(runbook, "/verification")), http.StatusNoContent, "ben takes the verification away")
		want(t, ben.delete(t, pagePath(runbook, "/verification")), http.StatusNoContent, "and again")
		want(t, admin.delete(t, pagePath(runbook, "/owner")), http.StatusNoContent, "the admin leaves it without an owner")
		want(t, admin.delete(t, pagePath(runbook, "/owner")), http.StatusNoContent, "and again")
		h.settle(t)
		p := obj(t, want(t, ann.get(t, pagePath(runbook)), http.StatusOK, "ann reads the runbook"), "page")
		if p["owner"] != nil || p["verification"] != nil {
			t.Errorf("the page still shows %v and %v", p["owner"], p["verification"])
		}
		got := pageActions(t, h, org.org, runbook)
		wanted := []string{
			audit.ActionPageRestrictionsSet,
			audit.ActionPageOwnerSet, audit.ActionPageOwnerSet, audit.ActionPageOwnerSet,
			audit.ActionPageVerified, audit.ActionPageVerified, audit.ActionPageVerified,
			audit.ActionPageUnverified, audit.ActionPageOwnerRemoved,
		}
		sameList(t, "the runbook's audit log", got, wanted...)
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2 AND actor_user_id = $3 AND data->>'owner' = $4`,
			org.org, audit.ActionPageOwnerSet, benID, annID.String()); n != 2 {
			t.Errorf("%d entries say ben named ann", n)
		}
	})
}

// A term that runs out is noticed by the worker and told once, to the owner
// while they may view the page and to whoever verified it once they may not.
func TestAVerificationThatRunsOutTellsTheOwner(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "lapses")
	slug := h.slugOf(t, org.org)
	admin := api.as(t, org.user, org.org, slug)
	annID, benID := h.namedPerson(t, org.org, "Ann Owner"), h.namedPerson(t, org.org, "Ben Verifier")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)
	h.runWorker(t, notify.NewFanOut(h.cluster, testMailer(t), testAppURL, discard()))
	watch := page.NewLapseWatch(h.cluster, discard(), time.Hour)
	ctx := context.Background()

	docs := newTree(t, admin, "LPS", "Lapses")
	guide := docs.add(docs.homeID, "Guide")
	want(t, ben.put(t, pagePath(guide, "/owner"), map[string]any{"userId": annID}), http.StatusOK, "ann owns the guide")
	want(t, ben.put(t, pagePath(guide, "/verification"), map[string]any{"days": 30}), http.StatusOK, "ben verifies it")

	expiredOf := func(c *client) []map[string]any {
		var out []map[string]any
		for _, n := range notificationsOf(t, c, "") {
			if n["kind"] == string(notify.KindExpired) {
				out = append(out, n)
			}
		}
		return out
	}
	lapsedEvents := func() int {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2 AND payload->>'pageId' = $3`,
			org.org, events.TopicVerificationLapsed, guide)
	}

	t.Run("the page says it ran out at once, and the owner is told once", func(t *testing.T) {
		h.lapse(t, guide)
		v := obj(t, want(t, ann.get(t, pagePath(guide)), http.StatusOK, "ann reads the guide"), "page")["verification"].(map[string]any)
		if v["status"] != "expired" {
			t.Errorf("a verification that ran out reads %v", v)
		}
		if _, err := watch.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := watch.Once(ctx); err != nil {
			t.Fatal(err)
		}
		h.drained(t, org.org)
		if n := lapsedEvents(); n != 1 {
			t.Errorf("the lapse was noted %d times", n)
		}
		told := expiredOf(ann)
		if len(told) != 1 || told[0]["page"].(map[string]any)["id"] != guide || told[0]["actorId"] != nil || number(told[0]["version"]) != 1 {
			t.Errorf("ann was told %v", told)
		}
		if got := expiredOf(ben); len(got) != 0 {
			t.Errorf("ben, who is not the owner, was told %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_verification WHERE page_id = $1 AND lapse_noticed_at IS NOT NULL`, guide); n != 1 {
			t.Errorf("the lapse is not marked as noticed")
		}
	})

	t.Run("verifying again starts a new term and clears the notice", func(t *testing.T) {
		v := obj(t, want(t, ben.put(t, pagePath(guide, "/verification"), map[string]any{"days": 30}), http.StatusOK, "ben verifies again"), "verification")
		if v["status"] != "verified" {
			t.Errorf("a renewed verification reads %v", v)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_verification WHERE page_id = $1 AND lapse_noticed_at IS NULL`, guide); n != 1 {
			t.Errorf("the notice of the last lapse was kept")
		}
		if n, err := watch.Once(ctx); err != nil || n != 0 {
			t.Errorf("a verification still running was noted: %d, %v", n, err)
		}
	})

	t.Run("an owner who lost access is flagged, and the verifier is told instead", func(t *testing.T) {
		want(t, restrict(t, admin, guide, []any{user(org.user), user(benID)}, nil), http.StatusOK, "ann may no longer view the guide")
		h.settle(t)
		p := obj(t, want(t, ben.get(t, pagePath(guide)), http.StatusOK, "ben reads the guide"), "page")
		if o, _ := p["owner"].(map[string]any); o == nil || o["id"] != annID.String() || o["canView"] != false {
			t.Errorf("an owner who lost access reads %v", p["owner"])
		}
		h.lapse(t, guide)
		if _, err := watch.Once(ctx); err != nil {
			t.Fatal(err)
		}
		h.drained(t, org.org)
		if n := lapsedEvents(); n != 2 {
			t.Errorf("%d lapses were noted, want 2", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND kind = 'expired'`, annID); n != 1 {
			t.Errorf("ann was told %d times, once after she lost access", n)
		}
		told := expiredOf(ben)
		if len(told) != 1 || told[0]["page"].(map[string]any)["id"] != guide {
			t.Errorf("ben was told %v", told)
		}
	})

	t.Run("an owner who leaves the organization leaves the page without one", func(t *testing.T) {
		want(t, admin.delete(t, "/api/v1/users/"+annID.String()), http.StatusNoContent, "ann leaves")
		h.settle(t)
		if p := obj(t, want(t, ben.get(t, pagePath(guide)), http.StatusOK, "ben reads the guide"), "page"); p["owner"] != nil {
			t.Errorf("the guide still names %v", p["owner"])
		}
	})
}

// Straight through SQL as stator_app: only an editor writes owners and
// verifications, in their own name, of somebody who may view the page, and
// never the worker's notice or a backdated check.
func TestStewardshipIsWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stewards-rls")
	other := h.makeMember(t, "stewards-rls-other")
	slug := h.slugOf(t, org.org)
	admin := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member")
	ben := api.as(t, benID, org.org, slug)

	docs := newTree(t, admin, "SWL", "Walls")
	open := docs.add(docs.homeID, "Open")
	secret := docs.add(docs.homeID, "Secret")
	sketch := obj(t, want(t, ben.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Sketch"}), http.StatusCreated, "ben's unpublished page"), "page")["id"].(string)
	want(t, restrict(t, admin, open, nil, []any{user(org.user), user(benID)}), http.StatusOK, "ann reads Open and may not edit it")
	want(t, restrict(t, admin, secret, []any{user(org.user), user(benID)}, nil), http.StatusOK, "carl and ann may not view Secret")
	want(t, ben.put(t, pagePath(secret, "/owner"), map[string]any{"userId": benID}), http.StatusOK, "ben owns Secret")
	want(t, ben.put(t, pagePath(secret, "/verification"), map[string]any{"days": 30}), http.StatusOK, "ben verifies Secret")
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

	t.Run("a reader writes neither", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		denied(t, conn, "ann names herself owner", `INSERT INTO page_owner (org_id, page_id, user_id, set_by) VALUES ($1, $2, $3, $3)`, org.org, open, annID)
		denied(t, conn, "ann verifies", `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() + interval '1 day')`, org.org, open, annID)
		untouched(t, conn, "ann takes the owner of Secret away", `DELETE FROM page_owner WHERE page_id = $1`, secret)
		untouched(t, conn, "ann takes the verification of Secret away", `DELETE FROM page_verification WHERE page_id = $1`, secret)
		untouched(t, conn, "ann renews Secret's verification", `UPDATE page_verification SET expires_at = now() + interval '2 days' WHERE page_id = $1`, secret)
		if n := count(`SELECT count(*) FROM page_owner WHERE page_id = $1`, secret) + count(`SELECT count(*) FROM page_verification WHERE page_id = $1`, secret); n != 0 {
			t.Errorf("ann reads %d rows about a page she may not view", n)
		}
	})

	t.Run("an editor writes in their own name, for somebody who may view the page", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		denied(t, conn, "carl, who may not view Secret, as its owner", `UPDATE page_owner SET user_id = $2, set_by = $3 WHERE page_id = $1`, secret, carlID, benID)
		denied(t, conn, "an owner named in ann's name", `INSERT INTO page_owner (org_id, page_id, user_id, set_by) VALUES ($1, $2, $3, $4)`, org.org, open, benID, annID)
		denied(t, conn, "a verification in ann's name", `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() + interval '1 day')`, org.org, open, annID)
		denied(t, conn, "a term longer than allowed", `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() + interval '731 days')`, org.org, open, benID)
		denied(t, conn, "a term already over", `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() - interval '1 day')`, org.org, open, benID)
		denied(t, conn, "a backdated check", `INSERT INTO page_verification (org_id, page_id, verified_by, verified_at, expires_at) VALUES ($1, $2, $3, now() - interval '400 days', now() + interval '1 day')`, org.org, open, benID)
		denied(t, conn, "the worker's notice", `UPDATE page_verification SET lapse_noticed_at = now() WHERE page_id = $1`, secret)
		denied(t, conn, "a verification of an unpublished page", `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() + interval '1 day')`, org.org, sketch, benID)
		denied(t, conn, "a row in another organization", `INSERT INTO page_owner (org_id, page_id, user_id, set_by) VALUES ($1, $2, $3, $3)`, other.org, open, benID)
		if _, err := conn.Exec(ctx, `INSERT INTO page_verification (org_id, page_id, verified_by, expires_at) VALUES ($1, $2, $3, now() + interval '1 day')`, org.org, open, benID); err != nil {
			t.Fatalf("ben cannot verify Open: %v", err)
		}
		var version int
		var fresh bool
		if err := conn.QueryRow(ctx, `SELECT version, verified_at > now() - interval '1 minute' FROM page_verification WHERE page_id = $1`, open).Scan(&version, &fresh); err != nil || version != 1 || !fresh {
			t.Errorf("ben's check is of version %d, stamped now %v (%v)", version, fresh, err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO page_owner (org_id, page_id, user_id, set_by) VALUES ($1, $2, $3, $4)`, org.org, open, annID, benID); err != nil {
			t.Errorf("ben cannot name ann, who reads Open: %v", err)
		}
	})

	t.Run("another organization reads nothing", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM page_owner`) + count(`SELECT count(*) FROM page_verification`); n != 0 {
			t.Errorf("another organization reads %d rows", n)
		}
	})
}
