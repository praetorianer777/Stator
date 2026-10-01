//go:build integration

package test

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
)

// The issue block (#29): a page stores the key alone, and each reader's view
// asks for the issue's details as them.

func issueBlockDoc(key string, attrs map[string]any) map[string]any {
	if attrs == nil {
		attrs = map[string]any{"key": key}
	}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Tracked in"}}},
		map[string]any{"type": "armatureIssueBlock", "attrs": attrs},
	}}
}

func TestAnIssueBlockStoresItsKeyAndEachReaderSeesTheirOwnIssue(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-block")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bob := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	docs := newTree(t, owner, "BLK", "Blocks")
	page := docs.add(docs.homeID, "Key rotation", map[string]any{"body": issueBlockDoc("CP-4", nil)})

	t.Run("the page keeps the key and nothing else, and search finds it by the key", func(t *testing.T) {
		got := want(t, owner.get(t, pagePath(page)), http.StatusOK, "read the page")
		if !bytes.Contains(got.Raw, []byte(`"type":"armatureIssueBlock","attrs":{"key":"CP-4"}`)) || bytes.Contains(got.Raw, []byte("Rotate the signing keys")) {
			t.Errorf("the stored body is %s", got.Raw)
		}
		hits := list(t, searchFor(t, owner, url.Values{"q": {"CP-4"}}), "hits")
		if len(hits) != 1 || hits[0].(map[string]any)["page"].(map[string]any)["id"] != page {
			t.Errorf("searching the block's key finds %v", hits)
		}
	})

	t.Run("a block with anything but a key is refused, and so is one in a comment", func(t *testing.T) {
		for name, attrs := range map[string]map[string]any{
			"a lower case key": {"key": "cp-4"},
			"no key":           {"key": nil},
			"a stored summary": {"key": "CP-4", "summary": "Rotate the signing keys"},
		} {
			errorCode(t, want(t, owner.patch(t, pagePath(page), map[string]any{"body": issueBlockDoc("", attrs)}), http.StatusUnprocessableEntity, name))
		}
		block := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "armatureIssueBlock", "attrs": map[string]any{"key": "CP-4"}}}}
		errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": block}), http.StatusUnprocessableEntity, "a block in a comment"))
	})

	t.Run("the block's issue has every detail it draws, the due date as Armature's day", func(t *testing.T) {
		got := want(t, bob.get(t, "/api/v1/armature/issues/CP-4"), http.StatusOK, "bob reads CP-4")
		issue, _ := got.Body["issue"].(map[string]any)
		if got.Body["status"] != "ok" || issue == nil {
			t.Fatalf("CP-4 for bob is %s", got.Raw)
		}
		if issue["dueDate"] != "2026-10-15T00:00:00Z" || issue["priority"] != "highest" {
			t.Errorf("CP-4's due date and priority are %v, %v", issue["dueDate"], issue["priority"])
		}
		for field, name := range map[string]string{"assignee": "Alice", "reporter": "Bob"} {
			if person, _ := issue[field].(map[string]any); person["name"] != name {
				t.Errorf("CP-4's %s is %v", field, issue[field])
			}
		}
		none := want(t, bob.get(t, "/api/v1/armature/issues/CP-1"), http.StatusOK, "bob reads CP-1")
		if cp1 := none.Body["issue"].(map[string]any); cp1["dueDate"] != nil {
			t.Errorf("CP-1 has no due date, but answers %v", cp1["dueDate"])
		}
	})

	t.Run("an issue only Armature's admin sees is null for bob and drawn for the owner", func(t *testing.T) {
		mine := want(t, bob.get(t, "/api/v1/armature/issues/SEC-1"), http.StatusOK, "bob reads SEC-1")
		if mine.Body["status"] != "ok" || mine.Body["issue"] != nil || bytes.Contains(mine.Raw, []byte("vulnerability")) {
			t.Errorf("SEC-1 for bob is %s", mine.Raw)
		}
		theirs := want(t, owner.get(t, "/api/v1/armature/issues/SEC-1"), http.StatusOK, "the owner reads SEC-1")
		if issue, _ := theirs.Body["issue"].(map[string]any); issue["summary"] != "Patch the disclosed vulnerability" {
			t.Errorf("SEC-1 for the owner is %s", theirs.Raw)
		}
	})

	t.Run("a reader without a token gets the status and no issue", func(t *testing.T) {
		got := want(t, carol.get(t, "/api/v1/armature/issues/CP-4"), http.StatusOK, "carol reads CP-4")
		if got.Body["status"] != "not_connected" || got.Body["issue"] != nil {
			t.Errorf("CP-4 for carol is %s", got.Raw)
		}
		want(t, api.anonymous().get(t, "/api/v1/armature/issues/CP-4"), http.StatusUnauthorized, "nobody reads CP-4")
	})
}
