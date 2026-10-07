package docx

import (
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// rnode is a block or inline of the page being read, held by pointer while
// lists and merged cells are built up around it.
type rnode struct {
	typ     string
	attrs   map[string]any
	marks   []document.Mark
	text    string
	content []*rnode
	// block marks a block met inside a paragraph, such as a picture, which
	// splits the paragraph around it.
	block bool
	// join says the next block of the same kind continues this one: code
	// lines in one code block, quote paragraphs in one quote, one contents.
	join string
}

// conv is one document being read.
type conv struct {
	pkg          *pkg
	part         string
	opts         ReadOptions
	rels         map[string]rel
	styles       styleSheet
	numbering    numbering
	notes        map[string]*elem
	noteRefs     []*elem
	pictures     []Picture
	pictureAt    map[string]int
	pictureBytes int64
	// pictureWidths are each picture's own width in pixels.
	pictureWidths []int
	warnings      []string
	warned        map[string]bool
	// bookmarks are the headings a bookmark marks, for links within the document.
	bookmarks  map[string]*rnode
	fromStator bool
	// titlePara is the first Title paragraph's words, which the page takes
	// as its title and leaves out of its body.
	titlePara  string
	sawTitle   bool
	afterTitle bool
	err        error
}

// paraInfo is what a paragraph is, from its style and properties.
type paraInfo struct {
	kind   string
	level  int
	numID  string
	ilvl   int
	listed bool
	indent int
	align  string
	rule   bool
}

const (
	kindHeading  = "heading"
	kindCode     = "code"
	kindQuote    = "quote"
	kindTitle    = "title"
	kindSubtitle = "subtitle"
	kindTOC      = "toc"
	kindFormula  = "formula"
)

// indentSlack lets a paragraph set in a little less than a list's text still
// continue its item, as documents from other editors do.
const indentSlack = 60

func (c *conv) info(p *elem) paraInfo {
	pPr := p.child("pPr")
	id := c.styles.paraStyle(pPr)
	var info paraInfo
	switch outline := c.styles.outline(pPr, id); {
	case c.styles.is(id, "title"):
		info.kind = kindTitle
	case c.styles.is(id, "subtitle"):
		info.kind = kindSubtitle
	case c.toc(id):
		info.kind = kindTOC
	case outline >= 0:
		info.kind, info.level = kindHeading, outline+1
	case c.styles.is(id, "formula"):
		info.kind = kindFormula
	case c.styles.codeish(id):
		info.kind = kindCode
	case c.styles.is(id, "quote", "intensequote", "blockquote", "blocktext"):
		info.kind = kindQuote
	}
	if info.kind == "" || info.kind == kindQuote {
		info.numID, info.ilvl, info.listed = c.styles.numPr(pPr, id)
	}
	info.indent = indentOf(pPr.child("ind"))
	if info.indent == 0 {
		for _, st := range c.styles.chain(id) {
			if ind := st.pPr.child("ind"); ind != nil {
				info.indent = indentOf(ind)
				break
			}
		}
	}
	switch pPr.child("jc").val() {
	case "center":
		info.align = "center"
	case "right", "end":
		info.align = "right"
	}
	if b := pPr.child("pBdr"); b != nil && b.child("bottom").on() {
		info.rule = true
	}
	return info
}

func (c *conv) toc(id string) bool {
	for _, st := range c.styles.chain(id) {
		n := squash(st.name)
		if n == "tocheading" {
			return true
		}
		if rest, ok := strings.CutPrefix(n, "toc"); ok && len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
			return true
		}
	}
	return false
}

func indentOf(ind *elem) int {
	v := ind.attr("left")
	if v == "" {
		v = ind.attr("start")
	}
	n, _ := strconv.Atoi(v)
	return max(n, 0)
}

// listLevel is a list open at one level, waiting for its next item.
type listLevel struct {
	list   *rnode
	key    string
	level  int
	indent int
}

// builder lays a container's blocks out, gathering list paragraphs into
// lists nested by level, and what is set in under an item into that item.
type builder struct {
	c      *conv
	out    []*rnode
	stack  []listLevel
	counts map[string]int
}

