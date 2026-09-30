//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/notify"
)

func commentDoc(text string) map[string]any { return textDoc(text) }

// commenters is a space's grant to everyone without delete, which a new
// space gives everyone and which would let anybody delete anybody's comment.
var commenters = map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments"}}

// startThread posts a new thread below a page and answers it.
func startThread(t *testing.T, c *client, page, text string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.post(t, pagePath(page, "/comments"), map[string]any{"body": commentDoc(text)}), http.StatusCreated, "start a thread"), "thread")
}

func reply(t *testing.T, c *client, commentID, text string) response {
	t.Helper()
	return c.post(t, "/api/v1/comments/"+commentID+"/replies", map[string]any{"body": commentDoc(text)})
}

func threadsOf(t *testing.T, c *client, page, query string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, want(t, c.get(t, pagePath(page, "/comments"+query)), http.StatusOK, "list the threads"), "threads") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func commentsIn(thread map[string]any) []map[string]any {
	var out []map[string]any
	for _, each := range thread["comments"].([]any) {
		out = append(out, each.(map[string]any))
	}
	return out
}

// words is the first text of a comment's body, or "" once it is deleted.
func words(c map[string]any) string {
	body, ok := c["body"].(map[string]any)
	if !ok {
		return ""
	}
	return body["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
}

func commentCount(t *testing.T, c *client, page string) int {
	t.Helper()
	return number(obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["comments"].(map[string]any)["page"])
}

// Threads below a page: started, answered, edited by their authors only,
// deleted softly, counted on the page, and found by search, among what the
// caller may view.
func TestCommentsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "comments")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Commenter"), h.namedPerson(t, home.org, "Ben Commenter"), h.namedPerson(t, home.org, "Carl Outsider")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)

	docs := newTree(t, owner, "TALK", "Talk")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("The plan.")})
	secret := docs.add(docs.homeID, "Secret", map[string]any{"body": textDoc("Hidden.")})
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "restrict Secret to ann")
	want(t, owner.put(t, "/api/v1/spaces/TALK/permissions", map[string]any{"grants": []any{commenters}}), http.StatusOK, "nobody but the owner deletes in TALK")
	h.settle(t)

	var first map[string]any
	t.Run("a thread starts, takes replies at its end, and is counted", func(t *testing.T) {
		first = startThread(t, ann, plan, "Should we ship Friday?")
		if first["kind"] != "page" || first["anchor"] != nil || first["resolved"] != false || first["pageId"] != plan {
			t.Errorf("the thread is %v", first)
		}
		opening := commentsIn(first)[0]
		if opening["id"] != first["id"] || opening["threadId"] != first["id"] || opening["authorId"] != annID.String() ||
			opening["authorName"] != "Ann Commenter" || opening["deleted"] != false || opening["editedAt"] != nil {
			t.Errorf("the first comment is %v", opening)
		}
		if can := opening["can"].(map[string]any); can["edit"] != true || can["delete"] != true {
			t.Errorf("ann may %v her own comment", can)
		}
		r := obj(t, want(t, reply(t, ben, first["id"].(string), "Monday is safer."), http.StatusCreated, "ben replies"))
		made := r["comment"].(map[string]any)
		if words(made) != "Monday is safer." || made["threadId"] != first["id"] {
			t.Errorf("the reply is %v", made)
		}
		want(t, reply(t, ann, made["id"].(string), "Monday it is."), http.StatusCreated, "ann answers ben's reply")
		second := startThread(t, ben, plan, "Who writes the notes?")

		threads := threadsOf(t, carl, plan, "")
		if len(threads) != 2 || threads[0]["id"] != first["id"] || threads[1]["id"] != second["id"] {
			t.Fatalf("the page lists %v", threads)
		}
		got := commentsIn(threads[0])
		if len(got) != 3 || words(got[0]) != "Should we ship Friday?" || words(got[1]) != "Monday is safer." || words(got[2]) != "Monday it is." {
			t.Errorf("the first thread reads %v", got)
		}
		if can := got[0]["can"].(map[string]any); can["edit"] != false || can["delete"] != false {
			t.Errorf("carl may %v ann's comment", can)
		}
		if threads[0]["can"].(map[string]any)["reply"] != true {
			t.Error("carl may not reply")
		}
		if n := commentCount(t, carl, plan); n != 4 {
			t.Errorf("the page counts %d comments, want 4", n)
		}
		if got := threadsOf(t, carl, plan, "?kind=inline"); len(got) != 0 {
			t.Errorf("the page has inline threads %v", got)
		}
		if got := threadsOf(t, carl, plan, "?kind=page"); len(got) != 2 {
			t.Errorf("the page lists %d threads below it", len(got))
		}
		th := obj(t, want(t, carl.get(t, "/api/v1/comments/"+got[0]["id"].(string)), http.StatusOK, "the thread of its first comment"), "thread")
		if th["id"] != first["id"] || len(commentsIn(th)) != 3 {
			t.Errorf("the thread by its first comment is %v", th)
		}
	})

	t.Run("what is not a comment, or says nothing, is refused on body", func(t *testing.T) {
		for name, body := range map[string]any{
			"nothing":   nil,
			"empty":     map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}},
			"a table":   map[string]any{"type": "doc", "content": []any{map[string]any{"type": "table", "content": []any{}}}},
			"too long":  textDoc(strings.Repeat("a", 70<<10)),
			"a heading": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "heading", "attrs": map[string]any{"level": 9}}}},
		} {
			r := ann.post(t, pagePath(plan, "/comments"), map[string]any{"body": body})
			if r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusRequestEntityTooLarge {
				t.Errorf("%s answered %d", name, r.Status)
				continue
			}
			if r.Status == http.StatusUnprocessableEntity {
				if fields, _ := obj(t, r, "error")["fields"].(map[string]any); fields["body"] == nil {
					t.Errorf("%s is not refused on body: %v", name, r.Body)
				}
			}
		}
		want(t, ann.get(t, pagePath(plan, "/comments?kind=side")), http.StatusUnprocessableEntity, "an unknown kind")
		want(t, reply(t, ann, first["id"].(string), ""), http.StatusUnprocessableEntity, "an empty reply")
	})

	t.Run("an unpublished page takes no comments", func(t *testing.T) {
		draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft"}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
		r := want(t, ann.post(t, pagePath(draft, "/comments"), map[string]any{"body": commentDoc("Early word.")}), http.StatusConflict, "comment on it")
		if code := errorCode(t, r); code != "unpublished" {
			t.Errorf("the refusal is %s", code)
		}
		want(t, ben.post(t, pagePath(draft, "/comments"), map[string]any{"body": commentDoc("Can I?")}), http.StatusNotFound, "somebody who cannot see it")
	})

	t.Run("only the author edits, while they may still comment", func(t *testing.T) {
		id := first["id"].(string)
		r := obj(t, want(t, ann.patch(t, "/api/v1/comments/"+id, map[string]any{"body": commentDoc("Should we ship Monday?")}), http.StatusOK, "ann edits"), "comment")
		if words(r) != "Should we ship Monday?" || r["editedAt"] == nil {
			t.Errorf("the edited comment is %v", r)
		}
		for name, c := range map[string]*client{"ben": ben, "the owner": owner} {
			r := want(t, c.patch(t, "/api/v1/comments/"+id, map[string]any{"body": commentDoc("Rewritten.")}), http.StatusForbidden, name+" edits ann's comment")
			if code := errorCode(t, r); code != "forbidden" {
				t.Errorf("%s is refused with %s", name, code)
			}
		}
		want(t, ann.patch(t, "/api/v1/comments/"+uuid.NewString(), map[string]any{"body": commentDoc("x")}), http.StatusNotFound, "no such comment")
	})

	t.Run("a reader without addComments reads but cannot write", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/TALK/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
			map[string]any{"subject": user(annID), "permissions": []any{"view", "addComments"}},
		}}), http.StatusOK, "TALK is read only for everyone but ann")
		h.settle(t)
		threads := threadsOf(t, ben, plan, "")
		if threads[0]["can"].(map[string]any)["reply"] != false {
			t.Error("ben may still reply")
		}
		for _, c := range commentsIn(threads[1]) {
			if can := c["can"].(map[string]any); can["edit"] != false || can["delete"] != true {
				t.Errorf("ben may %v his own comment without addComments", can)
			}
		}
		r := want(t, ben.post(t, pagePath(plan, "/comments"), map[string]any{"body": commentDoc("Me too.")}), http.StatusForbidden, "ben comments")
		if code := errorCode(t, r); code != "forbidden" {
			t.Errorf("the refusal is %s", code)
		}
		want(t, reply(t, ben, first["id"].(string), "Me too."), http.StatusForbidden, "ben replies")
		want(t, ben.patch(t, "/api/v1/comments/"+commentsIn(threads[1])[0]["id"].(string), map[string]any{"body": commentDoc("Edited.")}), http.StatusForbidden, "ben edits his own")
		if page := obj(t, want(t, ben.get(t, pagePath(plan)), http.StatusOK, "the page"), "page"); page["can"].(map[string]any)["comment"] != false {
			t.Errorf("the page offers ben %v", page["can"])
		}
		want(t, owner.put(t, "/api/v1/spaces/TALK/permissions", map[string]any{"grants": []any{
			commenters,
		}}), http.StatusOK, "TALK is open again")
		h.settle(t)
	})

	t.Run("a deleted comment stays as a placeholder while its thread lives", func(t *testing.T) {
		threads := threadsOf(t, ann, plan, "")
		opening := commentsIn(threads[0])[0]["id"].(string)
		want(t, ben.delete(t, "/api/v1/comments/"+opening), http.StatusForbidden, "ben deletes ann's comment")
		want(t, ann.delete(t, "/api/v1/comments/"+opening), http.StatusNoContent, "ann deletes her own")
		want(t, ann.delete(t, "/api/v1/comments/"+opening), http.StatusNoContent, "again, no change")
		got := commentsIn(threadsOf(t, carl, plan, "")[0])
		if got[0]["deleted"] != true || got[0]["body"] != nil || got[0]["authorName"] != "Ann Commenter" {
			t.Errorf("the deleted comment reads %v", got[0])
		}
		if can := got[0]["can"].(map[string]any); can["edit"] != false || can["delete"] != false {
			t.Errorf("a deleted comment offers %v", can)
		}
		want(t, ann.patch(t, "/api/v1/comments/"+opening, map[string]any{"body": commentDoc("Back.")}), http.StatusNotFound, "editing a deleted comment")
		if n := commentCount(t, carl, plan); n != 3 {
			t.Errorf("the page counts %d comments after a delete, want 3", n)
		}
		var body *string
		if err := h.super.QueryRow(context.Background(), `SELECT body::text FROM comment WHERE id = $1`, opening).Scan(&body); err != nil || body != nil {
			t.Errorf("the words are still stored: %v %v", body, err)
		}
		want(t, reply(t, carl, opening, "Still here."), http.StatusCreated, "carl replies through the placeholder")
	})

	t.Run("a space deleter deletes anybody's, which is audited; a thread wholly deleted goes", func(t *testing.T) {
		threads := threadsOf(t, owner, plan, "")
		second := threads[1]
		secondID := second["id"].(string)
		want(t, owner.delete(t, "/api/v1/comments/"+secondID), http.StatusNoContent, "the owner deletes ben's thread")
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'comment.deleted' AND target_id = $2 AND actor_user_id = $3`, home.org, secondID, home.user); n != 1 {
			t.Errorf("the moderator's delete was audited %d times", n)
		}
		if got := threadsOf(t, carl, plan, ""); len(got) != 1 || got[0]["id"] == secondID {
			t.Errorf("a wholly deleted thread is listed: %v", got)
		}
		want(t, carl.get(t, "/api/v1/comments/"+secondID), http.StatusNotFound, "the deleted thread by its comment")
		want(t, reply(t, carl, secondID, "Hello?"), http.StatusNotFound, "a reply to a deleted thread")
		mine := commentsIn(threads[0])
		want(t, carl.delete(t, "/api/v1/comments/"+mine[len(mine)-1]["id"].(string)), http.StatusNoContent, "carl deletes his own")
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'comment.deleted' AND actor_user_id = $2`, home.org, carlID); n != 0 {
			t.Errorf("deleting one's own was audited %d times", n)
		}
	})

	t.Run("a restricted page's comments do not leak", func(t *testing.T) {
		hidden := startThread(t, ann, secret, "Only for us.")
		id := hidden["id"].(string)
		want(t, carl.get(t, pagePath(secret, "/comments")), http.StatusNotFound, "carl lists them")
		want(t, carl.get(t, "/api/v1/comments/"+id), http.StatusNotFound, "carl reads the thread")
		want(t, reply(t, carl, id, "Let me in."), http.StatusNotFound, "carl replies")
		want(t, carl.patch(t, "/api/v1/comments/"+id, map[string]any{"body": commentDoc("x")}), http.StatusNotFound, "carl edits")
		want(t, carl.delete(t, "/api/v1/comments/"+id), http.StatusNotFound, "carl deletes")
		want(t, carl.post(t, pagePath(secret, "/comments"), map[string]any{"body": commentDoc("x")}), http.StatusNotFound, "carl comments")
		h.settle(t)
		if got := hitTypes(t, carl, "/api/v1/search?q=only&type=comment"); len(got) != 0 {
			t.Errorf("carl finds %v", got)
		}
		if got := hitTypes(t, ann, "/api/v1/search?q=only&type=comment"); len(got) != 1 {
			t.Errorf("ann finds %v", got)
		}
	})

	t.Run("comments are found by their words, titled by their page", func(t *testing.T) {
		r := want(t, carl.get(t, "/api/v1/search?q=monday&type=comment"), http.StatusOK, "search the comments")
		if hits := list(t, r, "hits"); len(hits) != 2 {
			t.Fatalf("monday finds %d comments, want ben's and ann's reply but not her deleted question: %v", len(hits), hits)
		}
		r = want(t, carl.get(t, "/api/v1/search?q=monday&type=comment&author="+benID.String()), http.StatusOK, "search ben's comments")
		hits := list(t, r, "hits")
		if len(hits) != 1 {
			t.Fatalf("ben's comments with monday are %v", hits)
		}
		hit := hits[0].(map[string]any)
		if hit["type"] != "comment" || hit["commentId"] == nil || hit["page"].(map[string]any)["id"] != plan || hit["updatedByName"] != "Ben Commenter" {
			t.Errorf("the hit is %v", hit)
		}
		if got := hitTypes(t, carl, "/api/v1/search?q=monday&type=page"); len(got) != 0 {
			t.Errorf("pages with monday are %v", got)
		}
	})

	t.Run("a trashed page's comments are gone with it", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(plan)), http.StatusNoContent, "trash the page")
		want(t, carl.get(t, pagePath(plan, "/comments")), http.StatusNotFound, "its comments")
		want(t, carl.get(t, "/api/v1/comments/"+first["id"].(string)), http.StatusNotFound, "a thread on it")
	})
}

