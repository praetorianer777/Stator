package markdown

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	nethtml "golang.org/x/net/html"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// TargetKind is what a relative link or image in a file points at.
type TargetKind int

const (
	// TargetNone is a path that is not part of the import.
	TargetNone TargetKind = iota
	// TargetFile is a file that becomes an attachment of the page.
	TargetFile
	// TargetPage is another Markdown file of the import, now a page.
	TargetPage
)

// Target is where a relative reference leads once imported.
type Target struct {
	Kind TargetKind
	// AttachmentID and FileName name the file a TargetFile became.
	AttachmentID string
	FileName     string
	// Href is the address of the page a TargetPage became.
	Href string
}

// Resolver answers where a relative link or image target leads. It is given
// the target as written, escapes resolved, and is only asked about paths.
type Resolver func(dest string) Target

// Result is one Markdown file as a page.
type Result struct {
	// Title is the text of the file's one level 1 heading when the file
	// opens with it, and empty otherwise.
	Title string
	// Body is the document, already held to the allowlist.
	Body json.RawMessage
	// Warnings say, a sentence each, what did not come across as written.
	Warnings []string
}

var (
	// ErrTooLarge refuses a file over MaxSourceBytes.
	ErrTooLarge = fmt.Errorf("this Markdown file is over %d MB; split it into several files", MaxSourceBytes>>20)
	// ErrTooDeep refuses lists and quotes nested past document.MaxDepth.
	ErrTooDeep = errors.New("this Markdown file nests its lists and quotes too deeply; flatten them")
	// ErrTooTangled refuses long lines full of brackets, which take the
	// parser time with the square of their length.
	ErrTooTangled = errors.New("this Markdown file has long lines full of brackets; break them into shorter lines")
)

// replacement stands in for bytes that are not text.
const replacement = "\uFFFD"

// maxWarnings bounds how many different warnings one file reports.
const maxWarnings = 20

var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList)).Parser()

// Convert reads a Markdown file as a page. Raw HTML never reaches it: only
// the elements Render writes are read back, as the nodes they stand for.
func Convert(src []byte, resolve Resolver) (*Result, error) {
	return convert(src, resolve, false)
}

// ConvertTemplate reads a Markdown file as a template's body: as Convert
// does, and with a template's variables read back as variables, where a
// page keeps their words.
func ConvertTemplate(src []byte, resolve Resolver) (*Result, error) {
	return convert(src, resolve, true)
}

func convert(src []byte, resolve Resolver, template bool) (*Result, error) {
	if len(src) > MaxSourceBytes {
		return nil, ErrTooLarge
	}
	src = bytes.ReplaceAll(bytes.ToValidUTF8(src, []byte(replacement)), []byte{0}, []byte(replacement))
	if nestsTooDeeply(src) {
		return nil, ErrTooDeep
	}
	if tooTangled(src) {
		return nil, ErrTooTangled
	}
	if resolve == nil {
		resolve = func(string) Target { return Target{} }
	}
	c := &converter{resolve: resolve, seen: map[string]bool{}, template: template}
	root := parser.Parse(text.NewReader(src))
	c.src = src

	shift := 0
	var title string
	if first, ok := root.FirstChild().(*ast.Heading); ok && first.Level == 1 && countTitles(root) == 1 {
		title = strings.TrimSpace(c.plain(first))
		root.RemoveChild(root, first)
		shift = 1
	}
	c.shift = shift
	content, err := c.blocks(children(root), 0)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	doc := document.Normalize(document.Node{Type: "doc", Content: content})
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if err := check(body, template); err != nil {
		return nil, err
	}
	return &Result{Title: title, Body: body, Warnings: c.warnings}, nil
}

// nestsTooDeeply looks for a line that opens more quotes and list items than
// a page nests, before the parser spends time with the square of it.
func nestsTooDeeply(src []byte) bool {
	// Nesting by indentation alone takes two columns a level.
	const maxIndent = 4 * document.MaxDepth
	for line := range bytes.SplitSeq(src, []byte("\n")) {
		indent := 0
		for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
			indent++
		}
		if indent > maxIndent {
			return true
		}
		depth, i := 0, 0
		for i < len(line) {
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			if i >= len(line) {
				break
			}
			switch c := line[i]; {
			case c == '>':
				i++
			case (c == '-' || c == '*' || c == '+') && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t'):
				i++
			case c >= '0' && c <= '9':
				j := i
				for j < len(line) && j-i < 10 && line[j] >= '0' && line[j] <= '9' {
					j++
				}
				if j >= len(line) || (line[j] != '.' && line[j] != ')') {
					i = len(line)
					continue
				}
				i = j + 1
			default:
				i = len(line)
				continue
			}
			depth++
			if depth > document.MaxDepth {
				return true
			}
		}
	}
	return false
}

