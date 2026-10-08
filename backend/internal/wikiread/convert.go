package wikiread

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// target is where a link of the export leads in the new space: a page's
// address, or a file of a page; neither when the export lacks it.
type target struct {
	href string
	file *File
}

// converter turns one page's elements into a document the allowlist takes,
// noting on the page what it could not bring across.
type converter struct {
	key  string
	page string
	lost *losses
	// link resolves a relative link or picture of the HTML export.
	link func(raw string) target
	// named finds a file of the page by its name, for the XML export.
	named func(name string) *File
	// titled finds a page or post of the export by its title.
	titled func(title string, post bool) (uuid.UUID, bool)
	// person names somebody the XML export mentions by key.
	person func(key string) string
	// exportKey is the space key the XML export's links name their own space by.
	exportKey string
	depth     int
}

// nestLimit leaves room under document.MaxDepth for what a block holds
// inline; a container past it becomes its words.
const nestLimit = document.MaxDepth - 6

func (c *converter) lose(kind LossKind, detail string) {
	if c.lost != nil {
		c.lost.add(c.page, kind, detail)
	}
}

// item is one inline node read, or a block met inside a line, such as a
// picture, which ends the paragraph it stood in.
type item struct {
	node  document.Node
	block bool
}

// pageDoc reads a page's elements as a document, or, when what came out is
// not one the allowlist takes, as the words of the page.
func (c *converter) pageDoc(root *node) json.RawMessage {
	content := c.blocks(root.kids)
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	doc := document.Normalize(document.Node{Type: "doc", Content: content})
	if body, err := json.Marshal(doc); err == nil && document.Validate(body) == nil {
		return body
	}
	c.lose(LossPlainText, "")
	return plainDoc(paragraphsOf(root), document.MaxBytes/2)
}

// pageWithChildren is a page of the blocks given that lists the pages below
// it, as the index of an export does.
func pageWithChildren(body []document.Node) json.RawMessage {
	children := document.Node{Type: "childPages", Attrs: map[string]any{"scope": "children", "sort": "tree", "depth": nil}}
	doc := document.Normalize(document.Node{Type: "doc", Content: append(body, children)})
	if out, err := json.Marshal(doc); err == nil && document.Validate(out) == nil {
		return out
	}
	out, _ := json.Marshal(document.Node{Type: "doc", Content: []document.Node{children}})
	return out
}

// commentDoc reads a comment's elements as a comment's document, which
// holds text, lists, quotes and code.
func (c *converter) commentDoc(root *node) json.RawMessage {
	content := toComment(c.blocks(root.kids))
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	doc := document.Normalize(document.Node{Type: "doc", Content: content})
	if body, err := json.Marshal(doc); err == nil {
		if _, err := document.ParseComment(body); err == nil {
			return body
		}
	}
	c.lose(LossPlainText, "")
	return plainDoc(paragraphsOf(root), document.MaxCommentBytes/2)
}

// plainDoc is a document of paragraphs of words, cut at limit bytes.
func plainDoc(paras []string, limit int) json.RawMessage {
	content := []document.Node{}
	used := 0
	for _, p := range paras {
		if used+len(p) > limit {
			p = clipBytes(p, limit-used)
		}
		if p == "" {
			break
		}
		used += len(p)
		content = append(content, document.Node{Type: "paragraph", Content: []document.Node{{Type: "text", Text: p}}})
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	body, _ := json.Marshal(document.Node{Type: "doc", Content: content})
	return body
}

func clipBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// paragraphsOf is the words of an element, a paragraph for each block.
func paragraphsOf(root *node) []string {
	var (
		out []string
		cur strings.Builder
	)
	flush := func() {
		if p := strings.Join(strings.Fields(cur.String()), " "); p != "" {
			out = append(out, p)
		}
		cur.Reset()
	}
	var walk func(*node)
	walk = func(n *node) {
		switch {
		case n.tag == "":
			cur.WriteString(n.text)
			return
		case skipped[n.tag]:
			return
		case n.tag == "br":
			flush()
			return
		}
		block := blockTags[n.tag]
		if block {
			flush()
		}
		for _, k := range n.kids {
			walk(k)
		}
		if block {
			flush()
		}
	}
	walk(root)
	flush()
	return out
}

// blockTags are the elements that stand as blocks of their own.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true, "aside": true, "header": true, "footer": true,
	"nav": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "li": true,
	"dl": true, "dt": true, "dd": true, "pre": true, "blockquote": true, "table": true, "tr": true, "td": true, "th": true,
	"thead": true, "tbody": true, "tfoot": true, "caption": true, "hr": true, "details": true, "summary": true, "figure": true,
	"figcaption": true, "center": true, "address": true, "form": true, "fieldset": true, "iframe": true, "video": true,
	"audio": true, "object": true, "embed": true, "canvas": true, "body": true, "html": true,
	"ac:task-list": true, "ac:task": true, "ac:layout": true, "ac:layout-section": true, "ac:layout-cell": true,
	"ac:rich-text-body": true, "ac:plain-text-body": true,
}

// embedded are elements whose content a page cannot hold: other pages,
// players, programs and forms.
var embedded = map[string]bool{
	"iframe": true, "frame": true, "video": true, "audio": true, "object": true, "embed": true, "applet": true, "canvas": true,
	"form": true, "button": true, "select": true, "textarea": true, "svg": true, "math": true, "map": true,
}

