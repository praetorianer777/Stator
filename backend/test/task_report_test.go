//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A task report (#57): the tasks a filter picks by space, assignee, due day
// and state, each reader seeing only those on pages they may view.

// reportOf is the first word of each task a report holds, which names it, and
// the whole answer.
func reportOf(t *testing.T, c *client, query url.Values) ([]string, map[string]any) {
	t.Helper()
	r := want(t, c.get(t, "/api/v1/task-report?"+query.Encode()), http.StatusOK, "the task report "+query.Encode())
	var names []string
	for _, each := range list(t, r, "tasks") {
		names = append(names, strings.Fields(each.(map[string]any)["text"].(string))[0])
	}
	return names, r.Body
}

func TestATaskReportPicksTasksByItsFilter(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "task-report")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Report"), h.namedPerson(t, org.org, "Ben Report"), h.namedPerson(t, org.org, "Carl Report")
	ann, carl := api.as(t, annID, org.org, slug), api.as(t, carlID, org.org, slug)

	day := func(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format(time.DateOnly) }
	alpha := newTree(t, owner, "TRA", "Alpha")
	beta := newTree(t, owner, "TRB", "Beta")
	plan := alpha.add(alpha.homeID, "Plan")
	secret := alpha.add(alpha.homeID, "Secret plans")
	ops := beta.add(beta.homeID, "Ops")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(annID)}, nil), http.StatusOK, "only ann sees the secret plans")
	h.settle(t)
	publishBody(t, owner, plan, todoDoc("The plan.",
		todo{text: "Overdue", assignee: annID, due: day(-1)},
		todo{text: "Today", assignee: benID, due: day(0)},
		todo{text: "Soon", assignee: annID, due: day(3)},
		todo{text: "Later", due: day(10)},
		todo{text: "Someday", assignee: annID},
		todo{text: "Shipped", assignee: annID, due: day(-1), done: true},
	))
	publishBody(t, owner, ops, todoDoc("Ops.", todo{text: "Beta", assignee: annID, due: day(0)}))
	publishBody(t, owner, secret, todoDoc("Secret.", todo{text: "Secret", assignee: annID, due: day(0)}))
	h.drained(t, org.org)

	for _, c := range []struct {
		what   string
		reader *client
		query  url.Values
		want   []string
	}{
		{"open tasks, soonest first, the same day by page", ann, url.Values{}, []string{"Overdue", "Beta", "Today", "Secret", "Soon", "Later", "Someday"}},
		{"a page closed to the reader leaves its tasks out", carl, url.Values{}, []string{"Overdue", "Beta", "Today", "Soon", "Later", "Someday"}},
		{"one space", ann, url.Values{"space": {"TRB"}}, []string{"Beta"}},
		{"the reader's own", ann, url.Values{"space": {"TRA"}, "assignee": {"me"}}, []string{"Overdue", "Secret", "Soon", "Someday"}},
		{"another reader's own", carl, url.Values{"assignee": {"me"}}, nil},
		{"one person", carl, url.Values{"assignee": {benID.String()}}, []string{"Today"}},
		{"nobody's", ann, url.Values{"assignee": {"none"}}, []string{"Later"}},
		{"overdue", ann, url.Values{"due": {"overdue"}}, []string{"Overdue"}},
		{"due today", ann, url.Values{"due": {"today"}, "space": {"TRA"}}, []string{"Today", "Secret"}},
		{"due this week", ann, url.Values{"due": {"week"}}, []string{"Beta", "Today", "Secret", "Soon"}},
		{"no due day", ann, url.Values{"due": {"none"}}, []string{"Someday"}},
		{"done", ann, url.Values{"state": {"done"}}, []string{"Shipped"}},
		{"open, then done", ann, url.Values{"state": {"all"}, "assignee": {"me"}, "space": {"TRA"}}, []string{"Overdue", "Secret", "Soon", "Someday", "Shipped"}},
		{"overdue whether done or not", ann, url.Values{"state": {"all"}, "due": {"overdue"}}, []string{"Overdue", "Shipped"}},
	} {
		got, _ := reportOf(t, c.reader, c.query)
		sameList(t, c.what, got, c.want...)
	}

	t.Run("a report says when it holds fewer than matched", func(t *testing.T) {
		got, body := reportOf(t, ann, url.Values{"limit": {"2"}})
		sameList(t, "the first two", got, "Overdue", "Beta")
		if body["truncated"] != true {
			t.Errorf("two of seven are not truncated: %v", body)
		}
		if _, body := reportOf(t, ann, url.Values{"limit": {"7"}}); body["truncated"] != false {
			t.Errorf("seven of seven are truncated: %v", body)
		}
	})

	t.Run("a report for one person names them", func(t *testing.T) {
		_, body := reportOf(t, carl, url.Values{"assignee": {benID.String()}})
		if body["assigneeName"] != "Ben Report" {
			t.Errorf("the report names %v", body["assigneeName"])
		}
		if _, body := reportOf(t, carl, url.Values{"assignee": {"me"}}); body["assigneeName"] != "" {
			t.Errorf("the reader's own report names %v", body["assigneeName"])
		}
		got, body := reportOf(t, ann, url.Values{"assignee": {org.org.String()}})
		if len(got) != 0 || body["assigneeName"] != "" {
			t.Errorf("somebody not in the organization has %v, named %v", got, body["assigneeName"])
		}
	})

	t.Run("a task ticked off moves from open to done", func(t *testing.T) {
		ids := taskIDsOf(t, owner, plan)
		want(t, ann.patch(t, "/api/v1/pages/"+plan+"/tasks/"+ids["Soon"], map[string]any{"done": true}), http.StatusOK, "ann ticks soon off")
		got, _ := reportOf(t, ann, url.Values{"assignee": {"me"}, "space": {"TRA"}})
		sameList(t, "ann's open tasks", got, "Overdue", "Secret", "Someday")
		got, _ = reportOf(t, ann, url.Values{"assignee": {"me"}, "state": {"done"}})
		if len(got) != 2 || got[0] != "Soon" {
			t.Errorf("ann's done tasks, the latest first, are %v", got)
		}
	})

	t.Run("a filter a report does not offer is refused in words", func(t *testing.T) {
		for field, query := range map[string]string{
			"due":      "due=later",
			"state":    "state=half",
			"assignee": "assignee=ann",
			"limit":    "limit=101",
		} {
			r := want(t, ann.get(t, "/api/v1/task-report?"+query), http.StatusUnprocessableEntity, query)
			fields, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any)
			if msg, _ := fields[field].(string); !strings.HasSuffix(msg, ".") {
				t.Errorf("%s is refused with %v", query, r.Body)
			}
		}
		want(t, ann.get(t, "/api/v1/task-report?limit=none"), http.StatusUnprocessableEntity, "a limit that is no number")
		want(t, ann.get(t, "/api/v1/task-report?space=NOPE"), http.StatusNotFound, "a space that is not there")
	})
}