// maxParseCost bounds, per file, the brackets of each line times its length:
// the work a link or tag that never closes costs the parser.
const maxParseCost = 50_000_000

func tooTangled(src []byte) bool {
	cost := 0
	for line := range bytes.SplitSeq(src, []byte("\n")) {
		cost += (bytes.Count(line, []byte("[")) + bytes.Count(line, []byte("<"))) * len(line)
		if cost > maxParseCost {
			return true
		}
	}
	return false
}

// countTitles counts the level 1 headings at the top of a file, stopping at two.
func countTitles(root ast.Node) int {
	n := 0
	for c := root.FirstChild(); c != nil && n < 2; c = c.NextSibling() {
		if h, ok := c.(*ast.Heading); ok && h.Level == 1 {
			n++
		}
	}
	return n
}

type converter struct {
	src      []byte
	resolve  Resolver
	shift    int
	warnings []string
	seen     map[string]bool
	nest     int
	// detailsSeen remembers what each HTML block says of details elements.
	detailsSeen map[ast.Node]detailsInfo
	// template reads into a template's body, which may hold variables.
	template bool
}

// maxInlineNesting bounds how deep emphasis and links are followed; what is
// nested deeper keeps its words without their styles.
const maxInlineNesting = 2 * document.MaxDepth

func (c *converter) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if c.seen[msg] || len(c.warnings) >= maxWarnings {
		return
	}
	c.seen[msg] = true
	c.warnings = append(c.warnings, msg)
}

func children(n ast.Node) []ast.Node {
	var out []ast.Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, c)
	}
	return out
}

// blocks converts a run of sibling blocks. An expand block opens with a
// details element and closes with its end tag further along the same run.
func (c *converter) blocks(nodes []ast.Node, depth int) ([]document.Node, error) {
	if depth > document.MaxDepth {
		return nil, ErrTooDeep
	}
	var out []document.Node
	closes := c.detailsCloses(nodes)
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		if d := c.details(n); d.open {
			if d.whole {
				made, err := c.expandFrom(d.title, d.inner, depth)
				if err != nil {
					return nil, err
				}
				out = append(out, made)
				continue
			}
			end := closes[i]
			inner, err := c.blocks(nodes[i+1:end], depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, expand(d.title, inner))
			i = end
			continue
		}
		made, err := c.block(n, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, made...)
	}
	return out, nil
}

// detailsCloses pairs each details start in a run with the block that
// closes it, inner ones first; a start left open closes at the run's end.
func (c *converter) detailsCloses(nodes []ast.Node) map[int]int {
	closes := map[int]int{}
	var open []int
	for j, n := range nodes {
		d := c.details(n)
		if d.open && !d.whole {
			open = append(open, j)
			continue
		}
		if d.close && len(open) > 0 {
			closes[open[len(open)-1]] = j
			open = open[:len(open)-1]
		}
	}
	for _, j := range open {
		closes[j] = len(nodes)
	}
	return closes
}

// detailsInfo is what a block says of a details element: whether it opens
// one, all of it or just its start, or closes one.
type detailsInfo struct {
	open, whole, close bool
	title, inner       string
}

// details reads a block once, however many runs it is part of as the runs
// around an unclosed element are read again one level down.
func (c *converter) details(n ast.Node) detailsInfo {
	h, ok := n.(*ast.HTMLBlock)
	if !ok {
		return detailsInfo{}
	}
	if d, ok := c.detailsSeen[n]; ok {
		return d
	}
	raw := c.lines(h)
	var d detailsInfo
	if title, rest, ok := detailsOpen(raw); ok {
		d.open, d.title = true, title
		d.inner, d.whole = detailsWhole(rest)
	} else {
		d.close = detailsClose.MatchString(raw)
	}
	if c.detailsSeen == nil {
		c.detailsSeen = map[ast.Node]detailsInfo{}
	}
	c.detailsSeen[n] = d
	return d
}

func expand(title string, content []document.Node) document.Node {
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	return document.Node{Type: "expand", Attrs: map[string]any{"title": title}, Content: content}
}

