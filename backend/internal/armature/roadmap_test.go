package armature

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCheckRoadmapRefusesWhatArmatureWould(t *testing.T) {
	if err := CheckRoadmap(RoadmapInput{Project: "CP", Query: "project = CP", GroupBy: RoadmapByTeam}); err != nil {
		t.Errorf("a good roadmap was refused: %v", err)
	}
	for field, in := range map[string]RoadmapInput{
		"project": {Project: "C", Query: "project = CP", GroupBy: RoadmapByEpic},
		"q":       {Project: "CP", Query: "", GroupBy: RoadmapByEpic},
		"groupBy": {Project: "CP", Query: "project = CP", GroupBy: "sprint"},
	} {
		var refused *FieldError
		if err := CheckRoadmap(in); !errors.As(err, &refused) || refused.Field != field || refused.Message == "" {
			t.Errorf("%+v: %v, want a sentence on %s", in, err, field)
		}
	}
}

// plan reads a plan as Armature writes one: an initiative over an epic over
// two stories, a second epic, and issues outside any epic.
func plan(t *testing.T) []planItem {
	t.Helper()
	item := func(key, summary, kind string, level int, start, due, team string, derived bool, children ...string) string {
		days := ""
		if start != "" {
			days += fmt.Sprintf(`"start":"%sT00:00:00Z",`, start)
		}
		if due != "" {
			days += fmt.Sprintf(`"due":"%sT00:00:00Z",`, due)
		}
		teamOf := "null"
		if team != "" {
			teamOf = fmt.Sprintf(`{"id":%q,"name":%q}`, uuid.NewString(), team)
		}
		return fmt.Sprintf(`{"issue":{"key":%q,"summary":%q,"type":{"name":%q,"icon":%q,"level":%d},"status":{"name":"To do","category":"todo"},"team":%s},%s"derived":%t,"children":[%s]}`,
			key, summary, kind, strings.ToLower(kind), level, teamOf, days, derived, strings.Join(children, ","))
	}
	raw := "[" + strings.Join([]string{
		item("CP-1", "Grow abroad", "Initiative", 2, "2026-01-05", "2026-06-30", "", false,
			item("CP-2", "Launch in France", "Epic", 1, "2026-02-02", "2026-03-20", "Growth", true,
				item("CP-3", "Translate the shop", "Story", 0, "2026-02-02", "2026-02-27", "Growth", false),
				item("CP-10", "Hire support", "Story", 0, "2026-03-02", "2026-03-20", "", false),
				item("CP-4", "Pick a carrier", "Task", 0, "", "", "Growth", false),
			),
		),
		item("CP-5", "Rebuild checkout", "Epic", 1, "", "", "Platform", false),
		item("CP-6", "Rotate keys", "Task", 0, "", "2026-01-15", "Platform", false),
		item("CP-7", "Old chore", "Task", 0, "", "", "", false),
	}, ",") + "]"
	var out []planItem
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func names(r *Roadmap) string {
	var parts []string
	for _, g := range r.Groups {
		var keys []string
		for _, bar := range g.Rows {
			keys = append(keys, bar.Key)
		}
		head := g.Name
		if g.Epic != nil {
			head += "@" + g.Epic.Key
		}
		parts = append(parts, head+"["+strings.Join(keys, " ")+"]")
	}
	return strings.Join(parts, " ")
}

func TestARoadmapPutsEachIssueUnderItsNearestEpic(t *testing.T) {
	all := []string{"CP-1", "CP-2", "CP-3", "CP-4", "CP-5", "CP-6", "CP-7", "CP-10"}
	r := buildRoadmap(plan(t), all, RoadmapByEpic, "https://armature.test")
	if got := names(r); got != "Launch in France@CP-2[CP-3 CP-10] [CP-1 CP-6]" {
		t.Errorf("grouped by epic: %s", got)
	}
	// CP-4 and CP-7 have no days; CP-5 is an epic with neither days nor rows;
	// CP-1 is an initiative, a row like any issue outside an epic.
	if r.Unscheduled != 3 || *r.From != "2026-01-05" || *r.To != "2026-06-30" {
		t.Errorf("unscheduled %d, from %v to %v", r.Unscheduled, *r.From, *r.To)
	}
	epic := r.Groups[0].Epic
	if !epic.Derived || *epic.Start != "2026-02-02" || epic.URL != "https://armature.test/issues/CP-2" || epic.Type.Icon != "epic" {
		t.Errorf("the epic's own bar: %+v", epic)
	}
	if bar := r.Groups[1].Rows[1]; bar.Start != nil || *bar.Due != "2026-01-15" {
		t.Errorf("an issue with a due day alone keeps it alone: %+v", bar)
	}
}

func TestARoadmapShowsAnEpicTheQueryLeftOutAboveTheIssuesItMatched(t *testing.T) {
	r := buildRoadmap(plan(t), []string{"CP-3"}, RoadmapByEpic, "")
	if got := names(r); got != "Launch in France@CP-2[CP-3]" || *r.From != "2026-02-02" || *r.To != "2026-03-20" {
		t.Errorf("one story of an epic: %s, from %v to %v", got, *r.From, *r.To)
	}
	only := buildRoadmap(plan(t), []string{"CP-2", "CP-5"}, RoadmapByEpic, "")
	if got := names(only); got != "Launch in France@CP-2[]" || only.Unscheduled != 1 {
		t.Errorf("the epics alone: %s, %d unscheduled", got, only.Unscheduled)
	}
}

func TestARoadmapByTeamListsTeamsByNameAndTheRestLast(t *testing.T) {
	r := buildRoadmap(plan(t), []string{"CP-2", "CP-3", "CP-6", "CP-10"}, RoadmapByTeam, "")
	if got := names(r); got != "Growth[CP-2 CP-3] Platform[CP-6] [CP-10]" {
		t.Errorf("grouped by team: %s", got)
	}
	none := buildRoadmap(plan(t), nil, RoadmapByTeam, "")
	if len(none.Groups) != 0 || none.From != nil || none.To != nil {
		t.Errorf("nothing matched draws nothing: %+v", none)
	}
}

func TestARoadmapStopsAtItsRowLimit(t *testing.T) {
	var items []planItem
	var keys []string
	for i := range MaxRoadmapRows + 7 {
		var it planItem
		it.Issue.Key = fmt.Sprintf("CP-%d", i+1)
		day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		it.Due = &day
		items = append(items, it)
		keys = append(keys, it.Issue.Key)
	}
	r := buildRoadmap(items, keys, RoadmapByTeam, "")
	if len(r.Groups[0].Rows) != MaxRoadmapRows || r.Hidden != 7 || r.Groups[0].Rows[8].Key != "CP-9" || r.Groups[0].Rows[9].Key != "CP-10" {
		t.Errorf("%d rows, %d hidden", len(r.Groups[0].Rows), r.Hidden)
	}
}

func TestEachRoadmapIsCachedApart(t *testing.T) {
	token := uuid.New()
	base := RoadmapInput{Project: "CP", Query: "project = CP", GroupBy: RoadmapByEpic}
	seen := map[string]bool{RoadmapField(token, base): true}
	for _, other := range []RoadmapInput{
		{Project: "SEC", Query: base.Query, GroupBy: base.GroupBy},
		{Project: base.Project, Query: "project = SEC", GroupBy: base.GroupBy},
		{Project: base.Project, Query: base.Query, GroupBy: RoadmapByTeam},
	} {
		if seen[RoadmapField(token, other)] {
			t.Errorf("%+v shares a cache field with another roadmap", other)
		}
		seen[RoadmapField(token, other)] = true
	}
	chart := ChartInput{Project: base.Project, Query: base.Query, GroupBy: string(base.GroupBy)}
	if seen[ChartField(token, chart)] || RoadmapField(uuid.New(), base) == RoadmapField(token, base) {
		t.Error("a roadmap shares a cache field with a chart, or two people share one")
	}
}
