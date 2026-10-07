package docx

import (
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

type inlineWriter func(w *writer, n document.Node, rp runProps)

// inlineWriters writes each inline node of the allowlist, held to it by a
// test as blockWriters is.
var inlineWriters map[string]inlineWriter

// markStyles are the marks that style a run; a link is written around its runs.
var markStyles = map[string]func(*runProps){
	"bold":   func(rp *runProps) { rp.bold = true },
	"italic": func(rp *runProps) { rp.italic = true },
	"strike": func(rp *runProps) { rp.strike = true },
	"code":   func(rp *runProps) { rp.style = "InlineCode" },
	"link":   func(rp *runProps) { rp.style = "Hyperlink" },
}

// unstyled are the marks that leave their words as they are, and why.
var unstyled = map[string]string{
	"hint":              "a template's placeholder, stripped from whatever is published",
	document.AnchorMark: "an inline comment's passage, which belongs to the discussion in Stator",
}

func init() {
	inlineWriters = map[string]inlineWriter{
		"text":      func(w *writer, n document.Node, rp runProps) { w.text(n.Text, rp) },
		"hardBreak": func(w *writer, _ document.Node, rp runProps) { w.text("\n", rp) },
		"mention": func(w *writer, n document.Node, rp runProps) {
			label := stringAttr(n, "label")
			if label == "" {
				label = w.words.somebody
			}
			rp.color = accentText
			w.text("@"+label, rp)
		},
		"attachment": func(w *writer, n document.Node, rp runProps) {
			name := stringAttr(n, "fileName")
			id, err := uuid.Parse(stringAttr(n, "attachmentId"))
			// A file inside a link's words is written plainly: links do not nest.
			if err != nil || w.read.FileURL == nil || linkOf(n) != "" {
				w.text(name, rp)
				return
			}
			w.hyperlink(w.read.FileURL(id), func() {
				rp.style = "Hyperlink"
				w.text(name, rp)
			})
		},
		armature.NodeIssue: func(w *writer, n document.Node, rp runProps) {
			rp.bold = true
			w.text(stringAttr(n, "key"), rp)
		},
		document.NodeStatus: func(w *writer, n document.Node, rp runProps) {
			look := statusLooks[stringAttr(n, "color")]
			rp.caps, rp.bold, rp.color, rp.shade = true, true, look.text, look.fill
			w.text(" "+stringAttr(n, "label")+" ", rp)
		},
		document.NodeDate: func(w *writer, n document.Node, rp runProps) {
			w.text(w.words.date(stringAttr(n, "date")), rp)
		},
		document.NodeMathInline: func(w *writer, n document.Node, rp runProps) {
			rp.style = "FormulaChar"
			w.text(stringAttr(n, "latex"), rp)
		},
	}
}

// inline writes a block's inline content, its marks as run properties and
// each run of the same link as one hyperlink.
func (w *writer) inline(nodes []document.Node, f frame) {
	for i := 0; i < len(nodes); {
		href := linkOf(nodes[i])
		j := i + 1
		for j < len(nodes) && href != "" && linkOf(nodes[j]) == href {
			j++
		}
		run := nodes[i:j]
		write := func() {
			for _, n := range run {
				write, ok := inlineWriters[n.Type]
				if !ok {
					continue
				}
				rp := runProps{bold: f.bold}
				for _, m := range n.Marks {
					if style, ok := markStyles[m.Type]; ok {
						style(&rp)
					}
				}
				if hasMark(n, "code") && hasMark(n, "link") {
					rp.style = "Hyperlink"
				}
				write(w, n, rp)
			}
		}
		if href != "" {
			w.hyperlink(href, write)
		} else {
			write()
		}
		i = j
	}
}

func linkOf(n document.Node) string {
	for _, m := range n.Marks {
		if m.Type == "link" {
			href, _ := m.Attrs["href"].(string)
			return href
		}
	}
	return ""
}

func hasMark(n document.Node, t string) bool {
	for _, m := range n.Marks {
		if m.Type == t {
			return true
		}
	}
	return false
}