// expandFrom reads a details element written as one HTML block, whose
// inside is Markdown of its own.
func (c *converter) expandFrom(title, inner string, depth int) (document.Node, error) {
	if depth+1 > document.MaxDepth {
		return document.Node{}, ErrTooDeep
	}
	root := parser.Parse(text.NewReader([]byte(inner)))
	sub := &converter{src: []byte(inner), resolve: c.resolve, shift: c.shift, seen: c.seen, warnings: c.warnings}
	content, err := sub.blocks(children(root), depth+1)
	c.warnings = sub.warnings
	if err != nil {
		return document.Node{}, err
	}
	return expand(title, content), nil
}

var (
	detailsStart = regexp.MustCompile(`(?is)^\s*<details(?:\s[^>]*)?>\s*(?:<summary(?:\s[^>]*)?>(.*?)</summary>)?`)
	detailsEnd   = regexp.MustCompile(`(?is)</details>\s*$`)
	detailsClose = regexp.MustCompile(`(?is)^\s*</details>\s*$`)
	tagPattern   = regexp.MustCompile(`<[^>]*>`)
)

// detailsOpen reads the start of a details element: its summary as plain
// words, at most an expand block title long, and whatever follows.
func detailsOpen(raw string) (string, string, bool) {
	m := detailsStart.FindStringSubmatchIndex(raw)
	if m == nil {
		return "", "", false
	}
	title := ""
	if m[2] >= 0 {
		title = html.UnescapeString(tagPattern.ReplaceAllString(raw[m[2]:m[3]], ""))
		title = strings.Join(strings.Fields(title), " ")
		if utf8.RuneCountInString(title) > document.MaxExpandTitleLength {
			title = string([]rune(title)[:document.MaxExpandTitleLength])
		}
	}
	return title, raw[m[1]:], true
}

// detailsWhole says whether what follows a details start also holds its end,
// and answers what lies between.
func detailsWhole(rest string) (string, bool) {
	loc := detailsEnd.FindStringIndex(rest)
	if loc == nil {
		return "", false
	}
	return rest[:loc[0]], true
}

func (c *converter) lines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	if h, ok := n.(*ast.HTMLBlock); ok && h.HasClosure() {
		b.Write(h.ClosureLine.Value(c.src))
	}
	return b.String()
}

func (c *converter) block(n ast.Node, depth int) ([]document.Node, error) {
	if depth > document.MaxDepth {
		return nil, ErrTooDeep
	}
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return c.paragraphs(c.inlines(children(n), nil, true)), nil
	case *ast.Heading:
		level := min(max(n.Level-c.shift, 1), document.MaxHeadingLevel)
		content := mergeText(c.flatten(c.inlines(children(n), nil, false)))
		return []document.Node{{Type: "heading", Attrs: map[string]any{"level": level, "id": nil}, Content: trimInline(content)}}, nil
	case *ast.ThematicBreak:
		return []document.Node{{Type: "horizontalRule"}}, nil
	case *ast.CodeBlock:
		return []document.Node{codeBlock(c.lines(n), nil)}, nil
	case *ast.FencedCodeBlock:
		var language any
		if n.Info != nil {
			if fields := strings.Fields(string(n.Language(c.src))); len(fields) > 0 {
				lang := strings.ToLower(decode(fields[0]))
				if languagePattern.MatchString(lang) && len(lang) <= maxLanguageLength {
					language = lang
				}
			}
		}
		return []document.Node{codeBlock(c.lines(n), language)}, nil
	case *ast.Blockquote:
		return c.blockquote(n, depth)
	case *ast.List:
		return c.list(n, depth)
	case *ast.HTMLBlock:
		return c.htmlBlock(c.lines(n)), nil
	case *extast.Table:
		return c.table(n, depth)
	}
	return nil, nil
}

var languagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+#-]*$`)

// maxLanguageLength matches the longest language name a code block keeps.
const maxLanguageLength = 32

func codeBlock(code string, language any) document.Node {
	code = strings.TrimSuffix(code, "\n")
	n := document.Node{Type: "codeBlock", Attrs: map[string]any{"language": language}}
	if code != "" {
		n.Content = []document.Node{{Type: "text", Text: code}}
	}
	return n
}

var alertLine = regexp.MustCompile(`^\s*\[!([A-Za-z]+)\]\s*$`)