// inlineMacros are the macros of the XML export that sit in a line of text.
var inlineMacros = map[string]bool{"status": true, "anchor": true}

// isBlock says whether an element stands as a block where it is met: a
// block element, or an inline one with a block inside.
func isBlock(n *node) bool {
	switch n.tag {
	case "":
		return false
	case "ac:structured-macro", "ac:macro":
		return !inlineMacros[strings.ToLower(n.attr("ac:name"))]
	}
	if blockTags[n.tag] || embedded[n.tag] {
		return true
	}
	return n.find(func(k *node) bool {
		return blockTags[k.tag] || ((k.tag == "ac:structured-macro" || k.tag == "ac:macro") && !inlineMacros[strings.ToLower(k.attr("ac:name"))])
	}) != nil
}

// blocks reads a run of elements as blocks, the inline ones between them
// gathered into paragraphs.
func (c *converter) blocks(kids []*node) []document.Node {
	var (
		out []document.Node
		run []item
	)
	flush := func() {
		out = append(out, c.paragraphs(run)...)
		run = nil
	}
	for _, k := range kids {
		if !isBlock(k) {
			run = append(run, c.inline(k, nil)...)
			continue
		}
		flush()
		out = append(out, c.block(k)...)
	}
	flush()
	return out
}

// nested reads what a container holds n levels deeper, or nothing when that
// is past the depth a document nests to.
func (c *converter) nested(n int, read func() []document.Node) ([]document.Node, bool) {
	if c.depth+n > nestLimit {
		return nil, false
	}
	c.depth += n
	defer func() { c.depth -= n }()
	return read(), true
}

// flat is an element as paragraphs of its words, for a container nested too deeply.
func flat(n *node) []document.Node {
	var out []document.Node
	for _, p := range paragraphsOf(n) {
		out = append(out, document.Node{Type: "paragraph", Content: []document.Node{{Type: "text", Text: p}}})
	}
	return out
}

func (c *converter) block(k *node) []document.Node {
	switch k.tag {
	case "p":
		return c.paragraphs(c.inlines(k.kids, nil))
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return c.heading(k)
	case "ul", "ol":
		return c.list(k)
	case "pre":
		return []document.Node{codeBlock(preText(k), language(k))}
	case "blockquote":
		if kind := panelKind(k); kind != "" {
			return c.panel(kind, k.kids)
		}
		return c.container("blockquote", nil, k)
	case "table":
		return c.table(k)
	case "hr":
		return []document.Node{{Type: "horizontalRule"}}
	case "details":
		summary := k.child("summary")
		title := ""
		var body []*node
		for _, kid := range k.kids {
			if kid != summary {
				body = append(body, kid)
			}
		}
		if summary != nil {
			title = summary.words()
		}
		return c.expand(title, body)
	case "dt":
		return c.paragraphs(c.inlines(k.kids, []document.Mark{{Type: "bold"}}))
	case "br", "summary", "caption":
		return nil
	case "ac:structured-macro", "ac:macro":
		return c.macro(k)
	case "ac:task-list":
		return c.tasks(k)
	case "ac:layout-section":
		return c.layoutSection(k)
	case "ac:plain-text-body":
		return []document.Node{codeBlock(rawText(k), nil)}
	}
	if skipped[k.tag] {
		return nil
	}
	if embedded[k.tag] {
		c.lose(LossEmbed, k.tag)
		return nil
	}
	if k.tag == "div" || k.tag == "section" || k.tag == "aside" {
		if kind := panelKind(k); kind != "" {
			return c.panel(kind, k.kids)
		}
		if title, body, ok := expandParts(k); ok {
			return c.expand(title, body.kids)
		}
		if isTOC(k) {
			return []document.Node{{Type: "tableOfContents", Attrs: map[string]any{"maxLevel": document.MaxHeadingLevel}}}
		}
	}
	return c.blocks(k.kids)
}

func (c *converter) heading(k *node) []document.Node {
	level := int(k.tag[1] - '0')
	if level > document.MaxHeadingLevel {
		level = document.MaxHeadingLevel
	}
	content := trimInline(mergeText(lineOf(c.inlines(k.kids, nil))))
	if len(content) == 0 {
		return nil
	}
	return []document.Node{{Type: "heading", Attrs: map[string]any{"level": level}, Content: content}}
}

// lineOf is inline items as one line: a block met inside becomes its words.
func lineOf(items []item) []document.Node {
	var out []document.Node
	for _, i := range items {
		if !i.block {
			out = append(out, i.node)
			continue
		}
		if words := nodeWords(i.node); words != "" {
			out = append(out, document.Node{Type: "text", Text: " " + words + " "})
		}
	}
	return out
}

