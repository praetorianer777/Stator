package docx

import (
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

type blockWriter func(w *writer, n document.Node, f frame, depth int)

// blockWriters writes each block of the allowlist; a test holds it to the
// allowlist, so a new block cannot be left out of the export unnoticed.
var blockWriters map[string]blockWriter

// parts are the nodes their parent writes, never found on their own.
var parts = map[string]string{
	"doc":                     "the page itself",
	"listItem":                "bulletList and orderedList",
	"taskItem":                "taskList",
	"tableRow":                "table",
	"tableCell":               "table",
	"tableHeader":             "table",
	"column":                  "columns",
	document.NodeGalleryImage: "gallery",
	document.NodePropertyRow:  "properties",
}

func init() {
	blockWriters = map[string]blockWriter{
		"paragraph": func(w *writer, n document.Node, f frame, _ int) {
			w.para(f, paraProps{}, func() { w.inline(n.Content, f) })
		},
		"heading":        (*writer).headingBlock,
		"bulletList":     (*writer).list,
		"orderedList":    (*writer).list,
		"taskList":       (*writer).list,
		"blockquote":     (*writer).quote,
		"codeBlock":      func(w *writer, n document.Node, f frame, _ int) { w.code(textOf(n), f) },
		"horizontalRule": (*writer).rule,
		"table": func(w *writer, n document.Node, f frame, depth int) {
			w.table(n, f, depth)
		},
		"panel":                 (*writer).panel,
		"expand":                (*writer).expand,
		"columns":               (*writer).columns,
		document.NodeDecision:   (*writer).decision,
		document.NodeMathBlock:  (*writer).mathBlock,
		document.NodeDiagram:    (*writer).diagram,
		document.NodeLinkCard:   (*writer).linkCard,
		document.NodeExcerpt:    func(w *writer, n document.Node, f frame, depth int) { w.blocks(n.Content, f, depth+1) },
		document.NodeInclude:    (*writer).include,
		document.NodeProperties: (*writer).properties,
		document.NodePropertiesReport: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.propertiesReport(stringsAttr(n, "labels"), stringAttr(n, "space")))
		},
		document.NodeLabelledPages: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.labelledPages(stringsAttr(n, "labels"), stringAttr(n, "match") == document.MatchAll, stringAttr(n, "space")))
		},
		document.NodeRecentlyUpdated: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.recentlyUpdated(stringAttr(n, "space")))
		},
		document.NodeBlogPosts: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.blogPosts(stringAttr(n, "space")))
		},
		document.NodeTaskReport: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.taskReport(stringAttr(n, "space")))
		},
		document.NodeAttachmentList: func(w *writer, _ document.Node, f frame, _ int) {
			w.note(f, w.words.attachmentList)
		},
		document.NodeTableChart: (*writer).tableChart,
		document.NodeCalendar: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.calendar(stringAttr(n, "project")))
		},
		document.NodeTemplateButton: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.templateButton(stringAttr(n, "label")))
		},
		document.NodeContributors: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.contributors(stringAttr(n, "scope") == document.ContributorsTree))
		},
		"image":                 (*writer).imageBlock,
		document.NodeGallery:    (*writer).gallery,
		"tableOfContents":       (*writer).contents,
		"childPages":            func(w *writer, _ document.Node, f frame, _ int) { w.note(f, w.words.childPages) },
		armature.NodeIssueBlock: func(w *writer, n document.Node, f frame, _ int) { w.note(f, w.words.issue(stringAttr(n, "key"))) },
		armature.NodeIssueList:  func(w *writer, n document.Node, f frame, _ int) { w.note(f, w.words.issueList(stringAttr(n, "query"))) },
		armature.NodeChart: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.issueChart(stringAttr(n, "project"), stringAttr(n, "query")))
		},
		armature.NodeRoadmap: func(w *writer, n document.Node, f frame, _ int) {
			w.note(f, w.words.roadmap(stringAttr(n, "project"), stringAttr(n, "query")))
		},
	}
}

// blocks writes a run of blocks into a frame.
func (w *writer) blocks(nodes []document.Node, f frame, depth int) {
	if depth > document.MaxDepth {
		return
	}
	for _, n := range nodes {
		if write, ok := blockWriters[n.Type]; ok {
			write(w, n, f, depth)
		}
	}
}