// blockquote reads a quote, or a panel when its first line is an alert.
func (c *converter) blockquote(n *ast.Blockquote, depth int) ([]document.Node, error) {
	kids := children(n)
	kind := ""
	if p, ok := n.FirstChild().(*ast.Paragraph); ok && p.Lines().Len() > 0 {
		first := p.Lines().At(0)
		firstLine := first.Value(c.src)
		if m := alertLine.FindStringSubmatch(string(firstLine)); m != nil {
			for k, alert := range panelAlerts {
				if strings.EqualFold(alert, m[1]) {
					kind = k
				}
			}
			if kind != "" {
				end := first.Stop
				for child := p.FirstChild(); child != nil; {
					next := child.NextSibling()
					if t, ok := child.(*ast.Text); ok && t.Segment.Start < end {
						p.RemoveChild(p, child)
					} else {
						break
					}
					child = next
				}
				if p.FirstChild() == nil {
					kids = kids[1:]
				}
			}
		}
	}
	content, err := c.blocks(kids, depth+1)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	if kind != "" {
		return []document.Node{{Type: "panel", Attrs: map[string]any{"kind": kind}, Content: content}}, nil
	}
	return []document.Node{{Type: "blockquote", Content: content}}, nil
}

// list reads a list; one whose every item opens with a check box is a task
// list, and in any other list a check box stays as its text.
func (c *converter) list(n *ast.List, depth int) ([]document.Node, error) {
	items := children(n)
	task := len(items) > 0
	for _, item := range items {
		if checkBox(item) == nil {
			task = false
		}
	}
	out := document.Node{Type: "bulletList"}
	itemType := "listItem"
	switch {
	case n.IsOrdered():
		start := n.Start
		if start < 0 || start > document.MaxListStart {
			start = 1
		}
		out = document.Node{Type: "orderedList", Attrs: map[string]any{"start": start, "type": nil}}
	case task:
		out.Type = "taskList"
		itemType = "taskItem"
	}
	for _, item := range items {
		made := document.Node{Type: itemType}
		if task {
			box := checkBox(item)
			made.Attrs = map[string]any{"checked": box.IsChecked}
			parent := box.Parent()
			parent.RemoveChild(parent, box)
			if t, ok := parent.FirstChild().(*ast.Text); ok {
				v := t.Segment.Value(c.src)
				trimmed := bytes.TrimLeft(v, " \t")
				t.Segment = t.Segment.WithStart(t.Segment.Start + len(v) - len(trimmed))
			}
		}
		content, err := c.blocks(children(item), depth+2)
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			content = []document.Node{{Type: "paragraph"}}
		}
		made.Content = content
		out.Content = append(out.Content, made)
	}
	return []document.Node{out}, nil
}

func checkBox(item ast.Node) *extast.TaskCheckBox {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	box, _ := first.FirstChild().(*extast.TaskCheckBox)
	return box
}

// table reads a GFM table: its header row as header cells, each column's
// alignment on every cell of it.
func (c *converter) table(n *extast.Table, depth int) ([]document.Node, error) {
	out := document.Node{Type: "table"}
	for _, row := range children(n) {
		cellType := "tableCell"
		if _, ok := row.(*extast.TableHeader); ok {
			cellType = "tableHeader"
		}
		made := document.Node{Type: "tableRow"}
		for _, cell := range children(row) {
			attrs := map[string]any{"colspan": 1, "rowspan": 1, "colwidth": nil}
			if tc, ok := cell.(*extast.TableCell); ok {
				switch tc.Alignment {
				case extast.AlignLeft:
					attrs["align"] = "left"
				case extast.AlignCenter:
					attrs["align"] = "center"
				case extast.AlignRight:
					attrs["align"] = "right"
				}
			}
			content := c.paragraphs(c.inlines(children(cell), nil, true))
			if len(content) == 0 {
				content = []document.Node{{Type: "paragraph"}}
			}
			made.Content = append(made.Content, document.Node{Type: cellType, Attrs: attrs, Content: content})
		}
		if len(made.Content) > 0 {
			out.Content = append(out.Content, made)
		}
	}
	if depth+2 > document.MaxDepth {
		return nil, ErrTooDeep
	}
	if len(out.Content) == 0 {
		return nil, nil
	}
	return []document.Node{out}, nil
}

// htmlBlock reads an HTML block: an element Render writes is its node, and
// anything else but pictures and comments is its source in a code block.
func (c *converter) htmlBlock(raw string) []document.Node {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || (strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->")) || detailsClose.MatchString(trimmed) {
		return nil
	}
	if made, ok := c.statorDiv(trimmed); ok {
		return made
	}
	if images, ok := c.imagesOnly(trimmed); ok {
		return images
	}
	c.warn("HTML was kept as code, since a page holds no HTML.")
	return []document.Node{codeBlock(raw, "html")}
}

