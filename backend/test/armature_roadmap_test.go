//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The roadmap block (#52): Armature's plan read with each reader's own
// token, its matched issues drawn under their epics or their teams.

func roadmapPath(project, q, groupBy string) string {
	return "/api/v1/armature/roadmap?" + url.Values{"project": {project}, "q": {q}, "groupBy": {groupBy}}.Encode()
}

// lanes writes a roadmap's groups as name@epic[keys], to compare in one line.
func lanes(roadmap map[string]any) string {
	var out []string
	for _, each := range roadmap["groups"].([]any) {
		g := each.(map[string]any)
		head := g["name"].(string)
		if epic, ok := g["epic"].(map[string]any); ok {
			head += "@" + epic["key"].(string)
		}
		var keys []string
		for _, row := range g["rows"].([]any) {
			keys = append(keys, row.(map[string]any)["key"].(string))
		}
		out = append(out, head+"["+strings.Join(keys, " ")+"]")
	}
	return strings.Join(out, " ")
}

func TestARoadmapDrawsWhatEachReaderMaySee(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-road")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bob := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	carol := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	// CP-5 is SEC-2, moved; the epic is the next issue of CP.
	stubControl(t, http.MethodPost, slug+"/projects/CP/issues", map[string]any{"summary": "Launch the beta", "type": "Epic"})
	stubControl(t, http.MethodPatch, slug+"/issues/CP-1", map[string]any{"parent": "CP-6", "startDate": "2026-09-01", "dueDate": "2026-09-20", "team": "Platform"})
	stubControl(t, http.MethodPatch, slug+"/issues/CP-4", map[string]any{"parent": "CP-6", "startDate": "2026-10-01", "team": "Platform"})
	stubControl(t, http.MethodPatch, slug+"/issues/CP-2", map[string]any{"startDate": "2026-08-15", "dueDate": "2026-08-31", "team": "Support"})

	roadmap := func(c *client, path string) (string, map[string]any) {
		t.Helper()
		r := want(t, c.get(t, path), http.StatusOK, "roadmap "+path)
		status, _ := r.Body["status"].(string)
		got, _ := r.Body["roadmap"].(map[string]any)
		return status, got
	}
	byEpic := roadmapPath("CP", "project = CP", "epic")

	if status, got := roadmap(bob, byEpic); status != "not_configured" || got != nil {
		t.Errorf("before a connection the roadmap is %s %v", status, got)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("by epic, each issue is under its epic, which spans its children's days", func(t *testing.T) {
		status, got := roadmap(bob, byEpic)
		if status != "ok" || lanes(got) != "Launch the beta@CP-6[CP-1 CP-4] [CP-2]" {
			t.Fatalf("bob's roadmap is %s %s", status, lanes(got))
		}
		epic := got["groups"].([]any)[0].(map[string]any)["epic"].(map[string]any)
		if epic["start"] != "2026-09-01" || epic["due"] != "2026-10-15" || epic["derived"] != true || !strings.HasSuffix(epic["url"].(string), "/issues/CP-6") {
			t.Errorf("the epic's bar: %v", epic)
		}
		// CP-3 and CP-5 have no days.
		if got["unscheduled"] != float64(2) || got["from"] != "2026-08-15" || got["to"] != "2026-10-15" {
			t.Errorf("unscheduled %v, from %v to %v", got["unscheduled"], got["from"], got["to"])
		}
	})

	t.Run("by team, and a query that leaves the epic out still finds it above its issues", func(t *testing.T) {
		if _, got := roadmap(bob, roadmapPath("CP", "project = CP", "team")); lanes(got) != "Platform[CP-1 CP-4] Support[CP-2] [CP-6]" {
			t.Errorf("by team: %s", lanes(got))
		}
		if _, got := roadmap(bob, roadmapPath("CP", "key = CP-4", "epic")); lanes(got) != "Launch the beta@CP-6[CP-4]" {
			t.Errorf("one issue of the epic: %s", lanes(got))
		}
	})

	t.Run("a reader without a token is asked to connect, and a project kept from them is not drawn", func(t *testing.T) {
		if status, got := roadmap(carol, byEpic); status != "not_connected" || got != nil {
			t.Errorf("carol's roadmap is %s %v", status, got)
		}
		secret := roadmapPath("SEC", "project = SEC", "team")
		if got := bob.get(t, secret); got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
			t.Errorf("bob's roadmap of SEC: %d %s", got.Status, got.Raw)
		}
		if status, got := roadmap(owner, secret); status != "ok" || got["unscheduled"] != float64(1) {
			t.Errorf("the owner's roadmap of SEC is %s %v", status, got)
		}
	})

	t.Run("a query Armature cannot read, and settings it would not take, are refused", func(t *testing.T) {
		if got := bob.get(t, roadmapPath("CP", "project =", "epic")); got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "bad_query" {
			t.Errorf("a bad query: %d %s", got.Status, got.Raw)
		}
		for what, path := range map[string]string{
			"a project in lower case": roadmapPath("cp", "project = CP", "epic"),
			"no query":                roadmapPath("CP", " ", "epic"),
			"by sprint":               roadmapPath("CP", "project = CP", "sprint"),
		} {
			if got := bob.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a page keeps what a roadmap draws, never the days", func(t *testing.T) {
		docs := newTree(t, owner, "ROAD", "Roadmaps")
		block := map[string]any{"type": "armatureRoadmap", "attrs": map[string]any{"project": "CP", "query": "project = CP", "groupBy": "epic"}}
		page := docs.add(docs.homeID, "Plan", map[string]any{"body": docOf(block)})
		resp, data := owner.download(t, pagePath(page, "/markdown"))
		if md := string(data); resp.StatusCode != http.StatusOK || !strings.Contains(md, `data-stator="issue-roadmap"`) {
			t.Errorf("the export: %d\n%s", resp.StatusCode, md)
		}
		block["attrs"].(map[string]any)["groups"] = []any{}
		if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Stale", "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a roadmap with its rows: %d %s", got.Status, got.Raw)
		}
	})
}
