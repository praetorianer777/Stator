package document

import (
	"strings"
)

// Property is one named value of a page's properties: Key as it was first
// written, Text its words, Content its inline nodes for drawing it.
type Property struct {
	Key     string `json:"key"`
	Text    string `json:"text"`
	Content []Node `json:"content"`
}

// PropertyName is how two names are told apart: trimmed, in any case, one
// space between words, so "Owner" and " owner" are one column.
func PropertyName(key string) string {
	return strings.ToLower(strings.Join(strings.Fields(key), " "))
}

// Properties reads every properties block of a page in reading order. A name
// written twice keeps its first value; a row without a name is left out.
func Properties(root Node) []Property {
	out := []Property{}
	seen := map[string]bool{}
	var walk func(blocks []Node, depth int)
	walk = func(blocks []Node, depth int) {
		if depth > MaxDepth {
			return
		}
		for _, n := range blocks {
			if n.Type != NodeProperties {
				walk(n.Content, depth+1)
				continue
			}
			for _, row := range n.Content {
				key, _ := row.Attrs["key"].(string)
				name := PropertyName(key)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				content := row.Content
				if content == nil {
					content = []Node{}
				}
				out = append(out, Property{Key: strings.Join(strings.Fields(key), " "), Text: strings.TrimSpace(InlineText(row)), Content: content})
			}
		}
	}
	walk(root.Content, 0)
	return out
}
