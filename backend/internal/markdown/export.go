package markdown

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// Links says where the files and pages a document names are written. A
// function that is nil, or answers false, leaves the reference unresolved.
type Links struct {
	// File is the path, relative to the Markdown file, of a file on the page.
	File func(attachmentID string) (string, bool)
	// Page is the path, relative to the Markdown file, of another exported page.
	Page func(pageID string) (string, bool)
}

func (l Links) file(id string) (string, bool) {
	if l.File == nil {
		return "", false
	}
	return l.File(id)
}

func (l Links) page(id string) (string, bool) {
	if l.Page == nil {
		return "", false
	}
	return l.Page(id)
}

// Render writes a page as Markdown: its title as the one level 1 heading,
// then its document, each heading one level below where the page has it.
func Render(title string, root document.Node, links Links) string {
	r := renderer{links: links}
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(r.escape(strings.ReplaceAll(title, "\n", " "), escHeading, true))
	b.WriteString("\n")
	if body := r.blocks(root.Content, 0); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

type renderer struct {
	links Links
}

// blocks renders a run of blocks, a blank line apart. Two lists of the same
// kind in a row would read as one list, so a comment keeps them apart.
func (r renderer) blocks(nodes []document.Node, depth int) string {
	var parts []string
	prev := ""
	for _, n := range nodes {
		out, ok := r.block(n, depth)
		if !ok {
			continue
		}
		if listFamily(prev) != "" && listFamily(prev) == listFamily(n.Type) {
			parts = append(parts, "<!-- -->")
		}
		parts = append(parts, out)
		prev = n.Type
	}
	return strings.Join(parts, "\n\n")
}

// listFamily groups the lists Markdown would join: a bullet list and a task
// list both start with a dash.
func listFamily(t string) string {
	switch t {
	case "bulletList", "taskList":
		return "bullet"
	case "orderedList":
		return "ordered"
	}
	return ""
}

func (r renderer) block(n document.Node, depth int) (string, bool) {
	if depth > document.MaxDepth {
		return "", false
	}
	switch n.Type {
	case "paragraph":
		out := r.inline(n.Content, ctxBlock)
		return out, out != ""
	// Markdown has no decision items, so one reads as a line that says its state.
	case document.NodeDecision:
		label := "Undecided:"
		if stringAttr(n, "state") == document.DecisionDecided {
			label = "Decided:"
		}
		return strings.TrimSpace("**" + label + "** " + r.inline(n.Content, ctxBlock)), true
	case "heading":
		level := intAttr(n, "level", 1)
		return strings.Repeat("#", level+1) + " " + r.inline(n.Content, ctxHeading), true
	case "horizontalRule":
		return "---", true
	case "codeBlock":
		return codeFence(textOf(n), stringAttr(n, "language")), true
	// A math fence is how Markdown that typesets formulas writes one on its
	// own line, and the import reads it back as one.
	case document.NodeMathBlock:
		return codeFence(stringAttr(n, "latex"), mathLanguage), true
	// A mermaid fence is drawn as a diagram where Markdown draws them, and
	// reads as its source everywhere else.
	case document.NodeDiagram:
		return codeFence(stringAttr(n, "source"), diagramLanguage), true
	// A card's words are read for each reader, so its address stands alone
	// on its line, where Markdown readers that draw cards draw one.
	case document.NodeLinkCard:
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(stringAttr(n, "url")) + ">", true
	case "blockquote":
		return quote(r.blocks(n.Content, depth+1)), true
	case "panel":
		alert := panelAlerts[stringAttr(n, "kind")]
		if alert == "" {
			alert = panelAlerts["info"]
		}
		inner := r.blocks(n.Content, depth+1)
		if inner == "" {
			return quote("[!" + alert + "]"), true
		}
		return quote("[!" + alert + "]\n" + inner), true
	case "expand":
		var b strings.Builder
		b.WriteString("<details>\n<summary>")
		b.WriteString(htmlText(stringAttr(n, "title")))
		b.WriteString("</summary>\n\n")
		if inner := r.blocks(n.Content, depth+1); inner != "" {
			b.WriteString(inner)
			b.WriteString("\n\n")
		}
		b.WriteString("</details>")
		return b.String(), true
	// Markdown has no columns, so they read one after another, as they do
	// on a narrow screen; an excerpt reads as the blocks it marks, its name
	// being for pickers.
	case "columns", "column", document.NodeExcerpt:
		out := r.blocks(n.Content, depth+1)
		return out, out != ""
	case "bulletList", "orderedList", "taskList":
		return r.list(n, depth), true
	// Markdown has no properties, so they read as the two-column table they look like.
	case document.NodeProperties:
		return r.table(propertiesTable(n), depth)
	// A list's pages are each reader's, so the export keeps what it lists.
	case document.NodeLabelledPages:
		return div(kindLabelled, [][2]string{
			{"data-labels", strings.Join(stringsAttr(n, "labels"), ",")},
			{"data-match", stringAttr(n, "match")},
			{"data-space", stringAttr(n, "space")},
			{"data-sort", stringAttr(n, "sort")},
			{"data-limit", strconv.Itoa(intAttr(n, "limit", document.DefaultListedPages))},
		}, ""), true
	case document.NodeRecentlyUpdated:
		return div(kindUpdated, [][2]string{
			{"data-space", stringAttr(n, "space")},
			{"data-limit", strconv.Itoa(intAttr(n, "limit", document.DefaultListedPages))},
		}, ""), true
	// A report's tasks are each reader's, so the export keeps what it picks.
	case document.NodeTaskReport:
		return div(kindTasks, [][2]string{
			{"data-space", stringAttr(n, "space")},
			{"data-assignee", stringAttr(n, "assignee")},
			{"data-due", stringAttr(n, "due")},
			{"data-state", stringAttr(n, "state")},
			{"data-limit", strconv.Itoa(intAttr(n, "limit", document.DefaultReportedTasks))},
		}, ""), true
	// A report's rows are each reader's, so the export keeps what it gathers.
	case document.NodePropertiesReport:
		columns, _ := json.Marshal(stringsAttr(n, "columns"))
		return div(kindReport, [][2]string{
			{"data-labels", strings.Join(stringsAttr(n, "labels"), ",")},
			{"data-space", stringAttr(n, "space")},
			{"data-columns", string(columns)},
		}, ""), true
	case "table":
		return r.table(n, depth)
	case "image":
		return r.image(n)
	case armature.NodeIssueBlock:
		return div(kindIssue, nil, stringAttr(n, "key")), true
	case armature.NodeIssueList:
		attrs := [][2]string{{"data-columns", strings.Join(stringsAttr(n, "columns"), ",")}}
		if limit, ok := n.Attrs["limit"]; ok && limit != nil {
			attrs = append(attrs, [2]string{"data-limit", strconv.Itoa(intAttr(n, "limit", 0))})
		}
		return div(kindIssueList, attrs, stringAttr(n, "query")), true
	case "tableOfContents":
		return div(kindTOC, [][2]string{{"data-max-level", strconv.Itoa(intAttr(n, "maxLevel", document.MaxHeadingLevel))}}, ""), true
	case "childPages":
		attrs := [][2]string{{"data-scope", stringAttr(n, "scope")}}
		if d, ok := n.Attrs["depth"]; ok && d != nil {
			attrs = append(attrs, [2]string{"data-depth", strconv.Itoa(intAttr(n, "depth", 1))})
		}
		attrs = append(attrs, [2]string{"data-sort", stringAttr(n, "sort")})
		return div(kindChildPages, attrs, ""), true
	// What an include shows is read for each reader, so the export keeps
	// what it points at, which an import into the same organization finds.
	// A chart's counts are each reader's, so the export keeps what it counts.
	case armature.NodeChart:
		return div(kindChart, [][2]string{
			{"data-project", stringAttr(n, "project")},
			{"data-chart", stringAttr(n, "chart")},
			{"data-group-by", stringAttr(n, "groupBy")},
			{"data-days", strconv.Itoa(intAttr(n, "days", armature.DefaultChartDays))},
		}, stringAttr(n, "query")), true
	case armature.NodeRoadmap:
		return div(kindRoadmap, [][2]string{
			{"data-project", stringAttr(n, "project")},
			{"data-group-by", stringAttr(n, "groupBy")},
		}, stringAttr(n, "query")), true
	case document.NodeInclude:
		attrs := [][2]string{{"data-page", stringAttr(n, "pageId")}}
		if excerpt := stringAttr(n, "excerptId"); excerpt != "" {
			attrs = append(attrs, [2]string{"data-excerpt", excerpt})
		}
		return div(kindInclude, attrs, ""), true
	}
	return "", false
}

// list writes each item's blocks under its marker. A list whose items are
// one paragraph each, nested lists aside, is tight: no blank lines between.
func (r renderer) list(n document.Node, depth int) string {
	tight := true
	for _, item := range n.Content {
		paragraphs := 0
		for _, c := range item.Content {
			switch {
			case c.Type == "paragraph":
				paragraphs++
			case listFamily(c.Type) != "":
			default:
				tight = false
			}
		}
		if paragraphs > 1 {
			tight = false
		}
	}
	start := intAttr(n, "start", 1)
	items := make([]string, 0, len(n.Content))
	for i, item := range n.Content {
		var marker string
		switch n.Type {
		case "orderedList":
			marker = strconv.Itoa(start+i) + "."
		case "taskList":
			marker = "- [ ]"
			if checked, _ := item.Attrs["checked"].(bool); checked {
				marker = "- [x]"
			}
		default:
			marker = "-"
		}
		var inner string
		if tight {
			var parts []string
			for _, c := range item.Content {
				if out, ok := r.block(c, depth+1); ok {
					parts = append(parts, out)
				}
			}
			inner = strings.Join(parts, "\n")
		} else {
			inner = r.blocks(item.Content, depth+1)
		}
		pad := strings.Repeat(" ", len(marker)+1)
		if n.Type == "taskList" {
			pad = "  "
		}
		if inner == "" {
			items = append(items, marker)
			continue
		}
		items = append(items, prefixLines(inner, marker+" ", pad))
	}
	if tight {
		return strings.Join(items, "\n")
	}
	return strings.Join(items, "\n\n")
}

func (r renderer) image(n document.Node) (string, bool) {
	alt := stringAttr(n, "alt")
	dest, ok := r.links.file(stringAttr(n, "attachmentId"))
	if !ok {
		out := r.escape(alt, escText, true)
		return out, out != ""
	}
	if width := intAttr(n, "width", 0); width > 0 {
		return fmt.Sprintf(`<img src="%s" alt="%s" width="%d">`, htmlText(dest), htmlText(alt), width), true
	}
	return "![" + r.escape(alt, escText, false) + "](" + destination(dest) + ")", true
}

// table writes a GFM table. The first row is its header; a merged cell's
// content goes in its first place, and each cell's blocks share one line.
// propertiesTable is a properties block as a table headed Property and Value.
func propertiesTable(n document.Node) document.Node {
	cell := func(kind string, content ...document.Node) document.Node {
		return document.Node{Type: kind, Content: []document.Node{{Type: "paragraph", Content: content}}}
	}
	text := func(s string) []document.Node {
		if s == "" {
			return nil
		}
		return []document.Node{{Type: "text", Text: s}}
	}
	rows := []document.Node{{Type: "tableRow", Content: []document.Node{cell("tableHeader", text("Property")...), cell("tableHeader", text("Value")...)}}}
	for _, row := range n.Content {
		rows = append(rows, document.Node{Type: "tableRow", Content: []document.Node{cell("tableCell", text(stringAttr(row, "key"))...), cell("tableCell", row.Content...)}})
	}
	return document.Node{Type: "table", Content: rows}
}

func (r renderer) table(n document.Node, depth int) (string, bool) {
	var grid [][]string
	var aligns []string
	spans := map[int]int{}
	for ri, row := range n.Content {
		var line []string
		col := 0
		next := func() {
			for spans[col] > 0 {
				spans[col]--
				line = append(line, "")
				col++
			}
		}
		for _, cell := range row.Content {
			next()
			colspan := max(intAttr(cell, "colspan", 1), 1)
			rowspan := max(intAttr(cell, "rowspan", 1), 1)
			if ri == 0 {
				for len(aligns) < col+colspan {
					aligns = append(aligns, "")
				}
				aligns[col] = stringAttr(cell, "align")
			}
			line = append(line, r.cell(cell.Content, depth+1))
			for i := range colspan {
				if i > 0 {
					line = append(line, "")
				}
				if rowspan > 1 {
					spans[col] = rowspan - 1
				}
				col++
			}
		}
		next()
		grid = append(grid, line)
	}
	width := 0
	for _, line := range grid {
		width = max(width, len(line))
	}
	if width == 0 {
		return "", false
	}
	var b strings.Builder
	for i, line := range grid {
		for len(line) < width {
			line = append(line, "")
		}
		b.WriteString("|")
		for _, cell := range line {
			b.WriteString(" " + cell + " |")
		}
		b.WriteString("\n")
		if i == 0 {
			b.WriteString("|")
			for c := range width {
				align := ""
				if c < len(aligns) {
					align = aligns[c]
				}
				switch align {
				case "left":
					b.WriteString(" :-- |")
				case "center":
					b.WriteString(" :-: |")
				case "right":
					b.WriteString(" --: |")
				default:
					b.WriteString(" --- |")
				}
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), true
}

// cell writes a cell's blocks on one line, joined by line breaks. Text and
// images keep their form; any other block keeps its words.
func (r renderer) cell(nodes []document.Node, depth int) string {
	var parts []string
	for _, c := range nodes {
		var out string
		switch c.Type {
		case "paragraph", "heading":
			out = r.inline(c.Content, ctxTable)
		case "image":
			if dest, ok := r.links.file(stringAttr(c, "attachmentId")); ok {
				out = "![" + r.escape(stringAttr(c, "alt"), escTable, false) + "](" + destination(dest) + ")"
			} else {
				out = r.escape(stringAttr(c, "alt"), escTable, false)
			}
		default:
			if depth > document.MaxDepth {
				continue
			}
			words := document.PlainText(document.Node{Type: "doc", Content: []document.Node{c}})
			lines := strings.Split(words, "\n")
			for i, line := range lines {
				lines[i] = r.escape(line, escTable, false)
			}
			out = strings.Join(lines, "<br>")
		}
		if out != "" {
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "<br>")
}

// The contexts inline content is written in: a block of its own lines, a
// heading's one line, or a table cell's.
type inlineCtx int

const (
	ctxBlock inlineCtx = iota
	ctxHeading
	ctxTable
)

// escMode is which characters a piece of text has to keep from Markdown.
type escMode int

const (
	escText escMode = iota
	escHeading
	escTable
)

func (c inlineCtx) esc() escMode {
	switch c {
	case ctxHeading:
		return escHeading
	case ctxTable:
		return escTable
	}
	return escText
}

// inline writes inline nodes with their marks, opened in markOrder and kept
// open across nodes that share them, so bold over a mention is one run.
func (r renderer) inline(nodes []document.Node, ctx inlineCtx) string {
	// A closing asterisk after a space does not close, so spaces at the edge
	// of a styled run move outside it.
	nodes = splitEdges(nodes)
	var b strings.Builder
	var open []document.Mark
	atStart := true
	closeTo := func(keep int) {
		for i := len(open) - 1; i >= keep; i-- {
			b.WriteString(r.closer(open[i]))
		}
		open = open[:keep]
	}
	for _, n := range nodes {
		marks := styleMarks(n.Marks)
		var code bool
		if last := len(marks) - 1; last >= 0 && marks[last].Type == "code" {
			code = true
			marks = marks[:last]
		}
		keep := 0
		for keep < len(open) && keep < len(marks) && sameMark(open[keep], marks[keep]) {
			keep++
		}
		closeTo(keep)
		for _, m := range marks[keep:] {
			b.WriteString(opener(m))
			open = append(open, m)
			atStart = false
		}
		switch {
		case n.Type == "text" && code:
			b.WriteString(codeSpan(n.Text))
			atStart = false
		case n.Type == "text":
			text := strings.ReplaceAll(n.Text, "\n", " ")
			b.WriteString(r.escape(text, ctx.esc(), atStart))
			if text != "" {
				atStart = false
			}
		case n.Type == "hardBreak":
			if ctx == ctxBlock {
				b.WriteString("\\\n")
				atStart = true
			} else {
				b.WriteString("<br>")
			}
		default:
			b.WriteString(r.atom(n, ctx))
			atStart = false
		}
	}
	closeTo(0)
	return b.String()
}

// splitEdges moves the spaces at either end of styled text into text of
// their own that carries only the link and code marks.
func splitEdges(nodes []document.Node) []document.Node {
	out := make([]document.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type != "text" || !hasEmphasis(n.Marks) || hasMark(n.Marks, "code") {
			out = append(out, n)
			continue
		}
		core := strings.TrimRightFunc(strings.TrimLeftFunc(n.Text, unicode.IsSpace), unicode.IsSpace)
		if core == "" {
			n.Marks = withoutEmphasis(n.Marks)
			out = append(out, n)
			continue
		}
		lead := n.Text[:strings.Index(n.Text, core)]
		trail := n.Text[len(lead)+len(core):]
		if lead != "" {
			out = append(out, document.Node{Type: "text", Text: lead, Marks: withoutEmphasis(n.Marks)})
		}
		mid := n
		mid.Text = core
		out = append(out, mid)
		if trail != "" {
			out = append(out, document.Node{Type: "text", Text: trail, Marks: withoutEmphasis(n.Marks)})
		}
	}
	return out
}

func hasEmphasis(marks []document.Mark) bool {
	return hasMark(marks, "bold") || hasMark(marks, "italic") || hasMark(marks, "strike")
}

func hasMark(marks []document.Mark, t string) bool {
	return slices.ContainsFunc(marks, func(m document.Mark) bool { return m.Type == t })
}

func withoutEmphasis(marks []document.Mark) []document.Mark {
	var out []document.Mark
	for _, m := range marks {
		if m.Type != "bold" && m.Type != "italic" && m.Type != "strike" {
			out = append(out, m)
		}
	}
	return out
}

// styleMarks keeps the marks Markdown writes, in markOrder. A hint and a
// thread's passage are this site's own, so both leave their words behind.
func styleMarks(marks []document.Mark) []document.Mark {
	var out []document.Mark
	for _, m := range marks {
		if markRank(m.Type) >= 0 {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b document.Mark) int { return markRank(a.Type) - markRank(b.Type) })
	return out
}

func sameMark(a, b document.Mark) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Type != "link" {
		return true
	}
	return a.Attrs["href"] == b.Attrs["href"] && a.Attrs["title"] == b.Attrs["title"]
}

func opener(m document.Mark) string {
	switch m.Type {
	case "link":
		return "["
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	}
	return ""
}

func (r renderer) closer(m document.Mark) string {
	switch m.Type {
	case "link":
		href, _ := m.Attrs["href"].(string)
		out := "](" + destination(r.href(href))
		if title, _ := m.Attrs["title"].(string); title != "" {
			out += ` "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title) + `"`
		}
		return out + ")"
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	}
	return ""
}

var (
	pageHref = regexp.MustCompile(`^/s/[^/?#]+/p/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:/[^?#]*)?(#.*)?$`)
	fileHref = regexp.MustCompile(`^/api/v1/attachments/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:\?[^#]*)?$`)
)

// href points a link at an exported page or file where it can, so the link
// still works in the exported tree.
func (r renderer) href(href string) string {
	if m := pageHref.FindStringSubmatch(href); m != nil {
		if path, ok := r.links.page(m[1]); ok {
			return path + m[2]
		}
	}
	if m := fileHref.FindStringSubmatch(href); m != nil {
		if path, ok := r.links.file(m[1]); ok {
			return path
		}
	}
	return href
}

// atom writes an inline node that is not text: a file as a link to it, and
// the rest as a span whose words read as they do on the page.
func (r renderer) atom(n document.Node, ctx inlineCtx) string {
	esc := ctx.esc()
	switch n.Type {
	case "attachment":
		name := stringAttr(n, "fileName")
		if dest, ok := r.links.file(stringAttr(n, "attachmentId")); ok {
			return "[" + r.escape(name, esc, false) + "](" + destination(dest) + ")"
		}
		return r.escape(name, esc, false)
	case "mention":
		return span(kindMention, [][2]string{{"data-id", stringAttr(n, "id")}}, r.escape("@"+stringAttr(n, "label"), esc, false))
	case armature.NodeIssue:
		return span(kindIssue, nil, r.escape(stringAttr(n, "key"), esc, false))
	case document.NodeStatus:
		return span(kindStatus, [][2]string{{"data-color", stringAttr(n, "color")}}, r.escape(stringAttr(n, "label"), esc, false))
	case document.NodeDate:
		return span(kindDate, nil, r.escape(stringAttr(n, "date"), esc, false))
	case document.NodeMathInline:
		return inlineMath(stringAttr(n, "latex"), ctx)
	}
	return ""
}

// mathLanguage and diagramLanguage are the fence languages a formula on its
// own line and a diagram are written with.
const (
	mathLanguage    = "math"
	diagramLanguage = "mermaid"
)

// inlineMath writes a formula between dollar signs, its source as it is: TeX
// already writes a dollar inside a formula as \$, so a bare one is escaped
// only to keep the formula closed. A table splits its cells before anything
// else and takes every \| back to a pipe, so each pipe there gains one.
func inlineMath(latex string, ctx inlineCtx) string {
	latex = strings.Join(strings.Fields(latex), " ")
	var b strings.Builder
	escaped := false
	for _, c := range latex {
		if (c == '$' && !escaped) || (c == '|' && ctx == ctxTable) {
			b.WriteByte('\\')
		}
		escaped = c == '\\' && !escaped
		b.WriteRune(c)
	}
	return "$" + b.String() + "$"
}

func span(kind string, attrs [][2]string, inner string) string {
	return "<span" + htmlAttrs(kind, attrs) + ">" + inner + "</span>"
}

func div(kind string, attrs [][2]string, text string) string {
	return "<div" + htmlAttrs(kind, attrs) + ">" + htmlText(text) + "</div>"
}

func htmlAttrs(kind string, attrs [][2]string) string {
	var b strings.Builder
	b.WriteString(" " + statorAttr + `="` + kind + `"`)
	for _, a := range attrs {
		b.WriteString(" " + a[0] + `="` + htmlText(a[1]) + `"`)
	}
	return b.String()
}

// htmlText escapes text for an HTML element on one line: a raw line break
// would end the HTML block it stands in.
func htmlText(s string) string {
	return strings.NewReplacer("\n", "&#10;", "\r", "&#13;").Replace(html.EscapeString(s))
}

var entityLike = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

var lineStartList = regexp.MustCompile(`^[0-9]{1,9}[.)]`)

// escape keeps text from being read as Markdown: what only means something
// at the start of a line is escaped there, the rest wherever it could mean it.
func (r renderer) escape(s string, mode escMode, atStart bool) string {
	var b strings.Builder
	if atStart {
		if m := lineStartList.FindString(s); m != "" {
			b.WriteString(m[:len(m)-1] + `\` + m[len(m)-1:])
			s = s[len(m):]
			atStart = false
		}
	}
	runes := []rune(s)
	for i, c := range runes {
		switch c {
		case '\\', '`', '*', '[', ']', '~':
			b.WriteRune('\\')
		case '_':
			if i == 0 || i == len(runes)-1 || !isWord(runes[i-1]) || !isWord(runes[i+1]) {
				b.WriteRune('\\')
			}
		case '<':
			if i+1 < len(runes) && (unicode.IsLetter(runes[i+1]) || strings.ContainsRune("/!?", runes[i+1])) {
				b.WriteRune('\\')
			}
		case '&':
			if entityLike.MatchString(string(runes[i:min(len(runes), i+40)])) {
				b.WriteRune('\\')
			}
		case '|':
			if mode == escTable {
				b.WriteRune('\\')
			}
		case '#':
			if mode == escHeading || (i == 0 && atStart) {
				b.WriteRune('\\')
			}
		case '-', '+', '=', '>':
			if i == 0 && atStart {
				b.WriteRune('\\')
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}

func isWord(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }

// codeSpan fences inline code with one backtick more than the longest run
// inside it, padded where a backtick or space would otherwise be lost.
func codeSpan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	fence := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") ||
		(strings.HasPrefix(s, " ") && strings.HasSuffix(s, " ") && strings.TrimSpace(s) != "") {
		s = " " + s + " "
	}
	return fence + s + fence
}

func codeFence(text, language string) string {
	fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return fence + language + "\n" + fence
	}
	return fence + language + "\n" + text + "\n" + fence
}

func longestRun(s string, c rune) int {
	best, run := 0, 0
	for _, r := range s {
		if r == c {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// destination writes a link target Markdown reads back whole: in angle
// brackets when it holds a space or a parenthesis.
func destination(href string) string {
	if !strings.ContainsAny(href, " ()<>") {
		return href
	}
	return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(href) + ">"
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n")
}

// prefixLines puts first before the first line and rest before every other
// line that is not blank.
func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		switch {
		case i == 0:
			lines[i] = first + line
		case line != "":
			lines[i] = rest + line
		}
	}
	return strings.Join(lines, "\n")
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
	}
	return fallback
}

// pathSegment escapes one name of a path for a link target.
func pathSegment(name string) string {
	var b strings.Builder
	for _, c := range []byte(name) {
		if c < utf8.RuneSelf && (unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || strings.IndexByte("-._~", c) >= 0) {
			b.WriteByte(c)
			continue
		}
		if c >= utf8.RuneSelf {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// PathRef makes a slash separated path a link target: each name escaped,
// letters beyond ASCII kept as they are, which Markdown reads as written.
func PathRef(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = pathSegment(p)
	}
	return strings.Join(parts, "/")
}
