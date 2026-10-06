//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// publicPath is an address of the reads for anybody in the organization slug.
func publicPath(slug string, rest ...string) string {
	return "/api/v1/public/" + slug + strings.Join(rest, "")
}

// actAnonymously scopes a raw connection to an organization and nobody in it,
// as the api's transactions for a reader who is not signed in are.
func actAnonymously(t *testing.T, conn *pgx.Conn, org uuid.UUID) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), `SELECT set_config($1, $2, false), set_config('app.user_id', '', false), set_config('app.anonymous', 'on', false)`,
		tenant.PostgresVar, org.String()); err != nil {
		t.Fatal(err)
	}
}

// Anonymous access (#79): an administrator opens the organization, a space
// administrator opens a space, and anybody then reads its published pages
// and nothing else, without a name in them. Every refusal has the owner
// reaching the same thing first.
func TestAnonymousReadersReadOnlyWhatIsOpenAndNobodysName(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "public-read")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.namedPerson(t, home.org, "Ina Insider")
	ina := api.as(t, inaID, home.org, slug)
	anon := api.anonymous()
	ctx := context.Background()
	var ownerEmail string
	if err := h.super.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, home.user).Scan(&ownerEmail); err != nil {
		t.Fatal(err)
	}

	open := newTree(t, owner, "OPEN", "Open handbook")
	shut := newTree(t, owner, "SHUT", "Shut away")
	guide := open.add(open.homeID, "Zebra guide")
	publishBody(t, owner, guide, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Cross at the zebra with "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": inaID.String(), "label": "Ina Insider"}},
		}},
	}})
	secret := open.add(open.homeID, "Zebra secret")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "hide Zebra secret")
	below := open.add(secret, "Zebra below the secret")
	draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": open.homeID, "title": "Zebra draft"}), http.StatusCreated, "a draft"), "page")["id"].(string)
	elsewhere := shut.add(shut.homeID, "Zebra elsewhere")
	want(t, ina.post(t, pagePath(guide, "/comments"), map[string]any{"body": commentDoc("Ina was here.")}), http.StatusCreated, "ina comments")
	want(t, ina.post(t, pagePath(guide, "/reactions"), map[string]any{"emoji": "👍"}), http.StatusOK, "ina reacts")
	want(t, ina.post(t, pagePath(guide, "/visit"), nil), http.StatusNoContent, "ina reads the guide")
	file := obj(t, want(t, owner.upload(t, pagePath(guide, "/attachments"), "crossing.txt", []byte("look both ways")), http.StatusCreated, "a file"), "attachment")["id"].(string)
	hidden := obj(t, want(t, owner.upload(t, pagePath(elsewhere, "/attachments"), "hidden.txt", []byte("not for you")), http.StatusCreated, "a hidden file"), "attachment")["id"].(string)

	t.Run("a space opened before the organization is not public", func(t *testing.T) {
		got := obj(t, want(t, owner.put(t, "/api/v1/spaces/OPEN/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open OPEN"), "anonymousAccess")
		if got["view"] != true || got["orgEnabled"] != false {
			t.Errorf("opening OPEN answered %v", got)
		}
		for _, path := range []string{publicPath(slug), publicPath(slug, "/pages/", guide), publicPath(slug, "/spaces/OPEN")} {
			if got := anon.get(t, path); got.Status != http.StatusNotFound || errorCode(t, got) != "not_public" {
				t.Errorf("%s = %d %s, want 404 not_public", path, got.Status, got.Raw)
			}
		}
	})

	t.Run("only administrators open the organization and the space", func(t *testing.T) {
		want(t, ina.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusForbidden, "ina opens the organization")
		want(t, ina.get(t, "/api/v1/org/anonymous-access"), http.StatusForbidden, "ina reads the switch")
		want(t, ina.put(t, "/api/v1/spaces/OPEN/anonymous-access", map[string]any{"view": true}), http.StatusForbidden, "ina opens OPEN")
	})

	got := obj(t, want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization"), "anonymousAccess")
	if got["enabled"] != true || got["indexable"] != false {
		t.Fatalf("the switch answered %v", got)
	}

	t.Run("both switches are in the audit log", func(t *testing.T) {
		for action, what := range map[string]string{"org.anonymous_access_set": "enabled", "space.anonymous_access_set": "view"} {
			var on bool
			if err := h.super.QueryRow(ctx, `SELECT (data->>$3)::boolean FROM audit_log WHERE org_id = $1 AND action = $2 AND actor_user_id = $4`,
				home.org, action, what, home.user).Scan(&on); err != nil || !on {
				t.Errorf("the audit log has %s = %v (%v)", action, on, err)
			}
		}
		if got := obj(t, want(t, owner.get(t, "/api/v1/org/anonymous-access"), http.StatusOK, "the switch"), "anonymousAccess"); got["enabled"] != true {
			t.Errorf("the switch reads %v", got)
		}
		access := obj(t, want(t, owner.get(t, "/api/v1/spaces/OPEN/anonymous-access"), http.StatusOK, "OPEN's access"), "anonymousAccess")
		if access["view"] != true || access["orgEnabled"] != true {
			t.Errorf("OPEN's access reads %v", access)
		}
		grid := list(t, want(t, owner.get(t, "/api/v1/spaces/OPEN/permissions"), http.StatusOK, "OPEN's grid"), "grants")
		for _, each := range grid {
			if each.(map[string]any)["subject"].(map[string]any)["type"] == "anonymous" {
				t.Errorf("the permission grid shows the anonymous grant: %v", each)
			}
		}
	})

	t.Run("the site lists the open space and asks search engines to stay away", func(t *testing.T) {
		got := want(t, anon.get(t, publicPath(slug)), http.StatusOK, "the site")
		if keys := spaceKeysOf(t, got); !slices.Equal(keys, []string{"OPEN"}) {
			t.Errorf("the site lists %v", keys)
		}
		if site := obj(t, got, "site"); site["name"] != "public-read" || site["indexable"] != false {
			t.Errorf("the site is %v", site)
		}
		resp, _ := anon.download(t, publicPath(slug))
		if resp.Header.Get("X-Robots-Tag") != "noindex, nofollow" || !strings.HasPrefix(resp.Header.Get("Cache-Control"), "public") {
			t.Errorf("the site is served with robots %q and caching %q", resp.Header.Get("X-Robots-Tag"), resp.Header.Get("Cache-Control"))
		}
	})

	t.Run("the tree holds published pages nobody restricted", func(t *testing.T) {
		pages := titlesOf(t, want(t, anon.get(t, publicPath(slug, "/spaces/OPEN")), http.StatusOK, "the open tree"), "pages")
		if !has(pages, "Zebra guide") || has(pages, "Zebra secret") || has(pages, "Zebra below the secret") || has(pages, "Zebra draft") {
			t.Errorf("the open tree lists %v", pages)
		}
		want(t, anon.get(t, publicPath(slug, "/spaces/SHUT")), http.StatusNotFound, "the shut tree")
	})

	t.Run("a page reads with nobody named in it", func(t *testing.T) {
		got := want(t, anon.get(t, publicPath(slug, "/pages/", guide)), http.StatusOK, "the guide")
		for _, personal := range []string{"Ina Insider", inaID.String(), home.user.String(), "Person of public-read", ownerEmail, "Ina was here", "👍"} {
			if containsText(got.Raw, personal) {
				t.Errorf("the public guide names %q: %s", personal, got.Raw)
			}
		}
		p := obj(t, got, "page")
		for _, field := range []string{"createdByName", "updatedByName", "comments", "reactions", "owner", "watching", "can"} {
			if _, ok := p[field]; ok {
				t.Errorf("the public guide carries %s", field)
			}
		}
		if !containsText(got.Raw, "Cross at the zebra") || !containsText(got.Raw, `"type":"mention"`) {
			t.Errorf("the public guide lost its words: %s", got.Raw)
		}
	})

	t.Run("what is not public is not found, whoever asks", func(t *testing.T) {
		for what, id := range map[string]string{"a restricted page": secret, "a page below it": below, "a draft": draft, "a page of a shut space": elsewhere} {
			want(t, owner.get(t, pagePath(id)), http.StatusOK, "the owner reads "+what)
			if got := anon.get(t, publicPath(slug, "/pages/", id)); got.Status != http.StatusNotFound {
				t.Errorf("%s = %d %s", what, got.Status, got.Raw)
			}
			// The owner's session rides along and widens nothing.
			if got := owner.get(t, publicPath(slug, "/pages/", id)); got.Status != http.StatusNotFound {
				t.Errorf("%s with a session riding along = %d", what, got.Status)
			}
		}
		other := h.makeMember(t, "public-other")
		want(t, anon.get(t, publicPath(h.slugOf(t, other.org))), http.StatusNotFound, "a closed organization")
		want(t, anon.get(t, publicPath("no-such-org-here")), http.StatusNotFound, "no organization")
	})

	t.Run("files of public pages download and others do not", func(t *testing.T) {
		resp, body := anon.download(t, publicPath(slug, "/attachments/", file))
		if resp.StatusCode != http.StatusOK || string(body) != "look both ways" {
			t.Errorf("the public file = %d %q", resp.StatusCode, body)
		}
		want(t, anon.get(t, publicPath(slug, "/attachments/", hidden)), http.StatusNotFound, "a file of a shut page")
	})

	t.Run("search finds public pages by their words, never by a name", func(t *testing.T) {
		q := url.Values{"q": {"zebra"}}
		if got := hitTitles(t, searchFor(t, owner, q)); !has(got, "Zebra secret") || !has(got, "Zebra elsewhere") {
			t.Errorf("the owner finds %v", got)
		}
		titles := func(r response) []string {
			var out []string
			for _, each := range list(t, r, "hits") {
				out = append(out, each.(map[string]any)["page"].(map[string]any)["title"].(string))
			}
			return out
		}
		got := want(t, anon.get(t, publicPath(slug, "/search?q=zebra")), http.StatusOK, "search zebra")
		if found := titles(got); !slices.Equal(found, []string{"Zebra guide"}) {
			t.Errorf("anybody finds %v", found)
		}
		if containsText(got.Raw, "Ina") {
			t.Errorf("a snippet names Ina: %s", got.Raw)
		}
		if found := titles(want(t, anon.get(t, publicPath(slug, "/search?q=Insider")), http.StatusOK, "search a name")); len(found) != 0 {
			t.Errorf("anybody finds pages by the name of a person they mention: %v", found)
		}
		want(t, anon.get(t, publicPath(slug, "/search?q=")), http.StatusUnprocessableEntity, "an empty search")
	})

	t.Run("everything else still wants a sign-in", func(t *testing.T) {
		for path, method := range map[string]string{
			pagePath(guide):                       http.MethodGet,
			pagePath(guide, "/comments"):          http.MethodGet,
			pagePath(guide, "/versions"):          http.MethodGet,
			pagePath(guide, "/contributors"):      http.MethodGet,
			pagePath(guide, "/draft"):             http.MethodGet,
			pagePath(guide, "/collab"):            http.MethodGet,
			"/api/v1/attachments/" + file:         http.MethodGet,
			"/api/v1/search?q=zebra":              http.MethodGet,
			"/api/v1/spaces/OPEN":                 http.MethodGet,
			pagePath(guide, "/comments") + "?x=1": http.MethodPost,
			"/api/v1/mcp":                         http.MethodPost,
		} {
			if got := anon.call(t, method, path, map[string]any{}); got.Status != http.StatusUnauthorized {
				t.Errorf("%s %s = %d, want 401", method, path, got.Status)
			}
		}
		if got := anon.post(t, publicPath(slug, "/pages/", guide), map[string]any{}); got.Status != http.StatusMethodNotAllowed {
			t.Errorf("a write to a public page = %d", got.Status)
		}
	})

	t.Run("a personal space stays private", func(t *testing.T) {
		mine := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "MINE", "name": "Mine", "personal": true}), http.StatusCreated, "a personal space"), "space")
		key := mine["key"].(string)
		fieldError(t, want(t, owner.put(t, "/api/v1/spaces/"+key+"/anonymous-access", map[string]any{"view": true}), http.StatusUnprocessableEntity, "open a personal space"), "view")
		conn := appConn(t)
		actAs(t, conn, home.org, home.user)
		denied(t, conn, "a personal space opened through SQL", `INSERT INTO space_grant (org_id, space_id, permission, subject_type) VALUES ($1, $2, 'view', 'anonymous')`,
			home.org, mine["id"])
		denied(t, conn, "an anonymous grant beyond view", `INSERT INTO space_grant (org_id, space_id, permission, subject_type)
			SELECT $1, id, 'addComments', 'anonymous' FROM space WHERE key = 'SHUT'`, home.org)
	})

	t.Run("straight through SQL an anonymous reader reaches the open pages and writes nothing", func(t *testing.T) {
		conn := appConn(t)
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		closed := map[string]string{
			"the restricted page":       `SELECT count(*) FROM page WHERE id = '` + secret + `'`,
			"the page below it":         `SELECT count(*) FROM page WHERE id = '` + below + `'`,
			"the draft":                 `SELECT count(*) FROM page WHERE id = '` + draft + `'`,
			"the shut space":            `SELECT count(*) FROM space WHERE key = 'SHUT'`,
			"the shut page":             `SELECT count(*) FROM page WHERE id = '` + elsewhere + `'`,
			"the shut file":             `SELECT count(*) FROM attachment WHERE id = '` + hidden + `'`,
			"people":                    `SELECT count(*) FROM app_user`,
			"memberships":               `SELECT count(*) FROM org_member`,
			"the organization":          `SELECT count(*) FROM org`,
			"the guide's versions":      `SELECT count(*) FROM page_version WHERE page_id = '` + guide + `'`,
			"the guide's comments":      `SELECT count(*) FROM comment WHERE page_id = '` + guide + `'`,
			"the guide's reactions":     `SELECT count(*) FROM reaction WHERE page_id = '` + guide + `'`,
			"the guide's views":         `SELECT count(*) FROM page_view WHERE page_id = '` + guide + `'`,
			"the grants":                `SELECT count(*) FROM space_grant`,
			"the restrictions":          `SELECT count(*) FROM page_restriction`,
			"the audit log":             `SELECT count(*) FROM audit_log`,
			"the sessions":              `SELECT count(*) FROM user_session`,
			"the shut page in the feed": `SELECT count(*) FROM home_edited(NULL, NULL, 100)`,
		}
		actAs(t, conn, home.org, home.user)
		for what, sql := range closed {
			if what == "people" || what == "memberships" || what == "the sessions" || what == "the guide's views" {
				continue
			}
			if count(sql) == 0 {
				t.Fatalf("the owner reads no rows of %s, so the anonymous zero would prove nothing", what)
			}
		}
		actAnonymously(t, conn, home.org)
		for what, sql := range closed {
			if n := count(sql); n != 0 {
				t.Errorf("an anonymous reader reads %d rows of %s", n, what)
			}
		}
		for what, sql := range map[string]string{
			"the guide":      `SELECT count(*) FROM page WHERE id = '` + guide + `'`,
			"the open space": `SELECT count(*) FROM space WHERE key = 'OPEN'`,
			"the guide file": `SELECT count(*) FROM attachment WHERE id = '` + file + `'`,
		} {
			if count(sql) != 1 {
				t.Errorf("an anonymous reader does not read %s", what)
			}
		}
		openID := open.spaceID(t)
		denied(t, conn, "a page under the guide", `INSERT INTO page (org_id, space_id, parent_id, rank, title) VALUES ($1, $2, $3, 'W', 'Planted')`, home.org, openID, guide)
		untouched(t, conn, "retitling the guide", `UPDATE page SET title = 'Taken' WHERE id = $1`, guide)
		untouched(t, conn, "deleting the guide", `DELETE FROM page WHERE id = $1`, guide)
		untouched(t, conn, "renaming the open space", `UPDATE space SET name = 'Taken' WHERE key = 'OPEN'`)
		untouched(t, conn, "deleting the guide's file", `DELETE FROM attachment WHERE id = $1`, file)
		denied(t, conn, "a comment", `INSERT INTO comment (org_id, page_id, body) VALUES ($1, $2, '{"type":"doc"}')`, home.org, guide)
		denied(t, conn, "a view", `INSERT INTO page_view (org_id, page_id) VALUES ($1, $2)`, home.org, guide)
		denied(t, conn, "a grant", `INSERT INTO space_grant (org_id, space_id, permission, subject_type) VALUES ($1, $2, 'addPages', 'everyone')`, home.org, openID)
		denied(t, conn, "an event", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'page.published', '{}')`, home.org)
		untouched(t, conn, "turning the switch", `UPDATE org SET anonymous_access = false`)
		refused(t, conn, "trashing the guide", `SELECT page_trash($1)`, guide)
	})

	t.Run("closing the space or the organization ends it at once", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/OPEN/anonymous-access", map[string]any{"view": false}), http.StatusOK, "close OPEN")
		want(t, anon.get(t, publicPath(slug, "/pages/", guide)), http.StatusNotFound, "the guide of a closed space")
		want(t, owner.put(t, "/api/v1/spaces/OPEN/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open OPEN again")
		want(t, anon.get(t, publicPath(slug, "/pages/", guide)), http.StatusOK, "the guide again")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": false}), http.StatusOK, "close the organization")
		want(t, anon.get(t, publicPath(slug, "/pages/", guide)), http.StatusNotFound, "the guide of a closed organization")
		want(t, anon.get(t, publicPath(slug)), http.StatusNotFound, "a closed site")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true, "indexable": true}), http.StatusOK, "open for search engines")
		resp, _ := anon.download(t, publicPath(slug))
		if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Robots-Tag") != "" {
			t.Errorf("an indexable site = %d with robots %q", resp.StatusCode, resp.Header.Get("X-Robots-Tag"))
		}
	})
}

// Every table holds an anonymous reader by a policy of its own: closed, or
// for the three a public page is read from, read only.
func TestAnonymousReadersAreHeldByEveryTable(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT c.relname,
		       EXISTS (SELECT 1 FROM pg_policies p WHERE p.schemaname = 'public' AND p.tablename = c.relname
		               AND p.permissive = 'RESTRICTIVE' AND p.cmd = 'ALL' AND 'stator_app' = ANY (p.roles)
		               AND p.qual LIKE '%current_anonymous()%' AND p.with_check LIKE '%current_anonymous()%'),
		       (SELECT count(*) FROM pg_policies p WHERE p.schemaname = 'public' AND p.tablename = c.relname
		               AND p.permissive = 'RESTRICTIVE' AND p.cmd IN ('INSERT', 'UPDATE', 'DELETE') AND 'stator_app' = ANY (p.roles)
		               AND COALESCE(p.qual, p.with_check) LIKE '%current_anonymous()%')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	readable := []string{"attachment", "page", "space"}
	seen := 0
	for rows.Next() {
		var (
			name   string
			closed bool
			writes int
		)
		if err := rows.Scan(&name, &closed, &writes); err != nil {
			t.Fatal(err)
		}
		seen++
		switch {
		case slices.Contains(readable, name):
			if closed || writes != 3 {
				t.Errorf("%s should be readable and not writable by an anonymous reader: closed %v, %d write policies", name, closed, writes)
			}
		case !closed:
			t.Errorf("%s has no policy closing it to anonymous readers; add one as 00470 does", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen < 3 {
		t.Fatalf("only %d tables found; the schema is not migrated", seen)
	}
}