func (c *converter) container(typ string, attrs map[string]any, k *node) []document.Node {
	content, ok := c.nested(1, func() []document.Node { return c.blocks(k.kids) })
	if !ok {
		return flat(k)
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	return []document.Node{{Type: typ, Attrs: attrs, Content: content}}
}

func (c *converter) panel(kind string, kids []*node) []document.Node {
	content, ok := c.nested(1, func() []document.Node { return c.blocks(kids) })
	if !ok {
		return flat(&node{tag: "div", kids: kids})
	}
	if len(content) == 0 {
		return nil
	}
	return []document.Node{{Type: "panel", Attrs: map[string]any{"kind": kind}, Content: content}}
}

func (c *converter) expand(title string, kids []*node) []document.Node {
	content, ok := c.nested(1, func() []document.Node { return c.blocks(kids) })
	if !ok {
		return flat(&node{tag: "div", kids: kids})
	}
	if len(content) == 0 {
		content = []document.Node{{Type: "paragraph"}}
	}
	title = clipRunes(strings.TrimSpace(title), document.MaxExpandTitleLength)
	return []document.Node{{Type: "expand", Attrs: map[string]any{"title": title}, Content: content}}
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// panelWords pair the words a callout's class names carry with the panel
// they read as, the most pressing first.
var panelWords = []struct {
	kind  string
	words []string
}{
	{"error", []string{"error", "danger"}},
	{"warning", []string{"warning", "warn", "caution"}},
	{"note", []string{"note", "important"}},
	{"success", []string{"success", "tip", "hint", "check"}},
	{"info", []string{"info", "information"}},
}

// panelKind is the panel a callout's class names say it is, or nothing.
func panelKind(n *node) string {
	words := n.classWords()
	for _, p := range panelWords {
		for _, w := range p.words {
			if words[w] {
				return p.kind
			}
		}
	}
	return ""
}

// expandParts finds a toggle's title and content: an element whose class
// says expand, holding one that says content.
func expandParts(n *node) (string, *node, bool) {
	if !n.classWords()["expand"] {
		return "", nil, false
	}
	body := n.find(func(k *node) bool { return k.classWords()["content"] })
	if body == nil {
		return "", nil, false
	}
	title := n.find(func(k *node) bool { w := k.classWords(); return w["control"] && w["text"] })
	if title == nil {
		title = n.find(func(k *node) bool { w := k.classWords(); return w["control"] || w["title"] || w["summary"] })
	}
	words := ""
	if title != nil && !title.within(func(k *node) bool { return k == body }) {
		words = title.words()
	}
	return words, body, true
}

func isTOC(n *node) bool {
	for _, c := range n.classes() {
		if c == "toc" || strings.HasPrefix(c, "toc-") || strings.HasSuffix(c, "-toc") {
			return true
		}
	}
	return false
}

func (c *converter) list(k *node) []document.Node {
	var items []*node
	for _, kid := range k.kids {
		switch {
		case kid.tag == "li":
			items = append(items, kid)
		case kid.tag == "" && strings.TrimSpace(kid.text) == "":
		case len(items) > 0:
			// A list put straight into a list, as editors write, belongs to the item before it.
			items[len(items)-1].kids = append(items[len(items)-1].kids, kid)
		default:
			items = append(items, &node{tag: "li", kids: []*node{kid}})
		}
	}
	if len(items) == 0 {
		return nil
	}
	task := k.classWords()["task"]
	if !task {
		task = true
		for _, it := range items {
			if !taskItem(it) {
				task = false
				break
			}
		}
	}
	out := document.Node{Type: "bulletList"}
	itemType := "listItem"
	switch {
	case task:
		out.Type, itemType = "taskList", "taskItem"
	case k.tag == "ol":
		start := 1
		if s, err := strconv.Atoi(strings.TrimSpace(k.attr("start"))); err == nil && s >= 0 && s <= document.MaxListStart {
			start = s
		}
		var typ any
		if t := k.attr("type"); slices.Contains([]string{"1", "a", "A", "i", "I"}, t) {
			typ = t
		}
		out = document.Node{Type: "orderedList", Attrs: map[string]any{"start": start, "type": typ}}
	}
	made, ok := c.nested(2, func() []document.Node {
		var made []document.Node
		for _, it := range items {
			content := firstParagraph(c.blocks(it.kids))
			n := document.Node{Type: itemType, Content: content}
			if task {
				n.Attrs = map[string]any{"checked": checked(it)}
			}
			made = append(made, n)
		}
		return made
	})
	if !ok {
		return flat(k)
	}
	out.Content = made
	return []document.Node{out}
}

// firstParagraph makes a list item's content open with a paragraph, as the
// editor's items do.
func firstParagraph(content []document.Node) []document.Node {
	if len(content) == 0 || content[0].Type != "paragraph" {
		return append([]document.Node{{Type: "paragraph"}}, content...)
	}
	return content
}

// taskItem says an item is a task: its class says so, or it opens with a check box.
func taskItem(n *node) bool {
	if w := n.classWords(); w["checked"] || w["task"] || n.attr("data-inline-task-id") != "" {
		return true
	}
	return checkBox(n) != nil
}

func checkBox(n *node) *node {
	for _, k := range n.kids {
		switch {
		case k.tag == "" && strings.TrimSpace(k.text) == "":
			continue
		case k.tag == "input" && strings.EqualFold(k.attr("type"), "checkbox"):
			return k
		case k.tag == "p" || k.tag == "span" || k.tag == "label":
			return checkBox(k)
		}
		return nil
	}
	return nil
}

func checked(n *node) bool {
	if w := n.classWords(); w["checked"] || w["complete"] || w["done"] {
		return true
	}
	if box := checkBox(n); box != nil {
		_, on := box.attrs["checked"]
		return on
	}
	return false
}

func (c *converter) table(k *node) []document.Node {
	var rows []*node
	for _, kid := range k.kids {
		switch kid.tag {
		case "tr":
			rows = append(rows, kid)
		case "thead", "tbody", "tfoot":
			for _, r := range kid.kids {
				if r.tag == "tr" {
					rows = append(rows, r)
				}
			}
		}
	}
	made, ok := c.nested(3, func() []document.Node {
		var made []document.Node
		for _, r := range rows {
			row := document.Node{Type: "tableRow"}
			for _, cell := range r.kids {
				if cell.tag != "td" && cell.tag != "th" {
					continue
				}
				typ := "tableCell"
				if cell.tag == "th" {
					typ = "tableHeader"
				}
				attrs := map[string]any{"colspan": span(cell.attr("colspan")), "rowspan": span(cell.attr("rowspan")), "colwidth": nil}
				content := c.blocks(cell.kids)
				if len(content) == 0 {
					content = []document.Node{{Type: "paragraph"}}
				}
				row.Content = append(row.Content, document.Node{Type: typ, Attrs: attrs, Content: content})
			}
			if len(row.Content) > 0 {
				made = append(made, row)
			}
		}
		return made
	})
	if !ok {
		return flat(k)
	}
	if len(made) == 0 {
		return nil
	}
	return []document.Node{{Type: "table", Content: made}}
}

func span(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, document.MaxTableSpan)
}

func codeBlock(code string, lang any) document.Node {
	code = strings.TrimSuffix(strings.TrimPrefix(code, "\n"), "\n")
	n := document.Node{Type: "codeBlock", Attrs: map[string]any{"language": lang}}
	if code != "" {
		n.Content = []document.Node{{Type: "text", Text: code}}
	}
	return n
}

// preText is preformatted text as written, a line break as a new line.
func preText(n *node) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(n *node) {
		switch {
		case n.tag == "":
			b.WriteString(n.text)
		case n.tag == "br":
			b.WriteByte('\n')
		case skipped[n.tag]:
		default:
			for _, k := range n.kids {
				walk(k)
			}
		}
	}
	walk(n)
	return strings.ReplaceAll(b.String(), "\r\n", "\n")
}

