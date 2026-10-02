//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/pageview"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// viewedOn says a person opened a page days ago, as the superuser, since the
// database counts every view on the day it happens.
func (h *harness) viewedOn(t *testing.T, org, person uuid.UUID, pageID string, days ...int) {
	t.Helper()
	for _, ago := range days {
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO page_view (org_id, page_id, user_id, day) VALUES ($1, $2, $3, page_view_today() - $4::integer)
			ON CONFLICT DO NOTHING`, org, pageID, person, ago); err != nil {
			t.Fatalf("a view of %s %d days ago: %v", pageID, ago, err)
		}
	}
}

// countsOf reads a page's counts as views, readers, recent views, recent readers.
func countsOf(t *testing.T, c *client, pageID string) [4]int {
	t.Helper()
	r := want(t, c.get(t, pagePath(pageID, "/views")), http.StatusOK, "the counts of "+pageID)
	if number(r.Body["days"]) != pageview.RecentDays {
		t.Errorf("the recent period is %v days, want %d", r.Body["days"], pageview.RecentDays)
	}
	return [4]int{number(r.Body["views"]), number(r.Body["readers"]), number(r.Body["recentViews"]), number(r.Body["recentReaders"])}
}

// namesOf reads a window of readers by name.
func namesOf(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "readers") {
		out = append(out, each.(map[string]any)["name"].(string))
	}
	return out
}

// Every page view operation, done and refused: the counts for anybody who
// may view the page, the names for its editors, and who chose not to be named.
func TestPageViewsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "views")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Editor"), h.namedPerson(t, org.org, "Ben Reader"), h.namedPerson(t, org.org, "Carl Outside")
	ann, ben, carl := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug), api.as(t, carlID, org.org, slug)
	nobody := api.anonymous()

	docs := newTree(t, owner, "PVW", "Viewed")
	want(t, owner.put(t, "/api/v1/spaces/PVW/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everyone reads PVW and ann edits it")
	guide := docs.add(docs.homeID, "Guide")
	secret := docs.add(docs.homeID, "Secret")
	gone := docs.add(docs.homeID, "Gone")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(annID), user(benID)}, nil), http.StatusOK, "carl may not read Secret")

	t.Run("a page nobody opened has no views", func(t *testing.T) {
		if got := countsOf(t, ben, guide); got != [4]int{} {
			t.Errorf("an unread page counts %v", got)
		}
	})

	want(t, ann.post(t, pagePath(guide, "/visit"), nil), http.StatusNoContent, "ann opens Guide")
	want(t, ann.post(t, pagePath(guide, "/visit"), nil), http.StatusNoContent, "ann opens Guide again")
	want(t, ben.post(t, pagePath(guide, "/visit"), nil), http.StatusNoContent, "ben opens Guide")
	want(t, ben.post(t, pagePath(secret, "/visit"), nil), http.StatusNoContent, "ben opens Secret")
	h.settle(t)

	t.Run("each person counts once a day, and everybody who may view the page reads the counts", func(t *testing.T) {
		for who, c := range map[string]*client{"the owner": owner, "ann": ann, "ben": ben, "carl": carl} {
			if got := countsOf(t, c, guide); got != [4]int{2, 2, 2, 2} {
				t.Errorf("%s reads %v, want two views by two readers", who, got)
			}
		}
		h.viewedOn(t, org.org, annID, guide, 5, 40)
		h.settle(t)
		if got := countsOf(t, ben, guide); got != [4]int{4, 2, 3, 2} {
			t.Errorf("with two older days of ann's, Guide counts %v, want 4 views, 2 readers, 3 and 2 lately", got)
		}
	})

	t.Run("only the page's editors may list its readers, and are told so", func(t *testing.T) {
		for who, tt := range map[string]struct {
			c    *client
			list bool
		}{"the owner": {owner, true}, "ann": {ann, true}, "ben": {ben, false}, "carl": {carl, false}} {
			r := want(t, tt.c.get(t, pagePath(guide, "/views")), http.StatusOK, who+" reads the counts")
			if r.Body["canListReaders"] != tt.list {
				t.Errorf("%s may list readers: %v, want %v", who, r.Body["canListReaders"], tt.list)
			}
		}
		r := want(t, ann.get(t, pagePath(guide, "/readers")), http.StatusOK, "ann lists Guide's readers")
		sameList(t, "Guide's readers, the latest first", namesOf(t, r), "Ben Reader", "Ann Editor")
		rows := list(t, r, "readers")
		if first, second := rows[0].(map[string]any), rows[1].(map[string]any); number(first["days"]) != 1 || number(second["days"]) != 3 ||
			first["id"] != benID.String() || first["viewedAt"] == nil {
			t.Errorf("the readers read %v", rows)
		}
		if number(r.Body["unnamed"]) != 0 || number(r.Body["retentionDays"]) != 90 || r.Body["next"] != nil {
			t.Errorf("the window reads unnamed %v, retention %v, next %v", r.Body["unnamed"], r.Body["retentionDays"], r.Body["next"])
		}

		var names []string
		cursor := ""
		for range 5 {
			r := want(t, owner.get(t, pagePath(guide, "/readers?limit=1&cursor="+url.QueryEscape(cursor))), http.StatusOK, "a window of one reader")
			names = append(names, namesOf(t, r)...)
			next, _ := r.Body["next"].(string)
			if next == "" {
				break
			}
			cursor = next
		}
		sameList(t, "a walk one reader at a time", names, "Ben Reader", "Ann Editor")

		refusal := want(t, ben.get(t, pagePath(guide, "/readers")), http.StatusForbidden, "ben may not list readers")
		if msg := refusal.Body["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "Only people who may edit this page see who read it") {
			t.Errorf("ben is refused with %q", msg)
		}
		errorCode(t, refusal)
	})

	t.Run("a page somebody may not view tells them nothing", func(t *testing.T) {
		for what, r := range map[string]response{
			"the counts of Secret":  carl.get(t, pagePath(secret, "/views")),
			"the readers of Secret": carl.get(t, pagePath(secret, "/readers")),
			"a visit of Secret":     carl.post(t, pagePath(secret, "/visit"), nil),
			"no such page":          owner.get(t, pagePath(uuid.NewString(), "/views")),
		} {
			if r.Status != http.StatusNotFound {
				t.Errorf("%s answered %d, want 404", what, r.Status)
				continue
			}
			errorCode(t, r)
		}
		if got := countsOf(t, ben, secret); got != [4]int{1, 1, 1, 1} {
			t.Errorf("Secret counts %v for somebody who may read it", got)
		}
		want(t, owner.delete(t, pagePath(gone)), http.StatusNoContent, "Gone goes to the trash")
		h.settle(t)
		want(t, owner.get(t, pagePath(gone, "/views")), http.StatusNotFound, "the counts of a trashed page")
		for what, tt := range map[string]struct {
			r      response
			status int
		}{
			"nobody signed in":                    {nobody.get(t, pagePath(guide, "/views")), http.StatusUnauthorized},
			"nobody signed in lists":              {nobody.get(t, pagePath(guide, "/readers")), http.StatusUnauthorized},
			"too large a window":                  {owner.get(t, pagePath(guide, "/readers?limit=101")), http.StatusUnprocessableEntity},
			"a cursor this list did not give out": {owner.get(t, pagePath(guide, "/readers?cursor=nonsense")), http.StatusUnprocessableEntity},
		} {
			if tt.r.Status != tt.status {
				t.Errorf("%s answered %d, want %d", what, tt.r.Status, tt.status)
				continue
			}
			errorCode(t, tt.r)
		}
	})

	t.Run("somebody who chose not to be named is counted but not listed", func(t *testing.T) {
		me := want(t, ben.patch(t, "/api/v1/auth/me", map[string]any{"showInReaders": false}), http.StatusOK, "ben hides his name")
		if obj(t, me, "user")["showInReaders"] != false {
			t.Errorf("ben's settings read %v", me.Body["user"])
		}
		if obj(t, want(t, ben.get(t, "/api/v1/auth/me"), http.StatusOK, "ben's settings"), "user")["showInReaders"] != false {
			t.Error("ben's choice was not kept")
		}
		h.settle(t)
		r := want(t, ann.get(t, pagePath(guide, "/readers")), http.StatusOK, "ann lists Guide's readers")
		sameList(t, "Guide's named readers", namesOf(t, r), "Ann Editor")
		if number(r.Body["unnamed"]) != 1 {
			t.Errorf("unnamed reads %v, want ben", r.Body["unnamed"])
		}
		if got := countsOf(t, ann, guide); got != [4]int{4, 2, 3, 2} {
			t.Errorf("hiding a name changed the counts to %v", got)
		}
		want(t, ben.patch(t, "/api/v1/auth/me", map[string]any{"showInReaders": true}), http.StatusOK, "ben shows his name again")
		h.settle(t)
		sameList(t, "Guide's readers again", namesOf(t, want(t, ann.get(t, pagePath(guide, "/readers")), http.StatusOK, "ann lists")), "Ben Reader", "Ann Editor")
	})

	t.Run("an assistant reads the counts as its person, and never the names", func(t *testing.T) {
		_, token := makeToken(t, ben, map[string]any{"name": "assistant", "scopes": []string{"read"}})
		agent := &mcpSession{c: api.withToken(token)}
		tools := agent.tools(t)
		if !tools["get_page_views"] {
			t.Fatal("a read-only token is not offered get_page_views")
		}
		res := agent.call(t, "get_page_views", map[string]any{"pageID": guide})
		if res["isError"] == true {
			t.Fatalf("the tool refused ben: %s", toolText(res))
		}
		if got := res["structuredContent"].(map[string]any); number(got["views"]) != 4 || number(got["readers"]) != 2 {
			t.Errorf("the tool answers %v", got)
		}
		_, carlToken := makeToken(t, carl, map[string]any{"name": "assistant", "scopes": []string{"read"}})
		if res := (&mcpSession{c: api.withToken(carlToken)}).call(t, "get_page_views", map[string]any{"pageID": secret}); res["isError"] != true {
			t.Errorf("carl's assistant reads Secret's counts: %v", res)
		}
	})

	t.Run("somebody who leaves takes their name and leaves their views", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, org.org, benID); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if got := countsOf(t, ann, guide); got != [4]int{4, 1, 3, 1} {
			t.Errorf("after ben left Guide counts %v, want his view kept and him gone from the readers", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1 AND page_id = $2 AND user_id IS NULL`, org.org, guide); n != 1 {
			t.Errorf("%d of ben's views are left without his name, want 1", n)
		}
		sameList(t, "Guide's readers without ben", namesOf(t, want(t, ann.get(t, pagePath(guide, "/readers")), http.StatusOK, "ann lists")), "Ann Editor")
	})
}

