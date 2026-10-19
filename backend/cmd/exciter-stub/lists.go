package main

import (
	"cmp"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// projectNames are the names of the projects the fixed requirements are in;
// a project a suite makes is named by its key.
var projectNames = map[string]string{"CHK": "Checkout", "SEC": "Security"}

func projectName(key string) string { return cmp.Or(projectNames[key], key) }

const (
	maxGridLimit    = 500
	defaultGrid     = 20
	maxSuggestions  = 20
	defaultSuggests = 8
)

// statementOf is a requirement's statement in Exciter's schema. CHK-R1's
// holds the nodes and marks a block draws, ones it shows as text, and a
// callout, which stands for a node a later Exciter may add.
func statementOf(r *requirement) map[string]any {
	text := func(s string, marks ...map[string]any) map[string]any {
		n := map[string]any{"type": "text", "text": s}
		if len(marks) > 0 {
			ms := make([]any, len(marks))
			for i, m := range marks {
				ms[i] = m
			}
			n["marks"] = ms
		}
		return n
	}
	para := func(content ...any) map[string]any { return map[string]any{"type": "paragraph", "content": content} }
	if r.Key != "CHK-R1" {
		return map[string]any{"type": "doc", "content": []any{para(text(r.Title + "."))}}
	}
	bold := map[string]any{"type": "bold"}
	link := map[string]any{"type": "link", "attrs": map[string]any{"href": "https://shop.example.test/basket", "target": "_blank", "rel": "noopener noreferrer nofollow", "class": nil, "title": nil}}
	term := map[string]any{"type": "term", "attrs": map[string]any{"termId": idOf("term", "basket").String()}}
	item := func(s string) map[string]any {
		return map[string]any{"type": "listItem", "content": []any{para(text(s))}}
	}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": 2}, "content": []any{text("Keeping the basket")}},
		para(text("The "), text("basket", term), text(" keeps its items for "), text("30 days", bold), text(", as the "), text("shop", link), text(" says.")),
		map[string]any{"type": "bulletList", "content": []any{item("Signed in"), item("Signed out, on the same device")}},
		map[string]any{"type": "codeBlock", "attrs": map[string]any{"language": "text"}, "content": []any{text("basket.ttl = 30d")}},
		map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": idOf("attachment", "basket").String(), "alt": "The basket after a month", "width": nil}},
		map[string]any{"type": "callout", "content": []any{para(text("A node Stator does not know."))}},
	}}
}

// rationaleOf is CHK-R1's reason, and none for the rest.
func rationaleOf(r *requirement) any {
	if r.Key != "CHK-R1" {
		return nil
	}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Most customers come back within a month."}}},
	}}
}

func ownerOf(t *tenant, r *requirement) any {
	if r.Owner == "" {
		return nil
	}
	return map[string]any{"id": idOf(t.Name, "person", r.Owner), "name": display(r.Owner)}
}

func statusOf(t *tenant, r *requirement) any {
	if r.Status == nil {
		return nil
	}
	return map[string]any{"id": idOf(t.Name, "status", r.Status.Name), "name": r.Status.Name, "category": r.Status.Category}
}

// refView is a requirement in Exciter's RequirementRef shape.
func refView(t *tenant, r *requirement) map[string]any {
	return map[string]any{
		"id": r.ID, "key": r.Key, "projectKey": r.project(), "kind": "requirement", "title": r.Title,
		"revision": r.Revision, "status": statusOf(t, r), "trashed": r.TrashedAt != nil,
	}
}

func hitView(t *tenant, r *requirement) map[string]any {
	return map[string]any{"requirement": refView(t, r), "projectName": projectName(r.project()), "outline": strconv.Itoa(r.number())}
}