// rawText is the text of an element as written, for a macro's plain body.
func rawText(n *node) string {
	var b strings.Builder
	for _, k := range n.kids {
		if k.tag == "" {
			b.WriteString(k.text)
		} else {
			b.WriteString(rawText(k))
		}
	}
	return b.String()
}

var (
	languagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+#-]*$`)
	classLanguage   = regexp.MustCompile(`(?:^|\s)(?:language|lang)-([A-Za-z0-9+#-]+)`)
	brushLanguage   = regexp.MustCompile(`brush:\s*([A-Za-z0-9+#-]+)`)
)

// language is a code block's language as its element or the one around it
// names it, when it is one a code block takes.
func language(n *node) any {
	look := []*node{n}
	if code := n.child("code"); code != nil {
		look = append(look, code)
	}
	if n.up != nil {
		look = append(look, n.up)
	}
	for _, e := range look {
		for _, v := range []string{e.attr("data-language"), e.attr("data-lang")} {
			if l := cleanLanguage(v); l != nil {
				return l
			}
		}
		for _, v := range []string{e.attr("class"), e.attr("data-syntaxhighlighter-params")} {
			if m := classLanguage.FindStringSubmatch(v); m != nil {
				if l := cleanLanguage(m[1]); l != nil {
					return l
				}
			}
			if m := brushLanguage.FindStringSubmatch(v); m != nil {
				if l := cleanLanguage(m[1]); l != nil {
					return l
				}
			}
		}
	}
	return nil
}

func cleanLanguage(v string) any {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || len(v) > 32 || !languagePattern.MatchString(v) || v == "none" || v == "text" || v == "plain" {
		return nil
	}
	return v
}

// paragraphs splits inline items at the blocks among them: the text between
// is a paragraph each.
func (c *converter) paragraphs(items []item) []document.Node {
	var (
		out []document.Node
		run []document.Node
	)
	flush := func() {
		run = trimInline(mergeText(run))
		if len(run) > 0 {
			out = append(out, document.Node{Type: "paragraph", Content: run})
		}
		run = nil
	}
	for _, i := range items {
		if i.block {
			flush()
			out = append(out, i.node)
			continue
		}
		run = append(run, i.node)
	}
	flush()
	return out
}

func (c *converter) inlines(kids []*node, marks []document.Mark) []item {
	var out []item
	for _, k := range kids {
		out = append(out, c.inline(k, marks)...)
	}
	return out
}

// spaces are the runs of white space HTML shows as one space; a no-break
// space is not among them.
var spaces = regexp.MustCompile(`[ \t\n\r\f]+`)

func textItem(s string, marks []document.Mark) []item {
	if s == "" {
		return nil
	}
	return []item{{node: document.Node{Type: "text", Text: s, Marks: marks}}}
}

