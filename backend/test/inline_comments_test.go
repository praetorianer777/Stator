//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/notify"
)

// marked is a copy of a body with a passage of one paragraph marked for a
// thread, as the reader's selection does it.
func marked(t *testing.T, body map[string]any, block int, passage string, thread string) map[string]any {
	t.Helper()
	var doc map[string]any
	raw, _ := json.Marshal(body)
	_ = json.Unmarshal(raw, &doc)
	para := doc["content"].([]any)[block].(map[string]any)
	var text strings.Builder
	for _, each := range para["content"].([]any) {
		text.WriteString(each.(map[string]any)["text"].(string))
	}
	start := strings.Index(text.String(), passage)
	if start < 0 {
		t.Fatalf("%q is not in block %d", passage, block)
	}
	end := start + len(passage)
	var content []any
	offset := 0
	for _, each := range para["content"].([]any) {
		n := each.(map[string]any)
		s := n["text"].(string)
		from, to := offset, offset+len(s)
		offset = to
		if to <= start || from >= end {
			content = append(content, n)
			continue
		}
		part := func(a, b int, mark bool) map[string]any {
			out := map[string]any{"type": "text", "text": s[a-from : b-from]}
			marks, _ := n["marks"].([]any)
			marks = append([]any{}, marks...)
			if mark {
				marks = append(marks, map[string]any{"type": "inlineComment", "attrs": map[string]any{"threadId": thread}})
			}
			if len(marks) > 0 {
				out["marks"] = marks
			}
			return out
		}
		if from < start {
			content = append(content, part(from, start, false))
		}
		content = append(content, part(max(from, start), min(to, end), true))
		if to > end {
			content = append(content, part(end, to, false))
		}
	}
	para["content"] = content
	return doc
}

// anchorsIn maps each thread marked in a body to the text its mark covers.
func anchorsIn(body any) map[string]string {
	out := map[string]string{}
	var walk func(n any)
	walk = func(n any) {
		node, ok := n.(map[string]any)
		if !ok {
			return
		}
		marks, _ := node["marks"].([]any)
		for _, m := range marks {
			mark := m.(map[string]any)
			if mark["type"] == "inlineComment" {
				id := mark["attrs"].(map[string]any)["threadId"].(string)
				out[id] += node["text"].(string)
			}
		}
		children, _ := node["content"].([]any)
		for _, c := range children {
			walk(c)
		}
	}
	walk(body)
	return out
}

func readPage(t *testing.T, c *client, id string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, pagePath(id)), http.StatusOK, "read the page"), "page")
}

func counts(page map[string]any) (int, int, int) {
	c := page["comments"].(map[string]any)
	return number(c["page"]), number(c["inline"]), number(c["detached"])
}

// startInline marks a passage of the page as the caller reads it and starts
// a thread on it.
func startInline(t *testing.T, c *client, page string, block int, passage, text string) (response, string) {
	t.Helper()
	id := uuid.NewString()
	body := readPage(t, c, page)["body"].(map[string]any)
	return c.post(t, pagePath(page, "/inline-comments"), map[string]any{
		"threadId": id, "body": commentDoc(text), "pageBody": marked(t, body, block, passage, id),
	}), id
}

func threadByID(t *testing.T, c *client, page, id string) map[string]any {
	t.Helper()
	for _, each := range threadsOf(t, c, page, "?kind=inline") {
		if each["id"] == id {
			return each
		}
	}
	t.Fatalf("thread %s is not listed", id)
	return nil
}

func anchorState(thread map[string]any) string {
	return thread["anchor"].(map[string]any)["state"].(string)
}

