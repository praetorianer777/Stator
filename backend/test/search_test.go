//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// hitTitles reads the plain titles of a search's hits, and checks the total.
func hitTitles(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "hits") {
		out = append(out, joined(each.(map[string]any)["title"]))
	}
	if total := number(r.Body["total"]); total < len(out) {
		t.Fatalf("total %d is less than the %d hits", total, len(out))
	}
	return out
}

// joined puts segments back together, the way the client does before marking.
func joined(segments any) string {
	var b strings.Builder
	for _, s := range segments.([]any) {
		b.WriteString(s.(map[string]any)["text"].(string))
	}
	return b.String()
}

// matched lists the runs of segments the query matched.
func matched(segments any) []string {
	var out []string
	for _, s := range segments.([]any) {
		if seg := s.(map[string]any); seg["match"] == true {
			out = append(out, seg["text"].(string))
		}
	}
	return out
}

func searchFor(t *testing.T, c *client, params url.Values) response {
	t.Helper()
	return want(t, c.get(t, "/api/v1/search?"+params.Encode()), http.StatusOK, "search "+params.Encode())
}

func pageTitles(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "pages") {
		out = append(out, each.(map[string]any)["title"].(string))
	}
	return out
}

// Search finds published words, ranks titles above bodies, filters, marks
// matches without markup, and leaves out what the caller may not read.
func TestSearchOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "search")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)
	ben := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	ops := newTree(t, owner, "OPS", "Operations")
	devSpace := newTree(t, owner, "DEV", "Development")
	dev := &tree{t: t, c: ann, key: devSpace.key, homeID: devSpace.homeID}
	runbook := ops.add(ops.homeID, "Deploy runbook", map[string]any{"body": textDoc("How we ship the service to production.")})
	ops.add(runbook, "Rollback", map[string]any{"body": textDoc("If a deploy goes wrong, roll back first and ask questions later.")})
	dev.add(dev.homeID, "Café notes", map[string]any{"body": textDoc("Crème brûlée <b>recipe</b> & more, for the blue green deploy party.")})
	legacy := ops.add(ops.homeID, "Legacy servers", map[string]any{"body": textDoc("Old deploy scripts nobody runs.")})

	t.Run("a title match ranks above a body match", func(t *testing.T) {
		got := hitTitles(t, searchFor(t, ben, url.Values{"q": {"deploy"}}))
		if len(got) != 4 || got[0] != "Deploy runbook" {
			t.Fatalf("deploy finds %v", got)
		}
		r := searchFor(t, ben, url.Values{"q": {"deploy"}, "sort": {"updated"}})
		if got := hitTitles(t, r); got[0] != "Legacy servers" {
			t.Fatalf("by the latest change deploy finds %v", got)
		}
	})

	t.Run("hits carry marked plain text and where the page lives", func(t *testing.T) {
		r := searchFor(t, ben, url.Values{"q": {"cafe creme"}})
		hits := list(t, r, "hits")
		if len(hits) != 1 || number(r.Body["total"]) != 1 {
			t.Fatalf("accents are not ignored: %s", r.Raw)
		}
		hit := hits[0].(map[string]any)
		page := hit["page"].(map[string]any)
		if hit["type"] != "page" || page["spaceKey"] != "DEV" || page["spaceName"] != "Development" || page["title"] != "Café notes" {
			t.Fatalf("the hit is %v", hit)
		}
		if got := matched(hit["title"]); len(got) != 1 || got[0] != "Café" {
			t.Errorf("the title marks %v", got)
		}
		if got := matched(hit["snippet"]); len(got) != 1 || got[0] != "Crème" {
			t.Errorf("the snippet marks %v", got)
		}
		if !strings.Contains(joined(hit["snippet"]), "<b>recipe</b> & more") {
			t.Errorf("the snippet changed the text: %q", joined(hit["snippet"]))
		}
		if hit["updatedByName"] == "" || hit["updatedAt"] == "" || len(hit["labels"].([]any)) != 0 {
			t.Errorf("the hit is %v", hit)
		}
		title := searchFor(t, ben, url.Values{"q": {"runbook"}})
		if snippet := joined(list(t, title, "hits")[0].(map[string]any)["snippet"]); snippet != "How we ship the service to production." {
			t.Errorf("a title match shows the body's start %q", snippet)
		}
	})

	t.Run("the query syntax is the web's", func(t *testing.T) {
		for q, wantTitles := range map[string][]string{
			`"roll back"`:        {"Rollback"},
			`"back roll"`:        nil,
			`rollback or legacy`: {"Rollback", "Legacy servers"},
			`deploy -production`: {"Café notes", "Rollback", "Legacy servers"},
			`DEPLOY runbook`:     {"Deploy runbook"},
		} {
			got := hitTitles(t, searchFor(t, ben, url.Values{"q": {q}}))
			if !sameSet(got, wantTitles) {
				t.Errorf("%s finds %v, want %v", q, got, wantTitles)
			}
		}
	})

	t.Run("every filter narrows", func(t *testing.T) {
		backdate(t, h, legacy)
		for what, c := range map[string]struct {
			params url.Values
			want   []string
		}{
			"a space":            {url.Values{"q": {"deploy"}, "space": {"dev"}}, []string{"Café notes"}},
			"two spaces":         {url.Values{"q": {"deploy"}, "space": {"DEV", "OPS"}}, []string{"Deploy runbook", "Rollback", "Café notes", "Legacy servers"}},
			"an unknown space":   {url.Values{"q": {"deploy"}, "space": {"NOPE"}}, nil},
			"an author":          {url.Values{"author": {annID.String()}}, []string{"Café notes"}},
			"pages":              {url.Values{"q": {"deploy"}, "type": {"page"}}, []string{"Deploy runbook", "Rollback", "Café notes", "Legacy servers"}},
			"comments, not yet":  {url.Values{"q": {"deploy"}, "type": {"comment"}}, nil},
			"a label, not yet":   {url.Values{"q": {"deploy"}, "label": {"runbook"}}, nil},
			"after tomorrow":     {url.Values{"q": {"deploy"}, "updatedAfter": {"2999-01-01"}}, nil},
			"before a past day":  {url.Values{"q": {"deploy"}, "updatedBefore": {"2025-07-01"}}, []string{"Legacy servers"}},
			"after a past day":   {url.Values{"q": {"deploy"}, "updatedAfter": {"2025-07-01"}}, []string{"Deploy runbook", "Rollback", "Café notes"}},
			"the filters alone":  {url.Values{"space": {"OPS"}}, []string{"Operations", "Deploy runbook", "Rollback", "Legacy servers"}},
			"a range of one day": {url.Values{"updatedAfter": {"2025-06-01"}, "updatedBefore": {"2025-06-02"}}, []string{"Legacy servers"}},
			"a range of no days": {url.Values{"updatedAfter": {"2025-06-02"}, "updatedBefore": {"2025-06-02"}}, nil},
		} {
			got := hitTitles(t, searchFor(t, ben, c.params))
			if !sameSet(got, c.want) {
				t.Errorf("%s: %v, want %v", what, got, c.want)
			}
		}
	})

	t.Run("total counts every hit, a page at a time", func(t *testing.T) {
		first := searchFor(t, ben, url.Values{"q": {"deploy"}, "limit": {"2"}})
		second := searchFor(t, ben, url.Values{"q": {"deploy"}, "limit": {"2"}, "offset": {"2"}})
		past := searchFor(t, ben, url.Values{"q": {"deploy"}, "offset": {"10"}})
		if number(first.Body["total"]) != 4 || number(past.Body["total"]) != 4 || len(list(t, past, "hits")) != 0 {
			t.Fatalf("totals %s %s", first.Raw, past.Raw)
		}
		if all := append(hitTitles(t, first), hitTitles(t, second)...); !sameSet(all, []string{"Deploy runbook", "Rollback", "Café notes", "Legacy servers"}) {
			t.Fatalf("two pages hold %v", all)
		}
	})

	t.Run("drafts, unpublished pages and the trash are never found", func(t *testing.T) {
		secret := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": dev.homeID, "title": "Zeppelin plan", "body": textDoc("zeppelin")}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
		below := dev.add(secret, "Zeppelin budget")
		want(t, owner.put(t, pagePath(runbook, "/draft"), map[string]any{"title": "Deploy runbook", "body": textDoc("zeppelin hangar"), "baseVersion": 1}), http.StatusOK, "a draft")
		for _, c := range []*client{ann, ben, owner} {
			r := searchFor(t, c, url.Values{"q": {"zeppelin"}})
			got := hitTitles(t, r)
			if c == ann {
				if !sameSet(got, []string{"Zeppelin budget"}) {
					t.Errorf("the creator finds %v", got)
				}
				continue
			}
			if len(got) != 0 || number(r.Body["total"]) != 0 {
				t.Errorf("somebody else finds %s", r.Raw)
			}
		}
		for _, q := range []string{"Zeppelin", "zep"} {
			if got := pageTitles(t, want(t, ben.get(t, "/api/v1/search/quick?q="+q), http.StatusOK, "quick")); len(got) != 0 {
				t.Errorf("quick search shows somebody else's unpublished pages: %v", got)
			}
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q=zep"), http.StatusOK, "quick")); !sameSet(got, []string{"Zeppelin budget"}) {
			t.Errorf("quick search shows the creator %v", got)
		}
		want(t, ann.post(t, pagePath(below, "/visit"), nil), http.StatusNoContent, "the creator visits")
		if got := ben.post(t, pagePath(below, "/visit"), nil); got.Status != http.StatusNotFound {
			t.Errorf("somebody else visits below an unpublished page: %d", got.Status)
		}

		want(t, owner.delete(t, pagePath(legacy)), http.StatusNoContent, "trash a page")
		if got := hitTitles(t, searchFor(t, ben, url.Values{"q": {"legacy"}})); len(got) != 0 {
			t.Errorf("the trash is found: %v", got)
		}
		if got := ben.post(t, pagePath(legacy, "/visit"), nil); got.Status != http.StatusNotFound {
			t.Errorf("a trashed page is visited: %d", got.Status)
		}
	})

	t.Run("another organization finds none of it", func(t *testing.T) {
		away := h.makeMember(t, "search-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		for _, q := range []string{"deploy", ""} {
			if r := searchFor(t, stranger, url.Values{"q": {q}}); number(r.Body["total"]) != 0 {
				t.Errorf("a stranger finds %s", r.Raw)
			}
		}
		if got := pageTitles(t, want(t, stranger.get(t, "/api/v1/search/quick?q=dep"), http.StatusOK, "quick")); len(got) != 0 {
			t.Errorf("a stranger's quick search shows %v", got)
		}
		if got := stranger.post(t, pagePath(runbook, "/visit"), nil); got.Status != http.StatusNotFound {
			t.Errorf("a stranger visits: %d", got.Status)
		}
	})

	t.Run("bad input is refused in a sentence", func(t *testing.T) {
		for what, path := range map[string]string{
			"a long query":    "/api/v1/search?q=" + strings.Repeat("x", 201),
			"a malformed day": "/api/v1/search?updatedAfter=2026-02-30",
			"an unknown type": "/api/v1/search?type=file",
			"an author":       "/api/v1/search?author=ann",
			"a limit":         "/api/v1/search?limit=101",
			"a quick limit":   "/api/v1/search/quick?q=x&limit=21",
			"a long typing":   "/api/v1/search/quick?q=" + strings.Repeat("x", 201),
			"a recent limit":  "/api/v1/recent-pages?limit=0",
		} {
			got := ben.get(t, path)
			if got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := ben.post(t, pagePath("0199a5e0-0000-7000-8000-000000000001", "/visit"), nil); got.Status != http.StatusNotFound {
			t.Errorf("a visit to no page: %d", got.Status)
		}
		if got := api.anonymous().get(t, "/api/v1/search?q=deploy"); got.Status != http.StatusUnauthorized {
			t.Errorf("nobody searches: %d", got.Status)
		}
	})
}