// blocks reads a run of block elements: a body, a cell, a text box or a note.
func (c *conv) blocks(children []*elem, depth int) []*rnode {
	b := &builder{c: c, counts: map[string]int{}}
	b.walk(children, depth)
	return b.out
}

func (b *builder) walk(children []*elem, depth int) {
	c := b.c
	for _, e := range children {
		if c.err != nil {
			return
		}
		switch e.name {
		case "p":
			b.paragraph(e, depth)
		case "tbl":
			blocks := c.table(e, depth)
			ind := e.child("tblPr").child("tblInd")
			w, _ := strconv.Atoi(ind.attr("w"))
			b.place(blocks, paraInfo{indent: max(w, 0)})
		case "sdt":
			if gallery := e.child("sdtPr").find("docPartGallery"); gallery != nil && strings.Contains(strings.ToLower(gallery.val()), "table of contents") {
				b.place([]*rnode{contents()}, paraInfo{})
				continue
			}
			b.walk(e.child("sdtContent").children, depth)
		case "customXml", "smartTag", "moveTo":
			b.walk(e.children, depth)
		case "ins":
			c.tracked()
			b.walk(e.children, depth)
		case "del", "moveFrom":
			c.tracked()
		case "AlternateContent":
			b.walk(choice(e), depth)
		case "altChunk":
			c.warn("A part of the document kept in another format (such as a web page pasted in) was left out.")
		}
	}
}

// choice is what a reader of current Word reads from alternate content.
func choice(e *elem) []*elem {
	if ch := e.child("Choice"); ch != nil {
		return ch.children
	}
	return e.child("Fallback").children
}

func (c *conv) tracked() {
	c.warn("Tracked changes were accepted: the page holds the text as it reads with every change made.")
}

func contents() *rnode {
	return &rnode{typ: "tableOfContents", attrs: map[string]any{"maxLevel": document.MaxHeadingLevel}, join: kindTOC}
}

func (b *builder) paragraph(p *elem, depth int) {
	c := b.c
	info := b.c.info(p)
	switch info.kind {
	case kindTitle:
		text := clean(p.textOf())
		if !c.sawTitle && text != "" {
			c.sawTitle, c.titlePara, c.afterTitle = true, text, true
			return
		}
		info.kind, info.level = kindHeading, 1
	case kindSubtitle:
		// The line Stator writes under the title says where the page came from.
		if c.fromStator && c.afterTitle {
			c.afterTitle = false
			return
		}
		info.kind = ""
	case kindTOC:
		b.place([]*rnode{contents()}, paraInfo{})
		return
	}
	c.afterTitle = false
	b.place(c.paragraphBlocks(p, info, depth), info)
}

// paragraphBlocks reads a paragraph as the blocks it makes: usually one,
// more when a picture or a text box splits it, none when it is empty.
func (c *conv) paragraphBlocks(p *elem, info paraInfo, depth int) []*rnode {
	in := &inliner{c: c, depth: depth, code: info.kind == kindCode}
	in.walk(p.children, "")
	items := in.items
	if info.kind == kindCode || (info.kind == "" && !info.listed && allCode(items)) {
		return []*rnode{{typ: "codeBlock", content: textNodes(plainText(items)), join: kindCode}}
	}
	if info.kind == kindFormula {
		latex := strings.TrimSpace(plainText(items))
		if latex == "" {
			return nil
		}
		return []*rnode{{typ: document.NodeMathBlock, attrs: map[string]any{"latex": latex}}}
	}
	if info.rule && !hasContent(items) {
		return []*rnode{{typ: "horizontalRule"}}
	}
	var out []*rnode
	var run []*rnode
	flush := func() {
		run = trimBreaks(run)
		if !hasContent(run) {
			run = nil
			return
		}
		var n *rnode
		switch info.kind {
		case kindHeading:
			level := info.level
			if level > document.MaxHeadingLevel {
				c.warn("Headings below level 3 became level 3 headings, the deepest a page has.")
				level = document.MaxHeadingLevel
			}
			n = &rnode{typ: "heading", attrs: map[string]any{"level": level}, content: run}
			for _, name := range in.bookmarks {
				if _, taken := c.bookmarks[name]; !taken {
					c.bookmarks[name] = n
				}
			}
		default:
			n = &rnode{typ: "paragraph", content: run}
		}
		out = append(out, n)
		run = nil
	}
	for _, it := range items {
		if it.block {
			flush()
			out = append(out, it)
			continue
		}
		run = append(run, it)
	}
	flush()
	if info.kind == kindQuote && len(out) > 0 {
		return []*rnode{{typ: "blockquote", content: out, join: kindQuote}}
	}
	return out
}

