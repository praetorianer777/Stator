//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Creating issues from a selection (#31): each item filed in Armature as the
// caller, in order, stopping at the first refusal; the stub's own controls
// show what reached Armature.

// stubIssue reads an issue as the stub holds it, its description included.
func stubIssue(t *testing.T, tenant, key string) map[string]any {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_stub/" + tenant + "/issues/" + key)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the stub has no %s: %d %s", key, resp.StatusCode, raw)
	}
	var out struct {
		Issue map[string]any `json:"issue"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Issue
}

func createBody(page, project string, summaries ...string) map[string]any {
	items := make([]any, len(summaries))
	for i, s := range summaries {
		items[i] = map[string]any{"summary": s}
	}
	return map[string]any{"pageId": page, "projectKey": project, "items": items}
}

func keysOfIssues(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, "issues") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return keys
}

func TestIssuesAreFiledFromASelectionAsTheCaller(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-create")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "CRT", "Reviews")
	page := docs.add(docs.homeID, "Review notes")
	const items = "/api/v1/armature/issues"

	errorCodeIs(t, want(t, bob.post(t, items, createBody(page, "CP", "Before a connection")), http.StatusConflict, "no connection"), "armature_not_configured")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	// In Armature the owner is its admin and bob is alice.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("the issue types, sub-tasks left out", func(t *testing.T) {
		r := want(t, bob.get(t, "/api/v1/armature/issue-types"), http.StatusOK, "bob's issue types")
		var names []string
		for _, each := range list(t, r, "issueTypes") {
			names = append(names, each.(map[string]any)["name"].(string))
		}
		if r.Body["status"] != "ok" || strings.Join(names, " ") != "Task Bug Story" {
			t.Errorf("the issue types are %s", r.Raw)
		}
		none := want(t, carol.get(t, "/api/v1/armature/issue-types"), http.StatusOK, "carol's issue types")
		if none.Body["status"] != "not_connected" || len(list(t, none, "issueTypes")) != 0 {
			t.Errorf("carol's issue types are %s", none.Raw)
		}
		want(t, api.anonymous().get(t, "/api/v1/armature/issue-types"), http.StatusUnauthorized, "nobody reads the issue types")
	})

	t.Run("each item is an issue, in order, filed as the caller and linking back to the page", func(t *testing.T) {
		bug := typeNamed(t, bob, "Bug")
		body := createBody(page, "cp", "  Renew the   TLS\ncertificate ", "Write the migration guide")
		body["typeId"] = bug
		r := want(t, bob.post(t, items, body), http.StatusCreated, "bob files two issues")
		if keys := keysOfIssues(t, r); strings.Join(keys, " ") != "CP-6 CP-7" || r.Body["failed"] != nil {
			t.Fatalf("bob filed %s", r.Raw)
		}
		first := list(t, r, "issues")[0].(map[string]any)
		if first["summary"] != "Renew the TLS certificate" || first["url"] != armatureURL(t)+"/issues/CP-6" || first["type"].(map[string]any)["name"] != "Bug" {
			t.Errorf("the first issue is %v", first)
		}
		held := stubIssue(t, slug, "CP-6")
		if held["reporter"].(map[string]any)["name"] != "Alice" {
			t.Errorf("Armature has CP-6 from %v, not from bob's Armature self", held["reporter"])
		}
		description, _ := json.Marshal(held["description"])
		pageURL := armature.PageURL(armatureAppURL, "CRT", uuid.MustParse(page))
		for _, part := range []string{`"text":"From "`, `"text":"Review notes"`, `"href":"` + pageURL + `"`, `"text":" in Stator"`} {
			if !strings.Contains(string(description), part) {
				t.Errorf("the description %s lacks %s", description, part)
			}
		}
		if _, cached := cachedIssue(t, h, home.org, tokenRowOf(t, h, home.org, bobID), "CP-6"); !cached {
			t.Error("the new issue is not in bob's cache, so its chip waits on Armature")
		}
		if again := stubIssue(t, slug, "CP-7"); again["reporter"].(map[string]any)["name"] != "Alice" {
			t.Errorf("CP-7 is from %v", again["reporter"])
		}
	})

	t.Run("filing stops at the first item Armature refuses", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/refused-summary", map[string]any{"summary": "Update the status page"})
		defer stubControl(t, http.MethodPut, slug+"/refused-summary", map[string]any{"summary": ""})
		r := want(t, bob.post(t, items, createBody(page, "CP", "Tell the support team", "Update the status page", "Never tried")), http.StatusCreated, "the second is refused")
		if keys := keysOfIssues(t, r); strings.Join(keys, " ") != "CP-8" {
			t.Errorf("bob filed %v", keys)
		}
		failed := obj(t, r, "failed")
		if failed["index"] != float64(1) || failed["code"] != "validation_failed" || failed["message"] != "Armature refuses this summary." {
			t.Errorf("the refusal is %v", failed)
		}
		search := want(t, owner.get(t, searchPath(`project = CP ORDER BY key`, 20, 0)), http.StatusOK, "what Armature holds")
		if strings.Contains(string(search.Raw), "Never tried") {
			t.Error("an item after the refusal reached Armature")
		}

		first := want(t, bob.post(t, items, createBody(page, "CP", "Update the status page", "Never tried")), http.StatusUnprocessableEntity, "the first is refused")
		if errorCode(t, first) != "validation_failed" {
			t.Errorf("a refused first item answers %s", first.Raw)
		}
	})

	t.Run("Armature's refusals are relayed", func(t *testing.T) {
		errorCodeIs(t, want(t, bob.post(t, items, createBody(page, "SEC", "Not hers to see")), http.StatusNotFound, "a project bob cannot see"), "not_found")
		stubControl(t, http.MethodPut, slug+"/people/alice/read-only-projects", map[string]any{"projects": []string{"CP"}})
		errorCodeIs(t, want(t, bob.post(t, items, createBody(page, "CP", "Only to read")), http.StatusForbidden, "a project bob may only read"), "forbidden")
		stubControl(t, http.MethodPut, slug+"/people/alice/read-only-projects", map[string]any{"projects": []string{}})
	})

	t.Run("what Stator refuses before asking Armature", func(t *testing.T) {
		fieldError(t, want(t, bob.post(t, items, createBody(page, " ", "No project")), http.StatusUnprocessableEntity, "no project"), "projectKey")
		fieldError(t, want(t, bob.post(t, items, createBody(page, "CP")), http.StatusUnprocessableEntity, "no items"), "items")
		fieldError(t, want(t, bob.post(t, items, createBody(page, "CP", "Fine", "   ")), http.StatusUnprocessableEntity, "a blank summary"), "items")
		fieldError(t, want(t, bob.post(t, items, createBody(page, "CP", strings.Repeat("a", armature.MaxSummaryLength+1))), http.StatusUnprocessableEntity, "a long summary"), "items")
		many := make([]string, armature.MaxCreateItems+1)
		for i := range many {
			many[i] = "Item"
		}
		fieldError(t, want(t, bob.post(t, items, createBody(page, "CP", many...)), http.StatusUnprocessableEntity, "too many items"), "items")

		want(t, bob.post(t, items, createBody(uuid.NewString(), "CP", "No such page")), http.StatusNotFound, "a page that is not there")
		want(t, restrict(t, owner, page, nil, []any{user(home.user)}), http.StatusOK, "only the owner edits the page")
		want(t, bob.post(t, items, createBody(page, "CP", "Not his page to edit")), http.StatusForbidden, "bob may only read the page")
		want(t, restrict(t, owner, page, nil, nil), http.StatusOK, "everybody edits again")

		errorCodeIs(t, want(t, carol.post(t, items, createBody(page, "CP", "Without a token")), http.StatusConflict, "carol has no token"), "armature_not_connected")
		_, readOnly := makeToken(t, bob, map[string]any{"name": "scripts", "scopes": []string{"read"}})
		want(t, api.withToken(readOnly).post(t, items, createBody(page, "CP", "Read only")), http.StatusForbidden, "a read-only Stator token")
		want(t, api.anonymous().post(t, items, createBody(page, "CP", "Nobody")), http.StatusUnauthorized, "nobody files")
	})

	t.Run("a token Armature no longer takes is marked and never sent again", func(t *testing.T) {
		box, _ := secret.New(testSecretKey)
		bound := append(append([]byte("armature.token:"), home.org[:]...), bobID[:]...)
		revoked, _ := box.Seal([]byte("armature_pat_revoked"), bound)
		if _, err := h.super.Exec(context.Background(), `UPDATE armature_token SET token = $2 WHERE user_id = $1`, bobID, revoked); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		errorCodeIs(t, want(t, bob.post(t, items, createBody(page, "CP", "Rejected")), http.StatusConflict, "a revoked token"), "armature_rejected")
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1 AND status = 'rejected'`, bobID); n != 1 {
			t.Error("the refused token was not marked rejected")
		}
		h.settle(t)
		errorCodeIs(t, want(t, bob.post(t, items, createBody(page, "CP", "Again")), http.StatusConflict, "the same token again"), "armature_rejected")
	})
}

func errorCodeIs(t *testing.T, r response, code string) {
	t.Helper()
	if got := errorCode(t, r); got != code {
		t.Errorf("the code is %s, want %s: %s", got, code, r.Raw)
	}
}

func typeNamed(t *testing.T, c *client, name string) string {
	t.Helper()
	for _, each := range list(t, want(t, c.get(t, "/api/v1/armature/issue-types"), http.StatusOK, "the issue types"), "issueTypes") {
		if kind := each.(map[string]any); kind["name"] == name {
			return kind["id"].(string)
		}
	}
	t.Fatalf("no issue type %s", name)
	return ""
}
