//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/stale"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// backdate says a page's latest version was published days ago, as the
// superuser with triggers off, since the database stamps every publish with now.
func (h *harness) backdate(t *testing.T, org uuid.UUID, where string, days int, args ...any) {
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
	if _, err := tx.Exec(ctx, `UPDATE page SET published_at = now() - make_interval(days => $2) WHERE org_id = $1 AND `+where,
		append([]any{org, days}, args...)...); err != nil {
		t.Fatalf("backdate %s: %v", where, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// visited says a person last opened a page days ago.
func (h *harness) visited(t *testing.T, org, person uuid.UUID, pageID string, days int) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO page_visit (org_id, user_id, page_id, visited_at) VALUES ($1, $2, $3, now() - make_interval(days => $4))
		ON CONFLICT (org_id, user_id, page_id) DO UPDATE SET visited_at = EXCLUDED.visited_at`, org, person, pageID, days); err != nil {
		t.Fatalf("a visit to %s: %v", pageID, err)
	}
}

// The report over the API: what it lists and in which order, each filter, a
// window at a time, the view rule, and who may read it at all.
func TestStaleReportOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stale")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Steward"), h.namedPerson(t, org.org, "Ben Member"), h.namedPerson(t, org.org, "Carl Owner")
	ann, ben := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug)
	nobody := api.anonymous()

	ops := newTree(t, owner, "OPS", "Operations")
	want(t, owner.put(t, "/api/v1/spaces/OPS/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments", "delete"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}}), http.StatusOK, "ann administers OPS")
	docs := newTree(t, owner, "DOCS", "Documents")
	ancient := ops.add(ops.homeID, "Ancient")
	lapsed := ops.add(ops.homeID, "Lapsed")
	old := ops.add(ops.homeID, "Old")
	lately := ops.add(ops.homeID, "Viewed lately")
	ops.add(ops.homeID, "Fresh")
	trashed := ops.add(ops.homeID, "Trashed")
	sketch := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": ops.homeID, "title": "Sketch"}), http.StatusCreated, "an unpublished sketch"), "page")["id"].(string)
	under := ops.add(sketch, "Under the sketch")
	docOld := docs.add(docs.homeID, "Documents of old")

	want(t, owner.put(t, pagePath(ancient, "/owner"), map[string]any{"userId": carlID}), http.StatusOK, "carl answers for Ancient")
	want(t, owner.put(t, pagePath(old, "/verification"), map[string]any{"days": 30}), http.StatusOK, "Old is verified")
	want(t, owner.put(t, pagePath(lapsed, "/verification"), map[string]any{"days": 30}), http.StatusOK, "Lapsed is verified")
	h.lapse(t, lapsed)
	want(t, owner.delete(t, pagePath(trashed)), http.StatusNoContent, "Trashed goes to the trash")

	h.backdate(t, org.org, `id = $3`, 500, docOld)
	h.backdate(t, org.org, `id = $3`, 450, under)
	h.backdate(t, org.org, `id = $3`, 400, ancient)
	h.backdate(t, org.org, `id = $3`, 400, trashed)
	h.backdate(t, org.org, `id = $3`, 250, lapsed)
	h.backdate(t, org.org, `id = $3`, 200, old)
	h.backdate(t, org.org, `id = $3`, 300, lately)
	h.visited(t, org.org, benID, old, 190)
	h.visited(t, org.org, carlID, old, 195)
	h.visited(t, org.org, benID, lately, 10)
	h.settle(t)

	t.Run("the stalest pages come first, in the spaces the reader administers", func(t *testing.T) {
		r := want(t, ann.get(t, "/api/v1/stale-pages"), http.StatusOK, "ann's report")
		sameList(t, "ann's report", titlesOf(t, r, "pages"), "Ancient", "Lapsed", "Old")
		if r.Body["next"] != nil {
			t.Errorf("the only window has a next cursor: %v", r.Body["next"])
		}
		rows := list(t, r, "pages")
		first, last := rows[0].(map[string]any), rows[2].(map[string]any)
		owned, _ := first["owner"].(map[string]any)
		if first["spaceKey"] != "OPS" || first["spaceName"] != "Operations" || first["viewedAt"] != nil || first["verification"] != "none" ||
			first["verificationExpiresAt"] != nil || owned == nil || owned["name"] != "Carl Owner" || owned["canView"] != true || number(first["version"]) != 1 {
			t.Errorf("Ancient reads %v", first)
		}
		viewed, _ := time.Parse(time.RFC3339Nano, last["viewedAt"].(string))
		if last["verification"] != "verified" || last["verificationExpiresAt"] == nil || last["owner"] != nil || last["activeAt"] != last["viewedAt"] ||
			time.Since(viewed) < 189*24*time.Hour || time.Since(viewed) > 191*24*time.Hour {
			t.Errorf("Old, last opened 190 days ago, reads %v", last)
		}
		if lapsedRow := rows[1].(map[string]any); lapsedRow["verification"] != "expired" || lapsedRow["activeAt"] != lapsedRow["publishedAt"] {
			t.Errorf("Lapsed reads %v", lapsedRow)
		}

		sameList(t, "the organization's administrator reads every space", titlesOf(t, want(t, owner.get(t, "/api/v1/stale-pages"), http.StatusOK, "the owner's report"), "pages"),
			"Documents of old", "Under the sketch", "Ancient", "Lapsed", "Old")
		sameList(t, "a window at a time", walk(t, owner, "/api/v1/stale-pages", "pages"), "Documents of old", "Under the sketch", "Ancient", "Lapsed", "Old")
	})

	t.Run("each filter narrows the report", func(t *testing.T) {
		for _, tt := range []struct {
			query string
			want  []string
		}{
			{"space=OPS", []string{"Under the sketch", "Ancient", "Lapsed", "Old"}},
			{"space=docs", []string{"Documents of old"}},
			{"olderThan=5", []string{"Documents of old", "Under the sketch", "Ancient", "Lapsed", "Old", "Viewed lately"}},
			{"olderThan=300", []string{"Documents of old", "Under the sketch", "Ancient"}},
			{"owner=" + carlID.String(), []string{"Ancient"}},
			{"owner=none&space=OPS", []string{"Under the sketch", "Lapsed", "Old"}},
			{"verification=verified", []string{"Old"}},
			{"verification=expired", []string{"Lapsed"}},
			{"verification=none&olderThan=420", []string{"Documents of old", "Under the sketch"}},
			{"space=OPS&owner=none&verification=none", []string{"Under the sketch"}},
		} {
			sameList(t, tt.query, titlesOf(t, want(t, owner.get(t, "/api/v1/stale-pages?"+tt.query), http.StatusOK, tt.query), "pages"), tt.want...)
		}
		sameList(t, "a filtered walk", walk(t, ann, "/api/v1/stale-pages?olderThan=5", "pages"), "Ancient", "Lapsed", "Old", "Viewed lately")
	})

	t.Run("a page the reader may not view stays out, though they administer its space", func(t *testing.T) {
		if has(titlesOf(t, want(t, ann.get(t, "/api/v1/stale-pages?space=OPS"), http.StatusOK, "ann's OPS"), "pages"), "Under the sketch") {
			t.Errorf("ann reads a page below somebody else's unpublished sketch")
		}
	})

	t.Run("only administrators read it, and what is not so is refused in a sentence", func(t *testing.T) {
		for what, tt := range map[string]struct {
			r      response
			status int
		}{
			"a member who administers nothing":       {ben.get(t, "/api/v1/stale-pages"), http.StatusForbidden},
			"a member asks for a space":              {ben.get(t, "/api/v1/stale-pages?space=OPS"), http.StatusForbidden},
			"a space the reader does not administer": {ann.get(t, "/api/v1/stale-pages?space=DOCS"), http.StatusForbidden},
			"no such space":                          {owner.get(t, "/api/v1/stale-pages?space=NOPE"), http.StatusNotFound},
			"an owner who is not an id":              {owner.get(t, "/api/v1/stale-pages?owner=carl"), http.StatusUnprocessableEntity},
			"an unknown verification":                {owner.get(t, "/api/v1/stale-pages?verification=checked"), http.StatusUnprocessableEntity},
			"no period":                              {owner.get(t, "/api/v1/stale-pages?olderThan=0"), http.StatusUnprocessableEntity},
			"too long a period":                      {owner.get(t, "/api/v1/stale-pages?olderThan=3651"), http.StatusUnprocessableEntity},
			"too large a window":                     {owner.get(t, "/api/v1/stale-pages?limit=101"), http.StatusUnprocessableEntity},
			"a cursor this list did not give out":    {owner.get(t, "/api/v1/stale-pages?cursor=nonsense"), http.StatusUnprocessableEntity},
			"nobody signed in":                       {nobody.get(t, "/api/v1/stale-pages"), http.StatusUnauthorized},
		} {
			if tt.r.Status != tt.status {
				t.Errorf("%s answered %d, want %d", what, tt.r.Status, tt.status)
				continue
			}
			errorCode(t, tt.r)
		}
	})

	t.Run("opening or publishing a page takes it off", func(t *testing.T) {
		want(t, ben.post(t, pagePath(ancient, "/visit"), nil), http.StatusNoContent, "ben opens Ancient")
		publishDraft(t, owner, lapsed, "Lapsed", "Brought up to date.", false, "Refreshed")
		h.settle(t)
		sameList(t, "ann's report after", titlesOf(t, want(t, ann.get(t, "/api/v1/stale-pages"), http.StatusOK, "ann's report"), "pages"), "Old")
	})
}

// Straight through SQL as stator_app: the report's function lists nothing to
// somebody who administers no space, keeps to the spaces one does, and opens
// nobody else's visits.
func TestStaleReportIsWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stale-rls")
	other := h.makeMember(t, "stale-rls-other")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID := h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member")

	ops := newTree(t, owner, "SRO", "Reviewed")
	want(t, owner.put(t, "/api/v1/spaces/SRO/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}}), http.StatusOK, "ann administers SRO")
	docs := newTree(t, owner, "SRD", "Not reviewed")
	gone := ops.add(ops.homeID, "Gone quiet")
	elsewhere := docs.add(docs.homeID, "Quiet elsewhere")
	h.backdate(t, org.org, `id IN ($3, $4)`, 400, gone, elsewhere)
	h.visited(t, org.org, org.user, gone, 300)
	h.settle(t)
	var docsID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM space WHERE org_id = $1 AND key = 'SRD'`, org.org).Scan(&docsID); err != nil {
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
	const all = `SELECT count(*) FROM stale_pages($1, NULL, false, NULL, 180, NULL, NULL, 100)`

	t.Run("somebody who administers no space reads nothing", func(t *testing.T) {
		actAs(t, conn, org.org, benID)
		if n := count(all, nil); n != 0 {
			t.Errorf("ben reads %d stale pages", n)
		}
		if n := count(all, docsID); n != 0 {
			t.Errorf("ben reads %d stale pages of a space he views", n)
		}
		var reviewer bool
		if err := conn.QueryRow(ctx, `SELECT stale_reviewer()`).Scan(&reviewer); err != nil || reviewer {
			t.Errorf("ben is a reviewer: %v, %v", reviewer, err)
		}
		if n := count(`SELECT count(*) FROM page_visit WHERE page_id = $1`, gone); n != 0 {
			t.Errorf("ben reads somebody else's visit")
		}
	})

	t.Run("a space administrator reads their spaces and no other", func(t *testing.T) {
		actAs(t, conn, org.org, annID)
		var title string
		var viewed *time.Time
		if err := conn.QueryRow(ctx, `SELECT title, viewed_at FROM stale_pages(NULL, NULL, false, NULL, 180, NULL, NULL, 100)`).Scan(&title, &viewed); err != nil || title != "Gone quiet" || viewed == nil {
			t.Errorf("ann reads %q last viewed %v (%v)", title, viewed, err)
		}
		if n := count(all, nil); n != 1 {
			t.Errorf("ann reads %d stale pages, want her space's one", n)
		}
		if n := count(all, docsID); n != 0 {
			t.Errorf("ann reads %d stale pages of a space she does not administer", n)
		}
		if n := count(`SELECT count(*) FROM page_visit WHERE page_id = $1`, gone); n != 0 {
			t.Errorf("the report opened the owner's visit to ann")
		}
	})

	t.Run("another organization reads nothing of this one", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(all, nil); n != 0 {
			t.Errorf("another organization reads %d stale pages", n)
		}
		if n := count(all, docsID); n != 0 {
			t.Errorf("another organization reads %d stale pages naming this one's space", n)
		}
	})
}

// The report on a realistic organization reads a window, not every page: the
// view rule runs for about the rows it returns, a space the reader does not
// administer is never judged, and the last view of each page is one probe of
// page_visit_latest_idx.
func TestStaleReportReadsAWindowNotTheWholeOrganization(t *testing.T) {
	const (
		pages    = 4000
		hidden   = 2000
		visitors = 3
		limit    = stale.DefaultLimit
	)
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "stale-plan")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.addPerson(t, org.org, "member")
	open := newTree(t, owner, "SPL", "Plenty")
	closed := newTree(t, owner, "SPC", "Closed")
	want(t, owner.put(t, "/api/v1/spaces/SPL/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
	}}), http.StatusOK, "ann administers SPL")
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
	bulk("SPL", open.homeID, pages)
	bulk("SPC", closed.homeID, hidden)
	h.backdate(t, org.org, `title LIKE 'Bulk %'`, 400)
	var people []uuid.UUID
	for range visitors {
		people = append(people, h.addPerson(t, org.org, "member"))
	}
	if _, err := h.super.Exec(ctx, `
		INSERT INTO page_visit (org_id, user_id, page_id, visited_at)
		SELECT $1, u, p.id, now() - make_interval(days => 200 + (random() * 100)::int)
		FROM page p, unnest($2::uuid[]) u
		WHERE p.org_id = $1 AND p.title LIKE 'Bulk %'`, org.org, people); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `ANALYZE page, page_visit`); err != nil {
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
	started := time.Now()
	rows, err := tx.Query(ctx, stale.Query, nil, nil, false, nil, stale.DefaultDays, nil, nil, limit+1)
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
	took := time.Since(started)
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
	probed := false
	for _, plan := range plans {
		if strings.Contains(plan, "page_visit_latest_idx") {
			probed = true
			t.Logf("the report's plan, %v in all:\n%s", took, plan)
		}
	}
	if !probed {
		t.Errorf("no plan probed page_visit_latest_idx; the plans were:\n%s", strings.Join(plans, "\n---\n"))
	}
}