// element is one start tag, read by the HTML tokenizer.
type element struct {
	name  string
	attrs map[string]string
}

func startTag(tok *nethtml.Tokenizer) element {
	name, more := tok.TagName()
	e := element{name: string(name), attrs: map[string]string{}}
	for more {
		var k, v []byte
		k, v, more = tok.TagAttr()
		e.attrs[string(k)] = string(v)
	}
	return e
}

// statorDiv reads the div Render writes for a block Markdown has no syntax
// for: its start tag, its text and its end tag, and nothing else.
func (c *converter) statorDiv(raw string) ([]document.Node, bool) {
	tok := nethtml.NewTokenizer(strings.NewReader(raw))
	if tok.Next() != nethtml.StartTagToken {
		return nil, false
	}
	e := startTag(tok)
	kind, ok := e.attrs[statorAttr]
	if e.name != "div" || !ok {
		return nil, false
	}
	var words strings.Builder
	for {
		switch tok.Next() {
		case nethtml.TextToken:
			words.Write(tok.Text())
			continue
		case nethtml.EndTagToken:
			if name, _ := tok.TagName(); string(name) == "div" {
				if rest := tok.Next(); rest != nethtml.ErrorToken {
					return nil, false
				}
				n, ok := divNode(kind, e.attrs, strings.TrimSpace(words.String()))
				if !ok || !validates(n, false) {
					c.warn("A %s block could not be read and was left out.", kind)
					return nil, true
				}
				return []document.Node{n}, true
			}
		}
		return nil, false
	}
}

func divNode(kind string, attrs map[string]string, words string) (document.Node, bool) {
	switch kind {
	case kindIssue:
		return document.Node{Type: armature.NodeIssueBlock, Attrs: map[string]any{"key": words}}, true
	case kindIssueList:
		columns := []string{}
		for col := range strings.SplitSeq(attrs["data-columns"], ",") {
			if col = strings.TrimSpace(col); col != "" {
				columns = append(columns, col)
			}
		}
		n := document.Node{Type: armature.NodeIssueList, Attrs: map[string]any{"query": words, "columns": columns}}
		if v, ok := attrs["data-limit"]; ok {
			limit, err := strconv.Atoi(v)
			if err != nil {
				return document.Node{}, false
			}
			n.Attrs["limit"] = limit
		}
		return n, true
	case kindTOC:
		level, err := strconv.Atoi(attrs["data-max-level"])
		if err != nil {
			level = document.MaxHeadingLevel
		}
		return document.Node{Type: "tableOfContents", Attrs: map[string]any{"maxLevel": level}}, true
	case kindChildPages:
		n := document.Node{Type: "childPages", Attrs: map[string]any{"scope": attrs["data-scope"], "sort": attrs["data-sort"], "depth": nil}}
		if v, ok := attrs["data-depth"]; ok {
			depth, err := strconv.Atoi(v)
			if err != nil {
				return document.Node{}, false
			}
			n.Attrs["depth"] = depth
		}
		return n, true
	}
	return document.Node{}, false
}

// imagesOnly reads an HTML block that is nothing but img tags, perhaps in a
// paragraph or a picture element, and spaces between them.
func (c *converter) imagesOnly(raw string) ([]document.Node, bool) {
	tok := nethtml.NewTokenizer(strings.NewReader(raw))
	var out []document.Node
	for {
		switch tok.Next() {
		case nethtml.ErrorToken:
			return out, len(out) > 0
		case nethtml.TextToken:
			if strings.TrimSpace(string(tok.Text())) != "" {
				return nil, false
			}
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			e := startTag(tok)
			switch e.name {
			case "img":
				out = append(out, c.imageItem(e.attrs["src"], e.attrs["alt"], e.attrs["width"]).nodes()...)
			case "p", "picture", "div", "a":
			default:
				return nil, false
			}
		case nethtml.EndTagToken:
		default:
			return nil, false
		}
	}
}

// item is one piece of a paragraph as it is read: an inline node, or an
// image, which a page holds as a block of its own.
type item struct {
	node  document.Node
	image bool
}

type items []item

func (it items) nodes() []document.Node {
	out := make([]document.Node, 0, len(it))
	for _, i := range it {
		out = append(out, i.node)
	}
	return out
}

