package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// The part of Armature's NQL that Stator sends: conditions on key, project,
// statusCategory and assignee joined by AND, then ORDER BY. Anything else is
// refused as Armature refuses a query it cannot read: with a position.

type queryError struct {
	Pos int
	Msg string
}

func (e *queryError) Error() string { return e.Msg }

func errAt(pos int, format string, args ...any) *queryError {
	return &queryError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

type token struct {
	kind string
	text string
	pos  int
}

func lex(q string) ([]token, error) {
	runes := []rune(q)
	var out []token
	for i := 0; i < len(runes); {
		r := runes[i]
		pos := i + 1
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{"open", "(", pos})
			i++
		case r == ')':
			out = append(out, token{"close", ")", pos})
			i++
		case r == ',':
			out = append(out, token{"comma", ",", pos})
			i++
		case r == '=':
			out = append(out, token{"op", "=", pos})
			i++
		case r == '!':
			if i+1 < len(runes) && runes[i+1] == '=' {
				out = append(out, token{"op", "!=", pos})
				i += 2
				continue
			}
			return nil, errAt(pos, "\"!\" on its own means nothing. Write !=.")
		case r == '"' || r == '\'':
			end := i + 1
			for end < len(runes) && runes[end] != r {
				end++
			}
			if end == len(runes) {
				return nil, errAt(pos, "The quote opened at character %d is never closed.", pos)
			}
			out = append(out, token{"string", string(runes[i+1 : end]), pos})
			i = end + 1
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			end := i
			for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_' || runes[end] == '-') {
				end++
			}
			out = append(out, token{"word", string(runes[i:end]), pos})
			i = end
		default:
			return nil, errAt(pos, "%q cannot be used here.", string(r))
		}
	}
	return out, nil
}

type condition struct {
	field  string
	negate bool
	values []string
}

type ordering struct {
	field string
	desc  bool
}

type query struct {
	where []condition
	order []ordering
}

var queryFields = []string{"key", "project", "statuscategory", "assignee"}
var orderFields = []string{"key", "created", "updated", "priority"}

type parser struct {
	toks []token
	at   int
	end  int
}

func (p *parser) peek() *token {
	if p.at < len(p.toks) {
		return &p.toks[p.at]
	}
	return nil
}

func (p *parser) word(w string) bool {
	t := p.peek()
	return t != nil && t.kind == "word" && strings.EqualFold(t.text, w)
}

func parseQuery(q string) (*query, error) {
	toks, err := lex(q)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, end: len([]rune(q)) + 1}
	out := &query{}
	for p.peek() != nil && !p.word("order") {
		if len(out.where) > 0 {
			if !p.word("and") {
				return nil, errAt(p.peek().pos, "%q was not expected here. Join conditions with AND.", p.peek().text)
			}
			p.at++
		}
		c, err := p.condition()
		if err != nil {
			return nil, err
		}
		out.where = append(out.where, c)
	}
	if p.word("order") {
		p.at++
		if !p.word("by") {
			return nil, errAt(p.pos(), "ORDER is followed by BY, then the field to sort by.")
		}
		p.at++
		for {
			t := p.peek()
			if t == nil || t.kind != "word" || !slices.Contains(orderFields, strings.ToLower(t.text)) {
				return nil, errAt(p.pos(), "Sort by key, created, updated or priority.")
			}
			o := ordering{field: strings.ToLower(t.text)}
			p.at++
			if p.word("desc") || p.word("asc") {
				o.desc = p.word("desc")
				p.at++
			}
			out.order = append(out.order, o)
			if next := p.peek(); next == nil || next.kind != "comma" {
				break
			}
			p.at++
		}
	}
	if t := p.peek(); t != nil {
		return nil, errAt(t.pos, "%q was not expected here.", t.text)
	}
	return out, nil
}

func (p *parser) pos() int {
	if t := p.peek(); t != nil {
		return t.pos
	}
	return p.end
}

