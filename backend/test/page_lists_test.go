//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Content by label and recently updated (#55): blocks that list published
// pages, each reader seeing only those they may read.

func listTitles(t *testing.T, c *client, path string) string {
	t.Helper()
	var titles []string
	for _, each := range list(t, want(t, c.get(t, path), http.StatusOK, "list "+path), "pages") {
		titles = append(titles, each.(map[string]any)["title"].(string))
	}
	return strings.Join(titles, ",")
}

func labelledPath(labels []string, match, space, sort string, limit int) string {
	v := url.Values{"label": labels}
	for k, val := range map[string]string{"match": match, "space": space, "sort": sort} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if limit != 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return "/api/v1/labelled-pages?" + v.Encode()
}

func updatedPath(space string, limit int) string {
	v := url.Values{}
	if space != "" {
		v.Set("space", space)
	}
	if limit != 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return "/api/v1/updated-pages?" + v.Encode()
}

func TestPageListsShowWhatEachReaderMayRead(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "lists")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "LDOC", "Docs")
	ops := newTree(t, owner, "LOPS", "Ops")
	labelled := func(tr *tree, title string, labels ...string) string {
		id := tr.add(tr.homeID, title)
		for _, name := range labels {
			want(t, owner.post(t, pagePath(id, "/labels"), map[string]any{"name": name}), http.StatusOK, "label "+title)
		}
		return id
	}
	// Published in this order, so the latest first is the other way round.
	guide := labelled(docs, "Guide", "howto")
	labelled(docs, "Alpha checklist", "howto", "release")
	labelled(ops, "Runbook", "howto", "ops")
	secret := labelled(docs, "Secret plan", "howto", "release")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "restrict Secret plan")
	gone := labelled(docs, "Gone", "howto")
	want(t, owner.delete(t, pagePath(gone)), http.StatusNoContent, "trash Gone")
	shelved := labelled(ops, "Shelved", "howto")
	want(t, owner.put(t, pagePath(shelved, "/archive"), nil), http.StatusOK, "archive Shelved")
	draft := docs.add(docs.homeID, "Draft", map[string]any{"publish": false})
	want(t, owner.post(t, pagePath(draft, "/labels"), map[string]any{"name": "howto"}), http.StatusOK, "label the draft")
	// Guide is published again, so it is the latest.
	want(t, owner.patch(t, pagePath(guide), map[string]any{"title": "Guide, revised", "version": 1}), http.StatusOK, "revise Guide")

	t.Run("content by label lists the published pages each reader may read, latest first or by title", func(t *testing.T) {
		if got := listTitles(t, member, labelledPath([]string{"howto"}, "", "", "", 0)); got != "Guide, revised,Runbook,Alpha checklist" {
			t.Errorf("the member's howtos, latest first: %s", got)
		}
		if got := listTitles(t, owner, labelledPath([]string{"HowTo"}, "all", "", "title", 0)); got != "Alpha checklist,Guide, revised,Runbook,Secret plan" {
			t.Errorf("the owner's howtos by title: %s", got)
		}
	})

	t.Run("all labels narrows, any widens, a space keeps it inside, and a limit cuts it", func(t *testing.T) {
		if got := listTitles(t, owner, labelledPath([]string{"release", "ops"}, "all", "", "title", 0)); got != "" {
			t.Errorf("release and ops: %s", got)
		}
		if got := listTitles(t, owner, labelledPath([]string{"release", "ops"}, "any", "", "title", 0)); got != "Alpha checklist,Runbook,Secret plan" {
			t.Errorf("release or ops: %s", got)
		}
		if got := listTitles(t, member, labelledPath([]string{"howto"}, "", "LOPS", "", 0)); got != "Runbook" {
			t.Errorf("howtos in Ops: %s", got)
		}
		if got := listTitles(t, member, labelledPath([]string{"howto"}, "", "", "title", 2)); got != "Alpha checklist,Guide, revised" {
			t.Errorf("the first two: %s", got)
		}
	})

	t.Run("recently updated lists the latest published pages, folders and the archive left out, with who published them", func(t *testing.T) {
		r := want(t, member.get(t, updatedPath("", 3)), http.StatusOK, "the member's updates")
		pages := list(t, r, "pages")
		if got := listTitles(t, member, updatedPath("", 3)); got != "Guide, revised,Runbook,Alpha checklist" {
			t.Errorf("the latest three: %s", got)
		}
		if name := pages[0].(map[string]any)["authorName"]; name == "" || name == nil {
			t.Errorf("the latest has no author: %v", pages[0])
		}
		if got := listTitles(t, owner, updatedPath("LOPS", 0)); got != "Runbook,Ops" {
			t.Errorf("in Ops: %s", got)
		}
	})

	t.Run("lists that cannot be are refused, and a space nobody has is not found", func(t *testing.T) {
		for what, path := range map[string]string{
			"no label":     labelledPath(nil, "", "", "", 0),
			"a slash":      labelledPath([]string{"a/b"}, "", "", "", 0),
			"some":         labelledPath([]string{"a"}, "some", "", "", 0),
			"by views":     labelledPath([]string{"a"}, "", "", "views", 0),
			"fifty-one":    labelledPath([]string{"a"}, "", "", "", 51),
			"no pages":     updatedPath("", -1),
			"not a number": "/api/v1/updated-pages?limit=many",
		} {
			if got := member.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		for _, path := range []string{labelledPath([]string{"a"}, "", "NOPE", "", 0), updatedPath("NOPE", 0)} {
			if got := member.get(t, path); got.Status != http.StatusNotFound {
				t.Errorf("%s: %d %s", path, got.Status, got.Raw)
			}
		}
	})

	t.Run("a page keeps what a list shows, never the pages", func(t *testing.T) {
		for what, block := range map[string]map[string]any{
			"by label": {"type": "labelledPages", "attrs": map[string]any{"labels": []any{"howto"}, "match": "any", "space": nil, "sort": "title", "limit": 5}},
			"updated":  {"type": "recentlyUpdated", "attrs": map[string]any{"space": "LDOC", "limit": 5}},
		} {
			docs.add(docs.homeID, "Overview "+what, map[string]any{"body": docOf(block)})
			block["attrs"].(map[string]any)["pages"] = []any{}
			if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Stale " + what, "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s with its pages: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}
