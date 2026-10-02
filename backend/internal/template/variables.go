package template

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// Kind is what shape a variable's value takes, as Armature's custom fields
// have kinds; person is ours, a member who may view the space.
type Kind string

const (
	Text   Kind = "text"
	Date   Kind = "date"
	Select Kind = "select"
	Person Kind = "person"
)

// Kinds is every kind, in the order the editor offers them.
var Kinds = []Kind{Text, Date, Select, Person}

const (
	// MaxVariables is how many blanks one template may have; the database
	// holds the same number.
	MaxVariables = 20
	// MaxLabelLength bounds the words the form shows for a variable.
	MaxLabelLength = 80
	// MaxTextLength is the longest text value, a line as in Armature's text
	// fields; a document is what the page is for.
	MaxTextLength = 500
	// MaxOptions and MaxOptionLength bound a select variable's choices.
	MaxOptions      = 50
	MaxOptionLength = 100
	// Today is the default that fills a date variable with the day the page
	// is made.
	Today = "today"
)

// Variable is one blank of a template: what the form asks for and where its
// answer goes.
type Variable struct {
	// Name is how the body's variable nodes and the title's braces name it.
	Name  string `json:"name"`
	Label string `json:"label"`
	Kind  Kind   `json:"kind"`
	// Options are a select variable's choices, in the order offered.
	Options []string `json:"options"`
	// Default fills a value the author leaves empty: words, a choice, a day,
	// or Today for a date; a person has none.
	Default string `json:"default"`
	// Required refuses a page without a value; an optional one left empty
	// becomes hint text, which publishing strips.
	Required bool `json:"required"`
}

var namePattern = regexp.MustCompile(document.VariableNamePattern)

// tokenPattern finds a name in braces in a title.
var tokenPattern = regexp.MustCompile(`\{([a-z][a-z0-9_]{0,39})\}`)

// dateName is DateToken without its braces, kept for the day the page is made.
var dateName = strings.Trim(DateToken, "{}")