// Reloading a page is the traffic a popular page gets: the second open of a
// day finds the person's rows and writes no new version of either.
func TestAReloadWritesNothing(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "views-reload")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	docs := newTree(t, owner, "PVR", "Reloaded")
	hot := docs.add(docs.homeID, "Hot")
	other := docs.add(docs.homeID, "Other")
	visits := search.NewService(h.cluster)
	actor := perm.Actor{UserID: org.user}
	visit := func(id string) {
		t.Helper()
		if _, err := visits.Visit(org.ctx, actor, uuid.MustParse(id)); err != nil {
			t.Fatalf("visit %s: %v", id, err)
		}
	}
	versions := func() (string, string) {
		t.Helper()
		var view, seen string
		if err := h.super.QueryRow(context.Background(), `
			SELECT (SELECT v.xmin::text || '/' || v.xmax::text FROM page_view v WHERE v.org_id = $1 AND v.page_id = $2),
			       (SELECT r.xmin::text || '/' || r.xmax::text FROM page_visit r WHERE r.org_id = $1 AND r.page_id = $2 AND r.user_id = $3)`,
			org.org, hot, org.user).Scan(&view, &seen); err != nil {
			t.Fatal(err)
		}
		return view, seen
	}

	visit(hot)
	view, seen := versions()
	if !strings.HasSuffix(view, "/0") || !strings.HasSuffix(seen, "/0") {
		t.Fatalf("the first open left the rows at %s and %s", view, seen)
	}

	t.Run("reloads change no row", func(t *testing.T) {
		for range 20 {
			visit(hot)
		}
		if v, s := versions(); v != view || s != seen {
			t.Errorf("twenty reloads moved the view from %s to %s and the visit from %s to %s", view, v, seen, s)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1 AND page_id = $2`, org.org, hot); n != 1 {
			t.Errorf("Hot has %d view rows, want 1", n)
		}
	})

	t.Run("opening it at once from many tabs counts it once", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				if _, err := visits.Visit(org.ctx, actor, uuid.MustParse(other)); err != nil {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("a visit at once failed: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1 AND page_id = $2`, org.org, other); n != 1 {
			t.Errorf("Other has %d view rows, want 1", n)
		}
	})

	t.Run("coming back from another page moves it up the recent pages, still one view", func(t *testing.T) {
		visit(hot)
		_, moved := versions()
		if moved == seen {
			t.Error("coming back to Hot after Other left it below Other in the recent pages")
		}
		h.settle(t)
		recent, err := visits.Recent(org.ctx, actor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(recent) != 2 || recent[0].ID.String() != hot {
			t.Errorf("the recent pages are %v, want Hot first", recent)
		}
		if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1 AND page_id = $2`, org.org, hot); n != 1 {
			t.Errorf("Hot has %d view rows, want 1", n)
		}
	})
}

// Straight through SQL as stator_app: a view is only one's own, today, of a
// page one may view; nobody rewrites, removes or reads another's; the
// functions answer only what the person may know.
func TestPageViewsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "views-rls")
	other := h.makeMember(t, "views-rls-other")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member")
	docs := newTree(t, owner, "PVS", "Walled")
	want(t, owner.put(t, "/api/v1/spaces/PVS/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everyone reads PVS and ann edits it")
	guide := docs.add(docs.homeID, "Guide")
	secret := docs.add(docs.homeID, "Secret")
	want(t, restrict(t, owner, secret, []any{user(org.user)}, nil), http.StatusOK, "only the owner reads Secret")
	h.viewedOn(t, org.org, annID, guide, 0, 3)
	h.visited(t, org.org, annID, guide, 0)
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
	const insert = `INSERT INTO page_view (org_id, page_id, user_id, day) VALUES ($1, $2, $3, page_view_today() - $4::integer)`

	t.Run("a view is one's own, today, of a page one may view", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		refused(t, conn, "a view counted for ann", insert, org.org, guide, annID, 1)
		refused(t, conn, "a view counted for ann today", insert, org.org, guide, annID, 0)
		refused(t, conn, "a view counted yesterday", insert, org.org, guide, benID, 1)
		refused(t, conn, "a view of a page ben may not view", insert, org.org, secret, benID, 0)
		refused(t, conn, "a view in another organization", insert, other.org, guide, benID, 0)
		if _, err := conn.Exec(ctx, insert, org.org, guide, benID, 0); err != nil {
			t.Fatalf("ben may not count his own view: %v", err)
		}
		refused(t, conn, "the same view twice without the conflict clause", insert, org.org, guide, benID, 0)
	})

	t.Run("nobody rewrites, removes or reads another's views, nor touches the tally", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		for what, sql := range map[string]string{
			"moving a view to another day":    `UPDATE page_view SET day = day - 1`,
			"handing a view to somebody else": `UPDATE page_view SET user_id = NULL`,
			"removing views":                  `DELETE FROM page_view`,
			"reading the tally":               `SELECT count(*) FROM page_view_tally`,
			"adding to the tally":             `INSERT INTO page_view_tally (org_id, page_id, views) VALUES ('` + org.org.String() + `', '` + guide + `', 100)`,
			"pruning":                         `SELECT page_view_prune(page_view_today() - 400)`,
			"hiding ann's name":               `UPDATE app_user SET show_in_readers = false WHERE id = '` + annID.String() + `'`,
		} {
			refused(t, conn, what, sql)
		}
		if n := count(`SELECT count(*) FROM page_view WHERE page_id = $1`, guide); n != 1 {
			t.Errorf("ben reads %d views of Guide, want his own one", n)
		}
		if _, err := conn.Exec(ctx, `UPDATE app_user SET show_in_readers = false WHERE id = $1`, benID); err != nil {
			t.Errorf("ben may not hide his own name: %v", err)
		}
		if _, err := conn.Exec(ctx, `UPDATE app_user SET show_in_readers = true WHERE id = $1`, benID); err != nil {
			t.Errorf("ben may not show his own name: %v", err)
		}
	})

	t.Run("the functions answer what the person may know and nothing more", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		var views, readers int
		var canList bool
		if err := conn.QueryRow(ctx, `SELECT views, readers, can_list FROM page_view_stats($1, 30)`, guide).Scan(&views, &readers, &canList); err != nil ||
			views != 3 || readers != 1 || canList {
			t.Errorf("ben reads Guide as %d views by %d readers, listing %v (%v)", views, readers, canList, err)
		}
		if n := count(`SELECT count(*) FROM page_view_stats($1, 30)`, secret); n != 0 {
			t.Errorf("ben reads the counts of a page he may not view")
		}
		if n := count(`SELECT count(*) FROM page_readers($1, NULL, NULL, 100)`, guide); n != 0 {
			t.Errorf("ben, who may not edit Guide, reads %d of its readers", n)
		}
		var unnamed *int64
		if err := conn.QueryRow(ctx, `SELECT page_readers_unnamed($1)`, guide).Scan(&unnamed); err != nil || unnamed != nil {
			t.Errorf("ben reads how many chose not to be named: %v, %v", unnamed, err)
		}

		actAs(t, conn, org.org, annID)
		if n := count(`SELECT count(*) FROM page_readers($1, NULL, NULL, 100)`, guide); n != 1 {
			t.Errorf("ann, who edits Guide, reads %d of its readers, want herself", n)
		}
		if n := count(`SELECT count(*) FROM page_readers($1, NULL, NULL, 100)`, secret); n != 0 {
			t.Errorf("ann reads the readers of a page she may not view")
		}

		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM page_view_stats($1, 30)`, guide); n != 0 {
			t.Errorf("another organization reads Guide's counts")
		}
		if n := count(`SELECT count(*) FROM page_readers($1, NULL, NULL, 100)`, guide); n != 0 {
			t.Errorf("another organization reads Guide's readers")
		}
		if n := count(`SELECT count(*) FROM page_view`); n != 0 {
			t.Errorf("another organization reads %d views", n)
		}
	})
}

