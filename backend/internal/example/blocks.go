package example

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// Node is a block or inline node of a page's document.
type Node = document.Node

// marker is a paragraph that names a block, %%name%% or %%name argument%%.
var marker = regexp.MustCompile(`^%%([a-z-]+)(?: (.+?))?%%$`)

// decisionMarker opens a paragraph that is a decision, %%decided%% or %%undecided%%.
var decisionMarker = regexp.MustCompile(`^%%(decided|undecided)%% `)

// inlineMath is code written $like this$, which is a formula in the text.
var inlineMath = regexp.MustCompile(`^\$(.+)\$$`)

// Fenced code in these languages is the block it describes rather than code.
const (
	diagramLanguage = "mermaid"
	mathLanguage    = "math"
)

// containers are markers whose blocks run to the next %%end%%.
var containers = []string{"columns", "column", "excerpt", "chart", "properties"}

// The defaults the blocks the content names are made with.
const (
	listedPages      = 10
	recentPages      = 5
	listedPosts      = 3
	reportedTasks    = 20
	namedPeople      = 10
	chartDays        = 30
	exampleChart     = string(armature.ChartPie)
	exampleGroupBy   = "status"
	exampleRoadmapBy = string(armature.RoadmapByEpic)
)

type builder struct {
	facts Facts
	page  string
}

func markerOf(n Node) (name, arg string, ok bool) {
	if n.Type != "paragraph" || len(n.Content) != 1 || n.Content[0].Type != "text" || len(n.Content[0].Marks) > 0 {
		return "", "", false
	}
	m := marker.FindStringSubmatch(n.Content[0].Text)
	if m == nil {
		if strings.Contains(n.Content[0].Text, "%%") && !decisionMarker.MatchString(n.Content[0].Text) {
			return "unknown", n.Content[0].Text, true
		}
		return "", "", false
	}
	return m[1], m[2], true
}

func (b builder) blocks(in []Node) ([]Node, error) {
	out, rest, err := b.run(in, false)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("a %%%%end%%%% closes nothing")
	}
	return out, nil
}

// run makes the blocks up to the %%end%% that closes a container, when
// nested, and returns the blocks after it.
func (b builder) run(in []Node, nested bool) ([]Node, []Node, error) {
	var out []Node
	for len(in) > 0 {
		n := in[0]
		in = in[1:]
		name, arg, ok := markerOf(n)
		if !ok {
			made, err := b.node(n)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, made)
			continue
		}
		switch {
		case name == "end":
			if !nested {
				return nil, nil, fmt.Errorf("a %%%%end%%%% closes nothing")
			}
			return out, in, nil
		case slices.Contains(containers, name):
			inner, rest, err := b.run(in, true)
			if err != nil {
				return nil, nil, err
			}
			in = rest
			made, err := b.container(name, arg, inner)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, made)
		default:
			made, err := b.block(name, arg)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, made...)
		}
	}
	if nested {
		return nil, nil, fmt.Errorf("a block opened with a marker has no %%%%end%%%%")
	}
	return out, nil, nil
}

// node makes the markers inside a block that is not one itself.
func (b builder) node(n Node) (Node, error) {
	switch {
	case n.Type == "codeBlock":
		return b.code(n), nil
	case n.Type == "paragraph" && len(n.Content) > 0 && n.Content[0].Type == "text" && decisionMarker.MatchString(n.Content[0].Text):
		m := decisionMarker.FindStringSubmatch(n.Content[0].Text)
		content := slices.Clone(n.Content)
		content[0].Text = strings.TrimPrefix(content[0].Text, m[0])
		return Node{Type: document.NodeDecision, Attrs: map[string]any{"state": m[1]}, Content: inline(content)}, nil
	case document.IsTextblock(n):
		n.Content = inline(n.Content)
		return n, nil
	case len(n.Content) > 0:
		var err error
		n.Content, err = b.blocks(n.Content)
		return n, err
	}
	return n, nil
}

func (b builder) code(n Node) Node {
	language, _ := n.Attrs["language"].(string)
	source := document.InlineText(n)
	switch language {
	case diagramLanguage:
		return Node{Type: document.NodeDiagram, Attrs: map[string]any{"source": source}}
	case mathLanguage:
		return Node{Type: document.NodeMathBlock, Attrs: map[string]any{"latex": strings.TrimSpace(source)}}
	}
	return n
}