// paragraphs splits read inline content at its images: the text between
// them is a paragraph each, the images blocks of their own.
func (c *converter) paragraphs(content items) []document.Node {
	var out []document.Node
	var run []document.Node
	flush := func() {
		run = trimInline(mergeText(run))
		if len(run) > 0 {
			out = append(out, document.Node{Type: "paragraph", Content: run})
		}
		run = nil
	}
	for _, i := range content {
		if i.image {
			flush()
			out = append(out, i.node)
			continue
		}
		run = append(run, i.node)
	}
	flush()
	return out
}

// mergeText joins neighbouring text of the same style in one pass, which
// joining them pairwise would make quadratic in a line of many pieces.
func mergeText(nodes []document.Node) []document.Node {
	var out []document.Node
	for i := 0; i < len(nodes); {
		n := nodes[i]
		j := i + 1
		if n.Type == "text" {
			var b strings.Builder
			b.WriteString(n.Text)
			for j < len(nodes) && nodes[j].Type == "text" && reflect.DeepEqual(nodes[j].Marks, n.Marks) {
				b.WriteString(nodes[j].Text)
				j++
			}
			n.Text = b.String()
		}
		out = append(out, n)
		i = j
	}
	return out
}

// flatten turns images into their words, for a heading, which holds none.
func (c *converter) flatten(content items) []document.Node {
	var out []document.Node
	for _, i := range content {
		if !i.image {
			out = append(out, i.node)
			continue
		}
		if alt, _ := i.node.Attrs["alt"].(string); alt != "" {
			out = append(out, document.Node{Type: "text", Text: alt})
		}
	}
	return out
}

// trimInline drops the spaces at either end of a line of inline nodes, which
// Markdown does not show.
func trimInline(nodes []document.Node) []document.Node {
	for len(nodes) > 0 && nodes[0].Type == "text" {
		nodes[0].Text = strings.TrimLeftFunc(nodes[0].Text, unicode.IsSpace)
		if nodes[0].Text != "" {
			break
		}
		nodes = nodes[1:]
	}
	for len(nodes) > 0 && nodes[len(nodes)-1].Type == "text" {
		last := len(nodes) - 1
		nodes[last].Text = strings.TrimRightFunc(nodes[last].Text, unicode.IsSpace)
		if nodes[last].Text != "" {
			break
		}
		nodes = nodes[:last]
	}
	return nodes
}

// withMark adds a mark unless one of its type is there, which nested
// emphasis of one kind would otherwise do twice.
func withMark(marks []document.Mark, m document.Mark) []document.Mark {
	if hasMark(marks, m.Type) {
		return marks
	}
	out := append(slices.Clone(marks), m)
	slices.SortStableFunc(out, func(a, b document.Mark) int { return markRank(a.Type) - markRank(b.Type) })
	return out
}

func textItem(s string, marks []document.Mark) items {
	if s == "" {
		return nil
	}
	return items{{node: document.Node{Type: "text", Text: s, Marks: marks}}}
}

// inlines reads a run of inline nodes under the marks given. A span Render
// wrote takes the nodes up to its end tag as its words.
func (c *converter) inlines(nodes []ast.Node, marks []document.Mark, images bool) items {
	closers := c.spanClosers(nodes)
	c.nest++
	defer func() { c.nest-- }()
	if c.nest > maxInlineNesting {
		var words strings.Builder
		for _, n := range nodes {
			words.WriteString(c.plain(n))
		}
		return textItem(words.String(), marks)
	}
	var out items
	for i := 0; i < len(nodes); i++ {
		switch n := nodes[i].(type) {
		case *ast.Text:
			v := n.Segment.Value(c.src)
			s := string(v)
			if !n.IsRaw() {
				s = decode(s)
			}
			out = append(out, textItem(s, marks)...)
			switch {
			case n.HardLineBreak():
				out = append(out, item{node: document.Node{Type: "hardBreak", Marks: marks}})
			case n.SoftLineBreak():
				out = append(out, textItem(" ", marks)...)
			}
		case *ast.String:
			out = append(out, textItem(string(n.Value), marks)...)
		case *ast.CodeSpan:
			out = append(out, textItem(c.code(n), withMark(marks, document.Mark{Type: "code"}))...)
		case *ast.Emphasis:
			mark := document.Mark{Type: "italic"}
			if n.Level >= 2 {
				mark.Type = "bold"
			}
			out = append(out, c.inlines(children(n), withMark(marks, mark), images)...)
		case *extast.Strikethrough:
			out = append(out, c.inlines(children(n), withMark(marks, document.Mark{Type: "strike"}), images)...)
		case *ast.Link:
			out = append(out, c.link(n, marks, images)...)
		case *ast.AutoLink:
			label := string(n.Label(c.src))
			href := string(n.URL(c.src))
			if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(href), "mailto:") {
				href = "mailto:" + href
			}
			if document.SafeHref(href) {
				out = append(out, textItem(label, withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": href}}))...)
			} else {
				out = append(out, textItem(label, marks)...)
			}
		case *ast.Image:
			alt := c.plain(n)
			if !images {
				out = append(out, textItem(alt, marks)...)
				continue
			}
			out = append(out, c.imageItem(decode(string(n.Destination)), alt, "")...)
		case *extast.TaskCheckBox:
			box := "[ ] "
			if n.IsChecked {
				box = "[x] "
			}
			out = append(out, textItem(box, marks)...)
		case *ast.RawHTML:
			raw := c.raw(n)
			made, skip := c.rawHTML(raw, nodes[i+1:], closers[i+1]-(i+1), marks, images)
			out = append(out, made...)
			i += skip
		default:
			out = append(out, c.inlines(children(n), marks, images)...)
		}
	}
	return out
}

func (c *converter) code(n *ast.CodeSpan) string {
	var b strings.Builder
	for k := n.FirstChild(); k != nil; k = k.NextSibling() {
		switch t := k.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(c.src))
		case *ast.String:
			b.Write(t.Value)
		}
	}
	return strings.ReplaceAll(b.String(), "\n", " ")
}

