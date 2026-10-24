//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/events"
)

// viaIn is the via an answer gives, nil for one written in a session.
func viaIn(entry any) map[string]any {
	v, _ := entry.(map[string]any)["via"].(map[string]any)
	return v
}

// throughOf says whether an answer names what it was written through, by kind and name.
func throughOf(entry any, kind, name string) bool {
	v := viaIn(entry)
	return v != nil && v["kind"] == kind && v["name"] == name
}

// What an app writes through a personal token says so wherever it is read, what a person writes in the browser
// does not, and a token revoked keeps its name on what it wrote (#498).
func TestWhatIsWrittenThroughATokenSaysSo(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runStackWorker(t)
	home := h.makeMember(t, "via")
	owner := api.as(t, home.user)
	docs := newTree(t, owner, "VIA", "Through")
	plan := docs.add(docs.homeID, "Plan")
	bin, address := hookBin(t)
	_, secret := makeHook(t, owner, "Comments", address, events.TopicCommentCreated)
	tokenID, token := makeToken(t, owner, map[string]any{"name": "Nightly script"})
	h.settle(t)
	script := api.withToken(token)

	want(t, script.patch(t, pagePath(plan), map[string]any{"title": "Plan, revised", "version": 1}), http.StatusOK, "publish through the token")
	byScript := startThread(t, script, plan, "Checked overnight.")
	startThread(t, owner, plan, "Written by hand.")
	want(t, putRemoteLink(t, script, plan, requirementLink("1")), http.StatusCreated, "link through the token")
	want(t, script.upload(t, pagePath(plan, "/attachments"), "notes.txt", []byte("Overnight notes.")), http.StatusCreated, "upload through the token")
	want(t, react(t, script, "pages", plan, "\U0001F44D"), http.StatusOK, "react through the token")
	made := obj(t, want(t, script.post(t, "/api/v1/spaces", map[string]any{"key": "VIB", "name": "Made overnight"}), http.StatusCreated,
		"make a space through the token"), "space")["id"]
	h.settle(t)

	check := func(t *testing.T, name string) {
		t.Helper()
		versions := list(t, want(t, owner.get(t, pagePath(plan, "/versions")), http.StatusOK, "list the versions"), "versions")
		if !throughOf(versions[0], "token", name) || viaIn(versions[1]) != nil {
			t.Errorf("the versions read %v and %v", viaIn(versions[0]), viaIn(versions[1]))
		}
		threads := threadsOf(t, owner, plan, "")
		first, second := threads[0]["comments"].([]any)[0], threads[1]["comments"].([]any)[0]
		if !throughOf(first, "token", name) || viaIn(second) != nil {
			t.Errorf("the comments read %v and %v", viaIn(first), viaIn(second))
		}
		links := remoteLinksOf(t, want(t, owner.get(t, pagePath(plan, "/remote-links")), http.StatusOK, "list the links"))
		if !throughOf(links[0], "token", name) {
			t.Errorf("the link reads %v", links[0])
		}
		files := list(t, want(t, owner.get(t, pagePath(plan, "/attachments")), http.StatusOK, "list the files"), "attachments")
		if !throughOf(files[0], "token", name) {
			t.Errorf("the file reads %v", files[0])
		}
		if people := pageReactions(t, owner, plan)[0]["people"].([]any); !throughOf(people[0], "token", name) {
			t.Errorf("the reaction reads %v", people)
		}
		created := 0
		for _, e := range list(t, want(t, owner.get(t, "/api/v1/audit?action=space.created"), http.StatusOK, "read the audit log"), "entries") {
			if e.(map[string]any)["targetId"] == made && throughOf(e, "token", name) {
				created++
			}
		}
		if created != 1 {
			t.Errorf("%d audit entries name the token for the space it made", created)
		}
	}
	t.Run("each read names the token", func(t *testing.T) { check(t, "Nightly script") })

	t.Run("the webhook's actor names the token", func(t *testing.T) {
		for _, d := range awaitTopic(t, bin, events.TopicCommentCreated, plan, 2) {
			actor := d.payload()["actor"].(map[string]any)
			byToken := d.payload()["comment"].(map[string]any)["id"] == byScript["comments"].([]any)[0].(map[string]any)["id"]
			if byToken != throughOf(actor, "token", "Nightly script") || (!byToken && actor["via"] != nil) {
				t.Errorf("the actor of %s reads %v", d.Body, actor)
			}
			asDocumented(t, d, secret)
		}
	})

	t.Run("a revoked token keeps its name on what it wrote", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/tokens/"+tokenID), http.StatusNoContent, "revoke the token")
		h.settle(t)
		check(t, "Nightly script")
	})
}

// What a connected app writes through MCP is named by the app (#498).
func TestWhatAConnectedAppWritesNamesTheApp(t *testing.T) {
	w := newConsentWorld(t, "via-app")
	h, api, alice := w.h, w.api, w.alice
	docs := newTree(t, w.admin, "VIAPP", "Through an app")
	plan := docs.add(docs.homeID, "Plan")
	assistant := w.allowed(t, "Desk assistant", "https://desk.example/oauth/callback", true)
	access := connect(t, h, api, alice, assistant, nil).str("access_token")

	if got := toolResult(t, api, access, "add_comment", map[string]any{"pageID": plan, "body": commentDoc("Summarised.")}); got["isError"] == true {
		t.Fatalf("add_comment through the app answered %v", got)
	}
	h.settle(t)
	comment := threadsOf(t, alice, plan, "")[0]["comments"].([]any)[0]
	if !throughOf(comment, "oauth", "Desk assistant") {
		t.Errorf("a comment through the app reads %v", viaIn(comment))
	}
	var id string
	if err := h.super.QueryRow(context.Background(), `SELECT via_id::text FROM comment WHERE page_id = $1`, plan).Scan(&id); err != nil ||
		h.countRows(t, `SELECT count(*) FROM api_token WHERE id = $1 AND client_id = $2`, id, assistant.id) != 1 {
		t.Errorf("the comment names the grant %q, want alice's grant to the app: %v", id, err)
	}
}