// hasContent says whether inlines hold anything a reader sees.
func hasContent(items []*rnode) bool {
	for _, it := range items {
		if it.block || it.typ != "text" && it.typ != "hardBreak" || strings.TrimSpace(it.text) != "" {
			return true
		}
	}
	return false
}

// trimBreaks drops line breaks a paragraph starts or ends with, which only
// make room on Word's sheet.
func trimBreaks(items []*rnode) []*rnode {
	for len(items) > 0 && items[0].typ == "hardBreak" {
		items = items[1:]
	}
	for len(items) > 0 && items[len(items)-1].typ == "hardBreak" {
		items = items[:len(items)-1]
	}
	return items
}

func allCode(items []*rnode) bool {
	seen := false
	for _, it := range items {
		switch {
		case it.block:
			return false
		case it.typ == "text" && strings.TrimSpace(it.text) != "":
			if !hasMarkOf(it.marks, "code") || hasMarkOf(it.marks, "link") {
				return false
			}
			seen = true
		case it.typ != "text" && it.typ != "hardBreak":
			return false
		}
	}
	return seen
}

func hasMarkOf(marks []document.Mark, t string) bool {
	for _, m := range marks {
		if m.Type == t {
			return true
		}
	}
	return false
}

func plainText(items []*rnode) string {
	var b strings.Builder
	for _, it := range items {
		switch it.typ {
		case "text":
			b.WriteString(it.text)
		case "hardBreak":
			b.WriteByte('\n')
		case document.NodeMathInline:
			b.WriteString(stringOf(it.attrs["latex"]))
		}
	}
	return b.String()
}

func textNodes(s string) []*rnode {
	if s == "" {
		return nil
	}
	return []*rnode{{typ: "text", text: s}}
}

// place puts a paragraph's or a table's blocks where they belong: into a
// list, under the item they are set in as far as, or into the container.
func (b *builder) place(blocks []*rnode, info paraInfo) {
	if len(blocks) == 0 {
		return
	}
	if checked, ok := taskBox(blocks[0]); ok && blocks[0].typ == "paragraph" {
		info.listed, info.numID = true, taskKey
		info.ilvl = min(max((info.indent+listIndent/2)/listIndent-1, 0), maxListLevel)
		b.item(blocks, info, &checked)
		return
	}
	if info.listed && blocks[0].typ == "paragraph" {
		b.item(blocks, info, nil)
		return
	}
	if len(b.stack) > 0 && info.indent > 0 {
		for i := len(b.stack) - 1; i >= 0; i-- {
			if b.stack[i].indent <= info.indent+indentSlack {
				b.stack = b.stack[:i+1]
				item := lastOf(b.stack[i].list)
				item.content = joinBlocks(item.content, blocks)
				return
			}
		}
	}
	b.stack = nil
	b.out = joinBlocks(b.out, blocks)
}

// taskKey stands for the lists of check boxes, which Word has no numbering of.
const taskKey = "task"

// taskBox reads a check box a paragraph starts with, as the export writes a
// task and as people type one, and takes it off the paragraph's words.
func taskBox(p *rnode) (bool, bool) {
	if p.typ != "paragraph" || len(p.content) == 0 || p.content[0].typ != "text" {
		return false, false
	}
	first := p.content[0]
	r := []rune(first.text)
	if len(r) == 0 {
		return false, false
	}
	var checked bool
	switch r[0] {
	case '☐':
	case '☒', '☑':
		checked = true
	default:
		return false, false
	}
	rest := strings.TrimLeft(string(r[1:]), " \t")
	if rest == "" {
		p.content = p.content[1:]
	} else {
		first.text = rest
	}
	if len(p.content) > 0 && p.content[0].typ == "text" {
		p.content[0].text = strings.TrimLeft(p.content[0].text, " \t")
	}
	return checked, true
}

