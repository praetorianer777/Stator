package document

import (
	"encoding/json"
	"reflect"
)

// Normalize joins neighbouring text that carries the same marks and drops
// empty lists and attributes, so two documents that read alike compare
// equal. The database's document_without_anchors does the same.
func Normalize(n Node) Node {
	return DropMarks(n, func(Mark) bool { return false })
}

// DropMarks takes every mark drop picks off the document, then normalizes it.
func DropMarks(n Node, drop func(Mark) bool) Node {
	return dropMarks(n, drop, 0)
}

func dropMarks(n Node, drop func(Mark) bool, depth int) Node {
	var marks []Mark
	for _, m := range n.Marks {
		if !drop(m) {
			marks = append(marks, m)
		}
	}
	n.Marks = marks
	if len(n.Attrs) == 0 {
		n.Attrs = nil
	}
	if depth > MaxDepth {
		return n
	}
	var content []Node
	for _, child := range n.Content {
		child = dropMarks(child, drop, depth+1)
		if last := len(content) - 1; last >= 0 && sameStyle(content[last], child) {
			content[last].Text += child.Text
			continue
		}
		content = append(content, child)
	}
	n.Content = content
	return n
}

func sameStyle(a, b Node) bool {
	return a.Type == "text" && b.Type == "text" && reflect.DeepEqual(a.Marks, b.Marks) && reflect.DeepEqual(a.Attrs, b.Attrs)
}

// IsTextblock says whether a node holds text directly: a paragraph, a heading
// or a code block.
func IsTextblock(n Node) bool {
	spec, ok := Allowed.Nodes[n.Type]
	if !ok {
		return false
	}
	for _, c := range spec.Content {
		if c == "text" {
			return true
		}
	}
	return false
}

// WithoutAnchors is a stored body with every inline thread's mark taken out,
// as a version holds it; comparisons read bodies this way.
func WithoutAnchors(body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 || string(body) == "null" {
		return body, nil
	}
	root, err := Parse(body)
	if err != nil {
		return nil, err
	}
	if !hasMark(root, AnchorMark, 0) {
		return body, nil
	}
	return json.Marshal(DropMarks(root, func(m Mark) bool { return m.Type == AnchorMark }))
}

func hasMark(n Node, mark string, depth int) bool {
	for _, m := range n.Marks {
		if m.Type == mark {
			return true
		}
	}
	if depth > MaxDepth {
		return false
	}
	for _, c := range n.Content {
		if hasMark(c, mark, depth+1) {
			return true
		}
	}
	return false
}