func (w *writer) headingBlock(n document.Node, f frame, _ int) {
	level := min(max(intAttr(n, "level", 1), 1), document.MaxHeadingLevel)
	bookmark := ""
	if f.top {
		if w.heading < len(w.headings) {
			bookmark = bookmarkName(w.heading)
		}
		w.heading++
	}
	w.para(f, paraProps{style: "Heading" + itoa(level)}, func() {
		if bookmark != "" {
			id := itoa(w.bookmark)
			w.bookmark++
			w.b.WriteString(`<w:bookmarkStart w:id="` + id + `" w:name="` + bookmark + `"/>`)
			defer w.b.WriteString(`<w:bookmarkEnd w:id="` + id + `"/>`)
		}
		w.inline(n.Content, f)
	})
}

// list writes a list's items, each item's first paragraph carrying its
// marker and the rest of it set in as far as the marker's words.
func (w *writer) list(n document.Node, f frame, depth int) {
	level := min(f.list, maxListLevel)
	num := 0
	switch n.Type {
	case "bulletList":
		num = bulletNum
	case "orderedList":
		num = w.orderedNum(stringAttr(n, "type"), intAttr(n, "start", 1))
	}
	inner := f
	inner.list = f.list + 1
	inner.indent = f.indent + listIndent
	inner.width = max(f.width-listIndent, minColumnWidth)
	for _, item := range n.Content {
		m := &marker{num: num, level: level}
		if n.Type == "taskList" {
			m.box = uncheckedBox
			if checked, _ := item.Attrs["checked"].(bool); checked {
				m.box = checkedBox
			}
		}
		itemFrame := inner
		itemFrame.marker = m
		if len(item.Content) == 0 || !carriesMarker(item.Content[0].Type) {
			w.para(itemFrame, paraProps{}, nil)
		}
		w.blocks(item.Content, itemFrame, depth+1)
	}
}

// The check boxes a task is written with.
const (
	uncheckedBox = "☐"
	checkedBox   = "☒"
)

// carriesMarker says whether a block is a paragraph a list marker can sit on.
func carriesMarker(t string) bool {
	return t == "paragraph" || t == "heading" || t == document.NodeDecision
}

func (w *writer) code(source string, f frame) {
	f.marker = nil
	w.para(f, paraProps{style: "Code"}, func() { w.text(strings.TrimSuffix(source, "\n"), runProps{}) })
}

func (w *writer) rule(_ document.Node, f frame, _ int) {
	w.para(f, paraProps{border: `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="` + ruleColor + `"/></w:pBdr>`}, nil)
}

// note writes a sentence where a block's contents are each reader's own in
// Stator, or cannot be carried into a document.
func (w *writer) note(f frame, sentence string) {
	w.para(f, paraProps{style: "Note"}, func() { w.text(sentence, runProps{}) })
}

func (w *writer) decision(n document.Node, f frame, _ int) {
	label, color := w.words.undecided, warningText
	if stringAttr(n, "state") == document.DecisionDecided {
		label, color = w.words.decided, successText
	}
	w.para(f, paraProps{}, func() {
		w.text(label+": ", runProps{bold: true, color: color})
		w.inline(n.Content, f)
	})
}

// mathBlock writes a formula's TeX source on a line of its own; Word's
// equations are another language, which TeX is not translated into.
func (w *writer) mathBlock(n document.Node, f frame, _ int) {
	w.para(f, paraProps{style: "Formula"}, func() { w.text(stringAttr(n, "latex"), runProps{}) })
}

// diagram writes a diagram's source, said to be one: it is drawn in the
// reader's browser, which a document has none of.
func (w *writer) diagram(n document.Node, f frame, _ int) {
	w.para(f, paraProps{style: "Note", keepNext: true}, func() { w.text(w.words.diagram, runProps{}) })
	w.code(stringAttr(n, "source"), f)
}

func (w *writer) linkCard(n document.Node, f frame, _ int) {
	url := stringAttr(n, "url")
	w.para(f, paraProps{}, func() {
		w.hyperlink(url, func() { w.text(url, runProps{style: "Hyperlink"}) })
	})
}

func (w *writer) quote(n document.Node, f frame, depth int) {
	w.box(f, depth, boxStyle{border: quoteBorder}, func(inner frame) { w.blocks(n.Content, inner, depth+1) })
}

func (w *writer) panel(n document.Node, f frame, depth int) {
	kind := stringAttr(n, "kind")
	look, ok := panelLooks[kind]
	if !ok {
		kind, look = "info", panelLooks["info"]
	}
	w.box(f, depth, look, func(inner frame) {
		w.para(inner, paraProps{keepNext: true}, func() { w.text(w.words.panels[kind], runProps{bold: true, color: look.border}) })
		w.blocks(n.Content, inner, depth+1)
	})
}