func lastOf(list *rnode) *rnode { return list.content[len(list.content)-1] }

// joinBlocks appends blocks, continuing a code block, a quote or the
// contents that the last block already is.
func joinBlocks(into, blocks []*rnode) []*rnode {
	for _, n := range blocks {
		if last := len(into) - 1; last >= 0 && n.join != "" && into[last].join == n.join {
			prev := into[last]
			switch n.join {
			case kindCode:
				text := ""
				if len(prev.content) > 0 {
					text = prev.content[0].text
				}
				if len(n.content) > 0 {
					text += "\n" + n.content[0].text
				} else {
					text += "\n"
				}
				prev.content = []*rnode{{typ: "text", text: text}}
			case kindQuote:
				prev.content = append(prev.content, n.content...)
			}
			continue
		}
		into = append(into, n)
	}
	return into
}

// item adds a list paragraph's blocks as an item of the list at its level,
// opening the list, inside the item above, where it starts.
func (b *builder) item(blocks []*rnode, info paraInfo, checked *bool) {
	key, kind, attrs, indent := info.numID, "taskList", map[string]any(nil), info.indent
	if checked == nil {
		lvl := b.c.numbering.level(info.numID, info.ilvl)
		if indent == 0 {
			indent = lvl.indent
		}
		kind = "bulletList"
		if lvl.format != "bullet" && lvl.format != "none" {
			kind = "orderedList"
			start := lvl.start + b.counts[countKey(key, info.ilvl)]
			attrs = map[string]any{"start": min(max(start, 0), document.MaxListStart)}
			if t := orderedTypes[lvl.format]; t != "" {
				attrs["type"] = t
			}
		}
		b.counts[countKey(key, info.ilvl)]++
		for deeper := info.ilvl + 1; deeper <= maxListLevel; deeper++ {
			delete(b.counts, countKey(key, deeper))
		}
	}
	for len(b.stack) > 0 && b.stack[len(b.stack)-1].level > info.ilvl {
		b.stack = b.stack[:len(b.stack)-1]
	}
	if n := len(b.stack); n > 0 && b.stack[n-1].level == info.ilvl && (b.stack[n-1].key != key || b.stack[n-1].list.typ != kind) {
		b.stack = b.stack[:n-1]
	}
	item := &rnode{typ: "listItem", content: blocks}
	if checked != nil {
		item.typ = "taskItem"
		item.attrs = map[string]any{"checked": *checked}
	}
	if n := len(b.stack); n > 0 && b.stack[n-1].level == info.ilvl {
		b.stack[n-1].list.content = append(b.stack[n-1].list.content, item)
		return
	}
	list := &rnode{typ: kind, attrs: attrs, content: []*rnode{item}}
	if n := len(b.stack); n > 0 {
		parent := lastOf(b.stack[n-1].list)
		parent.content = append(parent.content, list)
	} else {
		b.out = append(b.out, list)
	}
	b.stack = append(b.stack, listLevel{list: list, key: key, level: info.ilvl, indent: indent})
}

func countKey(numID string, ilvl int) string { return numID + ":" + strconv.Itoa(ilvl) }

// orderedTypes are Word's counting formats an ordered list has a type for;
// decimal is a list's own, and any other counts as decimal.
var orderedTypes = map[string]string{
	"lowerLetter": "a", "upperLetter": "A", "lowerRoman": "i", "upperRoman": "I",
}