// Threads on passages: started by anybody who may comment, without touching
// the history; kept on their passage by every publish, detached when it is
// gone; resolved and reopened.
func TestInlineCommentsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "inline")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Reviewer"), h.namedPerson(t, home.org, "Ben Reviewer"), h.namedPerson(t, home.org, "Carl Reader")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)

	docs := newTree(t, owner, "INL", "Inline")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("We ship on Friday after the review.", "Notes follow here.")})
	draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft", "body": textDoc("Not yet.")}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
	want(t, owner.put(t, "/api/v1/spaces/INL/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"view", "addComments"}},
		map[string]any{"subject": user(benID), "permissions": []any{"view", "addComments"}},
	}}), http.StatusOK, "ann and ben comment, nobody but the owner edits")
	h.settle(t)

	before := readPage(t, ann, plan)
	if before["can"].(map[string]any)["edit"] != false {
		t.Fatal("ann may edit, so the test proves nothing")
	}
	var friday string
	t.Run("a reader who may comment and not edit marks a passage", func(t *testing.T) {
		r, id := startInline(t, ann, plan, 0, "ship on Friday", "Why not Monday?")
		got := obj(t, want(t, r, http.StatusCreated, "ann starts a thread on a passage"))
		friday = id
		thread := got["thread"].(map[string]any)
		if thread["id"] != id || thread["kind"] != "inline" || anchorState(thread) != "anchored" ||
			thread["anchor"].(map[string]any)["quote"] != "ship on Friday" || thread["resolved"] != false {
			t.Errorf("the thread is %v", thread)
		}
		if can := thread["can"].(map[string]any); can["reply"] != true || can["resolve"] != true {
			t.Errorf("ann may %v", can)
		}
		page := got["page"].(map[string]any)
		if anchorsIn(page["body"])[id] != "ship on Friday" {
			t.Errorf("the page's body marks %v", anchorsIn(page["body"]))
		}
		if number(page["version"]) != number(before["version"]) || page["updatedAt"] != before["updatedAt"] {
			t.Errorf("marking a passage changed the page: version %v, updated %v", page["version"], page["updatedAt"])
		}
		if _, inline, detached := counts(page); inline != 1 || detached != 0 {
			t.Errorf("the page counts %d inline and %d detached", inline, detached)
		}
		if versions := list(t, want(t, ann.get(t, pagePath(plan, "/versions")), http.StatusOK, "history"), "versions"); len(versions) != 1 {
			t.Errorf("the history has %d versions", len(versions))
		}
		if got := threadsOf(t, carl, plan, "?kind=page"); len(got) != 0 {
			t.Errorf("the inline thread is listed below the page: %v", got)
		}
	})

	t.Run("a body that is not the page's plus the mark is refused", func(t *testing.T) {
		fresh := uuid.NewString()
		stale := marked(t, before["body"].(map[string]any), 1, "Notes", fresh)
		r := ann.post(t, pagePath(plan, "/inline-comments"), map[string]any{"threadId": fresh, "body": commentDoc("Late."), "pageBody": stale})
		if want(t, r, http.StatusConflict, "a passage marked on a stale body"); errorCode(t, r) != "anchor_conflict" {
			t.Errorf("the code is %s", errorCode(t, r))
		}
		current := readPage(t, ann, plan)["body"].(map[string]any)
		reused := marked(t, before["body"].(map[string]any), 1, "Notes", friday)
		r = ann.post(t, pagePath(plan, "/inline-comments"), map[string]any{"threadId": friday, "body": commentDoc("Again."), "pageBody": reused})
		if want(t, r, http.StatusConflict, "a thread id in use"); errorCode(t, r) != "conflict" {
			t.Errorf("the code is %s", errorCode(t, r))
		}

		spanning := marked(t, marked(t, current, 0, "review.", fresh), 1, "Notes", fresh)
		r = ann.post(t, pagePath(plan, "/inline-comments"), map[string]any{"threadId": fresh, "body": commentDoc("Both."), "pageBody": spanning})
		if want(t, r, http.StatusUnprocessableEntity, "a passage over two blocks"); r.Body["error"].(map[string]any)["fields"].(map[string]any)["pageBody"] == nil {
			t.Errorf("the refusal names no pageBody: %s", r.Raw)
		}
		r, _ = startInline(t, carl, plan, 1, "Notes", "Carl may not.")
		want(t, r, http.StatusForbidden, "carl without addComments")
		r, _ = startInline(t, owner, draft, 0, "Not yet", "Too early.")
		if want(t, r, http.StatusConflict, "an unpublished page"); errorCode(t, r) != "unpublished" {
			t.Errorf("the code is %s", errorCode(t, r))
		}
	})

	t.Run("resolving and reopening, by anybody who may comment", func(t *testing.T) {
		made := obj(t, want(t, reply(t, ben, friday, "Monday is safer."), http.StatusCreated, "ben replies"), "comment")["id"].(string)
		got := obj(t, want(t, ben.post(t, "/api/v1/comments/"+made+"/resolve", nil), http.StatusOK, "ben resolves by his reply"), "thread")
		if got["resolved"] != true || got["resolvedByName"] != "Ben Reviewer" || got["resolvedAt"] == nil {
			t.Errorf("the resolved thread is %v", got)
		}
		want(t, ben.post(t, "/api/v1/comments/"+friday+"/resolve", nil), http.StatusOK, "resolving twice is no change")
		if _, inline, _ := counts(readPage(t, carl, plan)); inline != 0 {
			t.Errorf("a resolved thread is counted: %d", inline)
		}
		want(t, carl.post(t, "/api/v1/comments/"+friday+"/reopen", nil), http.StatusForbidden, "carl reopens")
		below := startThread(t, ann, plan, "Below the page.")
		r := ann.post(t, "/api/v1/comments/"+below["id"].(string)+"/resolve", nil)
		if want(t, r, http.StatusConflict, "resolving a thread below the page"); errorCode(t, r) != "not_inline" {
			t.Errorf("the code is %s", errorCode(t, r))
		}
		want(t, ann.post(t, "/api/v1/comments/"+below["id"].(string)+"/reopen", nil), http.StatusConflict, "reopening one")
		want(t, ann.post(t, "/api/v1/comments/"+uuid.NewString()+"/resolve", nil), http.StatusNotFound, "resolving nothing")

		got = obj(t, want(t, ann.post(t, "/api/v1/comments/"+friday+"/reopen", nil), http.StatusOK, "ann reopens"), "thread")
		if got["resolved"] != false || got["resolvedAt"] != nil || got["resolvedByName"] != "" {
			t.Errorf("the reopened thread is %v", got)
		}
		want(t, ben.post(t, "/api/v1/comments/"+friday+"/resolve", nil), http.StatusOK, "ben resolves again")
		want(t, reply(t, ann, friday, "Not settled yet."), http.StatusCreated, "ann replies to the resolved thread")
		if th := threadByID(t, carl, plan, friday); th["resolved"] != false {
			t.Errorf("a reply left the thread resolved: %v", th)
		}
	})

	t.Run("every publish keeps, moves or detaches the passages", func(t *testing.T) {
		r, notes := startInline(t, ben, plan, 1, "Notes", "Which notes?")
		want(t, r, http.StatusCreated, "ben marks the notes")

		// A publish that knows nothing of the marks puts them back by their quotes.
		want(t, owner.patch(t, pagePath(plan), map[string]any{"version": 1, "body": textDoc("Now we ship on Friday after the review.", "Notes follow here, soon.")}), http.StatusOK, "the owner publishes without marks")
		page := readPage(t, carl, plan)
		if got := anchorsIn(page["body"]); got[friday] != "ship on Friday" || got[notes] != "Notes" || len(got) != 2 {
			t.Errorf("after the publish the marks are %v", got)
		}
		version := obj(t, want(t, carl.get(t, pagePath(plan, "/versions/2")), http.StatusOK, "version 2"), "version")
		if got := anchorsIn(version["body"]); len(got) != 0 {
			t.Errorf("version 2 holds marks %v", got)
		}

		// A draft holds the marks the editor loaded, and a mark it invents is dropped.
		body := marked(t, page["body"].(map[string]any), 1, "soon", uuid.NewString())
		want(t, owner.put(t, pagePath(plan, "/draft"), map[string]any{"title": "Plan", "body": body, "baseVersion": 2}), http.StatusOK, "the owner drafts")
		cmp := obj(t, want(t, owner.get(t, pagePath(plan, "/compare?from=2&to=draft")), http.StatusOK, "compare the draft"), "comparison")
		for _, each := range cmp["blocks"].([]any) {
			if change := each.(map[string]any)["change"]; change != "equal" {
				t.Errorf("a draft differing by marks alone compares as %v", change)
			}
		}
		want(t, owner.post(t, pagePath(plan, "/publish"), map[string]any{}), http.StatusOK, "the owner publishes the draft")
		page = readPage(t, carl, plan)
		if got := anchorsIn(page["body"]); got[friday] != "ship on Friday" || got[notes] != "Notes" || len(got) != 2 {
			t.Errorf("after the draft the marks are %v", got)
		}

		// The passage rewritten: its thread is detached, with its quote and comments.
		want(t, owner.patch(t, pagePath(plan), map[string]any{"version": 3, "body": textDoc("Now we ship on Monday after the review.", "Notes follow here, soon.")}), http.StatusOK, "the owner rewrites the passage")
		page = readPage(t, carl, plan)
		if got := anchorsIn(page["body"]); len(got) != 1 || got[notes] != "Notes" {
			t.Errorf("after the rewrite the marks are %v", got)
		}
		th := threadByID(t, carl, plan, friday)
		if anchorState(th) != "detached" || th["anchor"].(map[string]any)["quote"] != "ship on Friday" || len(commentsIn(th)) != 3 {
			t.Errorf("the rewritten passage's thread is %v", th)
		}
		if _, inline, detached := counts(page); inline != 1 || detached != 1 {
			t.Errorf("the page counts %d inline and %d detached", inline, detached)
		}

		// Restoring the old words does not bring a detached thread back.
		want(t, owner.post(t, pagePath(plan, "/versions/1/restore"), map[string]any{"baseVersion": 4}), http.StatusOK, "restore version 1")
		page = readPage(t, carl, plan)
		if got := anchorsIn(page["body"]); len(got) != 1 || got[notes] != "Notes" {
			t.Errorf("after the restore the marks are %v", got)
		}
		if anchorState(threadByID(t, carl, plan, friday)) != "detached" {
			t.Error("a restore anchored a detached thread again")
		}
	})

	t.Run("a copy takes no passages with it", func(t *testing.T) {
		copied := obj(t, want(t, owner.post(t, pagePath(plan, "/copy"), map[string]any{"parentId": docs.homeID}), http.StatusCreated, "copy Plan"), "page")
		if got := anchorsIn(copied["body"]); len(got) != 0 {
			t.Errorf("the copy holds marks %v", got)
		}
	})
}

