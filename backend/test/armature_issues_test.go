//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Smart links (#28): issues by key, each viewer seeing what Armature shows
// them, through the stub and the per-person cache.

func lookupPath(keys ...string) string {
	q := url.Values{"key": keys}
	return "/api/v1/armature/issues?" + q.Encode()
}

// lookup asks for keys and answers the status and each key's issue or nil.
func lookup(t *testing.T, c *client, keys ...string) (string, []map[string]any, response) {
	t.Helper()
	r := want(t, c.get(t, lookupPath(keys...)), http.StatusOK, "look up "+strings.Join(keys, ","))
	status, _ := r.Body["status"].(string)
	var out []map[string]any
	for _, each := range list(t, r, "issues") {
		out = append(out, each.(map[string]any))
	}
	return status, out, r
}

func issueOf(result map[string]any) map[string]any {
	issue, _ := result["issue"].(map[string]any)
	return issue
}

// stubControl changes the stub's world for a tenant, as the tests' own hand.
func stubControl(t *testing.T, method, path string, body any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, os.Getenv(envArmatureStub)+"/_stub/"+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("the stub answered %s %s with %d", method, path, resp.StatusCode)
	}
}

// cachedIssue reads what the cache holds for a token row and a key, and whether it holds anything.
func cachedIssue(t *testing.T, h *harness, org, tokenID uuid.UUID, key string) (map[string]any, bool) {
	t.Helper()
	raw, err := h.valkey(t).HGet(context.Background(), armature.IssueKey(org, key), tokenID.String()).Bytes()
	if err != nil {
		return nil, false
	}
	var entry struct {
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	return entry.Value, true
}

func tokenRowOf(t *testing.T, h *harness, org, user uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM armature_token WHERE org_id = $1 AND user_id = $2`, org, user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSmartLinksShowEachViewerTheIssuesArmatureShowsThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-links")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	base := armatureURL(t)

	if status, issues, _ := lookup(t, bob, "CP-1"); status != "not_configured" || len(issues) != 0 {
		t.Errorf("before a connection the lookup is %s %v", status, issues)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "orgSlug": slug}), http.StatusOK, "connect Armature")
	// In Armature the owner is its admin, who sees SEC; bob is alice, who does not.
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a member without a token gets the status and no issue", func(t *testing.T) {
		if status, issues, _ := lookup(t, carol, "CP-1", "SEC-1"); status != "not_connected" || len(issues) != 0 {
			t.Errorf("carol's lookup is %s %v", status, issues)
		}
		one := want(t, carol.get(t, "/api/v1/armature/issues/CP-1"), http.StatusOK, "carol reads one issue")
		if one.Body["status"] != "not_connected" || one.Body["issue"] != nil {
			t.Errorf("carol's issue is %s", one.Raw)
		}
		projects := want(t, carol.get(t, "/api/v1/armature/projects"), http.StatusOK, "carol's projects")
		if projects.Body["status"] != "not_connected" || len(list(t, projects, "projects")) != 0 {
			t.Errorf("carol's projects are %s", projects.Raw)
		}
	})

	t.Run("one lookup answers every key once, in order, moved issues by their old key", func(t *testing.T) {
		status, issues, raw := lookup(t, bob, "cp-1", "SEC-1", "SEC-2", "CP-99", "CP-1")
		if status != "ok" || len(issues) != 4 {
			t.Fatalf("bob's lookup is %s", raw.Raw)
		}
		var keys []string
		for _, each := range issues {
			keys = append(keys, each["key"].(string))
		}
		if strings.Join(keys, " ") != "CP-1 SEC-1 SEC-2 CP-99" {
			t.Errorf("the keys answered are %v", keys)
		}
		cp1 := issueOf(issues[0])
		if cp1["summary"] != "Set up the build pipeline" || cp1["url"] != base+"/issues/CP-1" || cp1["projectKey"] != "CP" {
			t.Errorf("CP-1 is %v", cp1)
		}
		if typ := cp1["type"].(map[string]any); typ["icon"] != "task" || typ["name"] != "Task" {
			t.Errorf("CP-1's type is %v", typ)
		}
		if st := cp1["status"].(map[string]any); st["category"] != "done" {
			t.Errorf("CP-1's status is %v", st)
		}
		if issueOf(issues[1]) != nil {
			t.Errorf("bob sees SEC-1: %v", issues[1])
		}
		if moved := issueOf(issues[2]); moved == nil || moved["key"] != "CP-5" || moved["url"] != base+"/issues/CP-5" {
			t.Errorf("SEC-2 does not find the issue it moved to: %v", issues[2])
		}
		if issueOf(issues[3]) != nil {
			t.Errorf("CP-99 is %v", issues[3])
		}
		if bytes.Contains(raw.Raw, []byte("vulnerability")) {
			t.Error("a secret issue's summary reached bob")
		}

		_, theirs, _ := lookup(t, owner, "SEC-1")
		if got := issueOf(theirs[0]); got == nil || got["summary"] != "Patch the disclosed vulnerability" {
			t.Errorf("Armature's admin does not see SEC-1: %v", theirs)
		}
	})

	t.Run("the cache keeps each viewer's answer, a hidden issue as null", func(t *testing.T) {
		bobRow, ownerRow := tokenRowOf(t, h, home.org, bobID), tokenRowOf(t, h, home.org, home.user)
		if got, ok := cachedIssue(t, h, home.org, bobRow, "SEC-1"); !ok || got != nil {
			t.Errorf("bob's SEC-1 is cached as %v, %v", got, ok)
		}
		if got, ok := cachedIssue(t, h, home.org, ownerRow, "SEC-1"); !ok || got["summary"] != "Patch the disclosed vulnerability" {
			t.Errorf("the owner's SEC-1 is cached as %v, %v", got, ok)
		}

		stubControl(t, http.MethodPatch, slug+"/issues/SEC-1", map[string]any{"summary": "Renamed in Armature"})
		stubControl(t, http.MethodPatch, slug+"/issues/CP-1", map[string]any{"summary": "Renamed in Armature"})
		_, mine, _ := lookup(t, bob, "SEC-1", "CP-1")
		_, theirs, _ := lookup(t, owner, "SEC-1")
		if issueOf(mine[0]) != nil || issueOf(mine[1])["summary"] != "Set up the build pipeline" || issueOf(theirs[0])["summary"] != "Patch the disclosed vulnerability" {
			t.Errorf("an answer was not served from the cache: %v %v", mine, theirs)
		}
		one := want(t, bob.get(t, "/api/v1/armature/issues/cp-1"), http.StatusOK, "bob reads CP-1 alone")
		if issue, _ := one.Body["issue"].(map[string]any); one.Body["status"] != "ok" || issue["summary"] != "Set up the build pipeline" {
			t.Errorf("the single read does not share the lookup's cache: %s", one.Raw)
		}

		armature.NewCache(h.valkey(t), discard()).ForgetIssues(context.Background(), home.org, "SEC-1", "CP-1")
		_, mine, _ = lookup(t, bob, "SEC-1", "CP-1")
		_, theirs, _ = lookup(t, owner, "SEC-1")
		if issueOf(mine[0]) != nil || issueOf(mine[1])["summary"] != "Renamed in Armature" || issueOf(theirs[0])["summary"] != "Renamed in Armature" {
			t.Errorf("after the cache cleared: %v %v", mine, theirs)
		}
	})

	t.Run("one issue, and the projects the viewer may see", func(t *testing.T) {
		moved := want(t, bob.get(t, "/api/v1/armature/issues/SEC-2"), http.StatusOK, "bob reads a moved key")
		if issue, _ := moved.Body["issue"].(map[string]any); moved.Body["status"] != "ok" || issue["key"] != "CP-5" {
			t.Errorf("SEC-2 alone is %s", moved.Raw)
		}
		hidden := want(t, bob.get(t, "/api/v1/armature/issues/SEC-1"), http.StatusOK, "bob reads a secret issue")
		if hidden.Body["status"] != "ok" || hidden.Body["issue"] != nil {
			t.Errorf("SEC-1 for bob is %s", hidden.Raw)
		}
		for who, c := range map[string]*client{"bob": bob, "the owner": owner} {
			r := want(t, c.get(t, "/api/v1/armature/projects"), http.StatusOK, who+"'s projects")
			var keys []string
			for _, each := range list(t, r, "projects") {
				p := each.(map[string]any)
				keys = append(keys, fmt.Sprintf("%s:%v", p["key"], p["canCreate"]))
			}
			wantKeys := "CP:true"
			if who == "the owner" {
				wantKeys = "CP:true SEC:true"
			}
			if r.Body["status"] != "ok" || strings.Join(keys, " ") != wantKeys {
				t.Errorf("%s's projects are %s", who, r.Raw)
			}
		}
	})

	t.Run("what the lookup refuses", func(t *testing.T) {
		many := make([]string, 0, armature.MaxLookupKeys+1)
		for i := 1; i <= armature.MaxLookupKeys+1; i++ {
			many = append(many, fmt.Sprintf("CP-%d", i))
		}
		for name, path := range map[string]string{
			"no key":       "/api/v1/armature/issues",
			"too many":     lookupPath(many...),
			"not a key":    lookupPath("CP-1", "UTF8"),
			"a whole link": lookupPath(base + "/issues/CP-1"),
		} {
			if msg := fieldError(t, want(t, bob.get(t, path), http.StatusUnprocessableEntity, name), "key"); msg == "" {
				t.Errorf("%s: no sentence", name)
			}
		}
		fieldError(t, want(t, bob.get(t, "/api/v1/armature/issues/CP-0"), http.StatusUnprocessableEntity, "one bad key"), "issueKey")
		for _, path := range []string{lookupPath("CP-1"), "/api/v1/armature/issues/CP-1", "/api/v1/armature/projects"} {
			want(t, api.anonymous().get(t, path), http.StatusUnauthorized, "nobody asks "+path)
		}
		_, readOnly := makeToken(t, bob, map[string]any{"name": "chips", "scopes": []string{"read"}})
		if status, issues, _ := lookup(t, api.withToken(readOnly), "CP-1"); status != "ok" || issueOf(issues[0]) == nil {
			t.Errorf("a read-only Stator token reads %s %v", status, issues)
		}
	})

	t.Run("a token Armature refuses is marked and not sent again", func(t *testing.T) {
		box, _ := secret.New(testSecretKey(t))
		bound := append(append([]byte("armature.token:"), home.org[:]...), bobID[:]...)
		revoked, _ := box.Seal([]byte("armature_pat_revoked"), bound)
		if _, err := h.super.Exec(context.Background(), `UPDATE armature_token SET token = $2 WHERE user_id = $1`, bobID, revoked); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if status, issues, _ := lookup(t, bob, "CP-2"); status != "rejected" || len(issues) != 0 {
			t.Errorf("a revoked token's lookup is %s %v", status, issues)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1 AND status = 'rejected'`, bobID); n != 1 {
			t.Error("the refused token was not marked rejected")
		}
		h.settle(t)
		if status, _, _ := lookup(t, bob, "CP-1"); status != "rejected" {
			t.Errorf("a rejected token was used again: %s", status)
		}
	})

	t.Run("the SSRF guard refuses an address inside the network, and the read says unreachable", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `
			ALTER TABLE armature_connection DISABLE TRIGGER armature_connection_moved;
			UPDATE armature_connection SET base_url = 'http://10.11.12.13' WHERE org_id = '`+home.org.String()+`';
			ALTER TABLE armature_connection ENABLE TRIGGER armature_connection_moved;`); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		started := time.Now()
		status, issues, _ := lookup(t, owner, "CP-3")
		if status != "unreachable" || len(issues) != 0 {
			t.Errorf("a lookup past the guard is %s %v", status, issues)
		}
		if took := time.Since(started); took >= armature.CallTimeout {
			t.Errorf("the guard did not refuse the address at once: %v", took)
		}
		one := want(t, owner.get(t, "/api/v1/armature/issues/CP-3"), http.StatusOK, "one issue past the guard")
		if one.Body["status"] != "unreachable" || one.Body["issue"] != nil {
			t.Errorf("one issue past the guard is %s", one.Raw)
		}
	})
}

