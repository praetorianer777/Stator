//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Finding an issue by words (#425): the picker's and the inline menu's
// suggestions, each reader getting what Armature's text search shows them.

func findPath(words string) string {
	return "/api/v1/armature/find?" + url.Values{"words": {words}}.Encode()
}

func findKeys(t *testing.T, c *client, words string) (string, []string, response) {
	t.Helper()
	r := want(t, c.get(t, findPath(words)), http.StatusOK, "find "+words)
	status, _ := r.Body["status"].(string)
	keys := []string{}
	for _, each := range list(t, r, "issues") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return status, keys, r
}

func TestFindingIssuesByWordsShowsEachReaderWhatArmatureShowsThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-find")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	carol := api.as(t, h.addPerson(t, "member"))

	if status, keys, _ := findKeys(t, bob, "onboarding"); status != "not_configured" || len(keys) != 0 {
		t.Errorf("before a connection the find is %s %v", status, keys)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a word of the summary, in any case, the most recently updated first", func(t *testing.T) {
		if _, keys, r := findKeys(t, bob, "onboarding"); strings.Join(keys, " ") != "CP-3" {
			t.Errorf("onboarding finds %s", r.Raw)
		}
		_, keys, r := findKeys(t, bob, "  SIGN  ")
		if strings.Join(keys, " ") != "CP-4 CP-2" {
			t.Errorf("SIGN finds %v", keys)
		}
		first := list(t, r, "issues")[0].(map[string]any)
		if first["url"] != armatureURL(t)+"/issues/CP-4" || first["projectKey"] != "CP" || first["summary"] != "Rotate the signing keys" {
			t.Errorf("the first issue is %v", first)
		}
		if !cachedSearch(t, h, tokenRowOf(t, h, bobID), armature.FindQuery("SIGN"), armature.FindLimit, 0) {
			t.Error("the find is not cached like the other searches")
		}
	})

	t.Run("words of the description find it too", func(t *testing.T) {
		description := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Customers see a blank screen after the payment."},
		}}}}
		stubControl(t, http.MethodPost, slug+"/projects/CP/issues", map[string]any{"summary": "Checkout hangs", "type": "Bug", "description": description})
		if _, keys, _ := findKeys(t, bob, "blank payment"); strings.Join(keys, " ") != "CP-6" {
			t.Errorf("words of the description find %v", keys)
		}
	})

	t.Run("an issue key puts that issue first", func(t *testing.T) {
		if _, keys, _ := findKeys(t, bob, "cp-2"); len(keys) == 0 || keys[0] != "CP-2" {
			t.Errorf("cp-2 finds %v", keys)
		}
		if status, keys, _ := findKeys(t, bob, "SEC-1"); status != "ok" || len(keys) != 0 {
			t.Errorf("bob finds a key he may not see: %s %v", status, keys)
		}
	})

	t.Run("each reader finds what they may see", func(t *testing.T) {
		if _, keys, r := findKeys(t, bob, "vulnerability"); len(keys) != 0 || strings.Contains(string(r.Raw), "disclosed") {
			t.Errorf("bob finds a secret issue: %s", r.Raw)
		}
		if _, keys, _ := findKeys(t, owner, "vulnerability"); strings.Join(keys, " ") != "SEC-1" {
			t.Errorf("the owner finds %v", keys)
		}
	})

	t.Run("typed quotes and backslashes stay words, never more of the query", func(t *testing.T) {
		for _, words := range []string{`" OR project = SEC OR text ~ "`, `onboarding" ORDER BY key`, `\`, `"`, `a\"b`} {
			if status, keys, r := findKeys(t, owner, words); status != "ok" || len(keys) != 0 {
				t.Errorf("%s finds %s", words, r.Raw)
			}
		}
	})

	t.Run("what the find refuses before asking Armature", func(t *testing.T) {
		fieldError(t, want(t, bob.get(t, "/api/v1/armature/find"), http.StatusUnprocessableEntity, "no words"), "words")
		fieldError(t, want(t, bob.get(t, findPath(" \t ")), http.StatusUnprocessableEntity, "blank words"), "words")
		fieldError(t, want(t, bob.get(t, findPath(strings.Repeat("a", armature.MaxFindLength+1))), http.StatusUnprocessableEntity, "too many"), "words")
		want(t, api.anonymous().get(t, findPath("onboarding")), http.StatusUnauthorized, "nobody finds")
	})

	t.Run("a reader without a token gets the status and no issues", func(t *testing.T) {
		if status, keys, _ := findKeys(t, carol, "onboarding"); status != "not_connected" || len(keys) != 0 {
			t.Errorf("carol's find is %s %v", status, keys)
		}
	})
}