func hitTypes(t *testing.T, c *client, path string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, want(t, c.get(t, path), http.StatusOK, path), "hits") {
		out = append(out, each.(map[string]any)["type"].(string))
	}
	return out
}

// Straight through SQL as stator_app, the database holds comments to the same
// rules: read with the page, written in one's own name where one may comment,
// changed by the author, deleted by the author or the space's deleter.
func TestCommentsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "comment-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)

	docs := newTree(t, owner, "CRAW", "Raw")
	open := docs.add(docs.homeID, "Open")
	secret := docs.add(docs.homeID, "Secret")
	draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft"}), http.StatusCreated, "ann's draft page"), "page")["id"].(string)
	anns := startThread(t, ann, open, "Ann's words.")["id"].(string)
	bens := startThread(t, ben, open, "Ben's words.")["id"].(string)
	hidden := startThread(t, ann, secret, "Hidden words.")["id"].(string)
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "restrict Secret")
	want(t, owner.put(t, "/api/v1/spaces/CRAW/permissions", map[string]any{"grants": []any{commenters}}), http.StatusOK, "nobody but the owner deletes in CRAW")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	insert := `INSERT INTO comment (org_id, id, thread_id, page_id, author_id, body) VALUES ($1, $2, $3, $4, $5, '{"type":"doc"}')`

	t.Run("somebody off a view list reads none of its comments", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		for table, column := range map[string]string{"comment": "page_id", "comment_thread": "page_id"} {
			if n := count(`SELECT count(*) FROM `+table+` WHERE `+column+` = $1`, secret); n != 0 {
				t.Errorf("carl reads %d rows of %s on Secret", n, table)
			}
		}
		if n := count(`SELECT count(*) FROM comment WHERE page_id = $1`, open); n != 2 {
			t.Errorf("carl reads %d comments on Open, want 2", n)
		}
		untouched(t, conn, "deleting a hidden comment", `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, hidden, carlID)
		denied(t, conn, "replying on a hidden page", insert, home.org, uuid.New(), hidden, secret, carlID)
		denied(t, conn, "a thread on a hidden page", `INSERT INTO comment_thread (org_id, page_id, kind, created_by) VALUES ($1, $2, 'page', $3)`, home.org, secret, carlID)
	})

	t.Run("nobody writes in somebody else's name", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "a reply as ann", insert, home.org, uuid.New(), anns, open, annID)
		denied(t, conn, "a thread as ann", `INSERT INTO comment_thread (org_id, page_id, kind, created_by) VALUES ($1, $2, 'page', $3)`, home.org, open, annID)
		denied(t, conn, "rewriting ann's words", `UPDATE comment SET body = '{"type":"doc"}', edited_at = now() WHERE id = $1`, anns)
		denied(t, conn, "deleting ann's comment", `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, anns, carlID)
		denied(t, conn, "taking ann's comment as his", `UPDATE comment SET author_id = $2 WHERE id = $1`, anns, carlID)
		denied(t, conn, "deleting outright", `DELETE FROM comment WHERE id = $1`, anns)
		denied(t, conn, "a comment on an unpublished page", insert, home.org, uuid.New(), anns, draft, carlID)
		if _, err := conn.Exec(ctx, insert, home.org, uuid.New(), anns, open, carlID); err != nil {
			t.Errorf("carl may not reply in his own name: %v", err)
		}
	})

	t.Run("the author changes and deletes their own, and says who deleted", func(t *testing.T) {
		actAs(t, conn, home.org, benID)
		if _, err := conn.Exec(ctx, `UPDATE comment SET body = '{"type":"doc","content":[]}', edited_at = now() WHERE id = $1`, bens); err != nil {
			t.Errorf("ben may not edit his own: %v", err)
		}
		denied(t, conn, "deleting in ann's name", `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, bens, annID)
		refused(t, conn, "deleting and keeping the words", `UPDATE comment SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, bens, benID)
		if _, err := conn.Exec(ctx, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, bens, benID); err != nil {
			t.Errorf("ben may not delete his own: %v", err)
		}
		denied(t, conn, "bringing a deleted comment back", `UPDATE comment SET body = '{"type":"doc"}', deleted_at = NULL, deleted_by = NULL WHERE id = $1`, bens)
	})

	t.Run("without addComments nothing is written, and without delete nobody else's goes", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/CRAW/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "CRAW is read only")
		h.settle(t)
		actAs(t, conn, home.org, annID)
		denied(t, conn, "a reply without addComments", insert, home.org, uuid.New(), anns, open, annID)
		denied(t, conn, "editing her own without addComments", `UPDATE comment SET body = '{"type":"doc"}', edited_at = now() WHERE id = $1`, anns)
		if _, err := conn.Exec(ctx, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, anns, annID); err != nil {
			t.Errorf("ann may not delete her own while she may view the page: %v", err)
		}
		actAs(t, conn, home.org, home.user)
		var others uuid.UUID
		if err := conn.QueryRow(ctx, `SELECT id FROM comment WHERE page_id = $1 AND author_id = $2`, open, carlID).Scan(&others); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, others, home.user); err != nil {
			t.Errorf("the owner may not delete carl's comment: %v", err)
		}
	})

	t.Run("notifications about a comment go with it, and are not even written after", func(t *testing.T) {
		var n int
		err := h.super.QueryRow(ctx, `
			WITH made AS (
				INSERT INTO notification (org_id, user_id, event_id, kind, page_id, thread_id, comment_id)
				VALUES ($1, $2, $3, 'commented', $4, $5, $5) RETURNING 1)
			SELECT count(*) FROM made`, home.org, carlID, uuid.New(), open, bens).Scan(&n)
		if err != nil || n != 0 {
			t.Errorf("a notification about a deleted comment was written: %d %v", n, err)
		}
	})
}

