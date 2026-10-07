package main

import (
	"net/http"
	"slices"
	"strings"
	"time"
)

// The plan a roadmap block draws (#52): the project's issues as a tree, each
// with its own start and due day or, failing those, the span of its
// children's. The tree is whole; matched names what the query selects.

type planItem struct {
	is       *issue
	start    *time.Time
	due      *time.Time
	derived  bool
	children []*planItem
}

func (s *stub) plan(c *call) {
	pr, matched, ok := scoped(c)
	if !ok {
		return
	}
	var mine []*issue
	for _, is := range c.tenant.visible(c.person) {
		if is.Project == pr {
			mine = append(mine, is)
		}
	}
	var grow func(parent *issue) []*planItem
	grow = func(parent *issue) []*planItem {
		out := []*planItem{}
		for _, is := range mine {
			top := is.Parent == nil || is.Parent.Project != pr
			if (parent == nil && top) || (parent != nil && is.Parent == parent) {
				out = append(out, span(&planItem{is: is, start: is.StartDate, due: is.DueDate, children: grow(is)}))
			}
		}
		return out
	}
	items := grow(nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	from, to := today, today
	unscheduled := 0
	var walk func(list []*planItem, depth int) []map[string]any
	walk = func(list []*planItem, depth int) []map[string]any {
		out := []map[string]any{}
		for _, it := range list {
			if it.start == nil && it.due == nil {
				unscheduled++
			}
			for _, d := range []*time.Time{it.start, it.due} {
				if d != nil && d.Before(from) {
					from = *d
				}
				if d != nil && d.After(to) {
					to = *d
				}
			}
			progress := map[string]int{"total": len(it.children), "done": 0, "inProgress": 0, "todo": 0}
			for _, ch := range it.children {
				progress[map[string]string{"done": "done", "in_progress": "inProgress", "todo": "todo"}[ch.is.Status.Category]]++
			}
			v := map[string]any{"issue": issueView(it.is), "depth": depth, "derived": it.derived, "progress": progress, "children": walk(it.children, depth+1)}
			if it.start != nil {
				v["start"] = it.start
			}
			if it.due != nil {
				v["due"] = it.due
			}
			out = append(out, v)
		}
		return out
	}
	tree := walk(items, 0)
	keys := []string{}
	for _, is := range matched {
		keys = append(keys, is.Key)
	}
	respond(c.w, http.StatusOK, map[string]any{
		"projectKey": pr.Key, "items": tree, "matched": keys, "from": from, "to": to,
		"dependencies": []any{}, "sprints": []any{}, "milestones": []any{}, "warnings": []any{}, "linkTypes": []any{},
		"load": map[string]any{"weeks": []any{}, "rows": []any{}}, "unscheduled": unscheduled, "unestimated": len(mine),
	})
}

// calendar answers a project's month as a calendar block reads it (#60):
// each issue with a start or a due day in the month, from the one to the
// other; Stator asks for no sprints, milestones or versions, so none are kept.
func (s *stub) calendar(c *call) {
	key, _, _ := strings.Cut(strings.ToUpper(c.r.PathValue("projectKey")), "-")
	pr := c.tenant.project(key)
	if pr == nil || !c.person.sees(pr) {
		refuse(c.w, http.StatusNotFound, "not_found", "That project was not found.")
		return
	}
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if raw := c.r.URL.Query().Get("month"); raw != "" {
		parsed, err := time.Parse("2006-01", raw)
		if err != nil {
			refuseField(c.w, "month", "Give the month as YYYY-MM.")
			return
		}
		first = parsed
	}
	next := first.AddDate(0, 1, 0)
	items := []map[string]any{}
	for _, is := range c.tenant.visible(c.person) {
		from, to := is.StartDate, is.DueDate
		if from == nil {
			from = to
		}
		if to == nil {
			to = from
		}
		if is.Project != pr || from == nil || !from.Before(next) || to.Before(first) {
			continue
		}
		items = append(items, map[string]any{
			"kind": "issue", "id": is.ID, "key": is.Key, "title": is.Summary,
			"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly),
			"done": is.Status.Category == "done", "category": is.Status.Category,
		})
	}
	respond(c.w, http.StatusOK, map[string]any{"month": map[string]any{
		"year": first.Year(), "month": int(first.Month()), "items": items, "truncated": false,
	}})
}

// span gives an item without a day of its own the span of its children's.
func span(it *planItem) *planItem {
	var days []time.Time
	for _, ch := range it.children {
		for _, d := range []*time.Time{ch.start, ch.due} {
			if d != nil {
				days = append(days, *d)
			}
		}
	}
	if len(days) == 0 {
		return it
	}
	if it.start == nil {
		first := slices.MinFunc(days, time.Time.Compare)
		it.start, it.derived = &first, true
	}
	if it.due == nil {
		last := slices.MaxFunc(days, time.Time.Compare)
		it.due, it.derived = &last, true
	}
	return it
}
