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
)

// Template buttons and contributors (#62): a page made from a template in one
// click, for whoever may add pages where it goes, and the people who
// published a page or a tree, as each reader may see them.

func templateButtonOf(t *testing.T, c *client, query url.Values, status int) map[string]any {
	t.Helper()
	return want(t, c.get(t, "/api/v1/template-button?"+query.Encode()), status, "the template button "+query.Encode()).Body
}

func fieldsOf(t *testing.T, r response) map[string]any {
	t.Helper()
	fields, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any)
	return fields
}

func TestTemplateButtonsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "template-button")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, carlID := h.namedPerson(t, org.org, "Ann Button"), h.namedPerson(t, org.org, "Carl Button")
	ann, carl := api.as(t, annID, org.org, slug), api.as(t, carlID, org.org, slug)

	docs := newTree(t, owner, "TBA", "Button space")
	want(t, owner.put(t, "/api/v1/spaces/TBA/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everybody reads TBA, ann adds pages")
	plans := docs.add(docs.homeID, "Plans")
	secret := docs.add(docs.homeID, "Secret")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(annID)}, nil), http.StatusOK, "only ann sees the secret")
	h.settle(t)
	today := time.Now().UTC().Format(time.DateOnly)

	t.Run("a button says what it makes, where, and whether the reader may", func(t *testing.T) {
		top := templateButtonOf(t, ann, url.Values{"template": {"meeting-notes"}, "spaceKey": {"tba"}}, http.StatusOK)
		parent := top["parent"].(map[string]any)
		if top["canCreate"] != true || parent["id"] != docs.homeID || parent["home"] != true || top["spaceKey"] != "TBA" || top["spaceName"] != "Button space" {
			t.Errorf("the top of the space for ann is %v", top)
		}
		if tpl := top["template"].(map[string]any); tpl["key"] != "meeting-notes" || tpl["name"] != "Meeting notes" || tpl["title"] != "Meeting notes {date}" {
			t.Errorf("the template is %v", tpl)
		}
		under := templateButtonOf(t, carl, url.Values{"template": {"how-to"}, "spaceKey": {"NOPE"}, "parentId": {plans}}, http.StatusOK)
		if under["canCreate"] != false || under["parent"].(map[string]any)["title"] != "Plans" {
			t.Errorf("plans for carl, who only reads, is %v", under)
		}
		templateButtonOf(t, carl, url.Values{"template": {"how-to"}, "parentId": {secret}}, http.StatusNotFound)
		templateButtonOf(t, carl, url.Values{"template": {"how-to"}, "spaceKey": {"NOPE"}}, http.StatusNotFound)
		for field, query := range map[string]url.Values{
			"template": {"template": {"no-such-template"}, "spaceKey": {"TBA"}},
			"spaceKey": {"template": {"how-to"}},
			"parentId": {"template": {"how-to"}, "parentId": {"plans"}},
		} {
			r := want(t, carl.get(t, "/api/v1/template-button?"+query.Encode()), http.StatusUnprocessableEntity, query.Encode())
			if msg, _ := fieldsOf(t, r)[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("%s is refused with %v", query.Encode(), r.Body)
			}
		}
	})

	t.Run("a click makes an unpublished page of the reader's from the template", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, "/api/v1/templates/meeting-notes/pages", map[string]any{"parentId": plans, "title": "Weekly {date}"}),
			http.StatusCreated, "ann makes notes under plans"), "page")
		if made["title"] != "Weekly "+today || made["parentId"] != plans || made["unpublished"] != true || made["spaceKey"] != "TBA" {
			t.Errorf("the page made is %v", made)
		}
		if hintsIn(t, made["body"]) == 0 {
			t.Errorf("the page lost the template's hints: %v", made["body"])
		}
		top := obj(t, want(t, ann.post(t, "/api/v1/templates/meeting-notes/pages", map[string]any{"spaceKey": "TBA"}), http.StatusCreated, "ann makes notes at the top"), "page")
		if top["title"] != "Meeting notes "+today || top["parentId"] != docs.homeID {
			t.Errorf("the page at the top is %v", top)
		}
		named := obj(t, want(t, ann.post(t, "/api/v1/templates/how-to/pages", map[string]any{"spaceKey": "TBA", "title": "  "}), http.StatusCreated, "a template without a title"), "page")
		if named["title"] != "How-to guide" {
			t.Errorf("a page from a template without a title is called %v", named["title"])
		}
		// The new page is ann's alone until she publishes it.
		want(t, carl.get(t, pagePath(made["id"].(string))), http.StatusNotFound, "carl reads ann's new page")
	})

	t.Run("whoever may not add pages there, or see there, makes nothing", func(t *testing.T) {
		before := h.countRows(t, `SELECT count(*) FROM page WHERE org_id = $1`, org.org)
		want(t, carl.post(t, "/api/v1/templates/how-to/pages", map[string]any{"parentId": plans}), http.StatusForbidden, "carl, who only reads")
		want(t, carl.post(t, "/api/v1/templates/how-to/pages", map[string]any{"parentId": secret}), http.StatusNotFound, "carl under the secret")
		r := want(t, ann.post(t, "/api/v1/templates/no-such-template/pages", map[string]any{"spaceKey": "TBA"}), http.StatusUnprocessableEntity, "a template that is not there")
		if _, ok := fieldsOf(t, r)["template"]; !ok {
			t.Errorf("a template that is not there is refused with %v", r.Body)
		}
		r = want(t, ann.post(t, "/api/v1/templates/how-to/pages", map[string]any{}), http.StatusUnprocessableEntity, "nowhere")
		if _, ok := fieldsOf(t, r)["spaceKey"]; !ok {
			t.Errorf("nowhere is refused with %v", r.Body)
		}
		if after := h.countRows(t, `SELECT count(*) FROM page WHERE org_id = $1`, org.org); after != before {
			t.Errorf("refused clicks made %d pages", after-before)
		}
	})

	// The service refusing is not proof: as stator_app, a reader adds no page
	// where the button would put one, and an adder does.
	t.Run("the database holds the button to who may add pages", func(t *testing.T) {
		spaceID := docs.spaceID(t)
		conn := appConn(t)
		insert := `INSERT INTO page (id, org_id, space_id, parent_id, rank, title, created_by, updated_by) VALUES ($1, $2, $3, $4, 'zz', 'Forged', $5, $5)`
		actAs(t, conn, org.org, carlID)
		denied(t, conn, "a reader adds a page under plans", insert, uuid.Must(uuid.NewV7()), org.org, spaceID, plans, carlID)
		actAs(t, conn, org.org, annID)
		if _, err := conn.Exec(context.Background(), insert, uuid.Must(uuid.NewV7()), org.org, spaceID, plans, annID); err != nil {
			t.Errorf("ann cannot add a page under plans: %v", err)
		}
	})
}