func (c *converter) raw(n *ast.RawHTML) string {
	var b strings.Builder
	for i := range n.Segments.Len() {
		seg := n.Segments.At(i)
		b.Write(seg.Value(c.src))
	}
	return b.String()
}

// spanClosers says, for each place in a run, where the next span end tag is
// at or after it, or the run's length when none follows.
func (c *converter) spanClosers(nodes []ast.Node) []int {
	out := make([]int, len(nodes)+1)
	out[len(nodes)] = len(nodes)
	for i := len(nodes) - 1; i >= 0; i-- {
		out[i] = out[i+1]
		if r, ok := nodes[i].(*ast.RawHTML); ok && strings.EqualFold(strings.TrimSpace(c.raw(r)), "</span>") {
			out[i] = i
		}
	}
	return out
}

var brTag = regexp.MustCompile(`(?i)^<br\s*/?>$`)

// rawHTML reads one inline tag, and for a span Render wrote, the nodes after
// it up to its end tag, answering how many of those it took.
func (c *converter) rawHTML(raw string, rest []ast.Node, end int, marks []document.Mark, images bool) (items, int) {
	if brTag.MatchString(strings.TrimSpace(raw)) {
		return items{{node: document.Node{Type: "hardBreak", Marks: marks}}}, 0
	}
	if strings.HasPrefix(raw, "<!--") {
		return nil, 0
	}
	tok := nethtml.NewTokenizer(strings.NewReader(raw))
	kind := tok.Next()
	if kind != nethtml.StartTagToken && kind != nethtml.SelfClosingTagToken {
		return nil, 0
	}
	e := startTag(tok)
	if e.name == "img" {
		if !images {
			return textItem(e.attrs["alt"], marks), 0
		}
		return c.imageItem(e.attrs["src"], e.attrs["alt"], e.attrs["width"]), 0
	}
	statorKind, ok := e.attrs[statorAttr]
	if e.name != "span" || !ok {
		c.warn("HTML tags were left out and their words kept, since a page holds no HTML.")
		return nil, 0
	}
	if end >= len(rest) {
		return nil, 0
	}
	var words strings.Builder
	for _, n := range rest[:end] {
		words.WriteString(c.plain(n))
	}
	text := strings.TrimSpace(words.String())
	n, ok := spanNode(statorKind, e.attrs, text)
	if !ok || !validatesIn(n, true, c.template) {
		return textItem(text, marks), end + 1
	}
	n.Marks = marks
	return items{{node: n}}, end + 1
}

func spanNode(kind string, attrs map[string]string, words string) (document.Node, bool) {
	switch kind {
	case kindMention:
		return document.Node{Type: "mention", Attrs: map[string]any{
			"id": attrs["data-id"], "label": strings.TrimPrefix(words, "@"), "mentionSuggestionChar": "@",
		}}, true
	case kindIssue:
		return document.Node{Type: armature.NodeIssue, Attrs: map[string]any{"key": words}}, true
	case kindStatus:
		return document.Node{Type: document.NodeStatus, Attrs: map[string]any{"label": words, "color": attrs["data-color"]}}, true
	case kindDate:
		return document.Node{Type: document.NodeDate, Attrs: map[string]any{"date": words}}, true
	case kindVariable:
		return document.Node{Type: document.NodeVariable, Attrs: map[string]any{"name": attrs["data-name"]}}, true
	}
	return document.Node{}, false
}