func (p *parser) condition() (condition, error) {
	t := p.peek()
	if t.kind != "word" || !slices.Contains(queryFields, strings.ToLower(t.text)) {
		return condition{}, errAt(t.pos, "%q is not a field. Use key, project, statusCategory or assignee.", t.text)
	}
	c := condition{field: strings.ToLower(t.text)}
	p.at++
	op := p.peek()
	switch {
	case op == nil:
		return c, errAt(p.end, "The query ends where an operator was expected after %s.", t.text)
	case op.kind == "op":
		c.negate = op.text == "!="
		if c.negate && c.field != "statuscategory" {
			return c, errAt(op.pos, "!= works on statusCategory only here.")
		}
		p.at++
		v, err := p.value(c.field)
		if err != nil {
			return c, err
		}
		c.values = []string{v}
	case op.kind == "word" && strings.EqualFold(op.text, "in"):
		if c.field != "key" && c.field != "project" {
			return c, errAt(op.pos, "IN works on key and project only here.")
		}
		p.at++
		if open := p.peek(); open == nil || open.kind != "open" {
			return c, errAt(p.pos(), "IN is followed by a list in brackets, such as IN (a, b).")
		}
		p.at++
		for {
			v, err := p.value(c.field)
			if err != nil {
				return c, err
			}
			c.values = append(c.values, v)
			next := p.peek()
			if next == nil {
				return c, errAt(p.end, "The list is missing its closing bracket.")
			}
			p.at++
			if next.kind == "close" {
				break
			}
			if next.kind != "comma" {
				return c, errAt(next.pos, "Separate the values of a list with commas.")
			}
		}
	default:
		return c, errAt(op.pos, "%q is not an operator. Use =, != or IN.", op.text)
	}
	return c, nil
}

func (p *parser) value(field string) (string, error) {
	t := p.peek()
	if t == nil {
		return "", errAt(p.end, "The query ends where a value was expected.")
	}
	if t.kind != "word" && t.kind != "string" {
		return "", errAt(t.pos, "A value was expected here.")
	}
	p.at++
	if field == "assignee" {
		if !strings.EqualFold(t.text, "currentUser") {
			return "", errAt(t.pos, "Compare assignee with currentUser() here.")
		}
		open, shut := p.peek(), (*token)(nil)
		if open != nil && open.kind == "open" {
			p.at++
			shut = p.peek()
		}
		if shut == nil || shut.kind != "close" {
			return "", errAt(p.pos(), "currentUser is followed by ().")
		}
		p.at++
		return "currentUser()", nil
	}
	if field == "statuscategory" {
		v := strings.ReplaceAll(strings.ToLower(t.text), " ", "_")
		if v != "todo" && v != "in_progress" && v != "done" {
			return "", errAt(t.pos, "%q is not a status category. Use todo, in_progress or done.", t.text)
		}
		return v, nil
	}
	return strings.ToUpper(t.text), nil
}

// run answers a parsed query for a person from what they may see.
func (q *query) run(t *tenant, p *person) []*issue {
	var out []*issue
	for _, is := range t.visible(p) {
		if q.matches(is, p) {
			out = append(out, is)
		}
	}
	for i := len(q.order) - 1; i >= 0; i-- {
		o := q.order[i]
		sort.SliceStable(out, func(a, b int) bool {
			less, more := compare(out[a], out[b], o.field), compare(out[b], out[a], o.field)
			if o.desc {
				return more
			}
			return less
		})
	}
	return out
}

func (q *query) matches(is *issue, p *person) bool {
	for _, c := range q.where {
		var got string
		switch c.field {
		case "key":
			got = is.Key
		case "project":
			got = is.Project.Key
		case "statuscategory":
			got = is.Status.Category
		case "assignee":
			if is.Assignee == nil || is.Assignee.ID != p.ID {
				return false
			}
			continue
		}
		if slices.Contains(c.values, got) == c.negate {
			return false
		}
	}
	return true
}

var priorityRank = map[string]int{"lowest": 0, "low": 1, "medium": 2, "high": 3, "highest": 4}

func compare(a, b *issue, field string) bool {
	switch field {
	case "created":
		return a.CreatedAt.Before(b.CreatedAt)
	case "updated":
		return a.UpdatedAt.Before(b.UpdatedAt)
	case "priority":
		return priorityRank[a.Priority] < priorityRank[b.Priority]
	}
	return keyLess(a, b)
}