// contributorsOf is the ids and counts of a contributors answer, in order.
func contributorsOf(t *testing.T, c *client, page string, query url.Values) ([]string, map[string]any) {
	t.Helper()
	r := want(t, c.get(t, pagePath(page, "/contributors?"+query.Encode())), http.StatusOK, "the contributors "+query.Encode())
	var out []string
	for _, each := range list(t, r, "contributors") {
		p := each.(map[string]any)
		if p["name"] == "" || p["lastEditedAt"] == "" {
			t.Errorf("a contributor without a name or a day: %v", p)
		}
		edits, _ := p["edits"].(float64)
		out = append(out, fmt.Sprintf("%s=%d", p["id"], int(edits)))
	}
	return out, r.Body
}

func TestContributorsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "contributors")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Writer"), h.namedPerson(t, org.org, "Ben Writer"), h.namedPerson(t, org.org, "Carl Reader")
	ann, ben, carl := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug), api.as(t, carlID, org.org, slug)

	docs := newTree(t, owner, "CTB", "Contributors")
	want(t, owner.put(t, "/api/v1/spaces/CTB/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
		map[string]any{"subject": user(benID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everybody reads CTB, ann and ben add pages")
	guide := docs.add(docs.homeID, "Guide")
	setup := docs.add(guide, "Setup")
	secret := docs.add(setup, "Secret")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(benID)}, nil), http.StatusOK, "only ben sees the secret")
	h.settle(t)
	para := func(text string) map[string]any {
		return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}}
	}
	for i := range 4 {
		publishBody(t, ann, guide, para("Ann's change "+string(rune('a'+i))))
	}
	publishBody(t, ben, setup, para("Ben's setup"))
	publishBody(t, ben, secret, para("Ben's secret"))
	h.settle(t)
	o, a, b := org.user.String(), annID.String(), benID.String()

	for _, c := range []struct {
		what   string
		reader *client
		page   string
		query  url.Values
		want   []string
	}{
		{"the page alone, the most versions first", carl, guide, url.Values{}, []string{a + "=4", o + "=1"}},
		{"the page and the pages below it", carl, guide, url.Values{"scope": {"tree"}}, []string{a + "=4", o + "=2", b + "=1"}},
		{"a page the reader may view counts for them", ben, guide, url.Values{"scope": {"tree"}}, []string{a + "=4", o + "=3", b + "=2"}},
		// One version each of the setup: the later leads.
		{"a tie goes to the latest", carl, setup, url.Values{}, []string{b + "=1", o + "=1"}},
	} {
		got, _ := contributorsOf(t, c.reader, c.page, c.query)
		sameList(t, c.what, got, c.want...)
	}

	t.Run("a block says when it names fewer than published", func(t *testing.T) {
		got, body := contributorsOf(t, carl, guide, url.Values{"scope": {"tree"}, "limit": {"2"}})
		sameList(t, "the first two", got, a+"=4", o+"=2")
		if body["truncated"] != true {
			t.Errorf("two of three are not truncated: %v", body)
		}
		if _, body := contributorsOf(t, carl, guide, url.Values{"scope": {"tree"}, "limit": {"3"}}); body["truncated"] != false {
			t.Errorf("three of three are truncated: %v", body)
		}
	})

	t.Run("what a block cannot count is refused in words", func(t *testing.T) {
		for field, query := range map[string]string{"scope": "scope=space", "limit": "limit=51"} {
			r := want(t, carl.get(t, pagePath(guide, "/contributors?"+query)), http.StatusUnprocessableEntity, query)
			if msg, _ := fieldsOf(t, r)[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("%s is refused with %v", query, r.Body)
			}
		}
		want(t, carl.get(t, pagePath(guide, "/contributors?limit=none")), http.StatusUnprocessableEntity, "a limit that is no number")
		want(t, carl.get(t, pagePath(secret, "/contributors")), http.StatusNotFound, "a page carl may not view")
	})

	// The answer is no wider than the history: as stator_app, a reader reads
	// no version of a page closed to them, whatever the query.
	t.Run("the database keeps a closed page's versions from a reader", func(t *testing.T) {
		conn := appConn(t)
		count := func(sql string, args ...any) int {
			t.Helper()
			var n int
			if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		versions := `SELECT count(*) FROM page_version WHERE page_id = $1`
		actAs(t, conn, org.org, carlID)
		if n := count(versions, secret); n != 0 {
			t.Errorf("carl reads %d versions of the secret", n)
		}
		if n := count(versions, guide); n != 5 {
			t.Errorf("carl reads %d versions of the guide", n)
		}
		actAs(t, conn, org.org, benID)
		if n := count(versions, secret); n != 2 {
			t.Errorf("ben reads %d versions of the secret", n)
		}
	})
}