// Straight through SQL as stator_app: a commenter may add the mark of their
// own new thread and nothing else, a thread's resolution and passage follow
// the same rules as the service, and no version ever holds a mark.
func TestInlineCommentsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "inline-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)

	docs := newTree(t, owner, "IRAW", "Inline raw")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("We ship on Friday.")})
	want(t, owner.put(t, "/api/v1/spaces/IRAW/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"view", "addComments"}},
		map[string]any{"subject": user(benID), "permissions": []any{"view", "addComments"}},
	}}), http.StatusOK, "ann and ben comment")
	h.settle(t)
	r, anns := startInline(t, ann, plan, 0, "Friday", "Why Friday?")
	want(t, r, http.StatusCreated, "ann's thread")
	below := startThread(t, ann, plan, "Below the page.")["id"].(string)
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	var body map[string]any
	var raw []byte
	if err := h.super.QueryRow(ctx, `SELECT body FROM page WHERE id = $1`, plan).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &body)
	asJSON := func(v any) string { out, _ := json.Marshal(v); return string(out) }
	setBody := `UPDATE page SET body = $2::jsonb WHERE id = $1`
	newThread := `INSERT INTO comment_thread (org_id, id, page_id, kind, created_by, quote) VALUES ($1, $2, $3, 'inline', $4, 'ship')`

	t.Run("a commenter adds only the mark of a thread of their own", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		carls := uuid.NewString()
		denied(t, conn, "a thread without addComments", newThread, home.org, carls, plan, carlID)
		denied(t, conn, "a mark without addComments", setBody, plan, asJSON(marked(t, body, 0, "ship", anns)))

		actAs(t, conn, home.org, annID)
		invented := uuid.NewString()
		denied(t, conn, "a mark naming no thread", setBody, plan, asJSON(marked(t, body, 0, "ship", invented)))
		bens := uuid.NewString()
		actAs(t, conn, home.org, benID)
		if _, err := conn.Exec(ctx, newThread, home.org, bens, plan, benID); err != nil {
			t.Fatalf("ben may not start a thread: %v", err)
		}
		actAs(t, conn, home.org, annID)
		denied(t, conn, "marking ben's thread", setBody, plan, asJSON(marked(t, body, 0, "ship", bens)))
		denied(t, conn, "a thread as somebody else", newThread, home.org, uuid.NewString(), plan, benID)
		denied(t, conn, "a thread resolved from the start",
			`INSERT INTO comment_thread (org_id, id, page_id, kind, created_by, quote, resolved_at, resolved_by) VALUES ($1, $2, $3, 'inline', $4, 'x', now(), $4)`,
			home.org, uuid.NewString(), plan, annID)
		mine := uuid.NewString()
		if _, err := conn.Exec(ctx, newThread, home.org, mine, plan, annID); err != nil {
			t.Fatalf("ann may not start a thread: %v", err)
		}
		reworded := marked(t, body, 0, "ship", mine)
		reworded["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] = "They "
		denied(t, conn, "a mark and a changed word", setBody, plan, asJSON(reworded))
		denied(t, conn, "removing ann's own earlier mark", setBody, plan, asJSON(textDoc("We ship on Friday.")))
		if _, err := conn.Exec(ctx, setBody, plan, asJSON(marked(t, body, 0, "ship", mine))); err != nil {
			t.Errorf("ann may not mark her own thread's passage: %v", err)
		}
		denied(t, conn, "a title change", `UPDATE page SET title = 'Mine' WHERE id = $1`, plan)
	})

	t.Run("a thread is resolved in one's own name and let go by publishing alone", func(t *testing.T) {
		resolve := `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "resolving without addComments", resolve, anns, carlID)
		actAs(t, conn, home.org, annID)
		denied(t, conn, "resolving in ben's name", resolve, anns, benID)
		denied(t, conn, "detaching without edit", `UPDATE comment_thread SET detached_at = now() WHERE id = $1`, anns)
		denied(t, conn, "rewriting the quote", `UPDATE comment_thread SET quote = 'x' WHERE id = $1`, anns)
		if _, err := conn.Exec(ctx, resolve, anns, annID); err != nil {
			t.Errorf("ann may not resolve: %v", err)
		}
		if _, err := conn.Exec(ctx, `UPDATE comment_thread SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, anns); err != nil {
			t.Errorf("ann may not reopen: %v", err)
		}
		actAs(t, conn, home.org, home.user)
		if _, err := conn.Exec(ctx, `UPDATE comment_thread SET detached_at = now() WHERE id = $1`, anns); err != nil {
			t.Errorf("the owner may not let a passage go: %v", err)
		}
		denied(t, conn, "anchoring a detached thread again", `UPDATE comment_thread SET detached_at = NULL WHERE id = $1`, anns)
		denied(t, conn, "resolving a thread below the page", resolve, below, home.user)
	})

	t.Run("no version holds a mark, however it is written", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		if _, err := conn.Exec(ctx, `
			INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
			SELECT org_id, id, version + 1, title, body, $2 FROM page WHERE id = $1`, plan, home.user); err != nil {
			t.Fatalf("the owner may not write a version: %v", err)
		}
		var marks int
		if err := h.super.QueryRow(ctx, `
			SELECT count(*) FROM page_version v, jsonb_path_query(v.body, 'lax $.**.marks[*] ? (@.type == "inlineComment")')
			WHERE v.page_id = $1`, plan).Scan(&marks); err != nil {
			t.Fatal(err)
		}
		if marks != 0 {
			t.Errorf("the versions hold %d marks", marks)
		}
	})
}