// expand writes its title and everything in it, open, as a print shows it.
func (w *writer) expand(n document.Node, f frame, depth int) {
	title := stringAttr(n, "title")
	if strings.TrimSpace(title) == "" {
		title = w.words.expand
	}
	w.box(f, depth, boxStyle{border: quoteBorder}, func(inner frame) {
		w.para(inner, paraProps{keepNext: true}, func() { w.text(title, runProps{bold: true}) })
		w.blocks(n.Content, inner, depth+1)
	})
}

func (w *writer) properties(n document.Node, f frame, depth int) {
	var rows []document.Node
	for _, row := range n.Content {
		if strings.TrimSpace(stringAttr(row, "key")) != "" {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return
	}
	width := f.width
	widths := []int{width * 3 / 10, width - width*3/10}
	w.tableStart(f, widths, true)
	for _, row := range rows {
		w.b.WriteString("<w:tr>")
		w.cell(widths[0], 1, "", neutralFill, func(inner frame) {
			inner.bold = true
			w.para(inner, paraProps{}, func() { w.text(stringAttr(row, "key"), runProps{bold: true}) })
		}, f, depth)
		w.cell(widths[1], 1, "", "", func(inner frame) {
			w.para(inner, paraProps{}, func() { w.inline(row.Content, inner) })
		}, f, depth)
		w.b.WriteString("</w:tr>")
	}
	w.tableEnd()
}

// tableChart writes the table a chart is drawn from, after a sentence saying
// what is drawn of it; the chart itself is drawn in the reader's browser.
func (w *writer) tableChart(n document.Node, f frame, depth int) {
	w.para(f, paraProps{style: "Note", keepNext: true}, func() { w.text(w.words.tableChart(stringAttr(n, "chart")), runProps{}) })
	for _, c := range n.Content {
		w.table(c, f, depth)
	}
}

// contents lists the page's headings down to its level, each a link to its
// heading. In an included page, whose headings are not the page's, it says so instead.
func (w *writer) contents(n document.Node, f frame, _ int) {
	if !f.top {
		w.note(f, w.words.contents)
		return
	}
	limit := intAttr(n, "maxLevel", document.MaxHeadingLevel)
	w.para(f, paraProps{style: "TOCHeading"}, func() { w.text(w.words.contents, runProps{}) })
	for i, h := range w.headings {
		if h.Level > limit || h.Text == "" {
			continue
		}
		level := min(max(h.Level, 1), document.MaxHeadingLevel)
		name := bookmarkName(i)
		w.para(f, paraProps{style: "TOC" + itoa(level)}, func() {
			w.b.WriteString(`<w:hyperlink w:anchor="` + name + `" w:history="1">`)
			w.text(h.Text, runProps{})
			w.b.WriteString("</w:hyperlink>")
		})
	}
}

// include writes what an include shows its reader, under a line naming the
// page it comes from; what the reader may not read is a sentence.
func (w *writer) include(n document.Node, f frame, depth int) {
	pageID, err := uuid.Parse(stringAttr(n, "pageId"))
	if err != nil {
		return
	}
	if w.read.Include == nil {
		url := ""
		if w.read.IncludeURL != nil {
			url = w.read.IncludeURL(pageID)
		}
		w.para(f, paraProps{style: "Note"}, func() {
			if url == "" {
				w.text(w.words.includedElsewhere, runProps{})
				return
			}
			w.hyperlink(url, func() { w.text(w.words.includedElsewhere, runProps{style: "Hyperlink"}) })
		})
		return
	}
	got, err := w.read.Include(w.ctx, pageID, stringAttr(n, "excerptId"), w.via)
	if err != nil {
		w.fail(err)
		return
	}
	if got == nil {
		w.note(f, w.words.includeMissing)
		return
	}
	w.box(f, depth, boxStyle{border: quoteBorder}, func(inner frame) {
		inner.top = false
		w.para(inner, paraProps{style: "Note", keepNext: true}, func() {
			if got.URL == "" {
				w.text(w.words.includedFrom(got.Title), runProps{})
				return
			}
			w.hyperlink(got.URL, func() { w.text(w.words.includedFrom(got.Title), runProps{style: "Hyperlink"}) })
		})
		w.via = append(w.via, pageID)
		w.blocks(got.Body.Content, inner, depth+1)
		w.via = w.via[:len(w.via)-1]
	})
}

func textOf(n document.Node) string {
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func stringAttr(n document.Node, name string) string {
	s, _ := n.Attrs[name].(string)
	return s
}

func stringsAttr(n document.Node, name string) []string {
	switch v := n.Attrs[name].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func intAttr(n document.Node, name string, fallback int) int {
	switch v := n.Attrs[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
