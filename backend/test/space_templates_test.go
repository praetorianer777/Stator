//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/template"
)

// spacePages is every page of a space, read past the policies in tree
// order, as its depth, title, version and labels.
func spacePages(t *testing.T, h *harness, space string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		WITH RECURSIVE tree AS (
			SELECT p.id, p.title, p.version, 0 AS depth, ARRAY[p.rank] AS path
			FROM page p JOIN space s ON s.home_page_id = p.id WHERE s.id = $1
			UNION ALL
			SELECT c.id, c.title, c.version, tree.depth + 1, tree.path || c.rank
			FROM page c JOIN tree ON c.parent_id = tree.id)
		SELECT depth, title, version,
		       COALESCE((SELECT string_agg(l.name, ',' ORDER BY l.name) FROM page_label l WHERE l.page_id = tree.id), '')
		FROM tree ORDER BY path`, space)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			depth, version int
			title, labels  string
		)
		if err := rows.Scan(&depth, &title, &version, &labels); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%d %s v%d [%s]", depth, title, version, labels))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// templatePages is what spacePages should read for a space made from tpl.
func templatePages(tpl template.SpaceTemplate, name string) []string {
	out := []string{fmt.Sprintf("0 %s v1 []", name)}
	var walk func(depth int, pages []template.SpacePage)
	walk = func(depth int, pages []template.SpacePage) {
		for _, p := range pages {
			labels := slices.Clone(p.Labels)
			slices.Sort(labels)
			out = append(out, fmt.Sprintf("%d %s v1 [%s]", depth, p.Title, strings.Join(labels, ",")))
			walk(depth+1, p.Children)
		}
	}
	walk(1, tpl.Pages)
	return out
}

// everyoneMay is what the space's permission table grants everyone.
func everyoneMay(t *testing.T, c *client, key string) []string {
	t.Helper()
	for _, g := range list(t, want(t, c.get(t, "/api/v1/spaces/"+key+"/permissions"), http.StatusOK, "read the permissions of "+key), "grants") {
		grant := g.(map[string]any)
		if grant["subject"].(map[string]any)["type"] == "everyone" {
			var out []string
			for _, p := range grant["permissions"].([]any) {
				out = append(out, p.(string))
			}
			return out
		}
	}
	return nil
}

// A space from a template has its pages, labels, permissions and an audit
// entry naming it; every member may do in it what the template grants.
func TestSpaceTemplatesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "space-templates")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	t.Run("members list the built-ins, and nobody else does", func(t *testing.T) {
		all := list(t, want(t, member.get(t, "/api/v1/space-templates"), http.StatusOK, "list space templates"), "templates")
		var keys []string
		for _, each := range all {
			tpl := each.(map[string]any)
			keys = append(keys, tpl["key"].(string))
			if tpl["builtIn"] != true || tpl["home"].(map[string]any)["type"] != "doc" || len(tpl["pages"].([]any)) == 0 {
				t.Errorf("space template %v", tpl)
			}
		}
		if strings.Join(keys, ",") != "knowledge-base,team,documentation" {
			t.Errorf("the space templates are %v", keys)
		}
		if got := api.anonymous().get(t, "/api/v1/space-templates"); got.Status != http.StatusUnauthorized {
			t.Errorf("without a session the list answers %d", got.Status)
		}
	})

	for _, c := range []struct {
		key, template string
		everyone      []string
		// What a member is offered in the new space.
		edit, comment bool
	}{
		{"KB", "knowledge-base", []string{"view", "addPages", "addComments"}, true, true},
		{"TEAM", "team", []string{"view", "addComments"}, false, true},
		{"DOCS", "documentation", []string{"view"}, false, false},
	} {
		t.Run(c.template, func(t *testing.T) {
			tpl, err := template.SpaceByKey(c.template)
			if err != nil {
				t.Fatal(err)
			}
			name := "From " + tpl.Name
			sp := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": c.key, "name": name, "template": c.template}), http.StatusCreated, "make the space"), "space")
			if can := sp["can"].(map[string]any); can["administer"] != true {
				t.Fatalf("its creator may not administer it: %v", sp)
			}
			id := sp["id"].(string)
			if got, want := spacePages(t, h, id), templatePages(tpl, name); !slices.Equal(got, want) {
				t.Errorf("the tree is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}

			homePage := obj(t, want(t, member.get(t, "/api/v1/pages/"+sp["homePageId"].(string)), http.StatusOK, "a member reads the home page"), "page")
			body := mustJSON(t, homePage["body"])
			if homePage["title"] != name || !strings.Contains(body, `"childPages"`) {
				t.Errorf("the home page is %v", homePage)
			}
			if strings.Contains(body, `"space":null`) || !strings.Contains(body, `"space":"`+c.key+`"`) {
				t.Errorf("the home page's lists do not read this space: %s", body)
			}

			if got := everyoneMay(t, owner, c.key); !slices.Equal(got, c.everyone) {
				t.Errorf("everyone may %v, want %v", got, c.everyone)
			}
			can := obj(t, want(t, member.get(t, "/api/v1/spaces/"+c.key), http.StatusOK, "a member reads the space"), "space", "can")
			if can["editPages"] != c.edit || can["addComments"] != c.comment || can["administer"] != false || can["deletePages"] != false {
				t.Errorf("a member is offered %v", can)
			}
			added := member.post(t, "/api/v1/pages", map[string]any{"parentId": sp["homePageId"], "title": "A member's page"})
			if (added.Status == http.StatusCreated) != c.edit {
				t.Errorf("a member adding a page answers %d: %s", added.Status, added.Raw)
			}

			var logged string
			if err := h.super.QueryRow(context.Background(), `SELECT data->>'template' FROM audit_log WHERE org_id = $1 AND action = $2 AND target_id = $3`,
				home.org, audit.ActionSpaceCreated, id).Scan(&logged); err != nil || logged != c.template {
				t.Errorf("the audit log names template %q (%v)", logged, err)
			}
		})
	}

	t.Run("a blank space is as it was, and a wrong template is refused by name", func(t *testing.T) {
		sp := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "BLANK", "name": "Blank"}), http.StatusCreated, "make a blank space"), "space")
		if got := spacePages(t, h, sp["id"].(string)); len(got) != 1 {
			t.Errorf("a blank space holds %v", got)
		}
		if got := everyoneMay(t, owner, "BLANK"); !slices.Equal(got, []string{"view", "addPages", "addComments", "delete"}) {
			t.Errorf("everyone may %v in a blank space", got)
		}
		for what, body := range map[string]map[string]any{
			"an unknown template":       {"key": "NOPE", "name": "Nope", "template": "no-such-template"},
			"a page template":           {"key": "NOPE", "name": "Nope", "template": "meeting-notes"},
			"a personal space from one": {"key": "MINE", "name": "Mine", "template": "team", "personal": true},
		} {
			if msg := fieldError(t, want(t, owner.post(t, "/api/v1/spaces", body), http.StatusUnprocessableEntity, what), "template"); !strings.HasSuffix(msg, ".") {
				t.Errorf("%s: %q", what, msg)
			}
		}
		if got := member.post(t, "/api/v1/spaces", map[string]any{"key": "MEMBER", "name": "Member's", "template": "team"}); got.Status != http.StatusForbidden {
			t.Errorf("a member who may not create spaces made one from a template: %d", got.Status)
		}
		if n := h.countRows(t, `SELECT count(*) FROM space WHERE org_id = $1 AND key IN ('NOPE', 'MINE', 'MEMBER')`, home.org); n != 0 {
			t.Errorf("%d refused spaces were made", n)
		}
	})
}

// A space from a template is made in one transaction: when its last row is
// refused, no space, page, label, grant or audit entry is left of it.
func TestASpaceFromATemplateIsMadeWholeOrNotAtAll(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "space-template-atomic")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	// The documentation template's last page carries release-notes; the
	// trigger refuses it in this organization only.
	ctx := context.Background()
	fn := "refuse_label_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := h.super.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %[1]s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.org_id = '%[2]s' AND NEW.name = 'release-notes' THEN
				RAISE EXCEPTION 'refused by the test';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER %[1]s BEFORE INSERT ON page_label FOR EACH ROW EXECUTE FUNCTION %[1]s();`, fn, home.org)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, fmt.Sprintf(`DROP TRIGGER IF EXISTS %[1]s ON page_label; DROP FUNCTION IF EXISTS %[1]s()`, fn))
	})

	got := owner.post(t, "/api/v1/spaces", map[string]any{"key": "HALF", "name": "Half made", "template": "documentation"})
	if got.Status < http.StatusInternalServerError {
		t.Fatalf("the refused label answered %d: %s", got.Status, got.Raw)
	}
	for what, sql := range map[string]string{
		"spaces":        `SELECT count(*) FROM space WHERE org_id = $1`,
		"pages":         `SELECT count(*) FROM page WHERE org_id = $1`,
		"versions":      `SELECT count(*) FROM page_version WHERE org_id = $1`,
		"labels":        `SELECT count(*) FROM page_label WHERE org_id = $1`,
		"grants":        `SELECT count(*) FROM space_grant WHERE org_id = $1`,
		"audit entries": `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'space.created'`,
	} {
		if n := h.countRows(t, sql, home.org); n != 0 {
			t.Errorf("%d %s were left of the space", n, what)
		}
	}
}