func TestAPageIsFoundByTheIssuesItNames(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-search")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "ISS", "Issues")
	body := map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Fixed in "},
			map[string]any{"type": "armatureIssue", "attrs": map[string]any{"key": "QA-417"}},
		}},
	}}
	page := docs.add(docs.homeID, "Release notes", map[string]any{"body": body})

	hits := list(t, searchFor(t, owner, url.Values{"q": {"QA-417"}}), "hits")
	if len(hits) != 1 {
		t.Fatalf("searching the key finds %d hits", len(hits))
	}
	hit := hits[0].(map[string]any)
	if hit["page"].(map[string]any)["id"] != page || !strings.Contains(joined(hit["snippet"]), "Fixed in QA-417") {
		t.Errorf("the hit is %v", hit)
	}

	for name, chip := range map[string]map[string]any{
		"a lower case key": {"type": "armatureIssue", "attrs": map[string]any{"key": "qa-417"}},
		"a stored summary": {"type": "armatureIssue", "attrs": map[string]any{"key": "QA-417", "summary": "Secret"}},
	} {
		bad := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{chip}}}}
		errorCode(t, want(t, owner.patch(t, pagePath(page), map[string]any{"body": bad}), http.StatusUnprocessableEntity, name))
	}
	comment := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
		map[string]any{"type": "armatureIssue", "attrs": map[string]any{"key": "QA-417"}},
	}}}}
	errorCode(t, want(t, owner.post(t, pagePath(page, "/comments"), map[string]any{"body": comment}), http.StatusUnprocessableEntity, "a chip in a comment"))

	raw := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"See "},{"type":"armatureIssue","attrs":{"key":"CP-12"}},{"type":"text","text":" and "},{"type":"armatureIssue","attrs":{"key":"SEC-2"}}]},
		{"type":"armatureIssueBlock","attrs":{"key":"CP-7"}},
		{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"armatureIssue","attrs":{"key":"CP-1"}}]}]}]}]}]}`
	root, err := document.Parse(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, raw).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := document.PlainText(root); got != want || want != "See CP-12 and SEC-2\nCP-7\nCP-1" {
		t.Errorf("the database reads %q, document.PlainText %q", got, want)
	}
}