// rowView is a requirement in Exciter's GridRow shape.
func rowView(t *tenant, r *requirement) map[string]any {
	return map[string]any{
		"id": r.ID, "key": r.Key, "projectKey": r.project(), "kind": "requirement", "title": r.Title, "revision": r.Revision,
		"status": statusOf(t, r), "trashed": r.TrashedAt != nil, "outline": strconv.Itoa(r.number()), "type": nil,
		"owner": ownerOf(t, r), "priority": nil, "labels": []string{}, "attributes": map[string]any{}, "updatedAt": seeded,
		"can":          map[string]any{"edit": false, "transition": false, "delete": false, "link": false, "comment": true},
		"suspectLinks": 0, "quality": nil,
	}
}

// visible are the requirements person finds, in project when it is not empty,
// by number.
func (t *tenant) visible(person, project string) []*requirement {
	var out []*requirement
	for _, r := range t.requirements {
		if r.visibleTo(person) && (project == "" || r.project() == project) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b *requirement) int {
		return cmp.Or(cmp.Compare(a.project(), b.project()), cmp.Compare(a.number(), b.number()))
	})
	return out
}

// intParam reads a query parameter between low and high, def when absent.
func intParam(c *call, name string, def, low, high int) (int, bool) {
	raw := c.r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < low || n > high {
		respond(c.w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
			"code": "validation", "message": "Check the fields.", "fields": map[string]string{name: "Use " + strconv.Itoa(low) + " to " + strconv.Itoa(high) + "."},
		}})
		return 0, false
	}
	return n, true
}

func (s *stub) quickSearch(c *call) {
	limit, ok := intParam(c, "limit", defaultSuggests, 1, maxSuggestions)
	if !ok {
		return
	}
	q := strings.ToLower(strings.TrimSpace(c.r.URL.Query().Get("q")))
	hits := []any{}
	for _, r := range c.tenant.visible(c.person, strings.ToUpper(c.r.URL.Query().Get("project"))) {
		if len(hits) == limit {
			break
		}
		if q != "" && matchesStart(r, q) {
			hits = append(hits, hitView(c.tenant, r))
		}
	}
	respond(c.w, http.StatusOK, map[string]any{"requirements": hits})
}

// matchesStart is quick search's test: the key or a word of the title starts with q.
func matchesStart(r *requirement, q string) bool {
	if strings.HasPrefix(strings.ToLower(r.Key), q) || strings.HasPrefix(strings.ToLower(r.Title), q) {
		return true
	}
	for _, word := range strings.Fields(strings.ToLower(r.Title)) {
		if strings.HasPrefix(word, q) {
			return true
		}
	}
	return false
}

func (s *stub) listProjects(c *call) {
	seen := map[string]bool{}
	projects := []any{}
	for _, r := range c.tenant.visible(c.person, "") {
		key := r.project()
		if seen[key] {
			continue
		}
		seen[key] = true
		projects = append(projects, map[string]any{
			"id": idOf(c.tenant.Name, "project", key), "key": key, "name": projectName(key), "description": "",
			"settings": map[string]any{"defaultTypeId": nil, "workflowId": nil, "requireRevisionComment": false, "signedApprovals": false,
				"library": false, "homeDashboardId": nil, "language": "en"},
			"template": "blank", "formerKeys": []string{}, "requirements": len(c.tenant.visible(c.person, key)),
			"createdAt": seeded, "updatedAt": seeded, "archivedAt": nil, "archivedByName": "",
			"can":   map[string]any{"view": true, "comment": true, "edit": false, "approve": false, "administer": false, "purge": false},
			"roles": []string{"viewer"}, "watching": false,
		})
	}
	respond(c.w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *stub) listRequirements(c *call) {
	project := strings.ToUpper(c.r.PathValue("projectKey"))
	if len(c.tenant.visible(c.person, project)) == 0 {
		refuse(c.w, http.StatusNotFound, "not_found", "That project was not found.")
		return
	}
	limit, ok := intParam(c, "limit", defaultGrid, 1, maxGridLimit)
	if !ok {
		return
	}
	offset, ok := intParam(c, "offset", 0, 0, 1<<30)
	if !ok {
		return
	}
	q, bad := parseQuery(c.r.URL.Query().Get("query"))
	if bad != nil {
		respond(c.w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "bad_query", "message": bad.message, "position": bad.position}})
		return
	}
	var found []*requirement
	for _, r := range c.tenant.visible(c.person, project) {
		if q.matches(r, c.person) {
			found = append(found, r)
		}
	}
	q.order(found)
	rows := []any{}
	for i := offset; i < len(found) && i < offset+limit; i++ {
		rows = append(rows, rowView(c.tenant, found[i]))
	}
	respond(c.w, http.StatusOK, map[string]any{"rows": rows, "total": len(found), "limit": limit, "offset": offset})
}