// Resolving and reopening tell everybody who wrote in the thread, quoting
// its passage, and nobody else.
func TestInlineCommentNotifications(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	home := h.makeMember(t, "inline-notify")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Asker"), h.namedPerson(t, home.org, "Ben Answerer"), h.namedPerson(t, home.org, "Carl Closer")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)

	docs := newTree(t, owner, "INOT", "Inline notes")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("We ship on Friday.")})
	h.settle(t)
	r, thread := startInline(t, ann, plan, 0, "ship on Friday", "Why Friday?")
	want(t, r, http.StatusCreated, "ann starts a thread")
	want(t, reply(t, ben, thread, "Because."), http.StatusCreated, "ben replies")
	h.drained(t, home.org)
	for name, c := range map[string]*client{"ann": ann, "ben": ben, "carl": carl, "the owner": owner} {
		want(t, c.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, name+" starts with nothing unread")
	}

	for _, act := range []string{"resolve", "reopen"} {
		t.Run(act+" tells the thread's writers", func(t *testing.T) {
			want(t, carl.post(t, "/api/v1/comments/"+thread+"/"+act, nil), http.StatusOK, "carl "+act+"s")
			h.drained(t, home.org)
			for name, c := range map[string]*client{"ann": ann, "ben": ben} {
				got := notificationsOf(t, c, "?unread=true")
				if len(got) != 1 || got[0]["kind"] != "resolved" || got[0]["threadId"] != thread || got[0]["excerpt"] != "ship on Friday" ||
					got[0]["actorName"] != "Carl Closer" || got[0]["commentId"] != nil {
					t.Errorf("%s was told %v", name, got)
				}
				want(t, c.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, name+" reads it")
			}
			for name, c := range map[string]*client{"carl, the actor": carl, "the owner, who watches": owner} {
				if got := notificationsOf(t, c, "?unread=true"); len(got) != 0 {
					t.Errorf("%s was told %v", name, got)
				}
			}
		})
	}

	t.Run("doing it twice tells nobody again", func(t *testing.T) {
		want(t, carl.post(t, "/api/v1/comments/"+thread+"/reopen", nil), http.StatusOK, "carl reopens an open thread")
		h.drained(t, home.org)
		if got := notificationsOf(t, ann, "?unread=true"); len(got) != 0 {
			t.Errorf("ann was told %v", got)
		}
	})
}