// The kinds of comment notification reach who they should, once each, and
// nobody who may not view the page; deleting the comment takes them back.
func TestCommentNotifications(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	home := h.makeMember(t, "comment-notify")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID, danID := h.namedPerson(t, home.org, "Ann Talker"), h.namedPerson(t, home.org, "Ben Talker"), h.namedPerson(t, home.org, "Carl Watcher"), h.namedPerson(t, home.org, "Dan Outsider")
	ann, ben, carl, dan := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug), api.as(t, danID, home.org, slug)

	docs := newTree(t, owner, "CNOT", "Comment notes")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("The plan.")})
	want(t, watchPage(t, carl, plan, false), http.StatusOK, "carl watches Plan")
	want(t, watchPage(t, dan, plan, false), http.StatusOK, "dan watches Plan")
	want(t, restrict(t, owner, plan, []any{user(home.user), user(annID), user(benID), user(carlID)}, nil), http.StatusOK, "Plan is not for dan")
	h.drained(t, home.org)
	for name, c := range map[string]*client{"ann": ann, "ben": ben, "carl": carl, "dan": dan, "the owner": owner} {
		want(t, c.post(t, "/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, name+" starts with nothing unread")
	}

	thread := startThread(t, ann, plan, "Friday or Monday?")
	h.drained(t, home.org)
	t.Run("a new thread tells the page's watchers who may view it", func(t *testing.T) {
		got := notificationsOf(t, carl, "?unread=true")
		if len(got) != 1 || got[0]["kind"] != "commented" || got[0]["threadId"] != thread["id"] || got[0]["commentId"] != thread["id"] ||
			got[0]["excerpt"] != "Friday or Monday?" || got[0]["actorName"] != "Ann Talker" {
			t.Errorf("carl was told %v", got)
		}
		for name, c := range map[string]*client{"dan, who may not view it": dan, "ann, the actor": ann, "ben": ben} {
			if got := notificationsOf(t, c, "?unread=true"); len(got) != 0 {
				t.Errorf("%s was told %v", name, got)
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE org_id = $1 AND user_id = $2`, home.org, danID); n != 0 {
			t.Errorf("dan has %d rows about a page he may not view", n)
		}
	})

	var benReply string
	t.Run("a reply tells everybody who wrote in the thread, and watchers once", func(t *testing.T) {
		benReply = obj(t, want(t, reply(t, ben, thread["id"].(string), "Monday."), http.StatusCreated, "ben replies"), "comment")["id"].(string)
		h.drained(t, home.org)
		got := notificationsOf(t, ann, "?unread=true")
		if len(got) != 1 || got[0]["kind"] != "replied" || got[0]["commentId"] != benReply || got[0]["threadId"] != thread["id"] || got[0]["excerpt"] != "Monday." {
			t.Errorf("ann was told %v", got)
		}
		if got := notificationsOf(t, carl, "?unread=true"); len(got) != 2 || got[0]["kind"] != "commented" || got[0]["commentId"] != benReply {
			t.Errorf("carl was told %v", got)
		}
		want(t, watchPage(t, ann, plan, false), http.StatusOK, "ann watches Plan too")
		want(t, reply(t, carl, thread["id"].(string), "Agreed."), http.StatusCreated, "carl replies")
		h.drained(t, home.org)
		got = notificationsOf(t, ann, "?unread=true")
		if len(got) != 2 || got[0]["kind"] != "replied" {
			t.Errorf("ann, who wrote in it and watches, was told %v", got)
		}
		if got := notificationsOf(t, ben, "?unread=true"); len(got) != 1 || got[0]["kind"] != "replied" {
			t.Errorf("ben was told %v", got)
		}
		if got := notificationsOf(t, dan, ""); len(got) != 0 {
			t.Errorf("dan was told %v", got)
		}
	})

	t.Run("deleting a comment takes back what people were told of it", func(t *testing.T) {
		want(t, ben.delete(t, "/api/v1/comments/"+benReply), http.StatusNoContent, "ben deletes his reply")
		h.settle(t)
		for name, c := range map[string]*client{"ann": ann, "carl": carl} {
			for _, n := range notificationsOf(t, c, "") {
				if n["commentId"] == benReply {
					t.Errorf("%s still has %v", name, n)
				}
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE org_id = $1 AND comment_id = $2`, home.org, benReply); n != 0 {
			t.Errorf("%d rows about the deleted reply remain", n)
		}
	})
}
