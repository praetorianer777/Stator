package document

// The languages a diagram is written in (#466). Mermaid is drawn in the
// reader's browser, the others by the installation's Kroki.
const (
	DiagramMermaid     = "mermaid"
	DiagramPlantUML    = "plantuml"
	DiagramStructurizr = "structurizr"
	DiagramGraphviz    = "graphviz"
	DiagramD2          = "d2"
)

// DiagramLanguages is every language a diagram may name, whether or not this
// server draws it, so a page moves between servers unchanged.
var DiagramLanguages = []string{DiagramMermaid, DiagramPlantUML, DiagramStructurizr, DiagramGraphviz, DiagramD2}

// Diagram is one diagram of a document, as its language and its source.
type Diagram struct {
	Language string
	Source   string
}

// DiagramLanguage is the language a diagram node is written in; a node that
// names none is Mermaid, as every diagram was before languages.
func DiagramLanguage(n Node) string {
	if language, _ := n.Attrs["language"].(string); language != "" {
		return language
	}
	return DiagramMermaid
}

// Diagrams lists the diagrams of a document, wherever they sit in it.
func Diagrams(root Node) []Diagram {
	var out []Diagram
	collectDiagrams(&out, root.Content, 0)
	return out
}

func collectDiagrams(out *[]Diagram, nodes []Node, depth int) {
	if depth > MaxDepth {
		return
	}
	for _, n := range nodes {
		if n.Type == NodeDiagram {
			source, _ := n.Attrs["source"].(string)
			*out = append(*out, Diagram{Language: DiagramLanguage(n), Source: source})
			continue
		}
		collectDiagrams(out, n.Content, depth+1)
	}
}
