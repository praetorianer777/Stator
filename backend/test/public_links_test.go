//go:build integration

package test

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/public"
)

// linkPath is the api's address of what a link opens, and below it.
func linkPath(slug, token string, rest ...string) string {
	return publicPath(slug, "/links/", token) + strings.Join(rest, "")
}

// readThrough scopes a raw connection to an organization as an anonymous
// reader holding a token, as the api's transactions for a link are.
func readThrough(t *testing.T, conn *pgx.Conn, org uuid.UUID, token string) {
	t.Helper()
	actAnonymously(t, conn, org)
	if _, err := conn.Exec(context.Background(), `SELECT set_config('app.page_link', $1, false)`, hex.EncodeToString(auth.HashToken(token))); err != nil {
		t.Fatal(err)
	}
}

// Public links (#80): whoever may edit a published page makes a link that
// lets anybody read that page, and nothing else, until it is revoked, runs
// out, or the organization stops links; who may not is refused, and the
// database holds every refusal itself.
func TestAPublicLinkOpensOnePageToAnybodyUntilItEnds(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "public-links")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.namedPerson(t, home.org, "Ina Insider")
	ina := api.as(t, inaID, home.org, slug)
	anon := api.anonymous()
	ctx := context.Background()

	docs := newTree(t, owner, "LINK", "Linked handbook")
	guide := docs.add(docs.homeID, "Linked guide")
	publishBody(t, owner, guide, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Ask at the desk, or ask "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": inaID.String(), "label": "Ina Insider"}},
		}},
	}})
	below := docs.add(guide, "Below the guide")
	beside := docs.add(docs.homeID, "Beside the guide")
	secret := docs.add(docs.homeID, "Restricted page")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "restrict the secret")
	draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft"}), http.StatusCreated, "a draft"), "page")["id"].(string)
	box := folder(docs, docs.homeID, "A folder")
	// Ina reads the guide and may not change it.
	want(t, restrict(t, owner, guide, nil, []any{user(home.user)}), http.StatusOK, "keep the guide's edits to the owner")
	want(t, ina.post(t, pagePath(guide, "/comments"), map[string]any{"body": commentDoc("Ina was here.")}), http.StatusCreated, "ina comments")
	file := obj(t, want(t, owner.upload(t, pagePath(guide, "/attachments"), "desk.txt", []byte("opening hours")), http.StatusCreated, "a file"), "attachment")["id"].(string)
	besideFile := obj(t, want(t, owner.upload(t, pagePath(beside, "/attachments"), "beside.txt", []byte("not linked")), http.StatusCreated, "a file beside"), "attachment")["id"].(string)

	if got := obj(t, want(t, owner.get(t, "/api/v1/org/public-links"), http.StatusOK, "the switch"), "publicLinks"); got["enabled"] != true {
		t.Fatalf("public links start %v, want allowed", got)
	}
	var max int
	if err := h.super.QueryRow(ctx, `SELECT page_link_max()`).Scan(&max); err != nil || max != public.MaxLinksPerPage {
		t.Errorf("the database allows %d links a page (%v), the service %d", max, err, public.MaxLinksPerPage)
	}

	made := want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"label": "For the auditors"}), http.StatusCreated, "make a link")
	token, _ := made.Body["token"].(string)
	linkID := obj(t, made, "link")["id"].(string)
	if !public.IsToken(token) || made.Body["path"] != "/public/"+slug+"/link/"+token {
		t.Fatalf("the link answered token %q and path %v", token, made.Body["path"])
	}

	t.Run("the editors see the link and its maker, never its token", func(t *testing.T) {
		got := obj(t, want(t, owner.get(t, pagePath(guide, "/public-links")), http.StatusOK, "the guide's links"), "publicLinks")
		links := got["links"].([]any)
		if len(links) != 1 || got["refusal"] != nil || got["max"] != float64(public.MaxLinksPerPage) {
			t.Fatalf("the guide's links read %v", got)
		}
		link := links[0].(map[string]any)
		if link["label"] != "For the auditors" || link["expiresAt"] != nil || link["createdBy"].(map[string]any)["name"] != "Person of public-links" {
			t.Errorf("the link reads %v", link)
		}
		if raw := want(t, owner.get(t, pagePath(guide, "/public-links")), http.StatusOK, "again").Raw; containsText(raw, token) {
			t.Error("the list carries the token")
		}
		if data := h.recordedOnce(t, home.org, "page.public_link_created", &home.user, guide); !strings.Contains(data, linkID) || strings.Contains(data, token) {
			t.Errorf("the link's record reads %s", data)
		}
	})

	t.Run("who may not edit the page sees no link and makes none", func(t *testing.T) {
		got := obj(t, want(t, ina.get(t, pagePath(guide, "/public-links")), http.StatusOK, "ina asks"), "publicLinks")
		if got["refusal"] != "cannotManage" || len(got["links"].([]any)) != 0 {
			t.Errorf("ina is shown %v", got)
		}
		want(t, ina.post(t, pagePath(guide, "/public-links"), map[string]any{}), http.StatusForbidden, "ina makes a link")
		want(t, ina.delete(t, pagePath(guide, "/public-links/", linkID)), http.StatusForbidden, "ina revokes the link")
		want(t, ina.put(t, "/api/v1/org/public-links", map[string]any{"enabled": false}), http.StatusForbidden, "ina turns links off")
		want(t, ina.get(t, "/api/v1/org/public-links"), http.StatusForbidden, "ina reads the switch")
		want(t, owner.get(t, pagePath(uuid.NewString(), "/public-links")), http.StatusNotFound, "the links of no page")
	})

	t.Run("anybody reads the page through it, with nobody named and nothing kept", func(t *testing.T) {
		resp, raw := anon.download(t, linkPath(slug, token))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the link = %d %s", resp.StatusCode, raw)
		}
		for header, want := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow"} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("the link is served with %s %q, want %q", header, got, want)
			}
		}
		got := want(t, anon.get(t, linkPath(slug, token)), http.StatusOK, "the link")
		p := obj(t, got, "page")
		if p["id"] != guide || p["title"] != "Linked guide" || !containsText(got.Raw, "Ask at the desk") {
			t.Errorf("the link opens %s", got.Raw)
		}
		for _, personal := range []string{"Ina Insider", inaID.String(), home.user.String(), "Person of public-links", "Ina was here", "Linked handbook", "LINK", "Below the guide"} {
			if containsText(got.Raw, personal) {
				t.Errorf("the linked page names %q: %s", personal, got.Raw)
			}
		}
		for _, field := range []string{"space", "ancestors", "children", "comments", "createdByName"} {
			if _, ok := p[field]; ok {
				t.Errorf("the linked page carries %s", field)
			}
		}
		if site := obj(t, got, "site"); site["slug"] != slug || site["indexable"] != false {
			t.Errorf("the link names the site %v", site)
		}
	})

	t.Run("its page's files open through it and no other", func(t *testing.T) {
		resp, body := anon.download(t, linkPath(slug, token, "/attachments/", file))
		if resp.StatusCode != http.StatusOK || string(body) != "opening hours" || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("the guide's file = %d %q, caching %q", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
		}
		want(t, anon.get(t, linkPath(slug, token, "/attachments/", besideFile)), http.StatusNotFound, "a file beside")
	})

	t.Run("nothing else opens through it", func(t *testing.T) {
		for what, path := range map[string]string{
			"the page through the public reads": publicPath(slug, "/pages/", guide),
			"the page below":                    publicPath(slug, "/pages/", below),
			"the site":                          publicPath(slug),
			"a token nobody holds":              linkPath(slug, strings.Repeat("x", 43)),
			"a token of the wrong shape":        linkPath(slug, "short"),
		} {
			if got := anon.get(t, path); got.Status != http.StatusNotFound {
				t.Errorf("%s = %d %s", what, got.Status, got.Raw)
			}
		}
		other := h.makeMember(t, "public-links-other")
		if got := anon.get(t, linkPath(h.slugOf(t, other.org), token)); got.Status != http.StatusNotFound || errorCode(t, got) != "link_gone" {
			t.Errorf("the token under another organization = %d %s", got.Status, got.Raw)
		}
		for path, method := range map[string]string{
			pagePath(guide):                  http.MethodGet,
			pagePath(guide, "/comments"):     http.MethodGet,
			pagePath(guide, "/versions"):     http.MethodGet,
			pagePath(guide, "/public-links"): http.MethodGet,
		} {
			if got := anon.call(t, method, path, nil); got.Status != http.StatusUnauthorized {
				t.Errorf("%s %s without a session = %d", method, path, got.Status)
			}
		}
	})

	t.Run("a page that may not be opened gets no link, and says why", func(t *testing.T) {
		personal := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "MINE", "name": "Mine", "personal": true}), http.StatusCreated, "a personal space"), "space")
		for what, c := range map[string]struct{ page, reason string }{
			"a restricted page":     {secret, "restricted"},
			"a page below it":       {docs.add(secret, "Below the secret"), "restricted"},
			"a draft":               {draft, "unpublished"},
			"a folder":              {box, "folder"},
			"a personal space page": {personal["homePageId"].(string), "personal"},
		} {
			refusal := obj(t, want(t, owner.get(t, pagePath(c.page, "/public-links")), http.StatusOK, what+"'s links"), "publicLinks")["refusal"]
			if refusal != c.reason {
				t.Errorf("%s is refused as %v, want %s", what, refusal, c.reason)
			}
			if got := owner.post(t, pagePath(c.page, "/public-links"), map[string]any{}); got.Status != http.StatusConflict || errorCode(t, got) != "link_refused" {
				t.Errorf("a link for %s = %d %s", what, got.Status, got.Raw)
			}
		}
		fieldError(t, want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"expiresAt": "2020-01-01T00:00:00Z"}), http.StatusUnprocessableEntity, "a link run out already"), "expiresAt")
		fieldError(t, want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"label": strings.Repeat("l", public.MaxLinkLabelLength+1)}), http.StatusUnprocessableEntity, "a long label"), "label")
	})

	t.Run("a page has as many links as it may and no more", func(t *testing.T) {
		var extra []string
		for i := 1; i < public.MaxLinksPerPage; i++ {
			extra = append(extra, obj(t, want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"expiresAt": "2099-01-01T00:00:00Z"}), http.StatusCreated, "another link"), "link")["id"].(string))
		}
		if got := owner.post(t, pagePath(guide, "/public-links"), map[string]any{}); got.Status != http.StatusConflict || !strings.Contains(string(got.Raw), "Revoke one first") {
			t.Errorf("one link too many = %d %s", got.Status, got.Raw)
		}
		if refusal := obj(t, want(t, owner.get(t, pagePath(guide, "/public-links")), http.StatusOK, "a full page's links"), "publicLinks")["refusal"]; refusal != "full" {
			t.Errorf("a full page is refused as %v", refusal)
		}
		for _, id := range extra {
			want(t, owner.delete(t, pagePath(guide, "/public-links/", id)), http.StatusNoContent, "revoke an extra link")
		}
	})

	t.Run("straight through SQL a token reaches its page, its files and nothing more", func(t *testing.T) {
		conn := appConn(t)
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		readThrough(t, conn, home.org, token)
		if n := count(`SELECT count(*) FROM page`); n != 1 {
			t.Errorf("a reader holding the link reads %d pages, want the guide alone", n)
		}
		if n := count(`SELECT count(*) FROM page WHERE id = $1`, guide); n != 1 {
			t.Error("a reader holding the link does not read the guide")
		}
		if n := count(`SELECT count(*) FROM attachment`); n != 1 {
			t.Errorf("a reader holding the link reads %d files, want the guide's one", n)
		}
		for what, sql := range map[string]string{
			"the space":    `SELECT count(*) FROM space`,
			"the links":    `SELECT count(*) FROM page_link`,
			"versions":     `SELECT count(*) FROM page_version`,
			"comments":     `SELECT count(*) FROM comment`,
			"people":       `SELECT count(*) FROM app_user`,
			"restrictions": `SELECT count(*) FROM page_restriction`,
			"the audit":    `SELECT count(*) FROM audit_log`,
		} {
			if n := count(sql); n != 0 {
				t.Errorf("a reader holding the link reads %d rows of %s", n, what)
			}
		}
		untouched(t, conn, "retitling the guide", `UPDATE page SET title = 'Taken' WHERE id = $1`, guide)
		denied(t, conn, "a link made by nobody", `INSERT INTO page_link (org_id, page_id, token_hash) VALUES ($1, $2, sha256('x'))`, home.org, guide)
		untouched(t, conn, "the link revoked by nobody", `UPDATE page_link SET revoked_at = now()`)

		readThrough(t, conn, home.org, strings.Repeat("x", 43))
		if n := count(`SELECT count(*) FROM page`); n != 0 {
			t.Errorf("a token nobody holds reads %d pages", n)
		}
		if _, err := conn.Exec(ctx, `SELECT set_config('app.page_link', 'not hex at all', false)`); err != nil {
			t.Fatal(err)
		}
		if n := count(`SELECT count(*) FROM page`); n != 0 {
			t.Errorf("a malformed digest reads %d pages", n)
		}

		// A member holding the digest is no anonymous reader, and is not widened by it.
		actAs(t, conn, home.org, inaID)
		if _, err := conn.Exec(ctx, `SELECT set_config('app.page_link', $1, false)`, hex.EncodeToString(auth.HashToken(token))); err != nil {
			t.Fatal(err)
		}
		if n := count(`SELECT count(*) FROM page WHERE id = $1`, secret); n != 0 {
			t.Error("ina reads the secret holding a link's digest")
		}
		if n := count(`SELECT count(*) FROM page_link`); n != 0 {
			t.Errorf("ina, who may not edit the guide, reads %d of its links", n)
		}
		denied(t, conn, "ina makes a link", `INSERT INTO page_link (org_id, page_id, token_hash, created_by) VALUES ($1, $2, sha256('ina'), $3)`, home.org, guide, inaID)
		untouched(t, conn, "ina revokes the link", `UPDATE page_link SET revoked_at = now(), revoked_by = $2 WHERE id = $1`, linkID, inaID)

		actAs(t, conn, home.org, home.user)
		denied(t, conn, "a link for the restricted page", `INSERT INTO page_link (org_id, page_id, token_hash, created_by) VALUES ($1, $2, sha256('secret'), $3)`, home.org, secret, home.user)
		denied(t, conn, "a link for the draft", `INSERT INTO page_link (org_id, page_id, token_hash, created_by) VALUES ($1, $2, sha256('draft'), $3)`, home.org, draft, home.user)
		denied(t, conn, "a link in somebody else's name", `INSERT INTO page_link (org_id, page_id, token_hash, created_by) VALUES ($1, $2, sha256('ina'), $3)`, home.org, guide, inaID)
		refused(t, conn, "changing which page a link opens", `UPDATE page_link SET page_id = $2 WHERE id = $1`, linkID, beside)
		refused(t, conn, "deleting a link", `DELETE FROM page_link WHERE id = $1`, linkID)
	})

	t.Run("a link stops while the page is restricted or links are off, and comes back", func(t *testing.T) {
		want(t, restrict(t, owner, guide, []any{user(home.user), user(inaID)}, []any{user(home.user)}), http.StatusOK, "restrict the guide")
		if got := anon.get(t, linkPath(slug, token)); got.Status != http.StatusNotFound || errorCode(t, got) != "link_gone" {
			t.Errorf("the link of a restricted page = %d %s", got.Status, got.Raw)
		}
		want(t, restrict(t, owner, guide, nil, []any{user(home.user)}), http.StatusOK, "lift the view list")
		want(t, anon.get(t, linkPath(slug, token)), http.StatusOK, "the link again")

		want(t, owner.put(t, "/api/v1/org/public-links", map[string]any{"enabled": false}), http.StatusOK, "turn links off")
		want(t, anon.get(t, linkPath(slug, token)), http.StatusNotFound, "the link while links are off")
		want(t, anon.get(t, linkPath(slug, token, "/attachments/", file)), http.StatusNotFound, "the file while links are off")
		if got := owner.post(t, pagePath(guide, "/public-links"), map[string]any{}); got.Status != http.StatusConflict || errorCode(t, got) != "link_refused" {
			t.Errorf("a link while links are off = %d %s", got.Status, got.Raw)
		}
		conn := appConn(t)
		readThrough(t, conn, home.org, token)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page`).Scan(&n); err != nil || n != 0 {
			t.Errorf("while links are off a reader holding one reads %d pages (%v)", n, err)
		}
		if data := h.recordedOnce(t, home.org, "org.public_links_set", &home.user, nil); !strings.Contains(data, `"enabled": false`) {
			t.Errorf("the switch's record reads %s", data)
		}
		want(t, owner.put(t, "/api/v1/org/public-links", map[string]any{"enabled": true}), http.StatusOK, "turn links on")
		want(t, anon.get(t, linkPath(slug, token)), http.StatusOK, "the link once links are on again")
	})

	t.Run("revoking ends the link for good and keeps the row", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(guide, "/public-links/", linkID)), http.StatusNoContent, "revoke the link")
		want(t, owner.delete(t, pagePath(guide, "/public-links/", linkID)), http.StatusNotFound, "revoke it twice")
		if got := anon.get(t, linkPath(slug, token)); got.Status != http.StatusNotFound || errorCode(t, got) != "link_gone" {
			t.Errorf("a revoked link = %d %s", got.Status, got.Raw)
		}
		want(t, anon.get(t, linkPath(slug, token, "/attachments/", file)), http.StatusNotFound, "a revoked link's file")
		var by uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT revoked_by FROM page_link WHERE id = $1 AND revoked_at IS NOT NULL`, linkID).Scan(&by); err != nil || by != home.user {
			t.Errorf("the revoked row is gone or unmarked: %v %v", by, err)
		}
		var records int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'page.public_link_revoked'
			AND actor_user_id = $2 AND target_id = $3 AND data->>'link' = $4`, home.org, home.user, guide, linkID).Scan(&records); err != nil || records != 1 {
			t.Errorf("the revocation is recorded %d times (%v), want once", records, err)
		}
		conn := appConn(t)
		actAs(t, conn, home.org, home.user)
		refused(t, conn, "revoking a revoked link again", `UPDATE page_link SET revoked_at = NULL, revoked_by = $2 WHERE id = $1`, linkID, home.user)
		readThrough(t, conn, home.org, token)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page`).Scan(&n); err != nil || n != 0 {
			t.Errorf("a revoked link reads %d pages (%v)", n, err)
		}
	})

	t.Run("a link that ran out opens nothing", func(t *testing.T) {
		brief := want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"expiresAt": "2099-01-01T00:00:00Z"}), http.StatusCreated, "a brief link")
		briefToken := brief.Body["token"].(string)
		want(t, anon.get(t, linkPath(slug, briefToken)), http.StatusOK, "the brief link")
		if _, err := h.super.Exec(ctx, `UPDATE page_link SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1`,
			obj(t, brief, "link")["id"]); err != nil {
			t.Fatal(err)
		}
		// Written past the api, so the replica the reader is served from may lag.
		waitFor(t, "the run out link to open nothing", func() bool { return anon.get(t, linkPath(slug, briefToken)).Status == http.StatusNotFound })
		conn := appConn(t)
		readThrough(t, conn, home.org, briefToken)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page`).Scan(&n); err != nil || n != 0 {
			t.Errorf("a run out link reads %d pages (%v)", n, err)
		}
		if links := obj(t, want(t, owner.get(t, pagePath(guide, "/public-links")), http.StatusOK, "the live links"), "publicLinks")["links"].([]any); len(links) != 0 {
			t.Errorf("revoked and run out links are listed: %v", links)
		}
	})
}
