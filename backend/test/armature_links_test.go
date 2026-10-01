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
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Pages in Armature (#32): publishing a page puts a remote link on every issue
// it names, as the person who published, and every later change keeps them in
// line; the stub's own list shows what reached Armature.

// linkSync is the worker's handler as cmd/worker builds it: the stack's own
// worker runs beside it with the same address and key, so either may sync.
func (h *harness) linkSync(t *testing.T) *armature.LinkSync {
	t.Helper()
	appURL := os.Getenv("STATOR_TEST_APP_URL")
	if appURL == "" {
		t.Fatal("STATOR_TEST_APP_URL is not set; run the suite with make test-integration against the running stack")
	}
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(armatureStubHost)
	client := armature.NewClient(allow, map[string]string{armatureURL(t): os.Getenv(envArmatureStub)})
	svc := armature.NewService(h.cluster, box, client, nil, armature.Options{AppURL: appURL, Allow: allow, Development: true, Log: discard()})
	return armature.NewLinkSync(svc, discard())
}

// stubLinks is every remote link the stub holds for a tenant.
func stubLinks(t *testing.T, tenant string) []map[string]any {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_stub/" + tenant + "/remote-links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		RemoteLinks []map[string]any `json:"remoteLinks"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("the stub's links: %d %s", resp.StatusCode, raw)
	}
	return out.RemoteLinks
}

// linksOn keeps the stub's links of one page, by issue key.
func linksOn(t *testing.T, tenant, pageURL string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, l := range stubLinks(t, tenant) {
		if l["url"] == pageURL {
			out[l["issueKey"].(string)] = l
		}
	}
	return out
}

func sameKeys(t *testing.T, what string, got map[string]map[string]any, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: links on %v, want %v", what, keysOf(got), want)
	}
	for _, key := range want {
		if got[key] == nil {
			t.Fatalf("%s: links on %v, want %v", what, keysOf(got), want)
		}
	}
}

func keysOf(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// syncStates is what GET /pages/{id}/armature-links says of each key.
func syncStates(t *testing.T, c *client, page string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, c.get(t, pagePath(page, "/armature-links")), http.StatusOK, "the page's links"), "links") {
		l := each.(map[string]any)
		out[l["key"].(string)] = l
	}
	return out
}

func chips(keys ...string) []any {
	var out []any
	for i, key := range keys {
		if i > 0 {
			out = append(out, map[string]any{"type": "text", "text": " and "})
		}
		out = append(out, map[string]any{"type": "armatureIssue", "attrs": map[string]any{"key": key}})
	}
	return out
}

// linkedDoc names chips in a paragraph, issue blocks, and a list block whose
// query names an issue too, which is no mention.
func linkedDoc(chipKeys []string, blockKeys ...string) map[string]any {
	content := []any{
		map[string]any{"type": "paragraph", "content": append([]any{map[string]any{"type": "text", "text": "Tracked in "}}, chips(chipKeys...)...)},
		map[string]any{"type": "armatureIssueList", "attrs": map[string]any{"query": "key = CP-3", "columns": []any{"key", "summary"}, "limit": 20}},
	}
	if len(chipKeys) == 0 {
		content[0] = map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Nothing named"}}}
	}
	for _, key := range blockKeys {
		content = append(content, map[string]any{"type": "armatureIssueBlock", "attrs": map[string]any{"key": key}})
	}
	return map[string]any{"type": "doc", "content": content}
}

// republish publishes a new body, and a new title when one is given, over
// the page's current version.
func republish(t *testing.T, c *client, page, title string, body map[string]any) {
	t.Helper()
	current := obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")
	change := map[string]any{"version": number(current["version"]), "body": body}
	if title != "" {
		change["title"] = title
	}
	want(t, c.patch(t, pagePath(page), change), http.StatusOK, "publish "+page)
}

func TestPagesAreLinkedOnTheIssuesTheyName(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-links")
	slug := h.slugOf(t, home.org)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)
	carolID := h.addPerson(t, home.org, "member")
	carol := api.as(t, carolID, home.org, slug)

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects as alice")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)
	sync := h.linkSync(t)
	h.runWorker(t, events.NewMux(nil).Route(events.TopicArmatureLinks, sync))

	docs := newTree(t, owner, "LNK", "Linked")
	page := docs.add(docs.homeID, "Key rotation runbook", map[string]any{"body": linkedDoc([]string{"CP-1"}, "CP-2")})
	pageURL := armature.PageURL(os.Getenv("STATOR_TEST_APP_URL"), "LNK", uuid.MustParse(page))
	h.drained(t, home.org)

	t.Run("publishing links the page on each issue it names, as the person who published", func(t *testing.T) {
		got := linksOn(t, slug, pageURL)
		sameKeys(t, "after the first publish", got, "CP-1", "CP-2")
		for key, l := range got {
			if l["title"] != "Key rotation runbook" || l["source"] != armature.LinkSource || l["createdByName"] != "Alice" {
				t.Errorf("the link on %s is %v", key, l)
			}
		}
		states := syncStates(t, bob, page)
		if len(states) != 2 || states["CP-1"]["state"] != "synced" || states["CP-2"]["state"] != "synced" || states["CP-1"]["syncedAt"] == nil {
			t.Errorf("the page's links read %v", states)
		}
	})

	t.Run("syncing again sends the same links and changes nothing", func(t *testing.T) {
		payload, _ := json.Marshal(events.ArmatureLinks{PageID: uuid.MustParse(page), ActorID: bobID})
		for range 2 {
			if err := sync.Handle(context.Background(), events.Event{OrgID: home.org, Topic: events.TopicArmatureLinks, Payload: payload}); err != nil {
				t.Fatal(err)
			}
		}
		sameKeys(t, "after syncing twice", linksOn(t, slug, pageURL), "CP-1", "CP-2")
		if n := len(stubLinks(t, slug)); n != 2 {
			t.Errorf("the stub holds %d links, want 2", n)
		}
	})

	t.Run("a chip taken out loses its link, and a rename retitles the rest", func(t *testing.T) {
		republish(t, bob, page, "Signing key runbook", linkedDoc(nil, "CP-2"))
		h.drained(t, home.org)
		got := linksOn(t, slug, pageURL)
		sameKeys(t, "after bob's change", got, "CP-2")
		if got["CP-2"]["title"] != "Signing key runbook" {
			t.Errorf("the link on CP-2 is titled %v", got["CP-2"]["title"])
		}
		if states := syncStates(t, owner, page); len(states) != 1 || states["CP-2"]["state"] != "synced" {
			t.Errorf("the page's links read %v", states)
		}
	})

	t.Run("a link bob adds is his", func(t *testing.T) {
		republish(t, bob, page, "", linkedDoc([]string{"CP-1"}, "CP-2"))
		h.drained(t, home.org)
		got := linksOn(t, slug, pageURL)
		sameKeys(t, "after bob adds CP-1", got, "CP-1", "CP-2")
		if got["CP-1"]["createdByName"] != "Bob" || got["CP-2"]["createdByName"] != "Alice" {
			t.Errorf("CP-1 was linked by %v and CP-2 by %v", got["CP-1"]["createdByName"], got["CP-2"]["createdByName"])
		}
	})

	t.Run("a page not every member may view is titled so on its issues", func(t *testing.T) {
		want(t, restrict(t, owner, page, []any{user(home.user), user(bobID)}, nil), http.StatusOK, "the owner hides the page")
		h.drained(t, home.org)
		for key, l := range linksOn(t, slug, pageURL) {
			if l["title"] != armature.RestrictedLinkTitle {
				t.Errorf("the link on %s of a restricted page is titled %v", key, l["title"])
			}
		}
		want(t, carol.get(t, pagePath(page, "/armature-links")), http.StatusNotFound, "carol asks for a page she may not view")
		want(t, restrict(t, owner, page, nil, nil), http.StatusOK, "the owner opens the page again")
		h.drained(t, home.org)
		for key, l := range linksOn(t, slug, pageURL) {
			if l["title"] != "Signing key runbook" {
				t.Errorf("the link on %s of an open page is titled %v", key, l["title"])
			}
		}
	})

	t.Run("an Armature that answers 503 is asked again through the outbox", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/remote-links/outage", map[string]any{"status": http.StatusServiceUnavailable, "count": 1})
		republish(t, bob, page, "", linkedDoc([]string{"CP-1", "CP-4"}, "CP-2"))
		h.drained(t, home.org)
		sameKeys(t, "after the outage", linksOn(t, slug, pageURL), "CP-1", "CP-2", "CP-4")
		if n := h.countRows(t, `
			SELECT count(*) FROM outbox_event
			WHERE org_id = $1 AND topic = $2 AND payload ->> 'pageId' = $3 AND attempts > 1 AND last_error LIKE '%503%'`,
			home.org, events.TopicArmatureLinks, page); n != 1 {
			t.Errorf("%d events were tried again after the 503, want 1", n)
		}
	})

	t.Run("a refusal marks the key failed, and the next change tries it again", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{"CP"}})
		republish(t, bob, page, "", linkedDoc([]string{"CP-1", "CP-4", "CP-5"}, "CP-2", "SEC-1"))
		h.drained(t, home.org)
		states := syncStates(t, owner, page)
		for _, key := range []string{"CP-5", "SEC-1"} {
			if states[key]["state"] != "failed" || !strings.Contains(states[key]["error"].(string), "next change") {
				t.Errorf("%s reads %v", key, states[key])
			}
		}
		if msg := states["CP-5"]["error"].(string); !strings.Contains(msg, "did not let") {
			t.Errorf("a 403 reads %q", msg)
		}
		if msg := states["SEC-1"]["error"].(string); !strings.Contains(msg, "no issue with this key") {
			t.Errorf("a 404 reads %q", msg)
		}
		if states["CP-1"]["state"] != "synced" {
			t.Errorf("CP-1, which needed nothing, reads %v", states["CP-1"])
		}

		stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{}})
		republish(t, carol, page, "", linkedDoc([]string{"CP-1", "CP-4", "CP-5"}, "CP-2", "SEC-1"))
		h.drained(t, home.org)
		if got := syncStates(t, owner, page)["CP-5"]; got["state"] != "failed" || !strings.Contains(got["error"].(string), "has not connected an Armature account") {
			t.Errorf("CP-5 after carol's change, who has no token, reads %v", got)
		}

		republish(t, owner, page, "", linkedDoc([]string{"CP-1", "CP-4", "CP-5"}, "CP-2"))
		h.drained(t, home.org)
		states = syncStates(t, owner, page)
		for _, key := range []string{"CP-1", "CP-2", "CP-4", "CP-5"} {
			if states[key]["state"] != "synced" || states[key]["error"] != nil {
				t.Errorf("%s after the owner's change reads %v", key, states[key])
			}
		}
		sameKeys(t, "after the owner's change", linksOn(t, slug, pageURL), "CP-1", "CP-2", "CP-4", "CP-5")
	})

	t.Run("moving the page to another space puts its links under the new address", func(t *testing.T) {
		ops := newTree(t, owner, "OPS", "Operations")
		want(t, docs.move(page, map[string]any{"parentId": ops.homeID}), http.StatusOK, "move the page")
		h.drained(t, home.org)
		sameKeys(t, "the old address", linksOn(t, slug, pageURL))
		pageURL = armature.PageURL(os.Getenv("STATOR_TEST_APP_URL"), "OPS", uuid.MustParse(page))
		sameKeys(t, "the new address", linksOn(t, slug, pageURL), "CP-1", "CP-2", "CP-4", "CP-5")
		docs = ops
	})

	t.Run("trashing takes the links off and restoring puts them back", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(page)), http.StatusNoContent, "trash the page")
		h.drained(t, home.org)
		sameKeys(t, "in the trash", linksOn(t, slug, pageURL))
		want(t, docs.restore(page), http.StatusOK, "restore the page")
		h.drained(t, home.org)
		sameKeys(t, "restored", linksOn(t, slug, pageURL), "CP-1", "CP-2", "CP-4", "CP-5")
	})

	t.Run("a copy is linked as a page of its own", func(t *testing.T) {
		made := obj(t, want(t, owner.post(t, pagePath(page, "/copy"), map[string]any{"parentId": docs.homeID}), http.StatusCreated, "copy the page"), "page")
		h.drained(t, home.org)
		copyURL := armature.PageURL(os.Getenv("STATOR_TEST_APP_URL"), "OPS", uuid.MustParse(made["id"].(string)))
		sameKeys(t, "the copy", linksOn(t, slug, copyURL), "CP-1", "CP-2", "CP-4", "CP-5")
		want(t, owner.delete(t, pagePath(made["id"].(string))), http.StatusNoContent, "trash the copy")
		want(t, owner.delete(t, "/api/v1/spaces/OPS/trash/"+made["id"].(string)), http.StatusNoContent, "purge the copy")
		h.drained(t, home.org)
		sameKeys(t, "the purged copy", linksOn(t, slug, copyURL))
		if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE page_id = $1`, made["id"]); n != 0 {
			t.Errorf("%d records of the purged copy are left", n)
		}
	})

	t.Run("the records are walled by the database", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, bobID)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM armature_remote_link WHERE page_id = $1`, page).Scan(&n); err != nil || n != 4 {
			t.Errorf("bob reads %d records of a page he may view (%v)", n, err)
		}
		denied(t, conn, "recording a link", `INSERT INTO armature_remote_link (org_id, page_id, issue_key, state) VALUES ($1, $2, 'CP-9', 'synced')`, home.org, page)
		denied(t, conn, "marking a link synced", `UPDATE armature_remote_link SET state = 'synced', error = NULL`)
		denied(t, conn, "forgetting the links", `DELETE FROM armature_remote_link`)
		denied(t, conn, "syncing as the owner", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'armature.links', $2)`,
			home.org, map[string]any{"pageId": page, "actorId": home.user})
		denied(t, conn, "an event with more than a page", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'armature.links', $2)`,
			home.org, map[string]any{"pageId": page, "actorId": bobID, "keys": []string{"CP-9"}})
		denied(t, conn, "an event without a page", `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'armature.links', $2)`,
			home.org, map[string]any{"pageId": "not a page", "actorId": bobID})

		want(t, restrict(t, owner, page, []any{user(home.user)}, nil), http.StatusOK, "the owner hides the page from bob")
		h.drained(t, home.org)
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM armature_remote_link WHERE page_id = $1`, page).Scan(&n); err != nil || n != 0 {
			t.Errorf("bob reads %d records of a page he may not view (%v)", n, err)
		}
		want(t, restrict(t, owner, page, nil, nil), http.StatusOK, "the owner opens the page again")
		h.drained(t, home.org)
	})

	t.Run("a new Armature address forgets the records", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug + "-moved"}), http.StatusOK, "point at another organization")
		if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE org_id = $1`, home.org); n != 0 {
			t.Errorf("%d records survived the move", n)
		}
	})
}
