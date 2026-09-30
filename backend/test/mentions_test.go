//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/notify"
)

// mentionDoc is one paragraph of text followed by a mention of each person.
func mentionDoc(text string, people ...uuid.UUID) map[string]any {
	inline := []any{map[string]any{"type": "text", "text": text}}
	for _, id := range people {
		inline = append(inline, map[string]any{"type": "text", "text": " "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": id.String(), "label": "Someone"}})
	}
	return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": inline}}}
}

// publishBody saves a draft of a page with the given body and publishes it,
// never with a notice, so only mentions can tell anybody.
func publishBody(t *testing.T, c *client, page string, body map[string]any) {
	t.Helper()
	p := obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")
	want(t, c.put(t, pagePath(page, "/draft"), map[string]any{"title": p["title"], "body": body, "baseVersion": p["version"]}), http.StatusOK, "save a draft")
	want(t, c.post(t, pagePath(page, "/publish"), map[string]any{"notifyWatchers": false}), http.StatusOK, "publish")
}

func mentionsOf(t *testing.T, c *client) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, n := range notificationsOf(t, c, "") {
		if n["kind"] == "mentioned" {
			out = append(out, n)
		}
	}
	return out
}

func mentionable(t *testing.T, c *client, page, q string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, each := range list(t, want(t, c.get(t, pagePath(page, "/mentionable?q="+q)), http.StatusOK, "the mention picker"), "people") {
		p := each.(map[string]any)
		out[p["name"].(string)] = p["canView"].(bool)
	}
	return out
}

// A mention tells the person named once, in a page when it is published and
// in a comment when it is posted or edited, and never somebody who may not
// view the page.
func TestMentionsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	home := h.makeMember(t, "mentions")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID, danID := h.namedPerson(t, home.org, "Ann Named"), h.namedPerson(t, home.org, "Ben Named"), h.namedPerson(t, home.org, "Carl Named"), h.namedPerson(t, home.org, "Dan Outsider")
	ann, ben, carl, dan := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug), api.as(t, danID, home.org, slug)

	docs := newTree(t, owner, "MENT", "Mentions")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("The plan.")})
	want(t, restrict(t, owner, plan, []any{user(home.user), user(annID), user(benID), user(carlID)}, nil), http.StatusOK, "Plan is not for dan")
	h.settle(t)

	t.Run("the picker offers everybody and says who may view the page", func(t *testing.T) {
		got := mentionable(t, owner, plan, "")
		if len(got) < 5 || !got["Ann Named"] || got["Dan Outsider"] {
			t.Errorf("the picker offers %v", got)
		}
		if got := mentionable(t, ann, plan, "nam"); len(got) != 3 || !got["Carl Named"] {
			t.Errorf("a word of the name finds %v", got)
		}
		fresh := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": plan, "title": "Unpublished"}), http.StatusCreated, "a page not yet published"), "page")["id"].(string)
		h.settle(t)
		if got := mentionable(t, owner, fresh, "ann"); !got["Ann Named"] {
			t.Errorf("an unpublished page's picker says ann may not view it once published: %v", got)
		}
		limited := want(t, owner.get(t, pagePath(plan, "/mentionable?limit=1")), http.StatusOK, "one at a time")
		if n := len(list(t, limited, "people")); n != 1 {
			t.Errorf("limit=1 answered %d people", n)
		}
		for _, bad := range []string{"0", "51", "x"} {
			want(t, owner.get(t, pagePath(plan, "/mentionable?limit="+bad)), http.StatusUnprocessableEntity, "limit="+bad)
		}
		want(t, dan.get(t, pagePath(plan, "/mentionable")), http.StatusNotFound, "dan asks who to mention on a page he may not view")
		want(t, owner.get(t, pagePath(uuid.NewString(), "/mentionable")), http.StatusNotFound, "no such page")
		want(t, api.anonymous().get(t, pagePath(plan, "/mentionable")), http.StatusUnauthorized, "nobody signed in")
	})

	t.Run("an id that is not a person's uuid is refused", func(t *testing.T) {
		body := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "mention", "attrs": map[string]any{"id": "ann", "label": "Ann"}}}}}}
		want(t, owner.put(t, pagePath(plan, "/draft"), map[string]any{"title": "Plan", "body": body, "baseVersion": 1}), http.StatusUnprocessableEntity, "a mention of no uuid")
		want(t, owner.post(t, pagePath(plan, "/comments"), map[string]any{"body": body}), http.StatusUnprocessableEntity, "a comment mentioning no uuid")
	})

	t.Run("publishing tells the people newly mentioned who may view the page", func(t *testing.T) {
		publishBody(t, owner, plan, mentionDoc("Ann owns the budget.", annID, danID, uuid.New(), home.user))
		h.drained(t, home.org)
		got := mentionsOf(t, ann)
		if len(got) != 1 || got[0]["page"].(map[string]any)["id"] != plan || number(got[0]["version"]) != 2 ||
			got[0]["excerpt"] != "Ann owns the budget. @Someone @Someone @Someone @Someone" || got[0]["actorId"] != home.user.String() || got[0]["threadId"] != nil {
			t.Errorf("ann was told %v", got)
		}
		if got := notificationsOf(t, dan, ""); len(got) != 0 {
			t.Errorf("dan, who may not view the page, was told %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE org_id = $1 AND user_id = $2`, home.org, danID); n != 0 {
			t.Errorf("dan has %d rows", n)
		}
		if got := notificationsOf(t, owner, ""); len(got) != 0 {
			t.Errorf("the actor was told of their own mention: %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM watch WHERE org_id = $1 AND user_id = $2`, home.org, annID); n != 0 {
			t.Errorf("a mention made ann watch %d things", n)
		}
	})

	t.Run("a republish tells nobody again, and an added person alone", func(t *testing.T) {
		publishBody(t, owner, plan, mentionDoc("Ann owns the budget, still.", annID, danID))
		h.drained(t, home.org)
		if got := mentionsOf(t, ann); len(got) != 1 {
			t.Errorf("a republish told ann again: %v", got)
		}
		publishBody(t, owner, plan, mentionDoc("Ann and Ben own it.", annID, benID))
		h.drained(t, home.org)
		if got := mentionsOf(t, ann); len(got) != 1 {
			t.Errorf("adding ben told ann again: %v", got)
		}
		if got := mentionsOf(t, ben); len(got) != 1 || number(got[0]["version"]) != 4 {
			t.Errorf("ben was told %v", got)
		}
		want(t, owner.post(t, pagePath(plan, "/versions/2/restore"), map[string]any{"baseVersion": 4}), http.StatusOK, "restore version 2")
		h.drained(t, home.org)
		if got := mentionsOf(t, ann); len(got) != 1 {
			t.Errorf("a restore that keeps ann told her again: %v", got)
		}
	})

	var thread string
	t.Run("a comment tells the people it mentions who may view the page", func(t *testing.T) {
		made := obj(t, want(t, ann.post(t, pagePath(plan, "/comments"), map[string]any{"body": mentionDoc("Carl, can you check?", carlID, danID, annID)}),
			http.StatusCreated, "ann mentions carl in a comment"), "thread")
		thread = made["id"].(string)
		h.drained(t, home.org)
		got := mentionsOf(t, carl)
		if len(got) != 1 || got[0]["threadId"] != thread || got[0]["commentId"] != thread || got[0]["excerpt"] != "Carl, can you check? @Someone @Someone @Someone" {
			t.Errorf("carl was told %v", got)
		}
		if got := notificationsOf(t, dan, ""); len(got) != 0 {
			t.Errorf("dan was told %v", got)
		}
		for _, n := range notificationsOf(t, ann, "") {
			if n["commentId"] == thread {
				t.Errorf("ann was told of her own mention: %v", n)
			}
		}
	})

	t.Run("an edit tells only the people it adds, and nobody twice", func(t *testing.T) {
		want(t, ann.patch(t, "/api/v1/comments/"+thread, map[string]any{"body": mentionDoc("Carl or Ben?", carlID, benID)}), http.StatusOK, "ann adds ben")
		h.drained(t, home.org)
		if got := mentionsOf(t, carl); len(got) != 1 {
			t.Errorf("the edit told carl again: %v", got)
		}
		got := mentionsOf(t, ben)
		if len(got) != 2 || got[0]["commentId"] != thread || got[0]["excerpt"] != "Carl or Ben? @Someone @Someone" {
			t.Errorf("ben was told %v", got)
		}
		want(t, ann.patch(t, "/api/v1/comments/"+thread, map[string]any{"body": mentionDoc("Ben?", benID)}), http.StatusOK, "ann drops carl")
		want(t, ann.patch(t, "/api/v1/comments/"+thread, map[string]any{"body": mentionDoc("Carl after all.", carlID)}), http.StatusOK, "ann names carl again")
		h.drained(t, home.org)
		if got := mentionsOf(t, carl); len(got) != 1 {
			t.Errorf("naming carl again told him twice: %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = 'comment.edited'`, home.org); n != 3 {
			t.Errorf("%d comment.edited events for three edits", n)
		}
	})

	t.Run("a reply mentions too", func(t *testing.T) {
		want(t, reply(t, carl, thread, "Done."), http.StatusCreated, "carl replies")
		r := obj(t, want(t, ben.post(t, "/api/v1/comments/"+thread+"/replies", map[string]any{"body": mentionDoc("Thanks", carlID)}), http.StatusCreated, "ben thanks carl"), "comment")
		h.drained(t, home.org)
		got := mentionsOf(t, carl)
		if len(got) != 2 || got[0]["commentId"] != r["id"] {
			t.Errorf("carl was told %v", got)
		}
	})
}

// Straight through SQL, an event cannot mention somebody the organization
// does not hold, and nobody is told of a page they may not view.
func TestMentionsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "mention-rls")
	other := h.makeMember(t, "mention-rls-other")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, danID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")

	docs := newTree(t, owner, "MRAW", "Raw")
	secret := docs.add(docs.homeID, "Secret")
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "Secret is not for dan")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	event := `INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'mention.probe', jsonb_build_object('actorId', $2::text, 'mentioned', $3::jsonb))`

	t.Run("an event mentions only members", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		denied(t, conn, "a mention of a stranger", event, home.org, home.user, `["`+uuid.NewString()+`"]`)
		denied(t, conn, "a mention of another organization's member", event, home.org, home.user, `["`+other.user.String()+`"]`)
		denied(t, conn, "a mention that is no id", event, home.org, home.user, `["ann"]`)
		denied(t, conn, "mentions that are no list", event, home.org, home.user, `{"id":"`+annID.String()+`"}`)
		if _, err := conn.Exec(ctx, event, home.org, home.user, `["`+annID.String()+`","`+danID.String()+`"]`); err != nil {
			t.Errorf("a mention of two members was refused: %v", err)
		}
	})

	t.Run("nobody is told of a mention on a page they may not view", func(t *testing.T) {
		insert := `INSERT INTO notification (org_id, user_id, event_id, kind, page_id) VALUES ($1, $2, $3, 'mentioned', $4)`
		actAs(t, conn, home.org, danID)
		denied(t, conn, "dan told of Secret", insert, home.org, danID, uuid.New(), secret)
		actAs(t, conn, home.org, home.user)
		denied(t, conn, "the owner writing ann's row", insert, home.org, annID, uuid.New(), secret)
	})

	t.Run("the picker's rule leaves out only the unpublished rule", func(t *testing.T) {
		draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": secret, "title": "Draft"}), http.StatusCreated, "a draft below Secret"), "page")["id"].(string)
		h.settle(t)
		actAs(t, conn, home.org, home.user)
		var annViews, annLater, danLater bool
		if err := conn.QueryRow(ctx, `SELECT perm_page_viewable($1, $2), perm_page_viewable_published($1, $2), perm_page_viewable_published($1, $3)`,
			draft, annID, danID).Scan(&annViews, &annLater, &danLater); err != nil {
			t.Fatal(err)
		}
		if annViews || !annLater || danLater {
			t.Errorf("ann views the draft %v, would once published %v; dan would %v", annViews, annLater, danLater)
		}
	})
}
