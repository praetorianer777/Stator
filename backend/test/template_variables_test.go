//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
)

func blankNode(name string, marks ...string) map[string]any {
	n := map[string]any{"type": "templateVariable", "attrs": map[string]any{"name": name}}
	if len(marks) > 0 {
		var ms []any
		for _, m := range marks {
			ms = append(ms, map[string]any{"type": m})
		}
		n["marks"] = ms
	}
	return n
}

func textNode(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func paraNode(inline ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": inline}
}

// kickoff is a template with a blank of every kind.
func kickoff(spaceKey, name string) map[string]any {
	in := map[string]any{
		"name":        name,
		"description": "Who we start with, and when.",
		"title":       "Kickoff with {customer}",
		"body": map[string]any{"type": "doc", "content": []any{
			paraNode(textNode("Customer: "), blankNode("customer", "bold")),
			paraNode(textNode("Day: "), blankNode("day"), textNode(" Stage: "), blankNode("stage")),
			paraNode(textNode("Owner: "), blankNode("owner")),
			paraNode(textNode("Notes: "), blankNode("notes")),
		}},
		"variables": []any{
			map[string]any{"name": "customer", "label": "Customer", "kind": "text", "required": true},
			map[string]any{"name": "day", "label": "Kickoff day", "kind": "date", "default": "today"},
			map[string]any{"name": "stage", "label": "Stage", "kind": "select", "options": []any{"Lead", "Won"}, "default": "Lead"},
			map[string]any{"name": "owner", "label": "Owner", "kind": "person"},
			map[string]any{"name": "notes", "label": "Anything else", "kind": "text"},
		},
	}
	if spaceKey != "" {
		in["spaceKey"] = spaceKey
	}
	return in
}

// Templates are kept by the administrators of their scope, and the server fills
// a page's values in, refusing what does not fit and who may not view the space.
func TestTemplatesWithVariablesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "template-vars")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, bobID, doraID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, bob, dora := api.as(t, annID, home.org, slug), api.as(t, bobID, home.org, slug), api.as(t, doraID, home.org, slug)

	kick := newTree(t, owner, "KICK", "Kickoffs")
	hide := newTree(t, owner, "HIDE", "Hidden")
	want(t, owner.put(t, "/api/v1/spaces/KICK/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
		map[string]any{"subject": user(bobID), "permissions": []any{"view", "addPages"}},
	}}), http.StatusOK, "KICK is ann's and bob's")
	want(t, owner.put(t, "/api/v1/spaces/HIDE/permissions", map[string]any{"grants": []any{map[string]any{"subject": user(home.user), "permissions": []any{"administer"}}}}), http.StatusOK, "HIDE is the owner's")
	h.settle(t)

	var tplID string
	t.Run("a space administrator defines a template with variables", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, "/api/v1/templates", kickoff("KICK", "Kickoff")), http.StatusCreated, "ann makes a template"), "template")
		tplID, _ = made["key"].(string)
		if _, err := uuid.Parse(tplID); err != nil || made["scope"] != "space" || made["spaceKey"] != "KICK" || made["canEdit"] != true || made["builtIn"] != false {
			t.Fatalf("the template is %v", made)
		}
		vars, _ := made["variables"].([]any)
		if len(vars) != 5 || vars[1].(map[string]any)["default"] != "today" {
			t.Errorf("the variables are %v", vars)
		}
		h.settle(t)
		all := list(t, want(t, bob.get(t, "/api/v1/templates?space=KICK"), http.StatusOK, "bob lists KICK's"), "templates")
		first := all[0].(map[string]any)
		if len(all) != 8 || first["key"] != tplID || first["canEdit"] != false || all[7].(map[string]any)["builtIn"] != true {
			t.Errorf("bob's list is %v", all)
		}
		if n := len(list(t, want(t, bob.get(t, "/api/v1/templates"), http.StatusOK, "bob lists without a space"), "templates")); n != 7 {
			t.Errorf("without a space bob lists %d templates", n)
		}
		want(t, bob.get(t, "/api/v1/templates/"+tplID), http.StatusOK, "bob reads it")
		want(t, dora.get(t, "/api/v1/templates/"+tplID), http.StatusNotFound, "dora may not view KICK")
		want(t, dora.get(t, "/api/v1/templates?space=KICK"), http.StatusNotFound, "dora lists KICK's")
	})

	t.Run("only the administrators of its scope keep a template", func(t *testing.T) {
		for what, got := range map[string]response{
			"bob makes one for KICK":        bob.post(t, "/api/v1/templates", kickoff("KICK", "Mine")),
			"ann makes one for every space": ann.post(t, "/api/v1/templates", kickoff("", "Everywhere")),
			"bob changes ann's":             bob.put(t, "/api/v1/templates/"+tplID, kickoff("", "Changed")),
			"bob deletes ann's":             bob.delete(t, "/api/v1/templates/"+tplID),
			"anybody changes a built-in":    owner.put(t, "/api/v1/templates/meeting-notes", kickoff("", "Mine")),
		} {
			if got.Status != http.StatusForbidden || errorCode(t, got) != "forbidden" {
				t.Errorf("%s = %d %s", what, got.Status, got.Raw)
			}
		}
		want(t, dora.put(t, "/api/v1/templates/"+tplID, kickoff("", "Changed")), http.StatusNotFound, "dora changes one she cannot see")
		want(t, owner.delete(t, "/api/v1/templates/"+uuid.NewString()), http.StatusNotFound, "delete an unknown one")
		want(t, owner.post(t, "/api/v1/templates", kickoff("NOPE", "Lost")), http.StatusNotFound, "make one for an unknown space")
	})

	t.Run("a template that no form could fill is refused", func(t *testing.T) {
		undefined := kickoff("KICK", "Undefined")
		undefined["variables"] = []any{}
		taken := kickoff("KICK", "kickoff")
		badKind := kickoff("KICK", "Bad kind")
		badKind["variables"].([]any)[0].(map[string]any)["kind"] = "number"
		inCode := kickoff("KICK", "In code")
		inCode["body"] = map[string]any{"type": "doc", "content": []any{map[string]any{"type": "codeBlock", "content": []any{blankNode("customer")}}}}
		for field, in := range map[string]map[string]any{"body": undefined, "name": taken, "variables": badKind} {
			got := want(t, ann.post(t, "/api/v1/templates", in), http.StatusUnprocessableEntity, "refused on "+field)
			if msg, _ := fieldsOf(t, got)[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("refused on %s with %s", field, got.Raw)
			}
		}
		want(t, ann.post(t, "/api/v1/templates", inCode), http.StatusUnprocessableEntity, "a blank inside code")
	})

	var orgTplID string
	t.Run("an organization administrator makes one for every space", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("", "Company kickoff")), http.StatusCreated, "the owner makes one"), "template")
		orgTplID = made["key"].(string)
		if made["scope"] != "organization" || made["spaceKey"] != "" {
			t.Errorf("the template is %v", made)
		}
		h.settle(t)
		all := list(t, want(t, dora.get(t, "/api/v1/templates"), http.StatusOK, "dora lists"), "templates")
		if len(all) != 8 || all[0].(map[string]any)["key"] != orgTplID {
			t.Errorf("dora lists %v", all)
		}
	})

	today := time.Now().UTC().Format(time.DateOnly)
	t.Run("a member makes a page and the server fills it in", func(t *testing.T) {
		made := obj(t, want(t, bob.post(t, "/api/v1/pages", map[string]any{
			"parentId": kick.homeID, "template": tplID,
			"values": map[string]any{"customer": " Acme  Corp ", "owner": bobID.String(), "stage": "Won"},
		}), http.StatusCreated, "bob makes a page from it"), "page")
		body := mustJSON(t, made["body"])
		if made["title"] != "Kickoff with Acme Corp" || made["unpublished"] != true {
			t.Errorf("the page is called %q, unpublished %v", made["title"], made["unpublished"])
		}
		for _, needle := range []string{
			`{"marks":[{"type":"bold"}],"text":"Acme Corp","type":"text"}`,
			`{"attrs":{"date":"` + today + `"},"type":"date"}`,
			`"text":" Stage: Won"`,
			`"id":"` + bobID.String() + `","label":"A member"`,
			`{"marks":[{"type":"hint"}],"text":"Anything else","type":"text"}`,
		} {
			if !strings.Contains(body, needle) {
				t.Errorf("the body lacks %s: %s", needle, body)
			}
		}
		if strings.Contains(body, "templateVariable") {
			t.Errorf("a blank is left: %s", body)
		}
		published := mustJSON(t, obj(t, want(t, bob.post(t, pagePath(made["id"].(string), "/publish"), map[string]any{}), http.StatusOK, "publish it"), "page")["body"])
		if strings.Contains(published, "Anything else") || strings.Contains(published, "hint") || !strings.Contains(published, "Notes: ") {
			t.Errorf("the published body is %s", published)
		}
	})

	t.Run("defaults fill what is left, and the title may be the author's", func(t *testing.T) {
		made := obj(t, want(t, bob.post(t, "/api/v1/pages", map[string]any{
			"parentId": kick.homeID, "template": orgTplID, "title": "Beta, {customer} at {stage}", "publish": true,
			"values": map[string]any{"customer": "Beta"},
		}), http.StatusCreated, "bob uses the organization's"), "page")
		body := mustJSON(t, made["body"])
		if made["title"] != "Beta, Beta at Lead" || !strings.Contains(body, `"text":" Stage: Lead"`) || !strings.Contains(body, today) {
			t.Errorf("the page is %q: %s", made["title"], body)
		}
		if strings.Contains(body, "Anything else") || strings.Contains(body, "mention") {
			t.Errorf("a published page kept an empty blank: %s", body)
		}
	})

	t.Run("a template button offers it and makes a page with its values", func(t *testing.T) {
		button := templateButtonOf(t, bob, url.Values{"template": {tplID}, "spaceKey": {"KICK"}}, http.StatusOK)
		tpl := button["template"].(map[string]any)
		if vars, _ := tpl["variables"].([]any); tpl["name"] != "Kickoff" || len(vars) != 5 || button["canCreate"] != true {
			t.Errorf("the button shows %v", button)
		}
		made := obj(t, want(t, bob.post(t, "/api/v1/templates/"+tplID+"/pages", map[string]any{
			"spaceKey": "KICK", "values": map[string]any{"customer": "Delta"},
		}), http.StatusCreated, "bob clicks it"), "page")
		if made["title"] != "Kickoff with Delta" || made["parentId"] != kick.homeID || !strings.Contains(mustJSON(t, made["body"]), `"text":"Delta"`) {
			t.Errorf("the button made %v", made)
		}
		r := want(t, bob.post(t, "/api/v1/templates/"+tplID+"/pages", map[string]any{"spaceKey": "KICK"}), http.StatusUnprocessableEntity, "without the customer")
		if _, ok := fieldsOf(t, r)["values.customer"]; !ok {
			t.Errorf("a click without the customer is refused with %s", r.Raw)
		}
		elsewhere := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("HIDE", "Elsewhere")), http.StatusCreated, "a template of HIDE"), "template")["key"].(string)
		r = want(t, owner.get(t, "/api/v1/template-button?"+url.Values{"template": {elsewhere}, "spaceKey": {"KICK"}}.Encode()), http.StatusUnprocessableEntity, "HIDE's template in KICK")
		if msg, _ := fieldsOf(t, r)["template"].(string); !strings.HasSuffix(msg, ".") {
			t.Errorf("HIDE's template in KICK is refused with %s", r.Raw)
		}
		want(t, owner.post(t, "/api/v1/templates/"+elsewhere+"/pages", map[string]any{"spaceKey": "KICK", "values": map[string]any{"customer": "x"}}),
			http.StatusUnprocessableEntity, "a page in KICK from HIDE's template")
		templateButtonOf(t, dora, url.Values{"template": {tplID}, "spaceKey": {"KICK"}}, http.StatusNotFound)
	})

	t.Run("a page in a folder starts from a template, and a folder from none", func(t *testing.T) {
		folder := obj(t, want(t, bob.post(t, "/api/v1/pages", map[string]any{"parentId": kick.homeID, "title": "Customers", "kind": "folder"}),
			http.StatusCreated, "bob makes a folder"), "page")["id"].(string)
		made := obj(t, want(t, bob.post(t, "/api/v1/pages", map[string]any{
			"parentId": folder, "template": tplID, "values": map[string]any{"customer": "Epsilon"},
		}), http.StatusCreated, "a page in the folder"), "page")
		if made["parentId"] != folder || made["title"] != "Kickoff with Epsilon" {
			t.Errorf("the page in the folder is %v", made)
		}
		got := bob.post(t, "/api/v1/pages", map[string]any{"parentId": kick.homeID, "title": "Kickoffs", "kind": "folder", "template": tplID, "values": map[string]any{"customer": "x"}})
		if got.Status != http.StatusConflict || errorCode(t, got) != "folder" {
			t.Errorf("a folder from a template = %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a blog post starts from a template too", func(t *testing.T) {
		made := obj(t, want(t, bob.post(t, "/api/v1/spaces/KICK/posts", map[string]any{
			"template": tplID, "values": map[string]any{"customer": "Zeta"},
		}), http.StatusCreated, "bob writes a post from it"), "page")
		if made["kind"] != "post" || made["title"] != "Kickoff with Zeta" || made["parentId"] != nil || !strings.Contains(mustJSON(t, made["body"]), `"text":"Zeta"`) {
			t.Errorf("the post is %v", made)
		}
		r := want(t, bob.post(t, "/api/v1/spaces/KICK/posts", map[string]any{"template": tplID}), http.StatusUnprocessableEntity, "a post without the customer")
		if _, ok := fieldsOf(t, r)["values.customer"]; !ok {
			t.Errorf("a post without the customer is refused with %s", r.Raw)
		}
		r = want(t, bob.post(t, "/api/v1/spaces/KICK/posts", map[string]any{"title": "Plain", "values": map[string]any{"customer": "x"}}), http.StatusUnprocessableEntity, "values without a template")
		if _, ok := fieldsOf(t, r)["values"]; !ok {
			t.Errorf("values without a template are refused with %s", r.Raw)
		}
		want(t, dora.post(t, "/api/v1/spaces/KICK/posts", map[string]any{"template": tplID, "values": map[string]any{"customer": "x"}}), http.StatusNotFound, "dora writes in KICK")
	})

	t.Run("values that do not fit are refused on their field", func(t *testing.T) {
		otherSpace := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("HIDE", "Hidden kickoff")), http.StatusCreated, "a template of HIDE"), "template")["key"].(string)
		make := func(more map[string]any) response {
			body := map[string]any{"parentId": kick.homeID, "template": tplID}
			for k, v := range more {
				body[k] = v
			}
			return bob.post(t, "/api/v1/pages", body)
		}
		for field, got := range map[string]response{
			"values.customer": make(map[string]any{"values": map[string]any{}}),
			"values.stage":    make(map[string]any{"values": map[string]any{"customer": "x", "stage": "Lost"}}),
			"values.day":      make(map[string]any{"values": map[string]any{"customer": "x", "day": "2026-02-30"}}),
			"values.budget":   make(map[string]any{"values": map[string]any{"customer": "x", "budget": "1"}}),
			"values.owner":    make(map[string]any{"values": map[string]any{"customer": "x", "owner": doraID.String()}}),
			"body":            make(map[string]any{"values": map[string]any{"customer": "x"}, "body": map[string]any{"type": "doc", "content": []any{paraNode()}}}),
			"template":        make(map[string]any{"template": otherSpace, "values": map[string]any{"customer": "x"}}),
			"values":          bob.post(t, "/api/v1/pages", map[string]any{"parentId": kick.homeID, "title": "x", "values": map[string]any{"customer": "x"}}),
		} {
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", field, got.Status, got.Raw)
				continue
			}
			if msg, _ := fieldsOf(t, got)[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("refused on another field than %s: %s", field, got.Raw)
			}
		}
		strangerID := h.addPerson(t, home.org, "member")
		want(t, owner.delete(t, "/api/v1/users/"+strangerID.String()), http.StatusNoContent, "the stranger leaves")
		got := make(map[string]any{"values": map[string]any{"customer": "x", "owner": strangerID.String()}})
		if got.Status != http.StatusUnprocessableEntity || fieldsOf(t, got)["values.owner"] == nil {
			t.Errorf("somebody who left is named: %d %s", got.Status, got.Raw)
		}
		want(t, make(map[string]any{"template": "no-such-template"}), http.StatusUnprocessableEntity, "an unknown template")
	})

	t.Run("an administrator changes and deletes it, and pages keep what they were given", func(t *testing.T) {
		changed := kickoff("", "Kickoff, revised")
		saved := obj(t, want(t, ann.put(t, "/api/v1/templates/"+tplID, changed), http.StatusOK, "ann changes it"), "template")
		if saved["name"] != "Kickoff, revised" || saved["spaceKey"] != "KICK" {
			t.Errorf("the change is %v", saved)
		}
		want(t, ann.delete(t, "/api/v1/templates/"+tplID), http.StatusNoContent, "ann deletes it")
		want(t, ann.get(t, "/api/v1/templates/"+tplID), http.StatusNotFound, "it is gone")
		// The two published pages and the folder; the button's page is still bob's alone.
		if n := len(kick.titles(kick.homeID)); n != 3 {
			t.Errorf("KICK holds %d pages", n)
		}
		var audited int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE org_id = $1 AND target_id = $2`, home.org, tplID).Scan(&audited); err != nil || audited != 3 {
			t.Errorf("%d audit entries (%v), want made, changed and deleted", audited, err)
		}
	})

	t.Run("a token limited to a space keeps to its templates", func(t *testing.T) {
		_, secret := makeToken(t, owner, map[string]any{"name": "kick only", "spaces": []string{"KICK"}})
		limited := api.withToken(secret)
		h.settle(t)
		want(t, limited.get(t, "/api/v1/templates?space=KICK"), http.StatusOK, "list KICK's")
		want(t, limited.get(t, "/api/v1/templates?space=HIDE"), http.StatusNotFound, "list HIDE's")
		got := limited.post(t, "/api/v1/templates", kickoff("", "From a token"))
		if got.Status != http.StatusForbidden {
			t.Errorf("a limited token made a template for every space: %d %s", got.Status, got.Raw)
		}
		want(t, limited.post(t, "/api/v1/templates", kickoff("HIDE", "From a token")), http.StatusNotFound, "a limited token makes one for HIDE")
		mine := obj(t, want(t, limited.post(t, "/api/v1/templates", kickoff("KICK", "From a token")), http.StatusCreated, "a limited token makes one for KICK"), "template")["key"].(string)
		want(t, limited.post(t, "/api/v1/pages", map[string]any{"parentId": kick.homeID, "template": mine, "values": map[string]any{"customer": "Gamma", "owner": home.user.String()}}),
			http.StatusCreated, "a page from it")
		_ = hide
	})
}

// Straight through SQL as the app role, templates keep to their scope's
// administrators and readers, and no page, draft or version holds a blank.
func TestTemplateRowsAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "template-sql")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	docs := newTree(t, owner, "TSQL", "Templates in SQL")
	hide := newTree(t, owner, "TSQH", "Hidden")
	want(t, owner.put(t, "/api/v1/spaces/TSQH/permissions", map[string]any{"grants": []any{map[string]any{"subject": user(home.user), "permissions": []any{"administer"}}}}), http.StatusOK, "TSQH is the owner's")
	spaceID := obj(t, want(t, owner.get(t, "/api/v1/spaces/TSQL"), http.StatusOK, "TSQL"), "space")["id"].(string)
	hideID := obj(t, want(t, owner.get(t, "/api/v1/spaces/TSQH"), http.StatusOK, "TSQH"), "space")["id"].(string)
	spaceTpl := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("TSQL", "Space")), http.StatusCreated, "a space template"), "template")["key"].(string)
	hiddenTpl := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("TSQH", "Hidden")), http.StatusCreated, "a hidden template"), "template")["key"].(string)
	page := docs.add(docs.homeID, "Plain")
	h.settle(t)
	_ = hide

	body := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"templateVariable","attrs":{"name":"x"}}]}]}`
	conn := appConn(t)
	ctx := context.Background()

	actAs(t, conn, home.org, bobID)
	refused(t, conn, "a member makes a template for every space",
		`INSERT INTO page_template (org_id, name, body) VALUES ($1, 'Mine', $2)`, home.org, body)
	refused(t, conn, "a member makes a template for a space they do not administer",
		`INSERT INTO page_template (org_id, space_id, name, body) VALUES ($1, $2, 'Mine', $3)`, home.org, spaceID, body)
	untouched(t, conn, "a member changes a space's template", `UPDATE page_template SET name = 'Taken' WHERE id = $1`, spaceTpl)
	untouched(t, conn, "a member deletes a space's template", `DELETE FROM page_template WHERE id = $1`, spaceTpl)
	var seen int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_template WHERE id = ANY($1::uuid[])`, []string{spaceTpl, hiddenTpl}).Scan(&seen); err != nil || seen != 1 {
		t.Errorf("the member sees %d templates (%v), want TSQL's alone", seen, err)
	}

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "a page holds a blank", `UPDATE page SET body = $2 WHERE id = $1`, page, body)
	refused(t, conn, "a version holds a blank",
		`INSERT INTO page_version (org_id, page_id, number, title, body, created_by) VALUES ($1, $2, 2, 'Raw', $3, $4)`, home.org, page, body, home.user)
	refused(t, conn, "a draft holds a blank",
		`INSERT INTO page_draft (org_id, page_id, user_id, title, body, base_version) VALUES ($1, $2, $3, 'Raw', $4, 1)`, home.org, page, home.user, body)
	refused(t, conn, "a template moves to another space", `UPDATE page_template SET space_id = $2 WHERE id = $1`, spaceTpl, hideID)
	if _, err := conn.Exec(ctx, `UPDATE page_template SET name = 'Renamed', created_by = $2 WHERE id = $1`, spaceTpl, bobID); err != nil {
		t.Fatalf("the owner renames a template: %v", err)
	}
	var createdBy, updatedBy uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT created_by, updated_by FROM page_template WHERE id = $1`, spaceTpl).Scan(&createdBy, &updatedBy); err != nil ||
		createdBy != home.user || updatedBy != home.user {
		t.Errorf("the template says %s made it and %s changed it (%v)", createdBy, updatedBy, err)
	}

	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.TokenSpacesVar, "{"+spaceID+"}"); err != nil {
		t.Fatal(err)
	}
	refused(t, conn, "a token limited to a space makes a template for every space",
		`INSERT INTO page_template (org_id, name, body) VALUES ($1, 'Token', $2)`, home.org, body)
	refused(t, conn, "a token limited to a space makes a template for another",
		`INSERT INTO page_template (org_id, space_id, name, body) VALUES ($1, $2, 'Token', $3)`, home.org, hideID, body)
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_template WHERE id = $1`, hiddenTpl).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a token limited to TSQL sees TSQH's template (%d, %v)", seen, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO page_template (org_id, space_id, name, body) VALUES ($1, $2, 'Token', $3)`, home.org, spaceID, body); err != nil {
		t.Errorf("a token limited to TSQL makes a template for it: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config($1, '', false)`, db.TokenSpacesVar); err != nil {
		t.Fatal(err)
	}

	// A guest of TSQL reads its templates but none of the organization's, and
	// somebody reading TSQL without signing in reads and writes none.
	orgTpl := obj(t, want(t, owner.post(t, "/api/v1/templates", kickoff("", "Everywhere")), http.StatusCreated, "a template for every space"), "template")["key"].(string)
	gwenEmail := fmt.Sprintf("gwen-%s@example.test", uuid.NewString()[:8])
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, gwenEmail) })
	gwen := obj(t, want(t, owner.post(t, "/api/v1/spaces/TSQL/guests", map[string]any{"email": gwenEmail, "role": "editor"}), http.StatusCreated, "invite gwen"), "guest")
	gwenID := uuid.MustParse(gwen["userId"].(string))
	want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
	want(t, owner.put(t, "/api/v1/spaces/TSQL/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open TSQL")
	h.settle(t)
	gwenAPI := api.as(t, gwenID, home.org, slug)
	for _, each := range list(t, want(t, gwenAPI.get(t, "/api/v1/templates?space=TSQL"), http.StatusOK, "gwen lists TSQL's"), "templates") {
		if key := each.(map[string]any)["key"]; key == orgTpl {
			t.Error("gwen is offered the organization's template")
		}
	}
	actAs(t, conn, home.org, gwenID)
	count := func(who string, n int) {
		t.Helper()
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_template WHERE id = ANY($1::uuid[])`, []string{spaceTpl, orgTpl, hiddenTpl}).Scan(&seen); err != nil || seen != n {
			t.Errorf("%s sees %d templates (%v), want %d", who, seen, err, n)
		}
	}
	count("gwen", 1)
	refused(t, conn, "gwen makes a template for TSQL",
		`INSERT INTO page_template (org_id, space_id, name, body) VALUES ($1, $2, 'Guest', $3)`, home.org, spaceID, body)
	actAs(t, conn, home.org, home.user)
	count("the owner", 3)
	actAnonymously(t, conn, home.org)
	count("an anonymous reader of TSQL", 0)
	refused(t, conn, "an anonymous reader makes a template for TSQL",
		`INSERT INTO page_template (org_id, space_id, name, body) VALUES ($1, $2, 'Anybody', $3)`, home.org, spaceID, body)
}
