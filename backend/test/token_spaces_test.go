//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// spaceKeysOf lists the keys of the spaces an answer lists.
func spaceKeysOf(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "spaces") {
		out = append(out, each.(map[string]any)["key"].(string))
	}
	slices.Sort(out)
	return out
}

// A token limited to spaces reaches those and nothing else, though the person
// who made it is the organization's owner and reaches everything. Each
// refusal is checked against that owner, so it is the token's and not theirs.
func TestATokenLimitedToSpacesReachesNothingElse(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "token-spaces")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)

	near := newTree(t, owner, "NEAR", "Near")
	far := newTree(t, owner, "FAR", "Far")
	nearPage := near.add(near.homeID, "Near zebra")
	farPage := far.add(far.homeID, "Far zebra")
	h.backdate(t, home.org, "id = ANY ($3::uuid[])", 400, []string{nearPage, farPage})
	nearID := obj(t, want(t, owner.get(t, "/api/v1/spaces/NEAR"), http.StatusOK, "NEAR"), "space")["id"].(string)
	farID := obj(t, want(t, owner.get(t, "/api/v1/spaces/FAR"), http.StatusOK, "FAR"), "space")["id"].(string)

	t.Run("a token names only spaces its maker sees", func(t *testing.T) {
		got := want(t, owner.post(t, "/api/v1/tokens", map[string]any{"name": "lost", "spaces": []string{"NEAR", "NOPE"}}), http.StatusUnprocessableEntity, "a token naming an unknown space")
		if fields, _ := obj(t, got, "error")["fields"].(map[string]any); fields["spaces"] == nil {
			t.Errorf("the refusal does not name the spaces: %s", got.Raw)
		}
		if n := h.count(t, home.ctx, `SELECT count(*) FROM api_token WHERE name = 'lost'`); n != 0 {
			t.Errorf("the refused token was kept: %d", n)
		}
	})

	tokenID, secret := makeToken(t, owner, map[string]any{"name": "pipeline", "spaces": []string{"near"}})
	_, wholeSecret := makeToken(t, owner, map[string]any{"name": "everywhere"})
	limited := api.withToken(secret)
	whole := api.withToken(wholeSecret)
	h.settle(t)

	t.Run("the list says what each token reaches", func(t *testing.T) {
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/tokens"), http.StatusOK, "list tokens"), "tokens") {
			tok := each.(map[string]any)
			spaces, _ := json.Marshal(tok["spaces"])
			switch tok["name"] {
			case "pipeline":
				if string(spaces) != `["NEAR"]` || tok["allSpaces"] != false {
					t.Errorf("pipeline reaches %s, all %v", spaces, tok["allSpaces"])
				}
			case "everywhere":
				if string(spaces) != `[]` || tok["allSpaces"] != true {
					t.Errorf("everywhere reaches %s, all %v", spaces, tok["allSpaces"])
				}
			}
		}
		var audited string
		if err := h.super.QueryRow(context.Background(), `SELECT data->>'spaces' FROM audit_log WHERE org_id = $1 AND target_id = $2 AND action = 'token.created'`,
			home.org, tokenID).Scan(&audited); err != nil || audited != `["NEAR"]` {
			t.Errorf("the audit entry names %q (%v), want NEAR", audited, err)
		}
	})

	t.Run("it reads and writes in its space", func(t *testing.T) {
		want(t, limited.get(t, "/api/v1/spaces/NEAR"), http.StatusOK, "read its space")
		want(t, limited.get(t, pagePath(nearPage)), http.StatusOK, "read a page of its space")
		want(t, limited.post(t, "/api/v1/pages", map[string]any{"parentId": nearPage, "title": "From the pipeline", "publish": true}), http.StatusCreated, "write in its space")
		want(t, limited.post(t, pagePath(nearPage, "/comments"), map[string]any{"body": commentDoc("Ran.")}), http.StatusCreated, "comment in its space")
		// The owner's role still holds inside it.
		want(t, limited.get(t, "/api/v1/spaces/NEAR/permissions"), http.StatusOK, "administer its space")
	})

	t.Run("anything else is not found, while its owner still sees it", func(t *testing.T) {
		if got := spaceKeysOf(t, want(t, limited.get(t, "/api/v1/spaces"), http.StatusOK, "list spaces")); !slices.Equal(got, []string{"NEAR"}) {
			t.Errorf("the token lists %v, want NEAR", got)
		}
		want(t, limited.get(t, "/api/v1/spaces/FAR"), http.StatusNotFound, "read another space")
		want(t, limited.get(t, pagePath(farPage)), http.StatusNotFound, "read a page of another space")
		if got := limited.post(t, "/api/v1/pages", map[string]any{"parentId": farPage, "title": "Should not land", "publish": true}); got.Status < 400 {
			t.Errorf("the token wrote into another space: %d %s", got.Status, got.Raw)
		}
		if got := limited.post(t, pagePath(farPage, "/comments"), map[string]any{"body": commentDoc("Should not land.")}); got.Status < 400 {
			t.Errorf("the token commented in another space: %d %s", got.Status, got.Raw)
		}
		if got := limited.delete(t, "/api/v1/spaces/FAR"); got.Status < 400 {
			t.Errorf("the token deleted another space: %d %s", got.Status, got.Raw)
		}
		want(t, owner.get(t, "/api/v1/spaces/FAR"), http.StatusOK, "the owner reads FAR")
		want(t, whole.get(t, pagePath(farPage)), http.StatusOK, "a token without spaces reads FAR")
	})

	t.Run("the organization as a whole is refused", func(t *testing.T) {
		for what, got := range map[string]response{
			"make a space":       limited.post(t, "/api/v1/spaces", map[string]any{"key": "MORE", "name": "More"}),
			"read the audit log": limited.get(t, "/api/v1/audit"),
			"list members":       limited.get(t, "/api/v1/users"),
			"list tokens":        limited.get(t, "/api/v1/tokens"),
			"revoke a token":     limited.delete(t, "/api/v1/tokens/"+tokenID),
			"list webhooks":      limited.get(t, "/api/v1/webhooks"),
		} {
			if got.Status != http.StatusForbidden || errorCode(t, got) != "spaces_token" {
				t.Errorf("%s = %d %s, want 403 spaces_token", what, got.Status, got.Raw)
			}
		}
		can := obj(t, want(t, limited.get(t, "/api/v1/access/me"), http.StatusOK, "what the token may do"), "can")
		if can["use"] != true || can["createSpace"] != false || can["administer"] != false {
			t.Errorf("the token may %v, want use and nothing more", can)
		}
		want(t, whole.get(t, "/api/v1/audit"), http.StatusOK, "a token without spaces reads the audit log")
	})

	t.Run("search, home and the stale report keep to its space", func(t *testing.T) {
		q := url.Values{"q": {"zebra"}}
		sameList(t, "the token's search", hitTitles(t, searchFor(t, limited, q)), "Near zebra")
		if got := hitTitles(t, searchFor(t, owner, q)); len(got) != 2 {
			t.Errorf("the owner finds %v, want both pages", got)
		}
		for _, title := range titlesOf(t, want(t, limited.get(t, "/api/v1/home/edited"), http.StatusOK, "the token's edits"), "pages") {
			if title == "Far zebra" || title == "Far" {
				t.Errorf("the token's edits name %s", title)
			}
		}
		if !has(titlesOf(t, want(t, owner.get(t, "/api/v1/home/edited"), http.StatusOK, "the owner's edits"), "pages"), "Far zebra") {
			t.Error("the owner's edits leave Far zebra out")
		}
		stale := titlesOf(t, want(t, limited.get(t, "/api/v1/stale-pages"), http.StatusOK, "the token's stale report"), "pages")
		if has(stale, "Far zebra") || !has(stale, "Near zebra") {
			t.Errorf("the token's stale report = %v, want Near zebra alone", stale)
		}
		if got := limited.get(t, "/api/v1/stale-pages?space=FAR"); got.Status == http.StatusOK && has(titlesOf(t, got, "pages"), "Far zebra") {
			t.Error("the stale report named FAR to the token")
		}
	})

	// After the stale report, which a visit would take Far zebra out of.
	bobID := h.addPerson(t, home.org, "member")
	t.Run("page views and shares keep to its space", func(t *testing.T) {
		want(t, owner.post(t, pagePath(farPage, "/visit"), nil), http.StatusNoContent, "the owner opens Far zebra")
		want(t, owner.post(t, pagePath(farPage, "/share"), map[string]any{"recipients": []any{user(bobID)}, "message": "Look."}), http.StatusCreated, "the owner shares Far zebra")
		h.settle(t)
		want(t, limited.get(t, pagePath(nearPage, "/views")), http.StatusOK, "the counts of a page in its space")
		want(t, limited.get(t, pagePath(nearPage, "/readers")), http.StatusOK, "the readers of a page in its space")
		for what, got := range map[string]response{
			"the counts of Far zebra":  limited.get(t, pagePath(farPage, "/views")),
			"the readers of Far zebra": limited.get(t, pagePath(farPage, "/readers")),
		} {
			if got.Status == http.StatusOK {
				t.Errorf("the token reads %s: %s", what, got.Raw)
			}
		}
		want(t, owner.get(t, pagePath(farPage, "/views")), http.StatusOK, "the owner reads Far zebra's counts")
	})

	t.Run("an assistant holding it keeps to its space", func(t *testing.T) {
		m := &mcpSession{c: limited}
		tools := m.tools(t)
		if tools["list_audit_log"] || !tools["search"] || !tools["create_page"] {
			t.Errorf("the token is offered %v", tools)
		}
		structured(t, m.call(t, "get_page", map[string]any{"pageID": nearPage}), "page")
		if res := m.call(t, "get_page", map[string]any{"pageID": farPage}); res["isError"] != true {
			t.Errorf("an assistant read another space: %v", res)
		}
		if res := m.call(t, "create_page", map[string]any{"parentId": farPage, "title": "Should not land", "publish": true}); res["isError"] != true {
			t.Errorf("an assistant wrote into another space: %v", res)
		}
		if res := m.call(t, "list_audit_log", map[string]any{}); res["isError"] != true {
			t.Errorf("an assistant read the audit log: %v", res)
		}
	})

	t.Run("straight through SQL the database refuses it too", func(t *testing.T) {
		conn := appConn(t)
		ctx := context.Background()
		actAs(t, conn, home.org, home.user)
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		// The owner first, so every zero below is the token's doing.
		// Each count below is non-zero for the owner without the token.
		ownRows := map[string]string{
			"its own views of Far zebra":    `SELECT count(*) FROM page_view WHERE page_id = '` + farPage + `'`,
			"its own share of Far zebra":    `SELECT count(*) FROM page_share WHERE page_id = '` + farPage + `'`,
			"the share's recipients":        `SELECT count(*) FROM page_share_recipient r JOIN page_share s ON s.id = r.share_id WHERE s.page_id = '` + farPage + `'`,
			"Far zebra's counts":            `SELECT count(*) FROM page_view_stats('` + farPage + `', 30)`,
			"Far zebra's readers":           `SELECT count(*) FROM page_readers('` + farPage + `', NULL, NULL, 100)`,
			"Far zebra's unnamed readers":   `SELECT count(*) FROM page_readers_unnamed('` + farPage + `') AS n WHERE n IS NOT NULL`,
			"a page of FAR":                 `SELECT count(*) FROM page WHERE id = '` + farPage + `'`,
			"the audit log":                 `SELECT count(*) FROM audit_log`,
			"FAR in the home feed":          `SELECT count(*) FROM home_edited(NULL, NULL, 100) WHERE page_id = '` + farPage + `'`,
			"FAR's grants for the token":    `SELECT count(*) FROM space_grant g JOIN space s ON s.id = g.space_id WHERE s.key = 'FAR'`,
			"FAR itself, as the owner sees": `SELECT count(*) FROM space WHERE key = 'FAR'`,
		}
		for what, sql := range ownRows {
			if count(sql) == 0 {
				t.Fatalf("the owner reads no rows of %s, so the token's zero would prove nothing", what)
			}
		}
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.TokenSpacesVar, "{"+nearID+"}"); err != nil {
			t.Fatal(err)
		}
		for what, sql := range map[string]string{
			"a page of FAR":          `SELECT count(*) FROM page WHERE id = '` + farPage + `'`,
			"FAR itself":             `SELECT count(*) FROM space WHERE key = 'FAR'`,
			"FAR's grants":           `SELECT count(*) FROM space_grant g JOIN space s ON s.id = g.space_id WHERE s.key = 'FAR'`,
			"the audit log":          `SELECT count(*) FROM audit_log`,
			"the webhooks":           `SELECT count(*) FROM webhook_endpoint`,
			"the tokens":             `SELECT count(*) FROM api_token`,
			"FAR in the home feed":   `SELECT count(*) FROM home_edited(NULL, NULL, 100) WHERE page_id = '` + farPage + `'`,
			"FAR in the stale pages": `SELECT count(*) FROM stale_pages(NULL, NULL, false, NULL, false, 30, NULL, NULL, 100) WHERE page_id = '` + farPage + `'`,
		} {
			if n := count(sql); n != 0 {
				t.Errorf("the token reads %d rows of %s", n, what)
			}
		}
		for what, sql := range ownRows {
			if n := count(sql); n != 0 {
				t.Errorf("the token reads %d rows of %s", n, what)
			}
		}
		if count(`SELECT count(*) FROM page_view_stats($1, 30)`, nearPage) != 1 {
			t.Error("the token does not read the counts of a page in its space")
		}
		denied(t, conn, "a view of Far zebra", `INSERT INTO page_view (org_id, page_id, user_id, day) VALUES ($1, $2, $3, page_view_today())`, home.org, farPage, home.user)
		if count(`SELECT count(*) FROM page WHERE id = $1`, nearPage) != 1 {
			t.Error("the token does not read its own space")
		}
		untouched(t, conn, "retitling a page of FAR", `UPDATE page SET title = 'Taken' WHERE id = $1`, farPage)
		denied(t, conn, "a page in FAR", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by)
			VALUES ($1, $2, $3, 'W', 'Planted', $4)`, home.org, farID, far.homeID, home.user)
		denied(t, conn, "trashing a page of FAR through the database's own function", `SELECT page_trash($1)`, farPage)
		denied(t, conn, "a new space", `INSERT INTO space (org_id, key, name, created_by) VALUES ($1, 'SQL', 'Sql', $2)`, home.org, home.user)
		denied(t, conn, "widening its own token", `INSERT INTO api_token_space (org_id, token_id, space_id)
			SELECT $1, $2, id FROM space WHERE key = 'NEAR'`, home.org, tokenID)
		denied(t, conn, "an organization grant", `INSERT INTO global_grant (org_id, permission, subject_type) VALUES ($1, 'createSpace', 'everyone')`, home.org)
		if _, err := conn.Exec(ctx, `SELECT set_config($1, '', false)`, db.TokenSpacesVar); err != nil {
			t.Fatal(err)
		}
		if count(`SELECT count(*) FROM page WHERE id = $1`, farPage) != 1 {
			t.Error("clearing the setting did not give the owner FAR back")
		}
	})

	t.Run("a token whose spaces are all gone reaches nothing", func(t *testing.T) {
		newTree(t, owner, "TEMP", "Temporary")
		_, tempSecret := makeToken(t, owner, map[string]any{"name": "temporary", "spaces": []string{"TEMP"}})
		temp := api.withToken(tempSecret)
		want(t, temp.get(t, "/api/v1/spaces/TEMP"), http.StatusOK, "read TEMP")
		want(t, owner.delete(t, "/api/v1/spaces/TEMP"), http.StatusNoContent, "delete TEMP")
		h.settle(t)
		if got := list(t, want(t, temp.get(t, "/api/v1/spaces"), http.StatusOK, "list spaces"), "spaces"); len(got) != 0 {
			t.Errorf("a token without spaces left lists %v", got)
		}
		want(t, temp.get(t, "/api/v1/spaces/NEAR"), http.StatusNotFound, "read NEAR")
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/tokens"), http.StatusOK, "list tokens"), "tokens") {
			if tok := each.(map[string]any); tok["name"] == "temporary" && tok["allSpaces"] != false {
				t.Errorf("the emptied token reads as reaching every space: %v", tok)
			}
		}
	})
}
