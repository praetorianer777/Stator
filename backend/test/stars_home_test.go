//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/home"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// titlesOf reads a list's items by their page title, or the space's name for
// a star on a space.
func titlesOf(t *testing.T, r response, key string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, key) {
		item := each.(map[string]any)
		switch {
		case item["title"] != nil:
			out = append(out, item["title"].(string))
		case item["page"] != nil:
			out = append(out, item["page"].(map[string]any)["title"].(string))
		default:
			out = append(out, "space "+item["spaceName"].(string))
		}
	}
	return out
}

// walk follows a list's cursors a window of one at a time and returns every
// title it met, failing when the walk does not end.
func walk(t *testing.T, c *client, path, key string) []string {
	t.Helper()
	var out []string
	cursor := ""
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for range 50 {
		r := want(t, c.get(t, path+sep+"limit=1&cursor="+url.QueryEscape(cursor)), http.StatusOK, "a window of "+path)
		got := titlesOf(t, r, key)
		if len(got) > 1 {
			t.Fatalf("a window of one held %v", got)
		}
		out = append(out, got...)
		next, _ := r.Body["next"].(string)
		if next == "" {
			return out
		}
		cursor = next
	}
	t.Fatalf("the walk through %s did not end", path)
	return nil
}

func has(titles []string, title string) bool {
	for _, each := range titles {
		if each == title {
			return true
		}
	}
	return false
}

