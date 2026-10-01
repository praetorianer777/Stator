//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// The issue list block (#30): a query's rows, each reader getting the ones
// Armature shows them, through the stub and the per-person cache.

func searchPath(q string, limit, offset int) string {
	v := url.Values{"q": {q}}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	return "/api/v1/armature/search?" + v.Encode()
}

// searchKeys asks a query and answers the status, the keys of the rows and the whole answer.
func searchKeys(t *testing.T, c *client, q string, limit, offset int) (string, []string, response) {
	t.Helper()
	r := want(t, c.get(t, searchPath(q, limit, offset)), http.StatusOK, "search "+q)
	status, _ := r.Body["status"].(string)
	var keys []string
	for _, each := range list(t, r, "issues") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return status, keys, r
}

func cachedSearch(t *testing.T, h *harness, org, tokenID uuid.UUID, q string, limit, offset int) bool {
	t.Helper()
	n, err := h.valkey(t).HExists(context.Background(), armature.SearchKey(org), armature.SearchField(tokenID, q, limit, offset)).Result()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAnIssueListShowsEachReaderTheRowsArmatureShowsThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-search")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	base := armatureURL(t)

	if status, keys, _ := searchKeys(t, bob, "project = CP", 0, 0); status != "not_configured" || len(keys) != 0 {
		t.Errorf("before a connection the search is %s %v", status, keys)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "orgSlug": slug}), http.StatusOK, "connect Armature")
	// In Armature the owner is its admin, who sees SEC; bob is alice, who does not.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	const both = "project in (CP, SEC) ORDER BY key"

	t.Run("a page of rows, the total, and where the query opens in Armature", func(t *testing.T) {
		status, keys, r := searchKeys(t, bob, both, 2, 1)
		if status != "ok" || strings.Join(keys, " ") != "CP-2 CP-3" {
			t.Fatalf("bob's page is %s", r.Raw)
		}
		if r.Body["total"] != float64(5) || r.Body["limit"] != float64(2) || r.Body["offset"] != float64(1) {
			t.Errorf("total, limit and offset are %v %v %v", r.Body["total"], r.Body["limit"], r.Body["offset"])
		}
		if r.Body["url"] != armature.SearchURL(base, both) {
			t.Errorf("the url is %v", r.Body["url"])
		}
		first := list(t, r, "issues")[0].(map[string]any)
		if first["url"] != base+"/issues/CP-2" || first["summary"] != "Sign-in fails with an expired session" {
			t.Errorf("the first row is %v", first)
		}
		if _, _, all := searchKeys(t, bob, both, 0, 0); all.Body["limit"] != float64(armature.DefaultListLimit) {
			t.Errorf("without a limit the search takes %v", all.Body["limit"])
		}
	})

	t.Run("each reader gets the rows they may see", func(t *testing.T) {
		_, mine, raw := searchKeys(t, bob, both, 0, 0)
		_, theirs, _ := searchKeys(t, owner, both, 0, 0)
		if strings.Join(mine, " ") != "CP-1 CP-2 CP-3 CP-4 CP-5" || strings.Join(theirs, " ") != "CP-1 CP-2 CP-3 CP-4 CP-5 SEC-1" {
			t.Errorf("bob sees %v and the owner %v", mine, theirs)
		}
		if strings.Contains(string(raw.Raw), "vulnerability") {
			t.Error("a secret issue's summary reached bob")
		}
		_, bobs, _ := searchKeys(t, bob, "assignee = currentUser() ORDER BY key", 0, 0)
		_, owners, _ := searchKeys(t, owner, "assignee = currentUser() ORDER BY key", 0, 0)
		if strings.Join(bobs, " ") != "CP-1 CP-4" || strings.Join(owners, " ") != "SEC-1" {
			t.Errorf("currentUser() is not the reader: bob %v, the owner %v", bobs, owners)
		}
	})

	t.Run("the cache keeps each reader's answer until it is cleared", func(t *testing.T) {
		bobRow, ownerRow := tokenRowOf(t, h, home.org, bobID), tokenRowOf(t, h, home.org, home.user)
		const q = "project = CP ORDER BY key"
		searchKeys(t, bob, q, 1, 0)
		if !cachedSearch(t, h, home.org, bobRow, q, 1, 0) || cachedSearch(t, h, home.org, ownerRow, q, 1, 0) {
			t.Error("the answer is not cached for bob alone")
		}
		stubControl(t, http.MethodPatch, slug+"/issues/CP-1", map[string]any{"summary": "Renamed in Armature"})
		_, _, cached := searchKeys(t, bob, q, 1, 0)
		_, _, fresh := searchKeys(t, owner, q, 1, 0)
		if !strings.Contains(string(cached.Raw), "Set up the build pipeline") || !strings.Contains(string(fresh.Raw), "Renamed in Armature") {
			t.Errorf("bob's answer was not cached, or the owner's was shared: %s %s", cached.Raw, fresh.Raw)
		}
		armature.NewCache(h.valkey(t), discard()).ForgetSearches(context.Background(), home.org)
		if _, _, again := searchKeys(t, bob, q, 1, 0); !strings.Contains(string(again.Raw), "Renamed in Armature") {
			t.Errorf("after the cache cleared bob reads %s", again.Raw)
		}
	})

	t.Run("a query Armature cannot read is 422 bad_query with its sentence and position", func(t *testing.T) {
		r := want(t, bob.get(t, searchPath(`project = CP AND summary ~ "keys"`, 0, 0)), http.StatusUnprocessableEntity, "a bad query")
		if code := errorCode(t, r); code != "bad_query" {
			t.Errorf("the code is %s", code)
		}
		e := r.Body["error"].(map[string]any)
		if e["position"] != float64(26) || !strings.HasSuffix(e["message"].(string), ".") {
			t.Errorf("the refusal is %s", r.Raw)
		}
	})

	t.Run("what the search refuses before asking Armature", func(t *testing.T) {
		fieldError(t, want(t, bob.get(t, "/api/v1/armature/search"), http.StatusUnprocessableEntity, "no query"), "q")
		fieldError(t, want(t, bob.get(t, searchPath("   ", 0, 0)), http.StatusUnprocessableEntity, "a blank query"), "q")
		fieldError(t, want(t, bob.get(t, searchPath(strings.Repeat("a", armature.MaxQueryLength+1), 0, 0)), http.StatusUnprocessableEntity, "a long query"), "q")
		for name, path := range map[string]string{
			"limit zero":      "/api/v1/armature/search?q=project+%3D+CP&limit=0",
			"limit too high":  "/api/v1/armature/search?q=project+%3D+CP&limit=101",
			"negative offset": "/api/v1/armature/search?q=project+%3D+CP&offset=-1",
		} {
			field := "limit"
			if strings.Contains(name, "offset") {
				field = "offset"
			}
			fieldError(t, want(t, bob.get(t, path), http.StatusUnprocessableEntity, name), field)
		}
		want(t, api.anonymous().get(t, searchPath("project = CP", 0, 0)), http.StatusUnauthorized, "nobody searches")
		_, readOnly := makeToken(t, bob, map[string]any{"name": "lists", "scopes": []string{"read"}})
		if status, keys, _ := searchKeys(t, api.withToken(readOnly), "project = CP", 0, 0); status != "ok" || len(keys) != 5 {
			t.Errorf("a read-only Stator token searches %s %v", status, keys)
		}
	})

	t.Run("a reader without a token gets the status and no rows", func(t *testing.T) {
		status, keys, r := searchKeys(t, carol, "project = CP", 0, 0)
		if status != "not_connected" || len(keys) != 0 || r.Body["total"] != float64(0) {
			t.Errorf("carol's search is %s", r.Raw)
		}
	})
}