// cleanVariables tidies a template's variables and refuses what a form could
// not ask for, in a sentence naming the variable.
func cleanVariables(in []Variable) ([]Variable, error) {
	if len(in) > MaxVariables {
		return nil, fieldError("variables", "A template has at most %d variables; take some out.", MaxVariables)
	}
	out := make([]Variable, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v.Name = strings.TrimSpace(v.Name)
		v.Label = strings.TrimSpace(v.Label)
		v.Default = strings.TrimSpace(v.Default)
		switch {
		case !namePattern.MatchString(v.Name):
			return nil, fieldError("variables", "Name each variable with lower case letters, digits and underscores, starting with a letter, such as customer_name; %q is not one.", v.Name)
		case v.Name == dateName:
			return nil, fieldError("variables", "The name date is kept for the day the page is made; choose another name for that variable.")
		case seen[v.Name]:
			return nil, fieldError("variables", "Two variables are called %s; give each its own name.", v.Name)
		}
		seen[v.Name] = true
		if v.Label == "" {
			v.Label = v.Name
		}
		if utf8.RuneCountInString(v.Label) > MaxLabelLength {
			return nil, fieldError("variables", "Keep the label of %s to %d characters.", v.Name, MaxLabelLength)
		}
		if !slices.Contains(Kinds, v.Kind) {
			return nil, fieldError("variables", "Make %s a text, date, choice or person variable.", v.Label)
		}
		options, err := cleanOptions(v)
		if err != nil {
			return nil, err
		}
		v.Options = options
		if v.Default != "" {
			switch v.Kind {
			case Person:
				return nil, fieldError("variables", "A person variable has no default; leave the default of %s empty.", v.Label)
			case Date:
				if v.Default != Today {
					if _, err := time.Parse(time.DateOnly, v.Default); err != nil {
						return nil, fieldError("variables", "The default of %s has to be a day such as 2026-01-31, or today.", v.Label)
					}
				}
			case Select:
				if !slices.Contains(v.Options, v.Default) {
					return nil, fieldError("variables", "The default of %s has to be one of its choices.", v.Label)
				}
			default:
				v.Default = strings.Join(strings.Fields(v.Default), " ")
				if utf8.RuneCountInString(v.Default) > MaxTextLength {
					return nil, fieldError("variables", "Keep the default of %s to %d characters.", v.Label, MaxTextLength)
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func cleanOptions(v Variable) ([]string, error) {
	if v.Kind != Select {
		return []string{}, nil
	}
	out := make([]string, 0, len(v.Options))
	seen := map[string]bool{}
	for _, option := range v.Options {
		option = strings.TrimSpace(option)
		if option == "" || seen[option] {
			continue
		}
		if utf8.RuneCountInString(option) > MaxOptionLength {
			return nil, fieldError("variables", "Keep each choice of %s to %d characters.", v.Label, MaxOptionLength)
		}
		seen[option] = true
		out = append(out, option)
	}
	switch {
	case len(out) == 0:
		return nil, fieldError("variables", "Give %s at least one choice.", v.Label)
	case len(out) > MaxOptions:
		return nil, fieldError("variables", "Give %s at most %d choices.", v.Label, MaxOptions)
	}
	return out, nil
}

// checkBody refuses a body that names a variable the template does not
// define, which no form would ever fill.
func checkBody(root document.Node, vars []Variable) error {
	for _, name := range variablesIn(root) {
		if !slices.ContainsFunc(vars, func(v Variable) bool { return v.Name == name }) {
			return fieldError("body", "The body has a blank for %s, which the template does not define; add the variable or take the blank out.", name)
		}
	}
	return nil
}

// variablesIn lists the names the body's variable nodes use, once each, in
// reading order.
func variablesIn(root document.Node) []string {
	var out []string
	var walk func(n document.Node, depth int)
	walk = func(n document.Node, depth int) {
		if depth > document.MaxDepth {
			return
		}
		if n.Type == document.NodeVariable {
			if name, _ := n.Attrs["name"].(string); !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
		for _, c := range n.Content {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// normalize reads one value for a variable, empty meaning none given. A
// person is checked against the space by the caller, who knows it.
func normalize(v Variable, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	switch v.Kind {
	case Text:
		value = strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(value) > MaxTextLength {
			return "", valueError(v, "Keep %s to %d characters.", v.Label, MaxTextLength)
		}
	case Date:
		day, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return "", valueError(v, "Give %s as a day such as 2026-01-31.", v.Label)
		}
		value = day.Format(time.DateOnly)
	case Select:
		if !slices.Contains(v.Options, value) {
			return "", valueError(v, "Choose %s from its list: %s.", v.Label, strings.Join(v.Options, ", "))
		}
	case Person:
		id, err := uuid.Parse(value)
		if err != nil {
			return "", valueError(v, "Pick %s from the people offered.", v.Label)
		}
		value = id.String()
	}
	return value, nil
}

// Resolve answers each variable's value for a new page: the one given, else
// its default, else empty. today is the day Today stands for. A value for a
// name the template lacks is refused, so a typing mistake is not dropped.
func Resolve(vars []Variable, values map[string]string, today string) (map[string]string, error) {
	for _, name := range sortedKeys(values) {
		if !slices.ContainsFunc(vars, func(v Variable) bool { return v.Name == name }) {
			return nil, fieldError("values."+name, "This template has no variable called %s; leave it out.", name)
		}
	}
	out := make(map[string]string, len(vars))
	for _, v := range vars {
		value, err := normalize(v, values[v.Name])
		if err != nil {
			return nil, err
		}
		if value == "" {
			value = v.Default
			if v.Kind == Date && value == Today {
				value = today
			}
		}
		if value == "" && v.Required {
			return nil, valueError(v, "Fill in %s; this template needs it.", v.Label)
		}
		out[v.Name] = value
	}
	return out, nil
}

// People are the names of the members a person variable may name, by id,
// read by the caller from the space the page goes in.
type People map[string]string

// Fill replaces every variable of a template's body with its value: words as
// text, a day as a date, a person as a mention, each with the variable's
// styles. A blank left empty becomes its label as hint text, which the
// author sees and publishing strips, so no placeholder reaches a reader.
func Fill(root document.Node, vars []Variable, values map[string]string, people People) document.Node {
	byName := make(map[string]Variable, len(vars))
	for _, v := range vars {
		byName[v.Name] = v
	}
	return document.Normalize(fill(root, byName, values, people, 0))
}

func fill(n document.Node, vars map[string]Variable, values map[string]string, people People, depth int) document.Node {
	if depth > document.MaxDepth || len(n.Content) == 0 {
		return n
	}
	content := make([]document.Node, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Type != document.NodeVariable {
			content = append(content, fill(c, vars, values, people, depth+1))
			continue
		}
		name, _ := c.Attrs["name"].(string)
		v, ok := vars[name]
		if !ok {
			continue
		}
		content = append(content, filled(v, values[name], people, c.Marks))
	}
	n.Content = content
	return n
}

func filled(v Variable, value string, people People, marks []document.Mark) document.Node {
	marks = slices.Clone(marks)
	switch {
	case value == "":
		if !slices.ContainsFunc(marks, func(m document.Mark) bool { return m.Type == "hint" }) {
			marks = append(marks, document.Mark{Type: "hint"})
		}
		return document.Node{Type: "text", Text: v.Label, Marks: marks}
	case v.Kind == Date:
		return document.Node{Type: document.NodeDate, Attrs: map[string]any{"date": value}, Marks: marks}
	case v.Kind == Person:
		return document.Node{Type: "mention", Marks: marks, Attrs: map[string]any{
			"id": value, "label": people[value], "mentionSuggestionChar": "@",
		}}
	}
	return document.Node{Type: "text", Text: value, Marks: marks}
}

// FillTitle puts each variable's value where the title names it in braces,
// a person by name and an empty value as nothing; braces naming no variable
// stay as typed.
func FillTitle(title string, vars []Variable, values map[string]string, people People) string {
	out := tokenPattern.ReplaceAllStringFunc(title, func(token string) string {
		name := token[1 : len(token)-1]
		i := slices.IndexFunc(vars, func(v Variable) bool { return v.Name == name })
		if i < 0 {
			return token
		}
		value := values[name]
		if vars[i].Kind == Person && value != "" {
			value = people[value]
		}
		return value
	})
	return strings.Join(strings.Fields(out), " ")
}

// FieldError is a refusal of one field of a request, which the client shows
// next to it; a value's field is values.<name>.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

func fieldError(field, format string, args ...any) error {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func valueError(v Variable, format string, args ...any) error {
	return fieldError("values."+v.Name, format, args...)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