// backdate moves a page's latest version into the past, which no endpoint can.
func backdate(t *testing.T, h *harness, id string) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(),
		`UPDATE page_version SET created_at = '2025-06-01 12:00+00' WHERE page_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		if seen[w]--; seen[w] < 0 {
			return false
		}
	}
	return true
}

// Quick search matches title prefixes, best first, and recent pages keep the
// last visit of each page, the latest first, leaving out what went away.
func TestQuickSearchAndRecentPagesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "quick")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	ann := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "DOCS", "Docs")
	other := newTree(t, owner, "MISC", "Misc")
	guide := docs.add(docs.homeID, "Release guide", map[string]any{"body": textDoc("Nothing about releasing here.")})
	checklist := docs.add(guide, "Release checklist")
	docs.add(docs.homeID, "The old release process")
	elsewhere := other.add(other.homeID, "Releases elsewhere")
	docs.add(docs.homeID, "Guidelines", map[string]any{"body": textDoc("release")})

	t.Run("every word is a title prefix", func(t *testing.T) {
		r := want(t, ann.get(t, "/api/v1/search/quick?q=rel"), http.StatusOK, "quick rel")
		got := pageTitles(t, r)
		if !sameSet(got, []string{"Release guide", "Release checklist", "The old release process", "Releases elsewhere"}) {
			t.Fatalf("rel finds %v", got)
		}
		if strings.HasPrefix(got[3], "Release") {
			t.Errorf("a title that starts with the words is not first: %v", got)
		}
		for _, p := range list(t, r, "pages") {
			page := p.(map[string]any)
			if page["title"] == "Release checklist" {
				if path := page["path"].([]any); len(path) != 2 || path[0] != "Docs" || path[1] != "Release guide" || page["spaceKey"] != "DOCS" {
					t.Errorf("the checklist is at %v", page)
				}
			}
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q=GUI+rel"), http.StatusOK, "two words")); !sameSet(got, []string{"Release guide"}) {
			t.Errorf("two words find %v", got)
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q=rel&space=misc"), http.StatusOK, "in a space")); !sameSet(got, []string{"Releases elsewhere"}) {
			t.Errorf("one space finds %v", got)
		}
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q=rel&limit=2"), http.StatusOK, "two")); len(got) != 2 {
			t.Errorf("a limit of 2 finds %v", got)
		}
		for _, q := range []string{"", "  ", "':*"} {
			if got := pageTitles(t, want(t, ann.get(t, "/api/v1/search/quick?q="+url.QueryEscape(q)), http.StatusOK, "nothing typed")); len(got) != 0 {
				t.Errorf("%q finds %v", q, got)
			}
		}
	})

	t.Run("recent pages are the latest visits the caller may still see", func(t *testing.T) {
		if got := pageTitles(t, want(t, ann.get(t, "/api/v1/recent-pages"), http.StatusOK, "none yet")); len(got) != 0 {
			t.Fatalf("recent pages before any visit: %v", got)
		}
		for _, id := range []string{guide, checklist, elsewhere, guide} {
			want(t, ann.post(t, pagePath(id, "/visit"), nil), http.StatusNoContent, "visit")
		}
		r := want(t, ann.get(t, "/api/v1/recent-pages"), http.StatusOK, "recent")
		sameTitles(t, "recent pages", pageTitles(t, r), "Release guide", "Releases elsewhere", "Release checklist")
		first := list(t, r, "pages")[0].(map[string]any)
		if first["visitedAt"] == "" || len(first["path"].([]any)) != 1 {
			t.Errorf("a recent page is %v", first)
		}
		sameTitles(t, "two recent pages", pageTitles(t, want(t, ann.get(t, "/api/v1/recent-pages?limit=2"), http.StatusOK, "two")), "Release guide", "Releases elsewhere")
		if got := pageTitles(t, want(t, owner.get(t, "/api/v1/recent-pages"), http.StatusOK, "another person's")); len(got) != 0 {
			t.Errorf("somebody else's visits show: %v", got)
		}
		want(t, owner.delete(t, pagePath(guide)), http.StatusNoContent, "trash the guide")
		sameTitles(t, "after the trash", pageTitles(t, want(t, ann.get(t, "/api/v1/recent-pages"), http.StatusOK, "recent")), "Releases elsewhere")
	})
}

// The index is the database's: the words it holds are document.PlainText's,
// and straight through SQL visits and the index stay inside their organization.
func TestSearchIsGuardedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "search-wall-a")
	b := h.makeMember(t, "search-wall-b")
	ownerA := api.as(t, a.user, a.org, h.slugOf(t, a.org))
	docsA := newTree(t, ownerA, "WALL", "Walled")
	pageA := docsA.add(docsA.homeID, "Secret plan", map[string]any{"body": textDoc("hidden words")})
	want(t, ownerA.post(t, pagePath(pageA, "/visit"), nil), http.StatusNoContent, "a visit in A")

	conn := appConn(t)
	ctx := context.Background()

	t.Run("the database reads a document as document.PlainText does", func(t *testing.T) {
		raw := `{"type":"doc","content":[
			{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title "},{"type":"text","text":"bold","marks":[{"type":"bold"}]}]},
			{"type":"paragraph","content":[{"type":"text","text":"Hi "},{"type":"mention","attrs":{"id":"u1","label":"Ann"}},{"type":"hardBreak"},{"type":"text","text":"next"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"item"}]}]}]},
			{"type":"table","content":[{"type":"tableRow","content":[
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"paragraph","content":[{"type":"text","text":"b"}]}]},
				{"type":"tableCell","content":[{"type":"paragraph"}]},
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}]}]},
			{"type":"codeBlock","content":[{"type":"text","text":"go test"}]},
			{"type":"panel","attrs":{"kind":"info"},"content":[{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]}]},
			{"type":"horizontalRule"},{"type":"paragraph"}]}`
		root, err := document.Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := conn.QueryRow(ctx, `SELECT page_plain_text($1::jsonb)`, raw).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})

	t.Run("B reads none of A's visits or index", func(t *testing.T) {
		actAs(t, conn, b.org)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_visit`).Scan(&n); err != nil || n != 0 {
			t.Errorf("B reads %d visits (%v)", n, err)
		}
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page WHERE search_vector @@ websearch_to_tsquery('stator_search', 'hidden')`).Scan(&n); err != nil || n != 0 {
			t.Errorf("B finds %d of A's pages (%v)", n, err)
		}
		refused(t, conn, "a visit of A's page, as A", `INSERT INTO page_visit (org_id, user_id, page_id) VALUES ($1, $2, $3)`, a.org, a.user, pageA)
		refused(t, conn, "a visit of A's page, as B", `INSERT INTO page_visit (org_id, user_id, page_id) VALUES ($1, $2, $3)`, b.org, b.user, pageA)
		untouched(t, conn, "A's visits from B", `UPDATE page_visit SET visited_at = now() WHERE page_id = $1`, pageA)
		untouched(t, conn, "deleting A's visits from B", `DELETE FROM page_visit WHERE page_id = $1`, pageA)
	})

	t.Run("the index cannot be written, only derived", func(t *testing.T) {
		actAs(t, conn, a.org)
		refused(t, conn, "planting words", `UPDATE page SET search_vector = to_tsvector('planted') WHERE id = $1`, pageA)
		refused(t, conn, "a visit for somebody outside the organization", `INSERT INTO page_visit (org_id, user_id, page_id) VALUES ($1, $2, $3)`, a.org, b.user, pageA)
		if _, err := conn.Exec(ctx, `UPDATE page SET title = 'Renamed plan' WHERE id = $1`, pageA); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page WHERE id = $1 AND search_vector @@ to_tsquery('stator_search', 'renamed:A')`, pageA).Scan(&n); err != nil || n != 1 {
			t.Errorf("a title written straight through SQL is not indexed: %d (%v)", n, err)
		}
	})
}
