//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// The chart block (#51): Armature's reports counted with each reader's own
// token, so each reader's chart counts the issues Armature shows them.

func chartPath(project, q, kind, groupBy string, days int) string {
	v := url.Values{"project": {project}, "q": {q}, "kind": {kind}}
	if groupBy != "" {
		v.Set("groupBy", groupBy)
	}
	if days > 0 {
		v.Set("days", strconv.Itoa(days))
	}
	return "/api/v1/armature/chart?" + v.Encode()
}

func TestAChartCountsWhatEachReaderMaySee(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-chart")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bob := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	chart := func(c *client, path string) (string, map[string]any) {
		t.Helper()
		r := want(t, c.get(t, path), http.StatusOK, "chart "+path)
		status, _ := r.Body["status"].(string)
		got, _ := r.Body["chart"].(map[string]any)
		return status, got
	}
	byCategory := chartPath("CP", "project = CP", "pie", "statusCategory", 0)

	if status, got := chart(bob, byCategory); status != "not_configured" || got != nil {
		t.Errorf("before a connection the chart is %s %v", status, got)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a pie shares a project's issues out by a field, as Armature counts them", func(t *testing.T) {
		status, got := chart(bob, byCategory)
		if status != "ok" || got["total"] != float64(5) || got["groupBy"] != "statusCategory" {
			t.Fatalf("bob's pie is %s %v", status, got)
		}
		counts := map[string]float64{}
		for _, s := range got["slices"].([]any) {
			slice := s.(map[string]any)
			counts[slice["category"].(string)] = slice["count"].(float64)
		}
		if counts["todo"] != 3 || counts["in_progress"] != 1 || counts["done"] != 1 {
			t.Errorf("by category: %v", counts)
		}
	})

	t.Run("a reader without a token is asked to connect, and a project kept from them is not counted", func(t *testing.T) {
		if status, got := chart(carol, byCategory); status != "not_connected" || got != nil {
			t.Errorf("carol's chart is %s %v", status, got)
		}
		secret := chartPath("SEC", "project = SEC", "pie", "type", 0)
		if got := bob.get(t, secret); got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
			t.Errorf("bob's chart of SEC: %d %s", got.Status, got.Raw)
		}
		if status, got := chart(owner, secret); status != "ok" || got["total"] == float64(0) {
			t.Errorf("the owner's chart of SEC is %s %v", status, got)
		}
	})

	t.Run("created against resolved counts today's resolution", func(t *testing.T) {
		stubControl(t, http.MethodPatch, slug+"/issues/CP-3", map[string]any{"statusCategory": "done"})
		status, got := chart(bob, chartPath("CP", "project = CP", "createdResolved", "", 7))
		days, _ := got["days"].([]any)
		if status != "ok" || len(days) != 7 {
			t.Fatalf("created against resolved is %s %v", status, got)
		}
		if today := days[6].(map[string]any); today["resolved"] != float64(1) {
			t.Errorf("today: %v", today)
		}
	})

	t.Run("a query Armature cannot read, and settings it would not take, are refused", func(t *testing.T) {
		if got := bob.get(t, chartPath("CP", "project =", "pie", "type", 0)); got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "bad_query" {
			t.Errorf("a bad query: %d %s", got.Status, got.Raw)
		}
		for what, path := range map[string]string{
			"a project in lower case": chartPath("cp", "project = CP", "pie", "type", 0),
			"a field Armature lacks":  chartPath("CP", "project = CP", "pie", "label", 0),
			"a year and a day":        chartPath("CP", "project = CP", "createdResolved", "", 366),
			"a bar":                   chartPath("CP", "project = CP", "bar", "type", 0),
		} {
			if got := bob.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a page keeps what a chart counts, never the counts", func(t *testing.T) {
		docs := newTree(t, owner, "CHART", "Charts")
		block := map[string]any{"type": "armatureChart", "attrs": map[string]any{"project": "CP", "query": "project = CP", "chart": "pie", "groupBy": "type", "days": 30}}
		docs.add(docs.homeID, "Report", map[string]any{"body": docOf(block)})
		block["attrs"].(map[string]any)["slices"] = []any{}
		if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Stale", "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a chart with its counts: %d %s", got.Status, got.Raw)
		}
	})
}
