package document

import "github.com/google/uuid"

// ForAnonymous is a document as somebody who is not signed in reads it: no
// mention names anybody, no task report asks for a person, and no passage
// carries the thread it is discussed in. The rest is the document as written.
func ForAnonymous(root Node) Node {
	return anonymize(root, 0)
}

func anonymize(n Node, depth int) Node {
	switch n.Type {
	case "mention":
		// The label is the person's name as it was written in.
		n.Attrs = nil
	case NodeTaskReport:
		if raw, _ := n.Attrs["assignee"].(string); raw != "" {
			if _, err := uuid.Parse(raw); err == nil {
				n.Attrs = withAttr(n.Attrs, "assignee", nil)
			}
		}
	}
	if len(n.Marks) > 0 {
		kept := make([]Mark, 0, len(n.Marks))
		for _, m := range n.Marks {
			if m.Type != AnchorMark {
				kept = append(kept, m)
			}
		}
		n.Marks = kept
	}
	if depth > MaxDepth || len(n.Content) == 0 {
		return n
	}
	content := make([]Node, len(n.Content))
	for i, c := range n.Content {
		content[i] = anonymize(c, depth+1)
	}
	n.Content = content
	return n
}

// withAttr is attrs with one attribute changed, leaving the original alone.
func withAttr(attrs map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	out[key] = value
	return out
}