// Straight through SQL as stator_app, a member cannot widen what a template
// granted, nor add pages where it lets them only read.
func TestATemplatesPermissionsHoldInTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "space-template-sql")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	memberID := h.addPerson(t, home.org, "member")
	sp := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "DOCS", "name": "Docs", "template": "documentation"}), http.StatusCreated, "make the space"), "space")
	spaceID, homeID := sp["id"].(string), sp["homePageId"].(string)

	conn := appConn(t)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "granting everyone addPages", `INSERT INTO space_grant (org_id, space_id, permission, subject_type) VALUES ($1, $2, 'addPages', 'everyone')`, home.org, spaceID)
	refused(t, conn, "granting themselves administer", `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id) VALUES ($1, $2, 'administer', 'user', $3)`, home.org, spaceID, memberID)
	untouched(t, conn, "taking view from everyone", `DELETE FROM space_grant WHERE space_id = $1`, spaceID)
	refused(t, conn, "adding a page", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, updated_by) VALUES ($1, $2, $3, 'W', 'Planted', $4, $4)`, home.org, spaceID, homeID, memberID)
	refused(t, conn, "retitling a page", `UPDATE page SET title = 'Taken' WHERE space_id = $1`, spaceID)
	refused(t, conn, "labelling a page", `INSERT INTO page_label (org_id, page_id, name, created_by) VALUES ($1, $2, 'planted', $3)`, home.org, homeID, memberID)

	docs, err := template.SpaceByKey("documentation")
	if err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE space_id = $1`, spaceID).Scan(&visible); err != nil || visible != len(templatePages(docs, "Docs")) {
		t.Errorf("the member reads %d pages (%v), want every page of the template", visible, err)
	}
}