// table reads a table: header rows, cells merged across and down, fills
// the editor offers. A table of one cell is a box drawn around blocks.
func (c *conv) table(tbl *elem, depth int) []*rnode {
	if depth >= maxNesting {
		c.warn("Tables nested deeper than a page shows were kept as their text.")
		var out []*rnode
		for _, tc := range tbl.findAll("tc", nil) {
			out = append(out, c.blocks(tc.children, depth)...)
		}
		return out
	}
	var rows []*rnode
	active := map[int]*rnode{}
	for _, tr := range rowsOf(tbl) {
		trPr := tr.child("trPr")
		if trPr.child("del") != nil {
			c.tracked()
			continue
		}
		col, _ := strconv.Atoi(trPr.child("gridBefore").val())
		row := &rnode{typ: "tableRow"}
		header := trPr.child("tblHeader").on()
		for _, tc := range cellsOf(tr) {
			tcPr := tc.child("tcPr")
			span, _ := strconv.Atoi(tcPr.child("gridSpan").val())
			span = min(max(span, 1), document.MaxTableSpan)
			merge := tcPr.child("vMerge")
			if merge != nil && merge.val() != "restart" {
				if start := active[col]; start != nil {
					start.attrs["rowspan"] = min(intOf(start.attrs["rowspan"])+1, document.MaxTableSpan)
					col += span
					continue
				}
			}
			cell := &rnode{typ: "tableCell", attrs: map[string]any{"colspan": span, "rowspan": 1}}
			cell.content = c.blocks(tc.children, depth+1)
			if fill := strings.ToUpper(tcPr.child("shd").attr("fill")); fill != "" {
				cell.attrs["fill"] = fill
			}
			if align := cellAlign(tc); align != "" {
				cell.attrs["align"] = align
			}
			for i := col; i < col+span; i++ {
				delete(active, i)
			}
			if merge != nil && merge.val() == "restart" {
				active[col] = cell
			}
			row.content = append(row.content, cell)
			col += span
		}
		if len(row.content) == 0 {
			continue
		}
		if header {
			row.attrs = map[string]any{"header": true}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 && len(rows[0].content) == 1 && intOf(rows[0].content[0].attrs["rowspan"]) == 1 {
		return c.box(rows[0].content[0])
	}
	if rows[0].attrs == nil && len(rows) > 1 && allBold(rows[0]) {
		rows[0].attrs = map[string]any{"header": true}
	}
	for _, row := range rows {
		header := row.attrs != nil
		row.attrs = nil
		for _, cell := range row.content {
			fill, _ := cell.attrs["fill"].(string)
			delete(cell.attrs, "fill")
			if header {
				cell.typ = "tableHeader"
				unbold(cell)
				if fill == neutralFill {
					fill = ""
				}
			}
			for role, hex := range cellFills {
				if hex == fill {
					cell.attrs["background"] = role
				}
			}
			if len(cell.content) == 0 {
				cell.content = []*rnode{{typ: "paragraph"}}
			}
		}
	}
	return []*rnode{{typ: "table", content: rows}}
}

// box reads a table of one cell: a panel when it has a panel's fill, its
// first line the panel's kind, and otherwise a quote around the blocks.
func (c *conv) box(cell *rnode) []*rnode {
	fill, _ := cell.attrs["fill"].(string)
	content := cell.content
	if len(content) == 0 {
		return nil
	}
	for kind, look := range panelLooks {
		if look.fill != fill {
			continue
		}
		if first := content[0]; first.typ == "paragraph" && allBold(&rnode{content: []*rnode{first}}) {
			content = content[1:]
		}
		if len(content) == 0 {
			content = []*rnode{{typ: "paragraph"}}
		}
		return []*rnode{{typ: "panel", attrs: map[string]any{"kind": kind}, content: content}}
	}
	return []*rnode{{typ: "blockquote", content: content}}
}

func rowsOf(tbl *elem) []*elem {
	var out []*elem
	var walk func(children []*elem)
	walk = func(children []*elem) {
		for _, e := range children {
			switch e.name {
			case "tr":
				out = append(out, e)
			case "sdt":
				walk(e.child("sdtContent").children)
			case "customXml", "ins", "moveTo":
				walk(e.children)
			}
		}
	}
	walk(tbl.children)
	return out
}

func cellsOf(tr *elem) []*elem {
	var out []*elem
	var walk func(children []*elem)
	walk = func(children []*elem) {
		for _, e := range children {
			switch e.name {
			case "tc":
				out = append(out, e)
			case "sdt":
				walk(e.child("sdtContent").children)
			case "customXml":
				walk(e.children)
			}
		}
	}
	walk(tr.children)
	return out
}

// cellAlign is the alignment every paragraph of a cell shares, if any.
func cellAlign(tc *elem) string {
	align := ""
	for i, p := range tc.findAll("p", nil) {
		a := ""
		switch p.child("pPr").child("jc").val() {
		case "center":
			a = "center"
		case "right", "end":
			a = "right"
		}
		if i > 0 && a != align {
			return ""
		}
		align = a
	}
	return align
}

// allBold says whether every word of a row or cell is bold, as a header row
// is written where Word's own header setting is not used.
func allBold(n *rnode) bool {
	seen := false
	var walk func(*rnode) bool
	walk = func(x *rnode) bool {
		if x.typ == "text" && strings.TrimSpace(x.text) != "" {
			seen = true
			if !hasMarkOf(x.marks, "bold") {
				return false
			}
		}
		for _, ch := range x.content {
			if !walk(ch) {
				return false
			}
		}
		return true
	}
	return walk(n) && seen
}

// unbold takes bold off a header cell's words, which a header shows bold anyway.
func unbold(n *rnode) {
	if n.typ == "text" {
		var marks []document.Mark
		for _, m := range n.marks {
			if m.Type != "bold" {
				marks = append(marks, m)
			}
		}
		n.marks = marks
	}
	for _, ch := range n.content {
		unbold(ch)
	}
}

// withNotes adds the footnotes and endnotes referred to, as a numbered list
// at the end of the page, each item the note its number in brackets names.
func (c *conv) withNotes(content []*rnode) []*rnode {
	if len(c.noteRefs) == 0 {
		return content
	}
	list := &rnode{typ: "orderedList", attrs: map[string]any{"start": 1}}
	for i := 0; i < len(c.noteRefs); i++ {
		blocks := c.blocks(c.noteRefs[i].children, maxNesting-1)
		if len(blocks) == 0 || blocks[0].typ != "paragraph" {
			blocks = append([]*rnode{{typ: "paragraph"}}, blocks...)
		}
		list.content = append(list.content, &rnode{typ: "listItem", content: blocks})
	}
	return append(content, &rnode{typ: "horizontalRule"}, list)
}

// finish turns the tree into the document's nodes, with links within the
// document pointed at the anchors their headings get.
func (c *conv) finish(content []*rnode) []document.Node {
	anchors := map[*rnode]string{}
	if len(c.bookmarks) > 0 {
		doc := document.Node{Type: "doc", Content: convert(content, nil)}
		var order []*rnode
		var walk func(list []*rnode, depth int)
		walk = func(list []*rnode, depth int) {
			if depth > document.MaxDepth {
				return
			}
			for _, n := range list {
				if n.typ == "heading" {
					order = append(order, n)
					continue
				}
				walk(n.content, depth+1)
			}
		}
		walk(content, 0)
		for i, h := range document.Headings(doc) {
			if i < len(order) {
				anchors[order[i]] = h.Anchor
			}
		}
	}
	resolve := func(href string) string {
		name, ok := strings.CutPrefix(href, bookmarkHref)
		if !ok {
			return href
		}
		if h := c.bookmarks[name]; h != nil && anchors[h] != "" {
			return "#" + anchors[h]
		}
		c.warn("Links to places in the document other than its headings were left out, their words kept.")
		return ""
	}
	return convert(content, resolve)
}

// bookmarkHref marks a link to a bookmark until its heading's anchor is known.
const bookmarkHref = "\x00bookmark:"

func convert(list []*rnode, resolve func(string) string) []document.Node {
	out := make([]document.Node, 0, len(list))
	for _, n := range list {
		d := document.Node{Type: n.typ, Text: n.text, Attrs: n.attrs}
		for _, m := range n.marks {
			if m.Type == "link" && resolve != nil {
				href := resolve(stringOf(m.Attrs["href"]))
				if href == "" {
					continue
				}
				m = document.Mark{Type: "link", Attrs: map[string]any{"href": href}}
			}
			d.Marks = append(d.Marks, m)
		}
		if n.typ == "text" && n.text == "" {
			continue
		}
		d.Content = convert(n.content, resolve)
		if len(d.Content) == 0 {
			d.Content = nil
		}
		out = append(out, d)
	}
	return out
}

func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	}
	return 0
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// inlineText is the words of a block, as a heading's title is read.
func inlineText(n *rnode) string {
	var b strings.Builder
	for _, c := range n.content {
		switch c.typ {
		case "text":
			b.WriteString(c.text)
		case "hardBreak":
			b.WriteByte(' ')
		}
	}
	return b.String()
}
