package document

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// PlainText is the words of a document, one block per line and table cells
// separated by tabs, for the search index and anywhere markup cannot go.
func PlainText(root Node) string {
	var b strings.Builder
	writeBlocks(&b, root.Content, 0)
	return strings.TrimSpace(b.String())
}

func writeBlocks(b *strings.Builder, blocks []Node, depth int) {
	if depth > MaxDepth {
		return
	}
	for _, n := range blocks {
		switch n.Type {
		case "paragraph", "heading", "codeBlock", NodeDecision:
			if text := InlineText(n); text != "" {
				b.WriteString(text)
				b.WriteByte('\n')
			}
		case "tableRow":
			cells := make([]string, 0, len(n.Content))
			for _, cell := range n.Content {
				var inner strings.Builder
				writeBlocks(&inner, cell.Content, depth+1)
				cells = append(cells, strings.ReplaceAll(strings.TrimSpace(inner.String()), "\n", " "))
			}
			b.WriteString(strings.Join(cells, "\t"))
			b.WriteByte('\n')
		// A property reads as a table row does: its name, a tab, its value.
		case NodePropertyRow:
			key, _ := n.Attrs["key"].(string)
			if line := strings.TrimRight(key+"\t"+InlineText(n), "\t"); line != "" {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		case "expand":
			if title, _ := n.Attrs["title"].(string); title != "" {
				b.WriteString(title)
				b.WriteByte('\n')
			}
			writeBlocks(b, n.Content, depth+1)
		case NodeMathBlock:
			if latex, _ := n.Attrs["latex"].(string); latex != "" {
				b.WriteString(latex)
				b.WriteByte('\n')
			}
		// A diagram's labels are in its source, among the arrows.
		case NodeDiagram:
			if source, _ := n.Attrs["source"].(string); source != "" {
				b.WriteString(source)
				b.WriteByte('\n')
			}
		case NodeSketch:
			if title, _ := n.Attrs["title"].(string); title != "" {
				b.WriteString(title)
				b.WriteByte('\n')
			}
			scene, _ := n.Attrs["scene"].(string)
			for _, words := range SketchWords(scene) {
				b.WriteString(words)
				b.WriteByte('\n')
			}
		case armature.NodeIssueBlock:
			if key, _ := n.Attrs["key"].(string); key != "" {
				b.WriteString(key)
				b.WriteByte('\n')
			}
		default:
			writeBlocks(b, n.Content, depth+1)
		}
	}
}

// InlineText flattens a block's inline children: text, mentions as @Name,
// files by name, issues by key, statuses by label, dates as YYYY-MM-DD,
// formulas by their source, a template's blanks as {name} and breaks as newlines.
func InlineText(n Node) string {
	var b strings.Builder
	for _, c := range n.Content {
		switch c.Type {
		case "text":
			b.WriteString(c.Text)
		case "mention":
			label, _ := c.Attrs["label"].(string)
			b.WriteString("@" + label)
		case "hardBreak":
			b.WriteByte('\n')
		case "attachment":
			name, _ := c.Attrs["fileName"].(string)
			b.WriteString(name)
		case armature.NodeIssue:
			key, _ := c.Attrs["key"].(string)
			b.WriteString(key)
		case NodeStatus:
			label, _ := c.Attrs["label"].(string)
			b.WriteString(label)
		case NodeDate:
			date, _ := c.Attrs["date"].(string)
			b.WriteString(date)
		case NodeVariable:
			name, _ := c.Attrs["name"].(string)
			b.WriteString("{" + name + "}")
		case NodeMathInline:
			latex, _ := c.Attrs["latex"].(string)
			b.WriteString(latex)
		}
	}
	return b.String()
}

// Heading is one entry of a page's table of contents.
type Heading struct {
	Level  int    `json:"level"`
	Anchor string `json:"anchor"`
	Text   string `json:"text"`
}

// Headings lists a document's headings in order, nested ones included; one
// saved without an anchor gets the one the editor would give it.
func Headings(root Node) []Heading {
	var found []Node
	var walk func(nodes []Node, depth int)
	walk = func(nodes []Node, depth int) {
		if depth > MaxDepth {
			return
		}
		for _, n := range nodes {
			if n.Type == "heading" {
				found = append(found, n)
				continue
			}
			walk(n.Content, depth+1)
		}
	}
	walk(root.Content, 0)

	taken := map[string]bool{}
	for _, n := range found {
		if id, _ := n.Attrs["id"].(string); id != "" {
			taken[id] = true
		}
	}
	out := make([]Heading, 0, len(found))
	for _, n := range found {
		text := strings.TrimSpace(strings.ReplaceAll(InlineText(n), "\n", " "))
		level, _ := n.Attrs["level"].(float64)
		anchor, _ := n.Attrs["id"].(string)
		if anchor == "" {
			anchor = Dedupe(Slug(text), taken)
			taken[anchor] = true
		}
		out = append(out, Heading{Level: int(level), Anchor: anchor, Text: text})
	}
	return out
}

// FallbackSlug anchors a heading whose text has no letters or digits.
const FallbackSlug = "section"

// Slug turns heading text into an anchor: lowercase letters and digits,
// runs of anything else as one hyphen. The web editor computes the same.
func Slug(text string) string {
	var b strings.Builder
	hyphen := false
	count := 0
	for _, r := range strings.ToLower(text) {
		if count >= MaxSlugLength {
			break
		}
		if unicode.In(r, unicode.Ll, unicode.Lo, unicode.Lm, unicode.N) {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
				count++
			}
			hyphen = false
			b.WriteRune(r)
			count++
			continue
		}
		hyphen = true
	}
	if b.Len() == 0 {
		return FallbackSlug
	}
	return b.String()
}

// Dedupe returns slug, or slug with the first free number from 2 appended.
func Dedupe(slug string, taken map[string]bool) string {
	if !taken[slug] {
		return slug
	}
	for i := 2; ; i++ {
		candidate := slug + "-" + strconv.Itoa(i)
		if !taken[candidate] {
			return candidate
		}
	}
}