func (c *converter) inline(k *node, marks []document.Mark) []item {
	switch k.tag {
	case "":
		return textItem(spaces.ReplaceAllString(k.text, " "), marks)
	case "strong", "b":
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "bold"}))
	case "em", "i", "cite", "dfn", "var":
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "italic"}))
	case "s", "strike", "del":
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "strike"}))
	case "code", "tt", "kbd", "samp":
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "code"}))
	case "br":
		return []item{{node: document.Node{Type: "hardBreak"}}}
	case "img":
		return c.image(k, marks)
	case "a":
		return c.anchor(k, marks)
	case "time":
		if d, ok := dateOf(k.attr("datetime")); ok {
			return []item{{node: document.Node{Type: document.NodeDate, Attrs: map[string]any{"date": d}}}}
		}
		return c.inlines(k.kids, marks)
	case "input", "wbr":
		return nil
	case "ac:link":
		return c.acLink(k, marks)
	case "ac:image":
		return c.acImage(k, marks)
	case "ac:emoticon":
		return textItem(emoticon(k), marks)
	case "ac:structured-macro", "ac:macro":
		return c.inlineMacro(k, marks)
	case "ac:placeholder":
		return nil
	}
	if skipped[k.tag] || strings.HasPrefix(k.tag, "ri:") {
		return nil
	}
	if embedded[k.tag] {
		c.lose(LossEmbed, k.tag)
		return nil
	}
	if blockTags[k.tag] {
		var out []item
		for _, b := range c.block(k) {
			out = append(out, item{node: b, block: true})
		}
		return out
	}
	words := k.classWords()
	if words["icon"] {
		return nil
	}
	style := strings.ToLower(strings.ReplaceAll(k.attr("style"), " ", ""))
	if strings.Contains(style, "font-weight:bold") || strings.Contains(style, "font-weight:700") {
		marks = withMark(marks, document.Mark{Type: "bold"})
	}
	if strings.Contains(style, "font-style:italic") {
		marks = withMark(marks, document.Mark{Type: "italic"})
	}
	if strings.Contains(style, "line-through") {
		marks = withMark(marks, document.Mark{Type: "strike"})
	}
	return c.inlines(k.kids, marks)
}

// markOrder is the order marks are kept in, as Markdown import keeps them.
var markOrder = []string{"link", "bold", "italic", "strike", "code"}

func withMark(marks []document.Mark, m document.Mark) []document.Mark {
	for _, have := range marks {
		if have.Type == m.Type {
			return marks
		}
	}
	out := append(slices.Clone(marks), m)
	slices.SortStableFunc(out, func(a, b document.Mark) int {
		return slices.Index(markOrder, a.Type) - slices.Index(markOrder, b.Type)
	})
	return out
}

func withoutMark(marks []document.Mark, t string) []document.Mark {
	var out []document.Mark
	for _, m := range marks {
		if m.Type != t {
			out = append(out, m)
		}
	}
	return out
}

