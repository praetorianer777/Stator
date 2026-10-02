//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Decision items (#44): a page marks decisions, decided or not, and a space's
// decision log quotes them from the published pages each reader may read.

func decision(state, text string) map[string]any {
	return map[string]any{"type": "decision", "attrs": map[string]any{"state": state}, "content": []any{map[string]any{"type": "text", "text": text}}}
}

func docOf(blocks ...any) map[string]any { return map[string]any{"type": "doc", "content": blocks} }

func TestTheDecisionLogQuotesWhatEachReaderMayRead(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "decisions")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "DEC", "Decisions")
	docs.add(docs.homeID, "Release", map[string]any{"body": docOf(
		decision("decided", "Ship on Tuesdays quokka"),
		map[string]any{"type": "panel", "attrs": map[string]any{"kind": "info"}, "content": []any{decision("undecided", "Which region first")}},
	)})
	secret := docs.add(docs.homeID, "Secret", map[string]any{"body": docOf(decision("decided", "Hire two"))})
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "restrict Secret")
	gone := docs.add(docs.homeID, "Gone", map[string]any{"body": docOf(decision("decided", "Dropped"))})
	want(t, owner.delete(t, pagePath(gone)), http.StatusNoContent, "trash Gone")
	docs.add(docs.homeID, "Draft", map[string]any{"publish": false, "body": docOf(decision("undecided", "Not yet published"))})

	texts := func(c *client, query string) []string {
		t.Helper()
		var out []string
		for _, d := range list(t, want(t, c.get(t, "/api/v1/spaces/DEC/decisions"+query), http.StatusOK, "read the log"+query), "decisions") {
			m := d.(map[string]any)
			out = append(out, m["state"].(string)+":"+m["text"].(string)+"@"+m["pageTitle"].(string))
		}
		return out
	}

	t.Run("the owner reads every published decision, nested ones too, newest page first", func(t *testing.T) {
		sameTitles(t, "the owner's log", texts(owner, ""),
			"decided:Hire two@Secret", "decided:Ship on Tuesdays quokka@Release", "undecided:Which region first@Release")
	})

	t.Run("a member reads only what they may read", func(t *testing.T) {
		sameTitles(t, "the member's log", texts(member, ""), "decided:Ship on Tuesdays quokka@Release", "undecided:Which region first@Release")
	})

	t.Run("the log keeps one state when asked", func(t *testing.T) {
		sameTitles(t, "the undecided", texts(member, "?state=undecided"), "undecided:Which region first@Release")
		if got := member.get(t, "/api/v1/spaces/DEC/decisions?state=maybe"); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("an unknown state: %d", got.Status)
		}
	})

	t.Run("search finds a page by what was decided", func(t *testing.T) {
		if got := hitTitles(t, searchFor(t, member, url.Values{"q": {"quokka"}})); len(got) != 1 || got[0] != "Release" {
			t.Errorf("searching for the decision finds %v", got)
		}
	})

	t.Run("a decision with an unknown state, or in a comment, is refused", func(t *testing.T) {
		bad := map[string]any{"type": "decision", "attrs": map[string]any{"state": "maybe"}}
		errorCode(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd", "body": docOf(bad)}), http.StatusUnprocessableEntity, "an unknown state"))
		errorCode(t, want(t, owner.post(t, pagePath(secret, "/comments"), map[string]any{"body": docOf(decision("decided", "x"))}), http.StatusUnprocessableEntity, "a decision in a comment"))
	})

	t.Run("the database reads a decision as document.PlainText does", func(t *testing.T) {
		raw, _ := json.Marshal(docOf(decision("decided", "Use Postgres"), map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "after"}}}))
		root, err := document.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := appConn(t).QueryRow(context.Background(), `SELECT page_plain_text($1::jsonb)`, string(raw)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(root); got != want || !strings.Contains(got, "Use Postgres\nafter") {
			t.Fatalf("the database reads %q, document.PlainText %q", got, want)
		}
	})
}
