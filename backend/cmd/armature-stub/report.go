package main

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The two of Armature's reports Stator's chart block draws: chart, here as
// a count grouped by one field, and created_vs_resolved, a day by day count
// over a window that ends today.

var categoryLabels = map[string]string{"todo": "To do", "in_progress": "In progress", "done": "Done"}

func (s *stub) report(c *call) {
	q := c.r.URL.Query()
	// Armature takes an issue key for its project too.
	key, _, _ := strings.Cut(strings.ToUpper(c.r.PathValue("projectKey")), "-")
	pr := c.tenant.project(key)
	if pr == nil || !c.person.sees(pr) {
		refuse(c.w, http.StatusNotFound, "not_found", "That project was not found.")
		return
	}
	parsed, err := parseQuery(q.Get("q"))
	var bad *queryError
	if errors.As(err, &bad) {
		pos := bad.Pos
		respond(c.w, http.StatusBadRequest, map[string]apiError{"error": {Code: "bad_query", Message: bad.Msg, Position: &pos}})
		return
	}
	var counted []*issue
	for _, is := range parsed.run(c.tenant, c.person) {
		if is.Project == pr {
			counted = append(counted, is)
		}
	}
	switch c.r.PathValue("kind") {
	case "chart":
		s.chart(c, q.Get("groupBy"), counted)
	case "created_vs_resolved":
		days := 30
		if raw := q.Get("days"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 365 {
				refuseField(c.w, "days", "Count 1 to 365 days.")
				return
			}
			days = n
		}
		createdResolved(c, days, counted)
	default:
		refuse(c.w, http.StatusNotFound, "not_found", "That report was not found.")
	}
}

func (s *stub) chart(c *call, groupBy string, counted []*issue) {
	label := map[string]func(*issue) (string, string){
		"status":         func(is *issue) (string, string) { return is.Status.Name, is.Status.Category },
		"statusCategory": func(is *issue) (string, string) { return categoryLabels[is.Status.Category], is.Status.Category },
		"type":           func(is *issue) (string, string) { return is.Type.Name, "" },
		"priority":       func(is *issue) (string, string) { return is.Priority, "" },
		"assignee": func(is *issue) (string, string) {
			if is.Assignee == nil {
				return "Unassigned", ""
			}
			return is.Assignee.Display, ""
		},
	}[groupBy]
	if label == nil {
		refuseField(c.w, "groupBy", "Group by status, statusCategory, type, priority or assignee.")
		return
	}
	type group struct {
		label, category string
		value           int
	}
	var groups []*group
	for _, is := range counted {
		l, cat := label(is)
		i := slices.IndexFunc(groups, func(g *group) bool { return g.label == l })
		if i < 0 {
			groups = append(groups, &group{label: l, category: cat})
			i = len(groups) - 1
		}
		groups[i].value++
	}
	slices.SortStableFunc(groups, func(a, b *group) int { return cmp.Or(b.value-a.value, strings.Compare(a.label, b.label)) })
	out := []map[string]any{}
	for _, g := range groups {
		v := map[string]any{"label": g.label, "value": g.value, "parts": []any{}}
		if g.category != "" {
			v["category"] = g.category
		}
		out = append(out, v)
	}
	respond(c.w, http.StatusOK, map[string]any{"groupBy": groupBy, "measure": "count", "total": len(counted), "groups": out})
}

func createdResolved(c *call, days int, counted []*issue) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := []map[string]any{}
	createdTotal, resolvedTotal := 0, 0
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		created, resolved := 0, 0
		for _, is := range counted {
			if is.CreatedAt.UTC().Truncate(24 * time.Hour).Equal(day) {
				created++
			}
			if is.ResolvedAt != nil && is.ResolvedAt.UTC().Truncate(24*time.Hour).Equal(day) {
				resolved++
			}
		}
		createdTotal += created
		resolvedTotal += resolved
		out = append(out, map[string]any{"day": day, "created": created, "resolved": resolved, "createdTotal": createdTotal, "resolvedTotal": resolvedTotal})
	}
	respond(c.w, http.StatusOK, map[string]any{"days": out, "window": days})
}