// link reads a link, asking the resolver where a relative one leads: to a
// file of the page, a chip when the words are its name, or to another page.
func (c *converter) link(n *ast.Link, marks []document.Mark, images bool) items {
	dest := decode(string(n.Destination))
	words := c.plain(n)
	if relative(dest) {
		target := c.resolve(dest)
		switch target.Kind {
		case TargetFile:
			if strings.TrimSpace(words) == "" || words == target.FileName {
				return items{{node: document.Node{Type: "attachment", Marks: marks, Attrs: map[string]any{
					"attachmentId": target.AttachmentID, "fileName": target.FileName,
				}}}}
			}
			dest = "/api/v1/attachments/" + target.AttachmentID
		case TargetPage:
			dest = target.Href
		default:
			c.warn("The link to %s points outside the import, so only its words were kept.", dest)
			return c.inlines(children(n), marks, images)
		}
	}
	if !document.SafeHref(dest) {
		c.warn("A link that is not a web, mail or site address was left out, its words kept.")
		return c.inlines(children(n), marks, images)
	}
	attrs := map[string]any{"href": dest}
	if title := decode(string(n.Title)); title != "" {
		attrs["title"] = title
	}
	return c.inlines(children(n), withMark(marks, document.Mark{Type: "link", Attrs: attrs}), images)
}

// imageItem reads an image. Only a file of the import can be one, since a
// page shows its own files; any other picture becomes a link to it.
func (c *converter) imageItem(src, alt, width string) items {
	if relative(src) {
		if target := c.resolve(src); target.Kind == TargetFile {
			attrs := map[string]any{"attachmentId": target.AttachmentID, "alt": nil, "width": nil}
			if alt != "" {
				attrs["alt"] = alt
			}
			if w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(width), "px")); err == nil && w > 0 {
				attrs["width"] = w
			}
			n := document.Node{Type: "image", Attrs: attrs}
			if validates(n, false) {
				return items{{node: n, image: true}}
			}
		}
		c.warn("The picture %s is not in the import, so only its description was kept.", src)
		return textItem(alt, nil)
	}
	words := alt
	if words == "" {
		words = src
	}
	if document.SafeHref(src) {
		c.warn("Pictures from elsewhere became links, since a page shows only its own files.")
		return textItem(words, []document.Mark{{Type: "link", Attrs: map[string]any{"href": src}}})
	}
	return textItem(alt, nil)
}

// relative says whether a target is a path within the import: no scheme, and
// neither a site address nor a fragment.
func relative(dest string) bool {
	if dest == "" || strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "?") {
		return false
	}
	colon := strings.IndexByte(dest, ':')
	end := strings.IndexAny(dest, "/?#")
	return colon < 0 || (end >= 0 && end < colon)
}

// plain is the words of a node and everything in it, escapes resolved.
func (c *converter) plain(n ast.Node) string {
	var b strings.Builder
	var walk func(n ast.Node, depth int)
	walk = func(n ast.Node, depth int) {
		if depth > document.MaxDepth {
			return
		}
		switch t := n.(type) {
		case *ast.Text:
			v := string(t.Segment.Value(c.src))
			if !t.IsRaw() {
				v = decode(v)
			}
			b.WriteString(v)
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
			return
		case *ast.String:
			b.Write(t.Value)
			return
		case *ast.CodeSpan:
			b.WriteString(c.code(t))
			return
		case *ast.RawHTML:
			return
		}
		for k := n.FirstChild(); k != nil; k = k.NextSibling() {
			walk(k, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// decode resolves backslash escapes and character references in one pass,
// as Markdown reads text, so an escaped ampersand stays an ampersand.
func decode(s string) string {
	if !strings.ContainsAny(s, `\&`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) && isASCIIPunct(s[i+1]) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
		case '&':
			if m := entityLike.FindString(s[i:]); m != "" {
				if out := html.UnescapeString(m); out != m {
					b.WriteString(strings.ReplaceAll(out, "\x00", replacement))
					i += len(m) - 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isASCIIPunct(c byte) bool {
	return c < 0x80 && unicode.IsPunct(rune(c)) || strings.IndexByte("$+<=>^`|~", c) >= 0
}