// The stub's part of Exciter's query language: conditions on key, title,
// status and owner joined by AND, and ORDER BY key, title or revision.

var (
	fields     = []string{"key", "title", "status", "owner"}
	operators  = []string{"=", "!=", "~"}
	keywords   = []string{"AND", "ORDER BY"}
	orderables = []string{"key", "title", "revision"}
	andShape   = regexp.MustCompile(`(?i)\s+AND\s+`)
	orderShape = regexp.MustCompile(`(?i)\s*\bORDER\s+BY\s+`)
	condShape  = regexp.MustCompile(`^([A-Za-z]+)\s*(!=|=|~)\s*(.*)$`)
)

type condition struct{ field, op, value string }

type parsed struct {
	conds   []condition
	orderBy string
	desc    bool
}

type badQuery struct {
	message  string
	position int
}

// at is the 1-based character position of byte offset i of q.
func at(q string, i int) int { return utf8.RuneCountInString(q[:i]) + 1 }

func parseQuery(q string) (*parsed, *badQuery) {
	p := &parsed{}
	where := q
	if loc := orderShape.FindStringIndex(q); loc != nil {
		where = q[:loc[0]]
		order := strings.Fields(q[loc[1]:])
		if len(order) == 0 || len(order) > 2 || !slices.Contains(orderables, strings.ToLower(order[0])) {
			return nil, &badQuery{"Order by key, title or revision.", at(q, loc[1])}
		}
		p.orderBy = strings.ToLower(order[0])
		if len(order) == 2 {
			switch strings.ToUpper(order[1]) {
			case "DESC":
				p.desc = true
			case "ASC":
			default:
				return nil, &badQuery{"Write ASC or DESC after the field to order by, or nothing.", at(q, loc[1]+strings.Index(q[loc[1]:], order[1]))}
			}
		}
	}
	if strings.TrimSpace(where) == "" {
		return p, nil
	}
	start := 0
	pieces := andShape.FindAllStringIndex(where, -1)
	for i := 0; i <= len(pieces); i++ {
		end := len(where)
		if i < len(pieces) {
			end = pieces[i][0]
		}
		piece := where[start:end]
		lead := len(piece) - len(strings.TrimLeft(piece, " \t"))
		m := condShape.FindStringSubmatch(strings.TrimSpace(piece))
		switch {
		case m == nil:
			return nil, &badQuery{"Write a condition such as status = Approved.", at(where, start+lead)}
		case !slices.Contains(fields, strings.ToLower(m[1])):
			return nil, &badQuery{"There is no field " + m[1] + ". Use key, title, status or owner.", at(where, start+lead)}
		case strings.TrimSpace(m[3]) == "":
			return nil, &badQuery{"Write a value after " + m[2] + ".", at(where, end)}
		}
		value := strings.TrimSpace(m[3])
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		p.conds = append(p.conds, condition{strings.ToLower(m[1]), m[2], value})
		if i < len(pieces) {
			start = pieces[i][1]
		}
	}
	return p, nil
}

func (p *parsed) matches(r *requirement, person string) bool {
	for _, c := range p.conds {
		var have string
		switch c.field {
		case "key":
			have = r.Key
		case "title":
			have = r.Title
		case "status":
			if r.Status != nil {
				have = r.Status.Name
			}
		case "owner":
			have = r.Owner
		}
		want := c.value
		if c.field == "owner" && strings.EqualFold(want, "currentUser()") {
			want = person
		}
		var hit bool
		if c.op == "~" {
			hit = have != "" && strings.Contains(strings.ToLower(have), strings.ToLower(want))
		} else {
			hit = have != "" && strings.EqualFold(have, want)
		}
		// A negation keeps what has nothing there, as Exciter's does.
		if c.op == "!=" {
			hit = !hit
		}
		if !hit {
			return false
		}
	}
	return true
}

