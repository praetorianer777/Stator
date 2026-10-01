//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/reaction"
)

func reactionsPath(on, id string) string {
	return "/api/v1/" + on + "/" + id + "/reactions"
}

func react(t *testing.T, c *client, on, id, emoji string) response {
	t.Helper()
	return c.post(t, reactionsPath(on, id), map[string]any{"emoji": emoji})
}

func unreact(t *testing.T, c *client, on, id, emoji string) response {
	t.Helper()
	return c.delete(t, reactionsPath(on, id)+"?emoji="+url.QueryEscape(emoji))
}

// reactionList reads the reactions out of an answer that carries them.
func reactionList(v any) []map[string]any {
	var out []map[string]any
	for _, each := range v.([]any) {
		out = append(out, each.(map[string]any))
	}
	return out
}

func names(r map[string]any) []string {
	var out []string
	for _, p := range r["people"].([]any) {
		out = append(out, p.(map[string]any)["name"].(string))
	}
	return out
}

func pageReactions(t *testing.T, c *client, page string) []map[string]any {
	t.Helper()
	return reactionList(obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["reactions"])
}

// Emoji on pages and on comments: put on and taken off in one's own name,
// counted, named for the tooltip, and only where one may comment.
func TestReactionsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "reactions")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.namedPerson(t, home.org, "Ann Reactor"), h.namedPerson(t, home.org, "Ben Reactor"), h.namedPerson(t, home.org, "Carl Outsider")
	ann, ben, carl := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, carlID, home.org, slug)

	docs := newTree(t, owner, "REACT", "React")
	plan := docs.add(docs.homeID, "Plan", map[string]any{"body": textDoc("The plan.")})
	secret := docs.add(docs.homeID, "Secret", map[string]any{"body": textDoc("Hidden.")})
	want(t, restrict(t, owner, secret, []any{user(home.user), user(annID)}, nil), http.StatusOK, "restrict Secret to ann")
	h.settle(t)

	t.Run("a page's reactions are counted, named and told apart for each reader", func(t *testing.T) {
		if got := pageReactions(t, ann, plan); len(got) != 0 {
			t.Fatalf("a new page has reactions %v", got)
		}
		got := reactionList(obj(t, want(t, react(t, ann, "pages", plan, "👍"), http.StatusOK, "ann reacts"))["reactions"])
		if len(got) != 1 || got[0]["emoji"] != "👍" || number(got[0]["count"]) != 1 || got[0]["mine"] != true {
			t.Fatalf("after ann's reaction the page has %v", got)
		}
		want(t, react(t, ann, "pages", plan, "👍"), http.StatusOK, "ann reacts again")
		want(t, react(t, ben, "pages", plan, "👍"), http.StatusOK, "ben agrees")
		want(t, react(t, ben, "pages", plan, "🎉"), http.StatusOK, "ben cheers")

		got = pageReactions(t, carl, plan)
		if len(got) != 2 || got[0]["emoji"] != "👍" || got[1]["emoji"] != "🎉" {
			t.Fatalf("the page reads %v, want thumbs up before the party in the order first used", got)
		}
		if number(got[0]["count"]) != 2 || got[0]["mine"] != false || number(got[1]["count"]) != 1 {
			t.Errorf("carl sees %v", got)
		}
		if who := names(got[0]); len(who) != 2 || who[0] != "Ann Reactor" || who[1] != "Ben Reactor" {
			t.Errorf("the thumbs up names %v, want ann then ben", who)
		}
		mine := pageReactions(t, ben, plan)
		if mine[0]["mine"] != true || mine[1]["mine"] != true {
			t.Errorf("ben does not see his own: %v", mine)
		}

		got = reactionList(obj(t, want(t, unreact(t, ann, "pages", plan, "👍"), http.StatusOK, "ann takes hers off"))["reactions"])
		if number(got[0]["count"]) != 1 || got[0]["mine"] != false || names(got[0])[0] != "Ben Reactor" {
			t.Errorf("after ann's takes hers off the page has %v", got)
		}
		want(t, unreact(t, ann, "pages", plan, "👍"), http.StatusOK, "again, no change")
		want(t, unreact(t, carl, "pages", plan, "🎉"), http.StatusOK, "carl takes off one he never put on")
		if got := pageReactions(t, ben, plan); len(got) != 2 {
			t.Errorf("somebody else's reaction went: %v", got)
		}
	})

	t.Run("the tooltip names the first people and the count says how many", func(t *testing.T) {
		var everybody []*client
		for i := 0; i < reaction.MaxPeople+1; i++ {
			id := h.addPerson(t, home.org, "member")
			everybody = append(everybody, api.as(t, id, home.org, slug))
		}
		h.settle(t)
		for _, c := range everybody {
			want(t, react(t, c, "pages", plan, "🚀"), http.StatusOK, "a member reacts")
		}
		last := everybody[len(everybody)-1]
		for name, c := range map[string]*client{"the last": last, "ann": ann} {
			got := pageReactions(t, c, plan)
			rocket := got[len(got)-1]
			if rocket["emoji"] != "🚀" || number(rocket["count"]) != reaction.MaxPeople+1 || len(names(rocket)) != reaction.MaxPeople {
				t.Errorf("%s sees the rocket as %v", name, rocket)
			}
			if mine := rocket["mine"] == true; mine != (c == last) {
				t.Errorf("%s is told mine=%v", name, mine)
			}
		}
	})

	t.Run("what is not one emoji is refused on emoji", func(t *testing.T) {
		for name, emoji := range map[string]string{"nothing": "", "a word": "yes", "two": "👍 🎉", "markup": "<b>"} {
			r := want(t, react(t, ann, "pages", plan, emoji), http.StatusUnprocessableEntity, name)
			if fields, _ := obj(t, r, "error")["fields"].(map[string]any); fields["emoji"] == nil {
				t.Errorf("%s is not refused on emoji: %v", name, r.Body)
			}
		}
		want(t, unreact(t, ann, "pages", plan, "yes"), http.StatusUnprocessableEntity, "taking a word off")
	})

	t.Run("one person puts at most so many different emoji on one thing", func(t *testing.T) {
		many := docs.add(docs.homeID, "Faces")
		h.settle(t)
		faces := []rune("😀😁😂😃😄😅😆😇😈😉😊😋😌😍😎😏😐😑😒😓😔")
		for _, face := range faces[:reaction.MaxPerPerson] {
			want(t, react(t, carl, "pages", many, string(face)), http.StatusOK, "carl reacts")
		}
		r := want(t, react(t, carl, "pages", many, string(faces[reaction.MaxPerPerson])), http.StatusUnprocessableEntity, "one too many")
		if fields, _ := obj(t, r, "error")["fields"].(map[string]any); fields["emoji"] == nil {
			t.Errorf("the one too many is not refused on emoji: %v", r.Body)
		}
		want(t, react(t, carl, "pages", many, string(faces[0])), http.StatusOK, "one he has already")
	})

	t.Run("a comment takes reactions, which go with its delete", func(t *testing.T) {
		before := len(pageReactions(t, ann, plan))
		th := startThread(t, ann, plan, "Ship it?")
		id := th["id"].(string)
		want(t, react(t, ben, "comments", id, "✅"), http.StatusOK, "ben reacts to ann's comment")
		got := reactionList(obj(t, want(t, react(t, carl, "comments", id, "✅"), http.StatusOK, "carl too"))["reactions"])
		if len(got) != 1 || number(got[0]["count"]) != 2 || got[0]["mine"] != true {
			t.Errorf("the comment has %v", got)
		}
		listed := commentsIn(threadsOf(t, ann, plan, "")[0])[0]
		on := reactionList(listed["reactions"])
		if len(on) != 1 || on[0]["emoji"] != "✅" || on[0]["mine"] != false {
			t.Errorf("the listed comment has %v", on)
		}
		if who := names(on[0]); len(who) != 2 || who[0] != "Ben Reactor" || who[1] != "Carl Outsider" {
			t.Errorf("the comment's reaction names %v", who)
		}
		if got := pageReactions(t, ann, plan); len(got) != before {
			t.Errorf("a comment's reaction shows on the page: %v", got)
		}
		r := obj(t, want(t, reply(t, ben, id, "Yes."), http.StatusCreated, "ben replies"))
		if got := r["comment"].(map[string]any)["reactions"].([]any); len(got) != 0 {
			t.Errorf("a new reply has reactions %v", got)
		}
		want(t, unreact(t, carl, "comments", id, "✅"), http.StatusOK, "carl takes his off")

		want(t, ann.delete(t, "/api/v1/comments/"+id), http.StatusNoContent, "ann deletes her comment")
		if n := h.countRows(t, `SELECT count(*) FROM reaction WHERE comment_id = $1`, id); n != 0 {
			t.Errorf("a deleted comment keeps %d reactions", n)
		}
		placeholder := commentsIn(threadsOf(t, ann, plan, "")[0])[0]
		if got := placeholder["reactions"].([]any); placeholder["deleted"] != true || len(got) != 0 {
			t.Errorf("the placeholder reads %v", placeholder)
		}
		want(t, react(t, ben, "comments", id, "👍"), http.StatusNotFound, "a reaction to a deleted comment")
		want(t, unreact(t, ben, "comments", id, "✅"), http.StatusOK, "taking one off a deleted comment is no change")
		want(t, react(t, ben, "comments", uuid.NewString(), "👍"), http.StatusNotFound, "no such comment")
		want(t, unreact(t, ben, "comments", uuid.NewString(), "👍"), http.StatusNotFound, "taking one off no such comment")
		want(t, react(t, ben, "comments", "nope", "👍"), http.StatusBadRequest, "not a comment id")
	})

	t.Run("a page one may not view takes and shows no reactions", func(t *testing.T) {
		hidden := startThread(t, ann, secret, "Only for us.")["id"].(string)
		want(t, react(t, ann, "pages", secret, "👀"), http.StatusOK, "ann reacts to Secret")
		want(t, react(t, ann, "comments", hidden, "👀"), http.StatusOK, "ann reacts to her hidden comment")
		want(t, react(t, carl, "pages", secret, "👀"), http.StatusNotFound, "carl reacts to Secret")
		want(t, unreact(t, carl, "pages", secret, "👀"), http.StatusNotFound, "carl takes one off Secret")
		want(t, react(t, carl, "comments", hidden, "👀"), http.StatusNotFound, "carl reacts to a hidden comment")
		want(t, unreact(t, carl, "comments", hidden, "👀"), http.StatusNotFound, "carl takes one off a hidden comment")
		want(t, react(t, carl, "pages", uuid.NewString(), "👀"), http.StatusNotFound, "no such page")
	})

	t.Run("an unpublished page takes no reactions", func(t *testing.T) {
		draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft"}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
		r := want(t, react(t, ann, "pages", draft, "👍"), http.StatusConflict, "react to it")
		if code := errorCode(t, r); code != "unpublished" {
			t.Errorf("the refusal is %s", code)
		}
	})

	t.Run("a reader without addComments sees reactions and adds none", func(t *testing.T) {
		th := startThread(t, ann, plan, "Read only soon.")["id"].(string)
		want(t, react(t, ben, "comments", th, "👍"), http.StatusOK, "ben reacts while he may")
		want(t, owner.put(t, "/api/v1/spaces/REACT/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "REACT is read only")
		h.settle(t)
		for what, r := range map[string]response{
			"ben reacts to the page":    react(t, ben, "pages", plan, "😀"),
			"ben reacts to the comment": react(t, ben, "comments", th, "😀"),
		} {
			if r.Status != http.StatusForbidden || errorCode(t, r) != "forbidden" {
				t.Errorf("%s: %d %s", what, r.Status, r.Raw)
			}
		}
		if got := pageReactions(t, ben, plan); len(got) == 0 {
			t.Error("ben sees no reactions on a page he may read")
		}
		want(t, unreact(t, ben, "comments", th, "👍"), http.StatusOK, "ben takes his own off while he may view")
		want(t, owner.put(t, "/api/v1/spaces/REACT/permissions", map[string]any{"grants": []any{commenters}}), http.StatusOK, "REACT is open again")
		h.settle(t)
	})

	t.Run("a trashed page's reactions are out of reach", func(t *testing.T) {
		want(t, owner.delete(t, pagePath(plan)), http.StatusNoContent, "trash the page")
		want(t, react(t, ben, "pages", plan, "👍"), http.StatusNotFound, "react to it")
		want(t, unreact(t, ben, "pages", plan, "🎉"), http.StatusNotFound, "take one off it")
	})
}

// Straight through SQL as stator_app, the database holds reactions to the
// same rules: read with the page, put on in one's own name where one may
// comment, taken off only one's own, and never on a deleted comment.
func TestReactionsAreEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "reaction-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, benID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)
	other := h.makeMember(t, "reaction-rls-other")

	docs := newTree(t, owner, "RRAW", "Raw")
	open := docs.add(docs.homeID, "Open")
	secret := docs.add(docs.homeID, "Secret")
	draft := obj(t, want(t, ann.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Draft"}), http.StatusCreated, "ann's draft page"), "page")["id"].(string)
	anns := startThread(t, ann, open, "Ann's words.")["id"].(string)
	gone := startThread(t, ann, open, "Going.")["id"].(string)
	elsewhere := startThread(t, ann, secret, "Elsewhere.")["id"].(string)
	want(t, react(t, ann, "pages", open, "👍"), http.StatusOK, "ann reacts to Open")
	want(t, react(t, ben, "pages", secret, "👀"), http.StatusOK, "ben reacts to Secret")
	want(t, ann.delete(t, "/api/v1/comments/"+gone), http.StatusNoContent, "ann deletes a comment")
	want(t, restrict(t, owner, secret, []any{user(home.user), user(benID)}, nil), http.StatusOK, "restrict Secret to ben")
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
	onPage := `INSERT INTO reaction (org_id, page_id, user_id, emoji) VALUES ($1, $2, $3, $4)`
	onComment := `INSERT INTO reaction (org_id, page_id, comment_id, user_id, emoji) VALUES ($1, $2, $3, $4, $5)`

	t.Run("somebody off a view list reads none and adds none", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		if n := count(`SELECT count(*) FROM reaction WHERE page_id = $1`, secret); n != 0 {
			t.Errorf("carl reads %d reactions on Secret", n)
		}
		if n := count(`SELECT count(*) FROM reaction WHERE page_id = $1`, open); n != 1 {
			t.Errorf("carl reads %d reactions on Open, want 1", n)
		}
		denied(t, conn, "a reaction on a hidden page", onPage, home.org, secret, carlID, "🙂")
		untouched(t, conn, "taking ben's off a hidden page", `DELETE FROM reaction WHERE page_id = $1`, secret)
	})

	t.Run("nobody reacts or unreacts in somebody else's name", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "a reaction as ann", onPage, home.org, open, annID, "🙂")
		untouched(t, conn, "taking ann's off", `DELETE FROM reaction WHERE user_id = $1`, annID)
		denied(t, conn, "changing ann's emoji", `UPDATE reaction SET emoji = '👎' WHERE user_id = $1`, annID)
		if _, err := conn.Exec(ctx, onPage, home.org, open, carlID, "🙂"); err != nil {
			t.Errorf("carl may not react in his own name: %v", err)
		}
		if n, err := conn.Exec(ctx, `DELETE FROM reaction WHERE user_id = $1`, carlID); err != nil || n.RowsAffected() != 1 {
			t.Errorf("carl may not take his own off: %v", err)
		}
	})

	t.Run("an unpublished page, a deleted comment and a comment of another page take none", func(t *testing.T) {
		actAs(t, conn, home.org, annID)
		denied(t, conn, "a reaction on an unpublished page", onPage, home.org, draft, annID, "🙂")
		denied(t, conn, "a reaction on a deleted comment", onComment, home.org, open, gone, annID, "🙂")
		refused(t, conn, "a comment's reaction naming another page", onComment, home.org, open, elsewhere, annID, "🙂")
		if _, err := conn.Exec(ctx, onComment, home.org, open, anns, annID, "🙂"); err != nil {
			t.Errorf("ann may not react to her own comment: %v", err)
		}
		refused(t, conn, "the same reaction twice", onComment, home.org, open, anns, annID, "🙂")
		denied(t, conn, "a word for an emoji", onPage, home.org, open, annID, "yes")
	})

	t.Run("without addComments nothing is added, and one's own still comes off", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/RRAW/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "RRAW is read only")
		h.settle(t)
		actAs(t, conn, home.org, annID)
		denied(t, conn, "a reaction without addComments", onPage, home.org, open, annID, "🎉")
		if n, err := conn.Exec(ctx, `DELETE FROM reaction WHERE user_id = $1 AND page_id = $2 AND comment_id IS NULL`, annID, open); err != nil || n.RowsAffected() != 1 {
			t.Errorf("ann may not take her own off while she may view the page: %v", err)
		}
	})

	t.Run("another organization reads none and points none here", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM reaction`); n != 0 {
			t.Errorf("another organization reads %d reactions", n)
		}
		denied(t, conn, "a reaction into this organization", onPage, home.org, open, other.user, "🙂")
	})
}