// inline makes code written $like this$ a formula.
func inline(content []Node) []Node {
	out := make([]Node, 0, len(content))
	for _, n := range content {
		if n.Type == "text" && len(n.Marks) == 1 && n.Marks[0].Type == "code" {
			if m := inlineMath.FindStringSubmatch(n.Text); m != nil {
				out = append(out, Node{Type: document.NodeMathInline, Attrs: map[string]any{"latex": m[1]}})
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func (b builder) container(name, arg string, inner []Node) (Node, error) {
	switch name {
	case "columns":
		for _, c := range inner {
			if c.Type != "column" {
				return Node{}, fmt.Errorf("columns hold a %s where only %%%%column%%%% goes", c.Type)
			}
		}
		return Node{Type: "columns", Content: inner}, nil
	case "column":
		var width any
		if arg != "" {
			w, err := strconv.Atoi(arg)
			if err != nil {
				return Node{}, fmt.Errorf("a column's width %q is not a number", arg)
			}
			width = w
		}
		return Node{Type: "column", Attrs: map[string]any{"width": width}, Content: inner}, nil
	case "excerpt":
		id, ok := b.facts.Excerpts[b.page]
		if !ok {
			return Node{}, fmt.Errorf("the page has no excerpt id")
		}
		return Node{Type: document.NodeExcerpt, Attrs: map[string]any{"id": id.String(), "name": arg}, Content: inner}, nil
	case "chart":
		if len(inner) != 1 || inner[0].Type != "table" {
			return Node{}, fmt.Errorf("a chart holds one table")
		}
		return Node{Type: document.NodeTableChart, Attrs: map[string]any{"chart": arg, "showTable": true}, Content: inner}, nil
	case "properties":
		if len(inner) != 1 || inner[0].Type != "table" {
			return Node{}, fmt.Errorf("properties are written as one table")
		}
		var rows []Node
		for _, row := range inner[0].Content {
			if len(row.Content) != 2 || len(row.Content[1].Content) != 1 {
				return Node{}, fmt.Errorf("a property is a row of two cells")
			}
			rows = append(rows, Node{
				Type:    document.NodePropertyRow,
				Attrs:   map[string]any{"key": strings.TrimSpace(document.PlainText(row.Content[0]))},
				Content: row.Content[1].Content[0].Content,
			})
		}
		return Node{Type: document.NodeProperties, Content: rows}, nil
	}
	return Node{}, fmt.Errorf("no container %q", name)
}

// split reads an argument of parts separated by colons, each trimmed.
func split(arg string, parts int) ([]string, error) {
	out := strings.SplitN(arg, ":", parts)
	if len(out) != parts {
		return nil, fmt.Errorf("%q needs %d parts separated by colons", arg, parts)
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out, nil
}

func list(csv string) []any {
	var out []any
	for v := range strings.SplitSeq(csv, ",") {
		out = append(out, strings.TrimSpace(v))
	}
	return out
}

// block is the block a marker paragraph names.
func (b builder) block(name, arg string) ([]Node, error) {
	f := b.facts
	one := func(kind string, attrs map[string]any) ([]Node, error) {
		return []Node{{Type: kind, Attrs: attrs}}, nil
	}
	switch name {
	case "include":
		pageID, ok := f.Pages[arg]
		excerpt, hasExcerpt := f.Excerpts[arg]
		if !ok || !hasExcerpt {
			return nil, fmt.Errorf("no excerpt of %q to include", arg)
		}
		return one(document.NodeInclude, map[string]any{"pageId": pageID.String(), "excerptId": excerpt.String()})
	case "labelled-pages":
		return one(document.NodeLabelledPages, map[string]any{"labels": list(arg), "match": document.MatchAny, "space": f.Key, "sort": document.SortTitle, "limit": listedPages})
	case "properties-report":
		parts, err := split(arg, 2)
		if err != nil {
			return nil, err
		}
		return one(document.NodePropertiesReport, map[string]any{"labels": list(parts[0]), "space": f.Key, "columns": list(parts[1])})
	case "recently-updated":
		return one(document.NodeRecentlyUpdated, map[string]any{"space": f.Key, "limit": recentPages})
	case "blog-posts":
		return one(document.NodeBlogPosts, map[string]any{"space": f.Key, "limit": listedPosts})
	case "task-report":
		return one(document.NodeTaskReport, map[string]any{"space": f.Key, "assignee": document.AssigneeReader, "due": document.DueAny, "state": document.TaskStateOpen, "limit": reportedTasks})
	case "files":
		if len(f.Files) == 0 {
			return nil, fmt.Errorf("a files block where the site keeps no files")
		}
		return one(document.NodeAttachmentList, nil)
	case "calendar":
		if f.Calendar == uuid.Nil {
			return nil, fmt.Errorf("a calendar block before the calendar is made")
		}
		var project any
		if f.Armature != nil {
			project = f.Armature.Project
		}
		return one(document.NodeCalendar, map[string]any{"calendarId": f.Calendar.String(), "project": project})
	case "template-button":
		parts, err := split(arg, 3)
		if err != nil {
			return nil, err
		}
		parent, ok := f.Pages[MeetingNotes]
		if !ok {
			return nil, fmt.Errorf("no folder for the template button's pages")
		}
		return one(document.NodeTemplateButton, map[string]any{"template": parts[0], "space": f.Key, "parent": parent.String(), "label": parts[1], "title": parts[2]})
	case "contributors":
		return one(document.NodeContributors, map[string]any{"scope": arg, "limit": namedPeople})
	case "link-card":
		parts := strings.Fields(arg)
		if len(parts) != 2 {
			return nil, fmt.Errorf("a link card names its view and its address")
		}
		return one(document.NodeLinkCard, map[string]any{"view": parts[0], "url": parts[1]})
	case "armature-chart", "armature-roadmap":
		if f.Armature == nil {
			return nil, fmt.Errorf("an Armature block where the maker sees no Armature project")
		}
		query := "project = " + f.Armature.Project
		if name == "armature-chart" {
			return one(armature.NodeChart, map[string]any{"project": f.Armature.Project, "query": query, "chart": exampleChart, "groupBy": exampleGroupBy, "days": chartDays})
		}
		return one(armature.NodeRoadmap, map[string]any{"project": f.Armature.Project, "query": query, "groupBy": exampleRoadmapBy})
	}
	return nil, fmt.Errorf("no block %q (%s)", name, arg)
}
