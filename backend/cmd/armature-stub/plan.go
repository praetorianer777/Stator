package main

import (
	"net/http"
	"slices"
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