func (p *parsed) order(rs []*requirement) {
	if p.orderBy == "" {
		return
	}
	slices.SortStableFunc(rs, func(a, b *requirement) int {
		var n int
		switch p.orderBy {
		case "key":
			n = cmp.Or(cmp.Compare(a.project(), b.project()), cmp.Compare(a.number(), b.number()))
		case "title":
			n = cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		case "revision":
			n = cmp.Compare(a.Revision, b.Revision)
		}
		if p.desc {
			return -n
		}
		return n
	})
}

// suggest completes the word at the caret: a field, an operator, a value of
// the field before it, or a keyword after a whole condition.
func (s *stub) suggest(c *call) {
	project := strings.ToUpper(c.r.PathValue("projectKey"))
	if len(c.tenant.visible(c.person, project)) == 0 {
		refuse(c.w, http.StatusNotFound, "not_found", "That project was not found.")
		return
	}
	limit, ok := intParam(c, "limit", defaultSuggests, 1, maxSuggestions)
	if !ok {
		return
	}
	q := []rune(c.r.URL.Query().Get("q"))
	caret, ok := intParam(c, "at", len(q), 0, len(q))
	if !ok {
		return
	}
	before := string(q[:caret])
	from := strings.LastIndexAny(before, " \t(") + 1
	prefix := before[from:]
	prev := strings.Fields(before[:from])
	last := func(n int) string {
		if len(prev) < n {
			return ""
		}
		return prev[len(prev)-n]
	}
	var (
		slot, field string
		candidates  []string
	)
	switch {
	case len(prev) == 0 || strings.EqualFold(last(1), "AND"):
		slot, candidates = "field", fields
	case strings.EqualFold(last(1), "BY") && strings.EqualFold(last(2), "ORDER"):
		slot, candidates = "field", orderables
	case slices.Contains(fields, strings.ToLower(last(1))):
		slot, candidates = "operator", operators
	case slices.Contains(operators, last(1)):
		slot, field = "value", strings.ToLower(last(2))
		candidates = s.values(c, project, field)
	default:
		slot, candidates = "keyword", keywords
	}
	texts := []string{}
	words := []any{}
	for _, w := range candidates {
		if len(texts) == limit {
			break
		}
		if strings.HasPrefix(strings.ToLower(w), strings.ToLower(prefix)) {
			texts = append(texts, w)
			words = append(words, map[string]any{"text": w, "label": w, "detail": slot})
		}
	}
	hits := []any{}
	if slot == "value" && field == "key" {
		for _, r := range c.tenant.visible(c.person, project) {
			if len(hits) < limit && strings.HasPrefix(strings.ToLower(r.Key), strings.ToLower(prefix)) {
				hits = append(hits, hitView(c.tenant, r))
			}
		}
	}
	completion := map[string]any{"slot": slot, "prefix": prefix, "from": utf8.RuneCountInString(before[:from]), "to": caret, "quoted": false, "words": texts}
	if field != "" {
		completion["field"] = field
	}
	respond(c.w, http.StatusOK, map[string]any{"completion": completion, "words": words, "requirements": hits})
}

// values are the project's names for a field, as the reader may see them.
func (s *stub) values(c *call, project, field string) []string {
	var out []string
	add := func(v string) {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	if field == "owner" {
		add("currentUser()")
	}
	for _, r := range c.tenant.visible(c.person, project) {
		switch field {
		case "key":
			add(r.Key)
		case "status":
			if r.Status != nil {
				add(r.Status.Name)
			}
		case "owner":
			add(r.Owner)
		}
	}
	return out
}