// The worker keeps named views for the retention and adds what it takes to
// each page's tally, so names go and totals stay.
func TestRetentionMovesOldViewsIntoTheTally(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "views-keep")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.namedPerson(t, org.org, "Ann Lately"), h.namedPerson(t, org.org, "Ben Long Ago")
	docs := newTree(t, owner, "PVK", "Kept")
	guide := docs.add(docs.homeID, "Guide")
	h.viewedOn(t, org.org, annID, guide, 100, 95, 10)
	h.viewedOn(t, org.org, benID, guide, 120)
	h.visited(t, org.org, annID, guide, 10)
	h.visited(t, org.org, benID, guide, 120)
	h.settle(t)
	if got := countsOf(t, owner, guide); got != [4]int{4, 2, 1, 1} {
		t.Fatalf("before pruning Guide counts %v", got)
	}

	var minDays int
	if err := h.super.QueryRow(context.Background(), `SELECT page_view_min_days()`).Scan(&minDays); err != nil || time.Duration(minDays)*24*time.Hour != pageview.MinRetention {
		t.Errorf("the database keeps views at least %d days, the package %s (%v)", minDays, pageview.MinRetention, err)
	}

	n, err := pageview.NewRetention(h.cluster, pageview.DefaultRetention, discard()).Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 3 {
		t.Errorf("retention took %d views, want at least the three old ones", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1 AND page_id = $2`, org.org, guide); n != 1 {
		t.Errorf("%d named views of Guide are left, want ann's of ten days ago", n)
	}
	if n := h.countRows(t, `SELECT views FROM page_view_tally WHERE org_id = $1 AND page_id = $2`, org.org, guide); n != 3 {
		t.Errorf("the tally holds %d views, want 3", n)
	}
	h.settle(t)
	if got := countsOf(t, owner, guide); got != [4]int{4, 2, 1, 1} {
		t.Errorf("after pruning Guide counts %v, want the same as before", got)
	}
	sameList(t, "the readers within the retention", namesOf(t, want(t, owner.get(t, pagePath(guide, "/readers")), http.StatusOK, "the owner lists")), "Ann Lately")

	if n, err := pageview.NewRetention(h.cluster, 0, discard()).Once(context.Background()); n != 0 || err != nil {
		t.Errorf("keeping forever took %d views, %v", n, err)
	}
	prune := func(ctx context.Context, cutoff string) error {
		_, err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `SELECT page_view_prune(`+cutoff+`)`)
			return err
		})
		return err
	}
	var pgErr *pgconn.PgError
	if err := prune(tenant.WithOrg(context.Background(), tenant.Org{ID: org.org}), `page_view_today() - 29`); !errors.As(err, &pgErr) || pgErr.Code != "22023" {
		t.Errorf("pruning the last 29 days = %v, want refused", err)
	}
	if err := prune(context.Background(), `page_view_today() - 400`); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("pruning outside an organization = %v, want refused", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM page_view WHERE org_id = $1`, org.org); n != 1 {
		t.Errorf("a refused prune took views: %d left", n)
	}
}

