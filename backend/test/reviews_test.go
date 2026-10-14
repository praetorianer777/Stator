//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// Reviews and read confirmations (#313): a rule holds a space or a branch to
// a review, a draft goes out once its reviewers approve it, as its author's,
// and a page asks its readers to confirm they read it.

func reviewRule(people ...any) map[string]any {
	return map[string]any{"reviewers": people, "required": 1}
}

func reviewOf(t *testing.T, c *client, id string) (rule, pending map[string]any) {
	t.Helper()
	status := obj(t, want(t, c.get(t, pagePath(id)), http.StatusOK, "read the page"), "page")["review"].(map[string]any)
	rule, _ = status["rule"].(map[string]any)
	pending, _ = status["pending"].(map[string]any)
	return rule, pending
}

func submit(t *testing.T, c *client, id string, body map[string]any) response {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	return c.post(t, pagePath(id, "/reviews"), body)
}

func decide(t *testing.T, c *client, id, review, decision, comment string) response {
	t.Helper()
	return c.post(t, pagePath(id, "/reviews/", review, "/decisions"), map[string]any{"decision": decision, "comment": comment})
}

func refusedWith(t *testing.T, what string, got response, status int, code string) {
	t.Helper()
	if got.Status != status || (code != "" && errorCode(t, got) != code) {
		t.Errorf("%s answered %d %s, want %d %s", what, got.Status, got.Raw, status, code)
	}
}

func TestReviewsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api, _ := wordAPI(t, h)
	home := h.makeMember(t, "reviews")
	owner := api.as(t, home.user)
	annID, bobID := h.namedPerson(t, "Ann Author"), h.namedPerson(t, "Bob Reviewer")
	carlID, daveID := h.namedPerson(t, "Carl Checker"), h.namedPerson(t, "Dave Reader")
	ann, bob, carl, dave := api.as(t, annID), api.as(t, bobID), api.as(t, carlID), api.as(t, daveID)
	checkers := h.makeGroup(t, "Checkers", carlID)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	watch := page.NewScheduleWatch(h.cluster, discard(), time.Hour)

	docs := newTree(t, owner, "REV", "Reviewed")
	policies := docs.add(docs.homeID, "Policies")
	leave := docs.add(policies, "Leave")
	other := docs.add(docs.homeID, "Other")
	want(t, dave.put(t, pagePath(leave, "/watch"), map[string]any{}), http.StatusOK, "dave watches the leave policy")

	t.Run("rules are the space's administrators', of people and groups", func(t *testing.T) {
		rule := reviewRule(user(bobID), group(checkers))
		rule["required"] = 2
		refusedWith(t, "a member setting a branch's rule", ann.put(t, pagePath(policies, "/review-rule"), rule), http.StatusForbidden, "forbidden")
		refusedWith(t, "a member setting the space's rule", ann.put(t, "/api/v1/spaces/REV/review-rule", rule), http.StatusForbidden, "forbidden")
		refusedWith(t, "a member removing the space's rule", ann.delete(t, "/api/v1/spaces/REV/review-rule"), http.StatusForbidden, "forbidden")
		for what, bad := range map[string]map[string]any{
			"everyone as a reviewer":       reviewRule(everyone),
			"nobody named":                 reviewRule(),
			"no approval asked for":        {"reviewers": []any{user(bobID)}, "required": 0},
			"more approvals than people":   {"reviewers": []any{user(bobID), group(checkers)}, "required": 3},
			"more approvals than the most": {"reviewers": []any{user(bobID)}, "required": 11},
		} {
			refusedWith(t, what, owner.put(t, pagePath(policies, "/review-rule"), bad), http.StatusUnprocessableEntity, "validation_failed")
		}
		got := obj(t, want(t, owner.put(t, pagePath(policies, "/review-rule"), rule), http.StatusOK, "hold the policies to a review"), "rule")
		if got["page"].(map[string]any)["id"] != policies || number(got["required"]) != 2 || len(got["reviewers"].([]any)) != 2 || got["spaceKey"] != "REV" {
			t.Errorf("the rule reads %v", got)
		}
		rules := list(t, want(t, dave.get(t, "/api/v1/spaces/REV/review-rules"), http.StatusOK, "a reader lists the rules"), "rules")
		if len(rules) != 1 {
			t.Errorf("the space has %d rules", len(rules))
		}
		refusedWith(t, "the rules of no space", owner.get(t, "/api/v1/spaces/NOSUCH/review-rules"), http.StatusNotFound, "not_found")
		if r, _ := reviewOf(t, dave, leave); r == nil || r["id"] != got["id"] {
			t.Errorf("a page below the branch is held by %v", r)
		}
		if r, _ := reviewOf(t, dave, other); r != nil {
			t.Errorf("a page outside the branch is held by %v", r)
		}
	})

	t.Run("a held page goes out through its review alone", func(t *testing.T) {
		draftOn(t, ann, leave, "Leave", "Twenty-five days")
		refusedWith(t, "publishing the draft", ann.post(t, pagePath(leave, "/publish"), map[string]any{}), http.StatusConflict, "review_required")
		refusedWith(t, "publishing at once", ann.patch(t, pagePath(leave), map[string]any{"title": "Leave!", "version": 1}), http.StatusConflict, "review_required")
		refusedWith(t, "restoring a version", ann.post(t, pagePath(leave, "/versions/1/restore"), map[string]any{"baseVersion": 1}), http.StatusConflict, "review_required")
		refusedWith(t, "making a published page below", ann.post(t, "/api/v1/pages", map[string]any{"parentId": policies, "title": "Travel", "publish": true}),
			http.StatusConflict, "review_required")
		refusedWith(t, "importing Markdown below", owner.importFiles(t, http.MethodPost, pagePath(policies, "/import"), [2]string{"travel.md", "# Travel\n\nBy train."}),
			http.StatusConflict, "review_required")
		refusedWith(t, "importing Word below", owner.importFiles(t, http.MethodPost, pagePath(policies, "/import/docx"),
			[2]string{"travel.docx", string(wordDoc(t, "Travel", nil, wPara("By train.")))}), http.StatusConflict, "review_required")
		refusedWith(t, "copying a page below", owner.post(t, pagePath(other, "/copy"), map[string]any{"parentId": policies}), http.StatusConflict, "review_required")
		refusedWith(t, "making the page live", owner.put(t, pagePath(leave, "/mode"), map[string]any{"mode": "live"}), http.StatusConflict, "review_required")
		refusedWith(t, "submitting a page no rule holds", submit(t, owner, other, nil), http.StatusConflict, "no_review_needed")
		want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": policies, "title": "Travel"}), http.StatusCreated, "an unpublished page below is made")
		if v := number(obj(t, want(t, dave.get(t, pagePath(leave)), http.StatusOK, "read"), "page")["version"]); v != 1 {
			t.Errorf("the page went to version %d", v)
		}
	})

	var reviewID string
	t.Run("a submitted draft waits for its reviewers, who are told", func(t *testing.T) {
		refusedWith(t, "submitting without a draft", submit(t, dave, leave, nil), http.StatusConflict, "no_draft")
		got := obj(t, want(t, submit(t, ann, leave, map[string]any{"comment": "Longer leave", "notifyWatchers": true}), http.StatusCreated, "ann submits"), "review")
		reviewID = got["id"].(string)
		can := got["can"].(map[string]any)
		if got["state"] != "pending" || number(got["required"]) != 2 || len(got["reviewers"].([]any)) != 2 || got["authorName"] != "Ann Author" ||
			can["withdraw"] != true || can["decide"] != false || number(got["baseVersion"]) != 1 {
			t.Errorf("the review reads %v", got)
		}
		refusedWith(t, "a second review", submit(t, ann, leave, nil), http.StatusConflict, "review_pending")
		if _, p := reviewOf(t, bob, leave); p == nil || p["id"] != reviewID || p["can"].(map[string]any)["decide"] != true {
			t.Errorf("bob reads the pending review as %v", p)
		}
		if _, p := reviewOf(t, carl, leave); p == nil || p["can"].(map[string]any)["decide"] != true {
			t.Errorf("carl, by his group, reads the pending review as %v", p)
		}
		if _, p := reviewOf(t, dave, leave); p == nil || p["can"].(map[string]any)["decide"] != false {
			t.Errorf("dave reads the pending review as %v", p)
		}
		if _, p := reviewOf(t, owner, leave); p == nil || p["can"].(map[string]any)["skip"] != true {
			t.Errorf("the administrator reads the pending review as %v", p)
		}
		waiting := list(t, want(t, carl.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "carl's reviews waiting"), "reviews")
		if len(waiting) != 1 {
			t.Fatalf("carl has %d reviews waiting", len(waiting))
		}
		if w := waiting[0].(map[string]any); w["id"] != reviewID || w["spaceKey"] != "REV" || w["page"].(map[string]any)["id"] != leave ||
			w["page"].(map[string]any)["title"] != "Leave" || w["can"].(map[string]any)["decide"] != true || w["body"] != nil {
			t.Errorf("carl's waiting review reads %v", w)
		}
		for who, c := range map[string]*client{"the author": ann, "a reader": dave} {
			if n := len(list(t, want(t, c.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "reviews waiting"), "reviews")); n != 0 {
				t.Errorf("%s has %d reviews waiting", who, n)
			}
		}
		refusedWith(t, "nobody signed in asking what waits", api.anonymous().get(t, "/api/v1/reviews/waiting"), http.StatusUnauthorized, "unauthorized")
		read := obj(t, want(t, bob.get(t, pagePath(leave, "/reviews/", reviewID)), http.StatusOK, "bob reads the review"), "review")
		if !strings.Contains(mustJSON(t, read["body"]), "Twenty-five days") {
			t.Errorf("the review holds %v", read["body"])
		}
		refusedWith(t, "a review that is not there", bob.get(t, pagePath(leave, "/reviews/", uuid.NewString())), http.StatusNotFound, "not_found")
		if all := list(t, want(t, dave.get(t, pagePath(leave, "/reviews")), http.StatusOK, "list the reviews"), "reviews"); len(all) != 1 {
			t.Errorf("the page lists %d reviews", len(all))
		}
		refusedWith(t, "the reviews of no page", dave.get(t, pagePath(uuid.NewString(), "/reviews")), http.StatusNotFound, "not_found")
		cmp := obj(t, want(t, bob.get(t, pagePath(leave, "/compare?to=review:"+reviewID)), http.StatusOK, "compare the review"), "comparison")
		if cmp["to"].(map[string]any)["reviewId"] != reviewID || number(cmp["from"].(map[string]any)["number"]) != 1 {
			t.Errorf("the comparison reads %v %v", cmp["from"], cmp["to"])
		}
		refusedWith(t, "a side that names no review", bob.get(t, pagePath(leave, "/compare?to=review:nonsense")), http.StatusUnprocessableEntity, "validation_failed")

		h.drained(t)
		for who, c := range map[string]*client{"bob": bob, "carl": carl} {
			if n := notificationsOfKind(t, c, string(notify.KindReview)); n != 1 {
				t.Errorf("%s was asked %d times", who, n)
			}
		}
		for who, c := range map[string]*client{"ann": ann, "dave": dave} {
			if n := notificationsOfKind(t, c, string(notify.KindReview)); n != 0 {
				t.Errorf("%s was asked to review %d times", who, n)
			}
		}
	})

	t.Run("named reviewers decide, never the author, and the last approval publishes it as the author's", func(t *testing.T) {
		draftOn(t, ann, leave, "Leave", "Thirty days, still a draft")
		refusedWith(t, "the author deciding", decide(t, ann, leave, reviewID, "approve", ""), http.StatusForbidden, "forbidden")
		refusedWith(t, "somebody not named deciding", decide(t, dave, leave, reviewID, "approve", ""), http.StatusForbidden, "forbidden")
		refusedWith(t, "a comment too long", decide(t, bob, leave, reviewID, "changes", strings.Repeat("x", 501)), http.StatusUnprocessableEntity, "validation_failed")
		refusedWith(t, "changes without a word", decide(t, bob, leave, reviewID, "changes", " "), http.StatusUnprocessableEntity, "validation_failed")
		for range 2 {
			got := want(t, decide(t, bob, leave, reviewID, "approve", "Fine by me"), http.StatusOK, "bob approves")
			if r := obj(t, got, "review"); number(r["approvals"]) != 1 || r["state"] != "pending" || got.Body["version"] != nil {
				t.Errorf("after bob's approval the review reads %v and %v", r, got.Body["version"])
			}
		}
		got := want(t, decide(t, carl, leave, reviewID, "approve", ""), http.StatusOK, "carl approves")
		if r := obj(t, got, "review"); r["state"] != "published" || number(r["version"]) != 2 {
			t.Errorf("after the last approval the review reads %v", r)
		}
		if v := obj(t, got, "version"); number(v["number"]) != 2 || v["authorName"] != "Ann Author" || v["comment"] != "Longer leave" {
			t.Errorf("the version reads %v", v)
		}
		read := obj(t, want(t, dave.get(t, pagePath(leave)), http.StatusOK, "dave reads"), "page")
		if !strings.Contains(mustJSON(t, read["body"]), "Twenty-five days") || number(read["version"]) != 2 {
			t.Errorf("the page reads %v", read)
		}
		if mine := obj(t, want(t, ann.get(t, pagePath(leave)), http.StatusOK, "ann reads"), "page"); mine["draft"] == nil {
			t.Error("ann's draft, changed since she submitted it, was thrown away")
		}
		latest := list(t, want(t, dave.get(t, pagePath(leave, "/versions")), http.StatusOK, "history"), "versions")[0].(map[string]any)
		approval, _ := latest["review"].(map[string]any)
		if approval == nil || approval["reviewId"] != reviewID || mustJSON(t, approval["approvedBy"]) != `["Bob Reviewer","Carl Checker"]` {
			t.Errorf("the history says %v", latest["review"])
		}
		if older := list(t, want(t, dave.get(t, pagePath(leave, "/versions")), http.StatusOK, "history"), "versions")[1].(map[string]any); older["review"] != nil {
			t.Errorf("a version without a review says %v", older["review"])
		}
		if n := len(list(t, want(t, bob.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "bob's reviews waiting"), "reviews")); n != 0 {
			t.Errorf("a review that went out still waits for bob: %d", n)
		}
		refusedWith(t, "deciding on a review that went out", decide(t, bob, leave, reviewID, "approve", ""), http.StatusConflict, "not_pending")

		h.drained(t)
		if n := notificationsOfKind(t, ann, string(notify.KindReviewed)); n != 3 {
			t.Errorf("ann heard of %d decisions, want 3", n)
		}
		var heard []map[string]any
		for _, n := range notificationsOf(t, dave, "") {
			if n["kind"] == "published" {
				heard = append(heard, n)
			}
		}
		if len(heard) != 1 || heard[0]["actorName"] != "Ann Author" || number(heard[0]["version"]) != 2 {
			t.Errorf("the watcher heard %v", heard)
		}
	})

	t.Run("asking for changes ends a review, and the author submits again", func(t *testing.T) {
		draftOn(t, ann, leave, "Leave", "Thirty days")
		id := idOf(t, want(t, submit(t, ann, leave, nil), http.StatusCreated, "ann submits"), "review")
		got := obj(t, want(t, decide(t, carl, leave, id, "changes", "Say from when"), http.StatusOK, "carl sends it back"), "review")
		if got["state"] != "changes_requested" || got["decisions"].([]any)[0].(map[string]any)["comment"] != "Say from when" {
			t.Errorf("the review sent back reads %v", got)
		}
		if mine := obj(t, want(t, ann.get(t, pagePath(leave)), http.StatusOK, "ann reads"), "page"); mine["draft"] == nil {
			t.Error("a draft sent back is gone")
		}
		want(t, submit(t, ann, leave, nil), http.StatusCreated, "ann submits again")
	})

	t.Run("the author withdraws, and an administrator publishes without the approvals for a reason", func(t *testing.T) {
		_, p := reviewOf(t, ann, leave)
		id := p["id"].(string)
		refusedWith(t, "a reviewer withdrawing", bob.post(t, pagePath(leave, "/reviews/", id, "/withdraw"), nil), http.StatusForbidden, "forbidden")
		if got := obj(t, want(t, ann.post(t, pagePath(leave, "/reviews/", id, "/withdraw"), nil), http.StatusOK, "ann withdraws"), "review"); got["state"] != "withdrawn" {
			t.Errorf("the withdrawn review reads %v", got)
		}
		refusedWith(t, "withdrawing again", ann.post(t, pagePath(leave, "/reviews/", id, "/withdraw"), nil), http.StatusConflict, "not_pending")

		id = idOf(t, want(t, submit(t, ann, leave, map[string]any{"comment": "Urgent"}), http.StatusCreated, "ann submits again"), "review")
		refusedWith(t, "a member skipping", ann.post(t, pagePath(leave, "/reviews/", id, "/skip"), map[string]any{"reason": "Mine"}), http.StatusForbidden, "forbidden")
		refusedWith(t, "a skip without a reason", owner.post(t, pagePath(leave, "/reviews/", id, "/skip"), map[string]any{"reason": "  "}), http.StatusUnprocessableEntity, "validation_failed")
		got := want(t, owner.post(t, pagePath(leave, "/reviews/", id, "/skip"), map[string]any{"reason": "The law changed today"}), http.StatusOK, "the administrator skips")
		if r := obj(t, got, "review"); r["state"] != "skipped" || r["skipReason"] != "The law changed today" {
			t.Errorf("the skipped review reads %v", r)
		}
		v := obj(t, got, "version")
		if v["authorName"] != "Ann Author" || number(v["number"]) != 3 {
			t.Errorf("the skipped version reads %v", v)
		}
		if r, _ := v["review"].(map[string]any); r == nil || r["skipReason"] != "The law changed today" || r["skippedByName"] == "" {
			t.Errorf("the history says %v", v["review"])
		}
		if mine := obj(t, want(t, ann.get(t, pagePath(leave)), http.StatusOK, "ann reads"), "page"); mine["draft"] != nil {
			t.Error("ann's draft, published as she submitted it, stayed")
		}
		if data := h.recordedOnce(t, "review.skipped", &home.user, leave); !strings.Contains(data, "The law changed today") {
			t.Errorf("the skip's record reads %s", data)
		}
	})

	t.Run("a scheduled publish of a held page is submitted for review at its time", func(t *testing.T) {
		draftOn(t, ann, leave, "Leave", "Thirty-five days")
		want(t, schedule(t, ann, leave, inAnHour(), map[string]any{"comment": "Planned"}), http.StatusOK, "ann schedules")
		h.comeDue(t, leave, time.Second)
		h.taken(t, watch, leave)
		_, p := reviewOf(t, ann, leave)
		if p == nil || p["authorName"] != "Ann Author" || p["comment"] != "Planned" {
			t.Fatalf("at its time the page's review reads %v", p)
		}
		read := obj(t, want(t, ann.get(t, pagePath(leave)), http.StatusOK, "ann reads"), "page")
		if number(read["version"]) != 3 || read["schedule"] != nil || read["draft"] == nil {
			t.Errorf("after the submit the page reads version %v, schedule %v, draft %v", read["version"], read["schedule"], read["draft"])
		}
		want(t, ann.post(t, pagePath(leave, "/reviews/", p["id"].(string), "/withdraw"), nil), http.StatusOK, "ann withdraws")
	})

	t.Run("a tick publishes without a review, but not around one", func(t *testing.T) {
		chores := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Chores", "publish": true,
			"body": todoDoc("To do", todo{text: "Water the plants"})}), http.StatusCreated, "make a checklist"), "page")["id"].(string)
		want(t, owner.post(t, pagePath(chores, "/move"), map[string]any{"parentId": policies}), http.StatusOK, "move it below the policies")
		task := taskIDsOf(t, owner, chores)["Water the plants"]
		want(t, owner.patch(t, pagePath(chores, "/tasks/"+task), map[string]any{"done": true}), http.StatusOK, "tick it off")
		draftOn(t, ann, chores, "Chores", "Nothing")
		id := idOf(t, want(t, submit(t, ann, chores, nil), http.StatusCreated, "ann submits"), "review")
		refusedWith(t, "a tick while a review waits", owner.patch(t, pagePath(chores, "/tasks/"+task), map[string]any{"done": false}), http.StatusConflict, "review_pending")
		want(t, ann.post(t, pagePath(chores, "/reviews/", id, "/withdraw"), nil), http.StatusOK, "ann withdraws")
	})

	t.Run("a rule never covers a live page", func(t *testing.T) {
		notes := docs.add(other, "Notes")
		want(t, owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "live"}), http.StatusOK, "make notes live")
		refusedWith(t, "a rule over a live page", owner.put(t, pagePath(other, "/review-rule"), reviewRule(user(bobID))), http.StatusConflict, "live_pages")
		refusedWith(t, "a space rule over a live page", owner.put(t, "/api/v1/spaces/REV/review-rule", reviewRule(user(bobID))), http.StatusConflict, "live_pages")
		refusedWith(t, "a live page moved under a rule", owner.post(t, pagePath(notes, "/move"), map[string]any{"parentId": policies}), http.StatusConflict, "live_pages")
		want(t, owner.put(t, pagePath(notes, "/mode"), map[string]any{"mode": "draft"}), http.StatusOK, "notes back to drafts")
		want(t, owner.put(t, "/api/v1/spaces/REV/review-rule", reviewRule(user(bobID))), http.StatusOK, "hold the space")
		if r, _ := reviewOf(t, owner, other); r == nil || r["page"] != nil {
			t.Errorf("a page outside the branch is held by %v", r)
		}
		if r, _ := reviewOf(t, owner, leave); r == nil || r["page"] == nil {
			t.Errorf("the branch's rule gave way to %v", r)
		}
		refusedWith(t, "a post published at once", ann.post(t, "/api/v1/spaces/REV/posts", map[string]any{"title": "News", "publish": true}), http.StatusConflict, "review_required")
		want(t, owner.delete(t, "/api/v1/spaces/REV/review-rule"), http.StatusNoContent, "stop holding the space")
		want(t, owner.delete(t, pagePath(other, "/review-rule")), http.StatusNotFound, "a rule that is not there")
	})

	t.Run("a page asks its readers to confirm, and its editors see who did", func(t *testing.T) {
		refusedWith(t, "confirming a page that does not ask", dave.post(t, pagePath(other, "/read-confirmations"), nil), http.StatusConflict, "not_asked")
		want(t, restrict(t, owner, other, nil, []any{user(home.user)}), http.StatusOK, "editing is the owner's")
		refusedWith(t, "a reader asking", dave.put(t, pagePath(other, "/read-ask"), map[string]any{"ask": true}), http.StatusForbidden, "forbidden")
		ask := obj(t, want(t, owner.put(t, pagePath(other, "/read-ask"), map[string]any{"ask": true}), http.StatusOK, "ask the readers"), "readAsk")
		if number(ask["since"]) != 1 || ask["confirmedAt"] != nil {
			t.Errorf("the ask reads %v", ask)
		}
		confirmed := obj(t, want(t, dave.post(t, pagePath(other, "/read-confirmations"), nil), http.StatusCreated, "dave confirms"), "readAsk")
		if confirmed["confirmedAt"] == nil {
			t.Errorf("dave's confirmation reads %v", confirmed)
		}
		refusedWith(t, "a reader listing the readers", dave.get(t, pagePath(other, "/read-confirmations")), http.StatusForbidden, "forbidden")
		readers := obj(t, want(t, owner.get(t, pagePath(other, "/read-confirmations")), http.StatusOK, "the owner lists"), "readers")
		names := func(key string) string {
			var out []string
			for _, each := range readers[key].([]any) {
				e := each.(map[string]any)
				if p, ok := e["person"].(map[string]any); ok {
					e = p
				}
				out = append(out, e["name"].(string))
			}
			return strings.Join(out, ",")
		}
		if names("confirmed") != "Dave Reader" || !strings.Contains(names("pending"), "Ann Author") || strings.Contains(names("pending"), "Dave") ||
			number(readers["more"]) != 0 || number(readers["since"]) != 1 {
			t.Errorf("the readers read %v", readers)
		}

		publishDraft(t, owner, other, "Other", "A new rule", false)
		ask = obj(t, want(t, owner.put(t, pagePath(other, "/read-ask"), map[string]any{"ask": true}), http.StatusOK, "ask again"), "readAsk")
		if number(ask["since"]) != 2 {
			t.Errorf("asking again reads %v", ask)
		}
		if mine := obj(t, want(t, dave.get(t, pagePath(other)), http.StatusOK, "dave reads"), "page")["readAsk"].(map[string]any); number(mine["since"]) != 2 || mine["confirmedAt"] != nil {
			t.Errorf("dave's ask reads %v after the new version", mine)
		}
		readers = obj(t, want(t, owner.get(t, pagePath(other, "/read-confirmations")), http.StatusOK, "the owner lists again"), "readers")
		if names("confirmed") != "" || !strings.Contains(names("pending"), "Dave Reader") {
			t.Errorf("after asking again the readers read %v", readers)
		}
		if got := obj(t, want(t, owner.put(t, pagePath(other, "/read-ask"), map[string]any{"ask": false}), http.StatusOK, "stop asking"), "readAsk"); got["since"] != nil {
			t.Errorf("a page that stopped asking reads %v", got)
		}
		draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Unread"}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
		refusedWith(t, "asking about an unpublished page", owner.put(t, pagePath(draft, "/read-ask"), map[string]any{"ask": true}), http.StatusConflict, "unpublished")
	})

	t.Run("the branch's rule is removed by an administrator", func(t *testing.T) {
		refusedWith(t, "a member removing it", ann.delete(t, pagePath(policies, "/review-rule")), http.StatusForbidden, "forbidden")
		want(t, owner.delete(t, pagePath(policies, "/review-rule")), http.StatusNoContent, "remove it")
		if r, _ := reviewOf(t, owner, leave); r != nil {
			t.Errorf("after its removal the page is held by %v", r)
		}
		if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1`, events.TopicReviewSubmitted); n < 5 {
			t.Errorf("%d submissions were announced", n)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app,
// only a named reviewer decides, never the author, only while pending; a
// page has one pending review and no version around it; a review goes out
// with its approvals or skipped by an administrator with a reason; rules
// are the administrators'; and a reader confirms for themselves alone, on a
// page that asks.
func TestReviewsAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "reviews-db")
	owner := api.as(t, home.user)
	annID, bobID, carlID, daveID := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	ann := api.as(t, annID)
	checkers := h.makeGroup(t, "Checkers", carlID)

	docs := newTree(t, owner, "REVDB", "Reviews")
	spaceID := obj(t, want(t, owner.get(t, "/api/v1/spaces/REVDB"), http.StatusOK, "the space"), "space")["id"].(string)
	held := docs.add(docs.homeID, "Held")
	asks := docs.add(docs.homeID, "Asks")
	quiet := docs.add(docs.homeID, "Quiet")
	rule := map[string]any{"reviewers": []any{user(bobID), group(checkers)}, "required": 2}
	ruleID := idOf(t, want(t, owner.put(t, pagePath(held, "/review-rule"), rule), http.StatusOK, "hold the page"), "rule")
	draftOn(t, ann, held, "Held", "Ann's words")
	reviewID := idOf(t, want(t, submit(t, ann, held, nil), http.StatusCreated, "ann submits"), "review")
	want(t, restrict(t, owner, asks, nil, []any{user(home.user)}), http.StatusOK, "editing asks is the owner's")
	want(t, restrict(t, owner, quiet, nil, []any{user(home.user)}), http.StatusOK, "editing quiet is the owner's")
	want(t, owner.put(t, pagePath(asks, "/read-ask"), map[string]any{"ask": true}), http.StatusOK, "the page asks")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	decision := `INSERT INTO page_review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`
	nextVersion := `INSERT INTO page_version (page_id, number, title, body, created_by, review_id)
		VALUES ($1, 2, 'Around', '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb, current_actor_id(), $2)`

	actAs(t, conn, annID)
	refused(t, conn, "the author approving her own draft", decision, reviewID, annID)
	refused(t, conn, "a second pending review of a page", `
		INSERT INTO page_review (page_id, author_id, base_version, title, body, required)
		VALUES ($1, current_actor_id(), 1, 'Again', '{}'::jsonb, 1)`, held)
	refused(t, conn, "a review born published", `
		INSERT INTO page_review (page_id, author_id, base_version, title, body, required, state, version, finished_at)
		VALUES ($1, current_actor_id(), 1, 'Done', '{}'::jsonb, 1, 'published', 2, now())`, quiet)
	refused(t, conn, "a review published without its approvals", `
		UPDATE page_review SET state = 'published', version = 2, finished_at = now() WHERE id = $1`, reviewID)
	refused(t, conn, "a review skipped by its author", `
		UPDATE page_review SET state = 'skipped', skip_reason = 'Mine', skipped_by = current_actor_id(), version = 2, finished_at = now() WHERE id = $1`, reviewID)

	actAs(t, conn, daveID)
	refused(t, conn, "somebody not named approving", decision, reviewID, daveID)
	refused(t, conn, "an approval in somebody else's name", decision, reviewID, bobID)

	actAs(t, conn, home.user)
	refused(t, conn, "an editor publishing around the review", nextVersion, held, nil)
	refused(t, conn, "an editor publishing the review in his own name", nextVersion, held, reviewID)
	refused(t, conn, "a skip without a reason", `
		UPDATE page_review SET state = 'skipped', skip_reason = ' ', skipped_by = current_actor_id(), version = 2, finished_at = now() WHERE id = $1`, reviewID)
	refused(t, conn, "a skip with no version behind it", `
		UPDATE page_review SET state = 'skipped', skip_reason = 'Urgent', skipped_by = current_actor_id(), version = 2, finished_at = now() WHERE id = $1`, reviewID)
	refused(t, conn, "a reviewer everyone", `
		INSERT INTO review_rule_subject (rule_id, subject_type) VALUES ($1, 'everyone')`, ruleID)
	if _, err := conn.Exec(ctx, `INSERT INTO review_rule (space_id, required, updated_by) VALUES ($1, 1, current_actor_id())`, spaceID); err != nil {
		t.Errorf("the administrator could not hold the space: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM review_rule WHERE space_id = $1`, spaceID); err != nil {
		t.Errorf("the administrator could not stop holding the space: %v", err)
	}

	actAs(t, conn, bobID)
	refused(t, conn, "a reviewer withdrawing", `UPDATE page_review SET state = 'withdrawn', finished_at = now() WHERE id = $1`, reviewID)
	if _, err := conn.Exec(ctx, decision, reviewID, bobID); err != nil {
		t.Fatalf("bob could not approve: %v", err)
	}
	refused(t, conn, "a reviewer publishing in the author's name", `
		INSERT INTO page_version (page_id, number, title, body, created_by, review_id)
		VALUES ($1, 2, 'Held', '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb, $2, $3)`, held, annID, reviewID)
	refused(t, conn, "a review sent back without asking for changes", `
		UPDATE page_review SET state = 'changes_requested', finished_at = now() WHERE id = $1`, reviewID)
	refused(t, conn, "a member holding the space", `
		INSERT INTO review_rule (space_id, required) VALUES ($1, 1)`, spaceID)
	refused(t, conn, "a member naming himself a reviewer", `
		INSERT INTO review_rule_subject (rule_id, subject_type, user_id) VALUES ($1, 'user', current_actor_id())`, ruleID)
	untouched(t, conn, "a member removing the rule", `DELETE FROM review_rule WHERE id = $1`, ruleID)
	untouched(t, conn, "a member changing the rule", `UPDATE review_rule SET required = 1 WHERE id = $1`, ruleID)

	actAs(t, conn, carlID)
	if _, err := conn.Exec(ctx, decision, reviewID, carlID); err != nil {
		t.Errorf("carl, named by his group, could not approve: %v", err)
	}

	actAs(t, conn, daveID)
	untouched(t, conn, "somebody changing bob's word", `UPDATE page_review_decision SET decision = 'changes', comment = 'No' WHERE reviewer_id = $1`, bobID)
	refused(t, conn, "a reader confirming for somebody else", `
		INSERT INTO page_read_confirmation (page_id, user_id, version) VALUES ($1, $2, 1)`, asks, bobID)
	refused(t, conn, "a reader confirming a page that does not ask", `
		INSERT INTO page_read_confirmation (page_id, user_id, version) VALUES ($1, current_actor_id(), 1)`, quiet)
	refused(t, conn, "a reader confirming a version the page is not at", `
		INSERT INTO page_read_confirmation (page_id, user_id, version) VALUES ($1, current_actor_id(), 0)`, asks)
	refused(t, conn, "a reader asking", `
		INSERT INTO page_read_ask (page_id, since, asked_by) VALUES ($1, 1, current_actor_id())`, quiet)
	untouched(t, conn, "a reader stopping the ask", `DELETE FROM page_read_ask WHERE page_id = $1`, asks)
	if _, err := conn.Exec(ctx, `
		INSERT INTO page_read_confirmation (page_id, user_id, version) VALUES ($1, current_actor_id(), 1)`, asks); err != nil {
		t.Errorf("dave could not confirm reading: %v", err)
	}

	actAs(t, conn, annID)
	untouched(t, conn, "somebody reading another's confirmation", `SELECT 1 FROM page_read_confirmation WHERE page_id = $1`, asks)
	if _, err := conn.Exec(ctx, `UPDATE page_review SET state = 'withdrawn', finished_at = now() WHERE id = $1`, reviewID); err != nil {
		t.Fatalf("ann could not withdraw her review: %v", err)
	}
	refused(t, conn, "a decided review made pending again", `UPDATE page_review SET state = 'pending', finished_at = NULL WHERE id = $1`, reviewID)

	actAs(t, conn, bobID)
	untouched(t, conn, "a decision on a review no longer pending", `UPDATE page_review_decision SET comment = 'Late' WHERE review_id = $1 AND reviewer_id = current_actor_id()`, reviewID)

	// The page is free again once nothing waits, which the service shows too.
	want(t, ann.post(t, pagePath(held, "/publish"), map[string]any{}), http.StatusConflict, "the page is still held")
	want(t, owner.delete(t, pagePath(held, "/review-rule")), http.StatusNoContent, "remove the rule")
	want(t, ann.post(t, pagePath(held, "/publish"), map[string]any{}), http.StatusOK, "ann publishes")
}
