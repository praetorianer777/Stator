package document

import (
	"strings"
)

// Excerpt is a named part of a page, as a picker lists it.
type Excerpt struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Text is the start of the excerpt's words, so a picker shows which it is.
	Text string `json:"text"`
}

// MaxExcerptTextLength is how much of an excerpt's words a picker shows.
const MaxExcerptTextLength = 200

// Excerpts lists a document's excerpts in reading order.
func Excerpts(root Node) []Excerpt {
	out := []Excerpt{}
	for _, n := range excerptNodes(root.Content, 0) {
		id, _ := n.Attrs["id"].(string)
		name, _ := n.Attrs["name"].(string)
		text := strings.Join(strings.Fields(PlainText(n)), " ")
		if r := []rune(text); len(r) > MaxExcerptTextLength {
			text = string(r[:MaxExcerptTextLength-3]) + "..."
		}
		out = append(out, Excerpt{ID: id, Name: name, Text: text})
	}
	return out
}

// FindExcerpt is the excerpt with the id given, or false.
func FindExcerpt(root Node, id string) (Node, bool) {
	for _, n := range excerptNodes(root.Content, 0) {
		if got, _ := n.Attrs["id"].(string); got == id {
			return n, true
		}
	}
	return Node{}, false
}

func excerptNodes(blocks []Node, depth int) []Node {
	if depth > MaxDepth {
		return nil
	}
	var out []Node
	for _, n := range blocks {
		if n.Type == NodeExcerpt {
			out = append(out, n)
			continue
		}
		out = append(out, excerptNodes(n.Content, depth+1)...)
	}
	return out
}

// IncludesPage says whether a document includes the page with the id given,
// itself or one of its excerpts, at any depth.
func IncludesPage(root Node, pageID string) bool {
	var walk func(blocks []Node, depth int) bool
	walk = func(blocks []Node, depth int) bool {
		if depth > MaxDepth {
			return false
		}
		for _, n := range blocks {
			if id, _ := n.Attrs["pageId"].(string); n.Type == NodeInclude && id == pageID {
				return true
			}
			if walk(n.Content, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(root.Content, 0)
}

// ValidatePage is Validate for the body of the page with the id given,
// which may include any page but itself.
func ValidatePage(body []byte, pageID string) error {
	if err := Validate(body); err != nil {
		return err
	}
	root, _ := Parse(body)
	if IncludesPage(root, pageID) {
		return invalid("This page includes itself, which would show it over and over; include another page, or one of this page's excerpts elsewhere.")
	}
	return nil
}

// checkExcerpts refuses an excerpt inside another, which an include could
// not tell apart, and two that share an id or a name, which a picker could not.
func checkExcerpts(root Node) error {
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, n := range excerptNodes(root.Content, 0) {
		if len(excerptNodes(n.Content, 0)) > 0 {
			return invalid("This page puts an excerpt inside another; take one of them out.")
		}
		id, _ := n.Attrs["id"].(string)
		name, _ := n.Attrs["name"].(string)
		key := strings.ToLower(strings.TrimSpace(name))
		if ids[id] {
			return invalid("Two excerpts on this page share one id; copy one of them again from the editor.")
		}
		if names[key] {
			return invalid("Two excerpts on this page are named %q; rename one of them.", strings.TrimSpace(name))
		}
		ids[id], names[key] = true, true
	}
	return nil
}