// The counts and the readers of a page read thousands of views a day of
// people by index, and run the view rule a handful of times, not per row.
func TestPageViewCountsReadAnIndexNotTheTable(t *testing.T) {
	const (
		people = 150
		days   = 90
		pages  = 10
	)
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "views-plan")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	docs := newTree(t, owner, "PVP", "Popular")
	hot := docs.add(docs.homeID, "Hot")
	ctx := context.Background()
	tag := "pv-plan-" + uuid.NewString()[:8]
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email LIKE $1`, tag+"-%") })
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO app_user (email, name) SELECT $1 || '-' || i || '@example.test', 'Reader ' || i FROM generate_series(1, $2) i`, []any{tag, people}},
		{`INSERT INTO org_member (org_id, user_id, org_role) SELECT $1, id, 'member' FROM app_user WHERE email LIKE $2`, []any{org.org, tag + "-%"}},
		{`INSERT INTO page (org_id, space_id, parent_id, rank, title, version, created_by, updated_by)
		  SELECT p.org_id, p.space_id, p.id, 'b' || lpad(i::text, 6, '0'), 'Other ' || i, 1, $2, $2
		  FROM page p, generate_series(1, $3) i WHERE p.id = $1`, []any{docs.homeID, org.user, pages}},
		{`INSERT INTO page_view (org_id, page_id, user_id, day)
		  SELECT $1, p.id, m.user_id, page_view_today() - d
		  FROM page p CROSS JOIN org_member m CROSS JOIN generate_series(0, $2 - 1) d
		  WHERE p.org_id = $1 AND m.org_id = $1 AND (p.id = $3 OR p.title LIKE 'Other %')`, []any{org.org, days, hot}},
		{`INSERT INTO page_visit (org_id, user_id, page_id, visited_at)
		  SELECT $1, m.user_id, $2, now() - make_interval(mins => (random() * 1000)::int) FROM org_member m WHERE m.org_id = $1`, []any{org.org, hot}},
		{`ANALYZE page_view, page_visit`, nil},
	} {
		if _, err := h.super.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.sql, err)
		}
	}
	h.settle(t)

	cfg, err := pgx.ParseConfig(os.Getenv(envSuperuser))
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) { plans = append(plans, n.Message) }
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, stmt := range []string{
		`SELECT set_config('` + tenant.PostgresVar + `', '` + org.org.String() + `', true)`,
		`SELECT set_config('` + db.UserVar + `', '` + org.user.String() + `', true)`,
		`SET LOCAL track_functions = 'all'`,
		`LOAD 'auto_explain'`,
		`SET LOCAL auto_explain.log_min_duration = 0`,
		`SET LOCAL auto_explain.log_nested_statements = on`,
		`SET LOCAL auto_explain.log_analyze = on`,
		`SET LOCAL auto_explain.log_level = 'notice'`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	started := time.Now()
	var views, readers, recentViews, recentReaders int64
	var canList bool
	if err := tx.QueryRow(ctx, pageview.StatsQuery, hot, pageview.RecentDays).Scan(&views, &readers, &recentViews, &recentReaders, &canList); err != nil {
		t.Fatal(err)
	}
	statsTook := time.Since(started)
	// The owner is a member too, and was counted with the people.
	const members = people + 1
	if views != members*days || readers != members || recentViews != members*pageview.RecentDays || recentReaders != members || !canList {
		t.Errorf("Hot counts %d views by %d readers, %d and %d lately, listing %v", views, readers, recentViews, recentReaders, canList)
	}
	started = time.Now()
	rows, err := tx.Query(ctx, pageview.ReadersQuery, hot, nil, nil, pageview.DefaultLimit+1)
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for rows.Next() {
		read++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	readersTook := time.Since(started)
	if read != pageview.DefaultLimit+1 {
		t.Errorf("the window held %d readers, want %d", read, pageview.DefaultLimit+1)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL auto_explain.log_min_duration = -1`); err != nil {
		t.Fatal(err)
	}
	var calls int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(pg_stat_get_xact_function_calls('perm_page_viewable(uuid, uuid)'::regprocedure), 0)`).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if calls == 0 || calls > 4 {
		t.Errorf("the view rule ran %d times for the counts and a window of readers", calls)
	}
	indexed := false
	for _, plan := range plans {
		if strings.Contains(plan, "Seq Scan on page_view ") {
			t.Errorf("a plan read every view:\n%s", plan)
		}
		if strings.Contains(plan, "page_view_day_idx") || strings.Contains(plan, "page_view_once_idx") {
			indexed = true
		}
	}
	if !indexed {
		t.Errorf("no plan used the view indexes; the plans were:\n%s", strings.Join(plans, "\n---\n"))
	}
	t.Logf("among %d views, the counts took %v and a window of readers %v", (people+1)*days*(pages+1), statsTook, readersTook)
}