func sameList(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

// Every star and home operation, done once and refused once, with what each
// list holds, in which order, a window at a time.
func TestStarsAndHomeOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stars")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.namedPerson(t, org.org, "Ann Star"), h.namedPerson(t, org.org, "Ben Star")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)
	nobody := api.anonymous()

	docs := newTree(t, owner, "STR", "Starred")
	top := docs.add(docs.homeID, "Top")
	child := docs.add(top, "Child")
	aside := docs.add(docs.homeID, "Aside")

	t.Run("pages and spaces are starred and unstarred, the latest first", func(t *testing.T) {
		want(t, ann.put(t, pagePath(top, "/star"), nil), http.StatusNoContent, "ann stars Top")
		want(t, ann.put(t, pagePath(top, "/star"), nil), http.StatusNoContent, "and again")
		want(t, ann.put(t, pagePath(aside, "/star"), nil), http.StatusNoContent, "ann stars Aside")
		want(t, ann.put(t, "/api/v1/spaces/STR/star", nil), http.StatusNoContent, "ann stars the space")
		if p := obj(t, want(t, ann.get(t, pagePath(top)), http.StatusOK, "Top"), "page"); p["starred"] != true {
			t.Errorf("Top says ann has not starred it: %v", p["starred"])
		}
		if p := obj(t, want(t, ben.get(t, pagePath(top)), http.StatusOK, "Top for ben"), "page"); p["starred"] != false {
			t.Errorf("Top says ben starred it: %v", p["starred"])
		}
		if sp := obj(t, want(t, ann.get(t, "/api/v1/spaces/STR"), http.StatusOK, "the space"), "space"); sp["starred"] != true {
			t.Errorf("the space says ann has not starred it: %v", sp["starred"])
		}
		r := want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars")
		sameList(t, "ann's stars", titlesOf(t, r, "stars"), "space Starred", "Aside", "Top")
		if r.Body["next"] != nil {
			t.Errorf("the only window has a next cursor: %v", r.Body["next"])
		}
		first := list(t, r, "stars")[0].(map[string]any)
		if first["kind"] != "space" || first["spaceKey"] != "STR" || first["page"] != nil || first["starredAt"] == "" {
			t.Errorf("the star on the space reads %v", first)
		}
		if page := list(t, r, "stars")[2].(map[string]any); page["kind"] != "page" || page["page"].(map[string]any)["id"] != top {
			t.Errorf("the star on Top reads %v", page)
		}
		sameList(t, "ann's stars a window at a time", walk(t, ann, "/api/v1/stars", "stars"), "space Starred", "Aside", "Top")
		if got := list(t, want(t, ben.get(t, "/api/v1/stars"), http.StatusOK, "ben's stars"), "stars"); len(got) != 0 {
			t.Errorf("ben lists ann's stars: %v", got)
		}

		want(t, ann.delete(t, pagePath(aside, "/star")), http.StatusNoContent, "ann unstars Aside")
		want(t, ann.delete(t, pagePath(aside, "/star")), http.StatusNoContent, "and again")
		want(t, ann.delete(t, "/api/v1/spaces/STR/star"), http.StatusNoContent, "ann unstars the space")
		want(t, ann.delete(t, "/api/v1/spaces/STR/star"), http.StatusNoContent, "and again")
		sameList(t, "ann's stars after unstarring", titlesOf(t, want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars"), "stars"), "Top")
	})

	t.Run("updates are what others published, each page once, the latest first", func(t *testing.T) {
		publishDraft(t, ben, child, "Child", "Ben's change.", false, "Tidied up")
		r := want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates")
		sameList(t, "ann's updates", titlesOf(t, r, "updates"), "Child", "Aside", "Top", "Starred")
		latest := list(t, r, "updates")[0].(map[string]any)
		if latest["authorName"] != "Ben Star" || number(latest["version"]) != 2 || latest["comment"] != "Tidied up" || latest["spaceKey"] != "STR" {
			t.Errorf("the latest update reads %v", latest)
		}
		sameList(t, "ann's updates a window at a time", walk(t, ann, "/api/v1/home/updates", "updates"), "Child", "Aside", "Top", "Starred")
		sameList(t, "ben's updates leave his own out", titlesOf(t, want(t, ben.get(t, "/api/v1/home/updates?scope=all"), http.StatusOK, "ben's updates"), "updates"), "Aside", "Top", "Starred")
		sameList(t, "the owner's updates", titlesOf(t, want(t, owner.get(t, "/api/v1/home/updates"), http.StatusOK, "the owner's updates"), "updates"), "Child")
	})

	t.Run("watched updates are what the caller's watches cover", func(t *testing.T) {
		if got := list(t, want(t, ann.get(t, "/api/v1/home/updates?scope=watched"), http.StatusOK, "ann's watched updates"), "updates"); len(got) != 0 {
			t.Errorf("ann watches nothing and reads %v", got)
		}
		want(t, watchPage(t, ann, top, true), http.StatusOK, "ann watches Top and below")
		sameList(t, "ann's watched updates", titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates?scope=watched"), http.StatusOK, "ann's watched updates"), "updates"), "Child", "Top")
		want(t, ann.delete(t, pagePath(top, "/watch")), http.StatusNoContent, "ann stops watching Top")
		want(t, watchPage(t, ann, aside, false), http.StatusOK, "ann watches Aside alone")
		sameList(t, "ann's watched updates of one page", titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates?scope=watched"), http.StatusOK, "ann's watched updates"), "updates"), "Aside")
		want(t, ann.put(t, "/api/v1/spaces/STR/watch", nil), http.StatusNoContent, "ann watches the space")
		sameList(t, "ann's watched updates of the space", walk(t, ann, "/api/v1/home/updates?scope=watched", "updates"), "Child", "Aside", "Top", "Starred")
		want(t, ann.delete(t, "/api/v1/spaces/STR/watch"), http.StatusNoContent, "ann stops watching the space")
	})

	t.Run("edited is what the caller published, drafts and unpublished pages", func(t *testing.T) {
		sameList(t, "ben's edits", titlesOf(t, want(t, ben.get(t, "/api/v1/home/edited"), http.StatusOK, "ben's edits"), "pages"), "Child")
		version := number(obj(t, want(t, ann.get(t, pagePath(top)), http.StatusOK, "Top"), "page")["version"])
		want(t, ann.put(t, pagePath(top, "/draft"), map[string]any{"title": "Top", "body": textDoc("Ann's draft."), "baseVersion": version}), http.StatusOK, "ann drafts Top")
		r := want(t, ann.get(t, "/api/v1/home/edited"), http.StatusOK, "ann's edits")
		sameList(t, "ann's edits", titlesOf(t, r, "pages"), "Top")
		if e := list(t, r, "pages")[0].(map[string]any); e["draft"] != true || e["unpublished"] != false || e["spaceKey"] != "STR" {
			t.Errorf("ann's draft of Top reads %v", e)
		}
		made := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Sketch"}), http.StatusCreated, "the owner makes a page he does not publish"), "page")["id"].(string)
		r = want(t, owner.get(t, "/api/v1/home/edited"), http.StatusOK, "the owner's edits")
		if e := list(t, r, "pages")[0].(map[string]any); e["title"] != "Sketch" || e["unpublished"] != true || e["draft"] != true {
			t.Errorf("the owner's unpublished page reads %v", e)
		}
		sameList(t, "the owner's edits a window at a time", walk(t, owner, "/api/v1/home/edited", "pages"), "Sketch", "Aside", "Child", "Top", "Starred")
		if has(titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates"), "updates"), "Sketch") {
			t.Errorf("ann reads an update about a page nobody published")
		}
		want(t, owner.delete(t, pagePath(made)), http.StatusNoContent, "the owner trashes Sketch")
	})

	t.Run("a page that becomes restricted leaves every list, and its star waits", func(t *testing.T) {
		want(t, restrict(t, owner, top, []any{user(org.user), user(benID)}, nil), http.StatusOK, "Top is for the owner and ben")
		h.settle(t)
		if got := titlesOf(t, want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars"), "stars"); has(got, "Top") {
			t.Errorf("ann still lists her star on Top: %v", got)
		}
		updates := titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates"), "updates")
		if has(updates, "Top") || has(updates, "Child") {
			t.Errorf("ann reads updates of pages she may not view: %v", updates)
		}
		if got := titlesOf(t, want(t, ann.get(t, "/api/v1/home/edited"), http.StatusOK, "ann's edits"), "pages"); has(got, "Top") {
			t.Errorf("ann lists her draft of a page she may not view: %v", got)
		}
		want(t, ann.put(t, pagePath(top, "/star"), nil), http.StatusNotFound, "ann stars a page she may not view")
		want(t, ann.delete(t, pagePath(child, "/star")), http.StatusNotFound, "ann unstars a page she may not view")
		if n := h.countRows(t, `SELECT count(*) FROM star WHERE user_id = $1 AND page_id = $2`, annID, top); n != 1 {
			t.Errorf("ann's star on Top was not kept: %d", n)
		}
		want(t, restrict(t, owner, top, nil, nil), http.StatusOK, "Top is open again")
		h.settle(t)
		sameList(t, "ann's stars once Top is open", titlesOf(t, want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars"), "stars"), "Top")
	})

	t.Run("a trashed page leaves the stars until it comes back", func(t *testing.T) {
		want(t, ann.put(t, pagePath(aside, "/star"), nil), http.StatusNoContent, "ann stars Aside")
		want(t, owner.delete(t, pagePath(aside)), http.StatusNoContent, "Aside goes to the trash")
		h.settle(t)
		sameList(t, "ann's stars with Aside in the trash", titlesOf(t, want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars"), "stars"), "Top")
		want(t, ann.put(t, pagePath(aside, "/star"), nil), http.StatusNotFound, "starring a trashed page")
		if has(titlesOf(t, want(t, ann.get(t, "/api/v1/home/updates"), http.StatusOK, "ann's updates"), "updates"), "Aside") {
			t.Errorf("ann reads an update of a trashed page")
		}
		want(t, docs.restore(aside), http.StatusOK, "Aside comes back")
		h.settle(t)
		sameList(t, "ann's stars with Aside back", titlesOf(t, want(t, ann.get(t, "/api/v1/stars"), http.StatusOK, "ann's stars"), "stars"), "Aside", "Top")
	})

	t.Run("what is not so is refused in a sentence", func(t *testing.T) {
		missing := uuid.NewString()
		for what, r := range map[string]response{
			"star no page":            ann.put(t, pagePath(missing, "/star"), nil),
			"unstar no page":          ann.delete(t, pagePath(missing, "/star")),
			"star no space":           ann.put(t, "/api/v1/spaces/NOPE/star", nil),
			"unstar no space":         ann.delete(t, "/api/v1/spaces/NOPE/star"),
			"too many stars":          ann.get(t, "/api/v1/stars?limit=101"),
			"a cursor not given out":  ann.get(t, "/api/v1/stars?cursor=nonsense"),
			"too many updates":        ann.get(t, "/api/v1/home/updates?limit=51"),
			"an unknown scope":        ann.get(t, "/api/v1/home/updates?scope=everything"),
			"no edits at all":         ann.get(t, "/api/v1/home/edited?limit=0"),
			"nobody lists stars":      nobody.get(t, "/api/v1/stars"),
			"nobody reads updates":    nobody.get(t, "/api/v1/home/updates"),
			"nobody reads edits":      nobody.get(t, "/api/v1/home/edited"),
			"nobody stars a page":     nobody.put(t, pagePath(top, "/star"), nil),
			"nobody stars a space":    nobody.put(t, "/api/v1/spaces/STR/star", nil),
			"nobody unstars a space":  nobody.delete(t, "/api/v1/spaces/STR/star"),
			"nobody unstars the page": nobody.delete(t, pagePath(top, "/star")),
		} {
			if r.Status < 400 || r.Status >= 500 {
				t.Errorf("%s answered %d", what, r.Status)
				continue
			}
			errorCode(t, r)
		}
	})
}

// Straight through SQL as stator_app, a person reads and writes only their own
// stars, on what they may view, and the home lists hold to the same rules.
func TestStarsAndHomeAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stars-rls")
	other := h.makeMember(t, "stars-rls-other")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)

	docs := newTree(t, owner, "SRL", "Walls")
	open := docs.add(docs.homeID, "Open")
	closed := docs.add(docs.homeID, "Closed")
	want(t, ann.put(t, pagePath(open, "/star"), nil), http.StatusNoContent, "ann stars Open")
	want(t, ben.put(t, pagePath(open, "/star"), nil), http.StatusNoContent, "ben stars Open")
	want(t, ben.put(t, pagePath(closed, "/star"), nil), http.StatusNoContent, "ben stars Closed")
	want(t, ben.put(t, "/api/v1/spaces/SRL/star", nil), http.StatusNoContent, "ben stars the space")
	want(t, restrict(t, owner, closed, []any{user(org.user), user(benID)}, nil), http.StatusOK, "Closed is not ann's")
	h.settle(t)
	var spaceID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM space WHERE org_id = $1 AND key = 'SRL'`, org.org).Scan(&spaceID); err != nil {
		t.Fatal(err)
	}

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

	t.Run("somebody reads and changes only their own stars", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		if n := count(`SELECT count(*) FROM star`); n != 1 {
			t.Errorf("ann reads %d stars, want her own one", n)
		}
		denied(t, conn, "starring for ben", `INSERT INTO star (org_id, user_id, page_id) VALUES ($1, $2, $3)`, org.org, benID, open)
		denied(t, conn, "starring a page she may not view", `INSERT INTO star (org_id, user_id, page_id) VALUES ($1, $2, $3)`, org.org, annID, closed)
		denied(t, conn, "a star on nothing", `INSERT INTO star (org_id, user_id) VALUES ($1, $2)`, org.org, annID)
		denied(t, conn, "a star on a page and a space at once", `INSERT INTO star (org_id, user_id, page_id, space_id) VALUES ($1, $2, $3, $4)`, org.org, annID, open, spaceID)
		denied(t, conn, "moving her star to another page", `UPDATE star SET page_id = $2 WHERE user_id = $1`, annID, closed)
		untouched(t, conn, "removing ben's stars", `DELETE FROM star WHERE user_id = $1`, benID)
		if _, err := conn.Exec(ctx, `INSERT INTO star (org_id, user_id, space_id) VALUES ($1, $2, $3)`, org.org, annID, spaceID); err != nil {
			t.Errorf("ann cannot star a space she may view: %v", err)
		}
	})

	t.Run("somebody who may no longer view a page reads nothing about it", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		if n := count(`SELECT count(*) FROM star WHERE page_id = $1`, closed); n != 1 {
			t.Fatalf("ben does not read his star on Closed")
		}
		if n := count(`SELECT count(*) FROM home_updates(false, NULL, NULL, 100) WHERE page_id = $1`, closed); n != 1 {
			t.Errorf("ben does not read the update of Closed")
		}
		want(t, restrict(t, owner, closed, []any{user(org.user)}, nil), http.StatusOK, "Closed is not ben's either")
		h.settle(t)
		actAs(t, conn, org.org, benID)
		if n := count(`SELECT count(*) FROM star WHERE page_id = $1`, closed); n != 0 {
			t.Errorf("ben reads his star on a page he may no longer view")
		}
		for _, scope := range []bool{false, true} {
			if n := count(`SELECT count(*) FROM home_updates($2, NULL, NULL, 100) WHERE page_id = $1`, closed, scope); n != 0 {
				t.Errorf("ben reads an update of a page he may no longer view (watched %v)", scope)
			}
		}
		if n := count(`SELECT count(*) FROM home_updates(false, NULL, NULL, 100) WHERE page_id = $1`, open); n != 1 {
			t.Errorf("ben does not read the update of Open")
		}
		untouched(t, conn, "unstarring a page he may not view", `DELETE FROM star WHERE page_id = $1`, closed)
		if n := h.countRows(t, `SELECT count(*) FROM star WHERE user_id = $1 AND page_id = $2`, benID, closed); n != 1 {
			t.Errorf("ben's star on Closed was not kept: %d", n)
		}
	})

	t.Run("nobody moves a page up the updates without publishing it", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		var before, after time.Time
		if err := conn.QueryRow(ctx, `SELECT published_at FROM page WHERE id = $1`, open).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `UPDATE page SET published_at = now() + interval '1 day' WHERE id = $1`, open); err != nil {
			t.Fatalf("the update itself is refused: %v", err)
		}
		if err := conn.QueryRow(ctx, `SELECT published_at FROM page WHERE id = $1`, open).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if !before.Equal(after) {
			t.Errorf("the owner moved Open's publish time from %v to %v", before, after)
		}
	})

	t.Run("another organization reads and writes nothing of this one", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM star`); n != 0 {
			t.Errorf("another organization reads %d stars", n)
		}
		if n := count(`SELECT count(*) FROM home_updates(false, NULL, NULL, 100)`); n != 0 {
			t.Errorf("another organization reads %d updates", n)
		}
		if n := count(`SELECT count(*) FROM home_edited(NULL, NULL, 100)`); n != 0 {
			t.Errorf("another organization reads %d edits", n)
		}
		denied(t, conn, "a star in the other organization", `INSERT INTO star (org_id, user_id, page_id) VALUES ($1, $2, $3)`, org.org, other.user, open)
		untouched(t, conn, "removing the other organization's stars", `DELETE FROM star WHERE org_id = $1`, org.org)
	})
}

// The updates walk stops at a window's end on a realistic organization: the
// view rule runs for about the rows it returns, not for every page there is,
// and the walk follows the index rather than sorting the pages.
func TestUpdatesReadAWindowNotTheWholeOrganization(t *testing.T) {
	const (
		pages  = 4000
		hidden = 2000
		limit  = home.DefaultLimit
	)
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "updates-plan")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.addPerson(t, org.org, "member")
	open := newTree(t, owner, "PLN", "Plenty")
	closed := newTree(t, owner, "PLC", "Closed")
	ctx := context.Background()
	bulk := func(key, home string, n int) {
		t.Helper()
		if _, err := h.super.Exec(ctx, `
			INSERT INTO page (org_id, space_id, parent_id, rank, title, version, created_by, updated_by)
			SELECT $1, s.id, $3, 'b' || lpad(i::text, 6, '0'), 'Bulk ' || i, 1, $4, $4
			FROM space s, generate_series(1, $5) i
			WHERE s.org_id = $1 AND s.key = $2`, org.org, key, home, org.user, n); err != nil {
			t.Fatalf("fill %s: %v", key, err)
		}
	}
	bulk("PLN", open.homeID, pages)
	bulk("PLC", closed.homeID, hidden)
	if _, err := h.super.Exec(ctx, `DELETE FROM space_grant g USING space s WHERE g.space_id = s.id AND s.org_id = $1 AND s.key = 'PLC' AND g.subject_type = 'everyone'`, org.org); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `ANALYZE page`); err != nil {
		t.Fatal(err)
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
		`SELECT set_config('` + db.UserVar + `', '` + annID.String() + `', true)`,
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
	rows, err := tx.Query(ctx, home.UpdatesQuery, false, nil, nil, limit+1)
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
	if read != limit+1 {
		t.Fatalf("the window held %d rows, want %d", read, limit+1)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL auto_explain.log_min_duration = -1`); err != nil {
		t.Fatal(err)
	}
	var calls int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(pg_stat_get_xact_function_calls('perm_page_viewable(uuid, uuid)'::regprocedure), 0)`).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if calls == 0 || calls > 2*(limit+1) {
		t.Errorf("the view rule ran %d times for a window of %d among %d pages", calls, limit+1, pages+hidden)
	}
	walked := false
	for _, plan := range plans {
		if strings.Contains(plan, "using page_published_idx on page") {
			walked = true
			t.Logf("the updates' plan:\n%s", plan)
		}
	}
	if !walked {
		t.Errorf("no plan walked page_published_idx; the plans were:\n%s", strings.Join(plans, "\n---\n"))
	}
}