// mergeText joins neighbouring text of one style, and a space that follows
// another, as a browser shows them.
func mergeText(nodes []document.Node) []document.Node {
	var out []document.Node
	for _, n := range nodes {
		if n.Type != "text" {
			out = append(out, n)
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last].Type == "text" {
			if strings.HasSuffix(out[last].Text, " ") {
				n.Text = strings.TrimLeft(n.Text, " ")
				if n.Text == "" {
					continue
				}
			}
			if reflect.DeepEqual(out[last].Marks, n.Marks) {
				out[last].Text += n.Text
				continue
			}
		} else if last < 0 || out[last].Type == "hardBreak" {
			n.Text = strings.TrimLeft(n.Text, " ")
			if n.Text == "" {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// trimInline drops the white space at either end of a line.
func trimInline(nodes []document.Node) []document.Node {
	for len(nodes) > 0 && (nodes[0].Type == "hardBreak" || nodes[0].Type == "text") {
		if nodes[0].Type == "text" {
			nodes[0].Text = strings.TrimLeftFunc(nodes[0].Text, unicode.IsSpace)
			if nodes[0].Text != "" {
				break
			}
		}
		nodes = nodes[1:]
	}
	for len(nodes) > 0 && (nodes[len(nodes)-1].Type == "hardBreak" || nodes[len(nodes)-1].Type == "text") {
		last := len(nodes) - 1
		if nodes[last].Type == "text" {
			nodes[last].Text = strings.TrimRightFunc(nodes[last].Text, unicode.IsSpace)
			if nodes[last].Text != "" {
				break
			}
		}
		nodes = nodes[:last]
	}
	return nodes
}

var dateLayouts = []string{time.DateOnly, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"}

func dateOf(v string) (string, bool) {
	v = strings.TrimSpace(v)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.Format(time.DateOnly), true
		}
	}
	return "", false
}

// image reads a picture of the HTML export: a file of the export is shown,
// one from another site becomes a link to it, and an icon its words.
func (c *converter) image(k *node, marks []document.Mark) []item {
	alt := strings.TrimSpace(k.attr("alt"))
	if alt == "" {
		alt = strings.TrimSpace(k.attr("title"))
	}
	if w := k.classWords(); w["emoticon"] || w["emoji"] || w["icon"] {
		if fallback := k.attr("data-emoji-fallback"); fallback != "" {
			return textItem(fallback, marks)
		}
		return textItem(alt, marks)
	}
	src := strings.TrimSpace(k.attr("src"))
	if src == "" {
		src = strings.TrimSpace(k.attr("data-image-src"))
	}
	switch {
	case strings.HasPrefix(strings.ToLower(src), "data:"):
		c.lose(LossMissingFile, alt)
		return textItem(alt, marks)
	case isWeb(src):
		return c.externalImage(src, alt, marks)
	}
	if c.link != nil {
		if t := c.link(src); t.file != nil {
			return []item{{node: imageNode(t.file, alt, k.attr("width")), block: true}}
		}
	}
	c.lose(LossMissingFile, baseName(src))
	return textItem(alt, marks)
}

func (c *converter) externalImage(src, alt string, marks []document.Mark) []item {
	c.lose(LossExternalImage, src)
	words := alt
	if words == "" {
		words = src
	}
	if document.SafeHref(src) {
		return textItem(words, withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": src}}))
	}
	return textItem(alt, marks)
}

func imageNode(f *File, alt, width string) document.Node {
	attrs := map[string]any{"attachmentId": f.ID.String(), "alt": nil, "width": nil}
	if alt != "" {
		attrs["alt"] = clipRunes(alt, document.MaxAltLength)
	}
	if w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(width), "px")); err == nil && w > 0 && w <= document.MaxImageWidth {
		attrs["width"] = w
	}
	return document.Node{Type: "image", Attrs: attrs}
}

func isWeb(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "//")
}

func baseName(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// anchor reads a link of the HTML export: to a page or file of the export
// where it now is, to the web as it was, and else as its words.
func (c *converter) anchor(k *node, marks []document.Mark) []item {
	href := strings.TrimSpace(k.attr("href"))
	if img := onlyImage(k); img != nil {
		return c.inline(img, marks)
	}
	words := k.classWords()
	switch {
	case href == "" || strings.HasPrefix(href, "#"):
		return c.inlines(k.kids, marks)
	case words["mention"] || words["userlink"] || words["user"]:
		return c.inlines(k.kids, withoutMark(marks, "link"))
	case isWeb(href) || strings.HasPrefix(strings.ToLower(href), "mailto:"):
		if strings.HasPrefix(href, "//") || !document.SafeHref(href) {
			return c.inlines(k.kids, marks)
		}
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": href}}))
	case strings.Contains(strings.SplitN(href, "/", 2)[0], ":"):
		return c.inlines(k.kids, marks)
	}
	var t target
	if c.link != nil {
		t = c.link(href)
	}
	switch {
	case t.file != nil:
		text := k.words()
		if text == "" || text == t.file.Name {
			return []item{{node: document.Node{Type: "attachment", Marks: marks, Attrs: map[string]any{
				"attachmentId": t.file.ID.String(), "fileName": clipRunes(t.file.Name, 200),
			}}}}
		}
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": "/api/v1/attachments/" + t.file.ID.String()}}))
	case t.href != "":
		return c.inlines(k.kids, withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": t.href}}))
	}
	c.lose(LossOutsideLink, href)
	return c.inlines(k.kids, marks)
}

// onlyImage is the one picture a link holds and nothing else, which is read
// as the picture.
func onlyImage(k *node) *node {
	var img *node
	for _, kid := range k.kids {
		switch {
		case kid.tag == "" && strings.TrimSpace(kid.text) == "":
		case kid.tag == "img" && img == nil:
			img = kid
		default:
			return nil
		}
	}
	return img
}

// pageHref is a page's address in the new space.
func (c *converter) pageHref(id uuid.UUID) string { return "/s/" + c.key + "/p/" + id.String() }

// acLink reads a link of the XML export, which names its target by title,
// file name or person rather than by address.
func (c *converter) acLink(k *node, marks []document.Mark) []item {
	body := k.child("ac:link-body")
	plain := k.child("ac:plain-text-link-body")
	words := func(fallback string, marks []document.Mark) []item {
		switch {
		case body != nil:
			return c.inlines(body.kids, marks)
		case plain != nil && strings.TrimSpace(rawText(plain)) != "":
			return textItem(rawText(plain), marks)
		}
		return textItem(fallback, marks)
	}
	link := func(href string) []document.Mark {
		return withMark(marks, document.Mark{Type: "link", Attrs: map[string]any{"href": href}})
	}
	for _, t := range k.kids {
		switch t.tag {
		case "ri:page", "ri:blog-post":
			title := t.attr("ri:content-title")
			if key := t.attr("ri:space-key"); key != "" && !strings.EqualFold(key, c.exportKey) {
				c.lose(LossOutsideLink, key+": "+title)
				return words(title, marks)
			}
			if c.titled != nil {
				if id, ok := c.titled(title, t.tag == "ri:blog-post"); ok {
					return words(title, link(c.pageHref(id)))
				}
			}
			c.lose(LossOutsideLink, title)
			return words(title, marks)
		case "ri:attachment":
			name := t.attr("ri:filename")
			var f *File
			if c.named != nil && t.child("ri:page") == nil {
				f = c.named(name)
			}
			if f == nil {
				c.lose(LossMissingFile, name)
				return words(name, marks)
			}
			if body == nil && (plain == nil || strings.TrimSpace(rawText(plain)) == "") {
				return []item{{node: document.Node{Type: "attachment", Marks: marks, Attrs: map[string]any{
					"attachmentId": f.ID.String(), "fileName": clipRunes(f.Name, 200),
				}}}}
			}
			return words(name, link("/api/v1/attachments/"+f.ID.String()))
		case "ri:user":
			key := t.attr("ri:userkey")
			if key == "" {
				key = t.attr("ri:account-id")
			}
			if key == "" {
				key = t.attr("ri:username")
			}
			name := ""
			if c.person != nil {
				name = c.person(key)
			}
			if name == "" {
				return words("", withoutMark(marks, "link"))
			}
			return textItem("@"+name, withoutMark(marks, "link"))
		case "ri:url":
			href := t.attr("ri:value")
			if document.SafeHref(href) && isWeb(href) {
				return words(href, link(href))
			}
			return words(href, marks)
		case "ri:space", "ri:content-entity", "ri:shortcut":
			c.lose(LossOutsideLink, t.attr("ri:space-key"))
			return words(t.attr("ri:space-key"), marks)
		}
	}
	return words("", marks)
}

// acImage reads a picture of the XML export: a file of the page by its name,
// or one from another site.
func (c *converter) acImage(k *node, marks []document.Mark) []item {
	alt := strings.TrimSpace(k.attr("ac:alt"))
	if alt == "" {
		alt = strings.TrimSpace(k.attr("ac:title"))
	}
	if a := k.child("ri:attachment"); a != nil {
		name := a.attr("ri:filename")
		if c.named != nil && a.child("ri:page") == nil {
			if f := c.named(name); f != nil {
				return []item{{node: imageNode(f, alt, k.attr("ac:width")), block: true}}
			}
		}
		c.lose(LossMissingFile, name)
		return textItem(alt, marks)
	}
	if u := k.child("ri:url"); u != nil {
		return c.externalImage(u.attr("ri:value"), alt, marks)
	}
	return textItem(alt, marks)
}

// emoticons are the pictures the XML export names by word, as emoji.
var emoticons = map[string]string{
	"smile": "🙂", "sad": "🙁", "cheeky": "😛", "laugh": "😀", "wink": "😉", "thumbs-up": "👍", "thumbs-down": "👎",
	"information": "ℹ️", "tick": "✅", "cross": "❌", "warning": "⚠️", "plus": "➕", "minus": "➖", "question": "❓",
	"light-on": "💡", "light-off": "💡", "yellow-star": "⭐", "red-star": "⭐", "green-star": "⭐", "blue-star": "⭐", "heart": "❤️",
}

func emoticon(k *node) string {
	if f := k.attr("ac:emoji-fallback"); f != "" && !strings.HasPrefix(f, ":") {
		return f
	}
	return emoticons[strings.ToLower(k.attr("ac:name"))]
}

// params are a macro's settings by name.
func params(k *node) map[string]string {
	out := map[string]string{}
	for _, p := range k.kids {
		if p.tag == "ac:parameter" {
			out[strings.ToLower(p.attr("ac:name"))] = strings.TrimSpace(rawText(p))
		}
	}
	return out
}

// statusColours pair a status macro's colour with a status's.
var statusColours = map[string]string{"green": "success", "yellow": "warning", "red": "danger", "blue": "accent", "grey": "neutral", "gray": "neutral"}

func (c *converter) inlineMacro(k *node, marks []document.Mark) []item {
	name := strings.ToLower(k.attr("ac:name"))
	switch name {
	case "anchor":
		return nil
	case "status":
		p := params(k)
		label := clipRunes(strings.TrimSpace(p["title"]), document.MaxStatusLength)
		if label == "" {
			label = strings.ToUpper(clipRunes(p["colour"], document.MaxStatusLength))
		}
		if label == "" {
			return nil
		}
		colour := statusColours[strings.ToLower(p["colour"])]
		if colour == "" {
			colour = "neutral"
		}
		return []item{{node: document.Node{Type: document.NodeStatus, Attrs: map[string]any{"label": label, "color": colour}}}}
	}
	var out []item
	for _, b := range c.macro(k) {
		out = append(out, item{node: b, block: true})
	}
	return out
}

// panelMacros pair the XML export's callout macros with a panel's kind.
var panelMacros = map[string]string{"info": "info", "note": "note", "tip": "success", "warning": "warning", "panel": "info", "error": "error", "success": "success"}

// macro reads a block macro of the XML export: those with a block like it
// here become that block, any other is named in the report, its body kept.
func (c *converter) macro(k *node) []document.Node {
	name := strings.ToLower(k.attr("ac:name"))
	p := params(k)
	body := k.child("ac:rich-text-body")
	plain := k.child("ac:plain-text-body")
	var bodyKids []*node
	if body != nil {
		bodyKids = body.kids
	}
	switch name {
	case "code", "noformat", "code-block":
		text := ""
		if plain != nil {
			text = rawText(plain)
		}
		return []document.Node{codeBlock(text, cleanLanguage(p["language"]))}
	case "toc":
		level := document.MaxHeadingLevel
		if l, err := strconv.Atoi(p["maxlevel"]); err == nil && l >= 1 && l < level {
			level = l
		}
		return []document.Node{{Type: "tableOfContents", Attrs: map[string]any{"maxLevel": level}}}
	case "children":
		return []document.Node{{Type: "childPages", Attrs: map[string]any{"scope": "children", "sort": "tree", "depth": nil}}}
	case "attachments":
		return []document.Node{{Type: document.NodeAttachmentList}}
	case "expand":
		return c.expand(p["title"], bodyKids)
	case "anchor":
		return nil
	case "status":
		var out []document.Node
		for _, i := range c.inlineMacro(k, nil) {
			out = append(out, document.Node{Type: "paragraph", Content: []document.Node{i.node}})
		}
		return out
	case "excerpt", "div", "span", "column":
		return c.blocks(bodyKids)
	case "section":
		return c.section(bodyKids)
	}
	if kind, ok := panelMacros[name]; ok {
		kids := bodyKids
		if title := p["title"]; title != "" {
			kids = append([]*node{{tag: "p", kids: []*node{{tag: "strong", kids: []*node{{text: title}}}}}}, kids...)
		}
		return c.panel(kind, kids)
	}
	c.lose(LossMacro, name)
	switch {
	case body != nil:
		return c.blocks(bodyKids)
	case plain != nil && strings.TrimSpace(rawText(plain)) != "":
		return []document.Node{codeBlock(rawText(plain), nil)}
	}
	return nil
}

// section reads a section macro of two or three column macros as columns.
func (c *converter) section(kids []*node) []document.Node {
	var cols []*node
	for _, k := range kids {
		if (k.tag == "ac:structured-macro" || k.tag == "ac:macro") && strings.EqualFold(k.attr("ac:name"), "column") {
			body := k.child("ac:rich-text-body")
			if body == nil {
				body = &node{tag: "div"}
			}
			cols = append(cols, body)
		}
	}
	return c.columns(cols, kids)
}

// layoutSection reads a section of a page layout, its cells as columns.
func (c *converter) layoutSection(k *node) []document.Node {
	var cells []*node
	for _, kid := range k.kids {
		if kid.tag == "ac:layout-cell" {
			cells = append(cells, kid)
		}
	}
	return c.columns(cells, k.kids)
}

func (c *converter) columns(cells []*node, all []*node) []document.Node {
	if len(cells) < document.MinColumns || len(cells) > document.MaxColumns {
		return c.blocks(all)
	}
	made, ok := c.nested(2, func() []document.Node {
		var made []document.Node
		for _, cell := range cells {
			content := c.blocks(cell.kids)
			if len(content) == 0 {
				content = []document.Node{{Type: "paragraph"}}
			}
			made = append(made, document.Node{Type: "column", Attrs: map[string]any{"width": nil}, Content: content})
		}
		return made
	})
	if !ok {
		return c.blocks(all)
	}
	return []document.Node{{Type: "columns", Content: made}}
}

// tasks reads a task list of the XML export.
func (c *converter) tasks(k *node) []document.Node {
	made, ok := c.nested(2, func() []document.Node {
		var made []document.Node
		for _, t := range k.kids {
			if t.tag != "ac:task" {
				continue
			}
			done := false
			if s := t.child("ac:task-status"); s != nil {
				done = strings.EqualFold(strings.TrimSpace(rawText(s)), "complete")
			}
			var content []document.Node
			if b := t.child("ac:task-body"); b != nil {
				content = c.blocks(b.kids)
			}
			made = append(made, document.Node{Type: "taskItem", Attrs: map[string]any{"checked": done}, Content: firstParagraph(content)})
		}
		return made
	})
	if !ok {
		return flat(k)
	}
	if len(made) == 0 {
		return nil
	}
	return []document.Node{{Type: "taskList", Content: made}}
}

// nodeWords are the words a document node shows.
func nodeWords(n document.Node) string {
	var b strings.Builder
	var walk func(document.Node)
	walk = func(n document.Node) {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
		case "hardBreak":
			b.WriteByte(' ')
		case "image":
			if alt, _ := n.Attrs["alt"].(string); alt != "" {
				b.WriteString(alt)
			}
		case "attachment":
			name, _ := n.Attrs["fileName"].(string)
			b.WriteString(name)
		case document.NodeStatus:
			label, _ := n.Attrs["label"].(string)
			b.WriteString(label)
		case document.NodeDate:
			d, _ := n.Attrs["date"].(string)
			b.WriteString(d)
		}
		for i, k := range n.Content {
			if i > 0 && !document.IsTextblock(n) && n.Type != "paragraph" {
				b.WriteByte(' ')
			}
			walk(k)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// commentMarks are the styles a comment keeps.
var commentMarks = map[string]bool{"bold": true, "italic": true, "strike": true, "code": true, "link": true}

// toComment holds blocks to what a comment takes: a block a comment has no
// place for gives up its content, or its words.
func toComment(nodes []document.Node) []document.Node {
	var out []document.Node
	for _, n := range nodes {
		switch n.Type {
		case "paragraph", "heading":
			n.Content = commentInline(n.Content)
			out = append(out, n)
		case "codeBlock":
			out = append(out, n)
		case "bulletList", "orderedList", "taskList":
			if n.Type == "taskList" {
				n.Type, n.Attrs = "bulletList", nil
			}
			items := []document.Node{}
			for _, it := range n.Content {
				items = append(items, document.Node{Type: "listItem", Content: firstParagraph(toComment(it.Content))})
			}
			n.Content = items
			out = append(out, n)
		case "blockquote":
			n.Content = toComment(n.Content)
			if len(n.Content) == 0 {
				n.Content = []document.Node{{Type: "paragraph"}}
			}
			out = append(out, n)
		case "horizontalRule":
		case "tableRow":
			var cells []string
			for _, cell := range n.Content {
				cells = append(cells, nodeWords(cell))
			}
			if line := strings.Join(cells, " | "); strings.Trim(line, " |") != "" {
				out = append(out, document.Node{Type: "paragraph", Content: []document.Node{{Type: "text", Text: line}}})
			}
		default:
			if len(n.Content) > 0 && !document.IsTextblock(n) {
				out = append(out, toComment(n.Content)...)
			} else if words := nodeWords(n); words != "" {
				out = append(out, document.Node{Type: "paragraph", Content: []document.Node{{Type: "text", Text: words}}})
			}
		}
	}
	return out
}

func commentInline(nodes []document.Node) []document.Node {
	var out []document.Node
	for _, n := range nodes {
		switch n.Type {
		case "text", "hardBreak":
			var marks []document.Mark
			for _, m := range n.Marks {
				if commentMarks[m.Type] {
					marks = append(marks, m)
				}
			}
			n.Marks = marks
			out = append(out, n)
		default:
			if words := nodeWords(n); words != "" {
				out = append(out, document.Node{Type: "text", Text: words})
			}
		}
	}
	return out
}