func TestAnIssueListStoresItsQueryAndNeverItsRows(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-list")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "LST", "Lists")
	listNode := func(attrs map[string]any) map[string]any {
		return map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Open work"}}},
			map[string]any{"type": "armatureIssueList", "attrs": attrs},
		}}
	}
	good := map[string]any{"query": "statusCategory != done", "columns": []string{"key", "summary", "due"}, "limit": 20}
	page := docs.add(docs.homeID, "Status", map[string]any{"body": listNode(good)})

	if hits := list(t, searchFor(t, owner, url.Values{"q": {"statusCategory"}}), "hits"); len(hits) != 0 {
		t.Errorf("a list's query is among the page's words: %v", hits)
	}
	for name, attrs := range map[string]map[string]any{
		"no columns":       {"query": "project = CP", "columns": []string{}, "limit": 20},
		"a column twice":   {"query": "project = CP", "columns": []string{"key", "key"}, "limit": 20},
		"an unknown one":   {"query": "project = CP", "columns": []string{"points"}, "limit": 20},
		"a blank query":    {"query": " ", "columns": []string{"key"}, "limit": 20},
		"too many rows":    {"query": "project = CP", "columns": []string{"key"}, "limit": 101},
		"rows in the page": {"query": "project = CP", "columns": []string{"key"}, "limit": 20, "issues": []string{"CP-1"}},
	} {
		errorCode(t, want(t, owner.patch(t, pagePath(page), map[string]any{"body": listNode(attrs)}), http.StatusUnprocessableEntity, name))
	}

	raw, _ := json.Marshal(listNode(good))
	root, err := document.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, string(raw)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := document.PlainText(root); got != want || want != "Open work" {
		t.Errorf("the database reads %q, document.PlainText %q", got, want)
	}
}
