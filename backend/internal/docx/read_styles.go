package docx

import (
	"strconv"
	"strings"
)

// style is one of styles.xml's styles, as far as the reader asks of it.
type style struct {
	id, name, kind, basedOn string
	pPr, rPr                *elem
}

type styleSheet struct {
	byID map[string]*style
	// defaultPara is the style a paragraph naming none has.
	defaultPara string
}

func readStyles(root *elem) styleSheet {
	s := styleSheet{byID: map[string]*style{}}
	if root == nil {
		return s
	}
	for _, e := range root.children {
		if e.name != "style" {
			continue
		}
		st := &style{
			id:      e.attr("styleId"),
			kind:    e.attr("type"),
			name:    e.child("name").val(),
			basedOn: e.child("basedOn").val(),
			pPr:     e.child("pPr"),
			rPr:     e.child("rPr"),
		}
		s.byID[st.id] = st
		if st.kind == "paragraph" && e.attr("default") == "1" {
			s.defaultPara = st.id
		}
	}
	return s
}

// chain is a style and those it is based on, nearest first.
func (s styleSheet) chain(id string) []*style {
	var out []*style
	seen := map[string]bool{}
	for id != "" && !seen[id] && len(out) < maxStyleChain {
		seen[id] = true
		st := s.byID[id]
		if st == nil {
			break
		}
		out = append(out, st)
		id = st.basedOn
	}
	return out
}

// maxStyleChain bounds a chain of styles based on styles, which Word keeps short.
const maxStyleChain = 20

// paraStyle is the paragraph's style id, or the default one.
func (s styleSheet) paraStyle(pPr *elem) string {
	if id := pPr.child("pStyle").val(); id != "" {
		return id
	}
	return s.defaultPara
}

// outline is a paragraph's outline level, 0 for Heading 1, -1 for body text.
func (s styleSheet) outline(pPr *elem, styleID string) int {
	if lvl := pPr.child("outlineLvl"); lvl != nil {
		return outlineValue(lvl.val())
	}
	for _, st := range s.chain(styleID) {
		if lvl := st.pPr.child("outlineLvl"); lvl != nil {
			return outlineValue(lvl.val())
		}
		// Some tools name the built-in headings without their outline level.
		if n, ok := strings.CutPrefix(strings.ToLower(st.name), "heading "); ok {
			if level, err := strconv.Atoi(n); err == nil && level >= 1 && level <= 9 {
				return level - 1
			}
		}
	}
	return -1
}

func outlineValue(v string) int {
	n, err := strconv.Atoi(v)
	// Level 9 is Word's body text.
	if err != nil || n < 0 || n > 8 {
		return -1
	}
	return n
}

// is says whether the style, or one it is based on, is one of the names
// given, by its id or its name, without regard to case or spaces.
func (s styleSheet) is(styleID string, names ...string) bool {
	for _, st := range s.chain(styleID) {
		for _, n := range names {
			if squash(st.id) == n || squash(st.name) == n {
				return true
			}
		}
	}
	return false
}

func squash(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}

// codeish says whether a style is for code, by its name or its font.
func (s styleSheet) codeish(styleID string) bool {
	for _, st := range s.chain(styleID) {
		n := squash(st.name + " " + st.id)
		if strings.Contains(n, "code") || strings.Contains(n, "preformatted") || strings.Contains(n, "verbatim") || squash(st.name) == "plaintext" {
			return true
		}
		if f := st.rPr.child("rFonts"); f != nil {
			return monospace(f)
		}
	}
	return false
}

// monoFonts are parts of the names of the monospaced fonts documents use.
var monoFonts = []string{"courier", "consolas", "mono", "menlo", "monaco", "lucida console", "source code", "fira code", "cascadia", "inconsolata", "andale"}

func monospace(rFonts *elem) bool {
	for _, a := range []string{"ascii", "hAnsi"} {
		name := strings.ToLower(rFonts.attr(a))
		for _, m := range monoFonts {
			if name != "" && strings.Contains(name, m) {
				return true
			}
		}
	}
	return false
}

// runLook is what a run's properties say that the reader keeps or reports.
type runLook struct {
	bold, italic, strike, underline, mono, hidden, shifted bool
}

// apply lays an rPr over the look so far, toggles switched as they say.
func (l *runLook) apply(rPr *elem) {
	if rPr == nil {
		return
	}
	if e := rPr.child("b"); e != nil {
		l.bold = e.on()
	}
	if e := rPr.child("i"); e != nil {
		l.italic = e.on()
	}
	if e := rPr.child("strike"); e != nil {
		l.strike = e.on()
	}
	if e := rPr.child("dstrike"); e != nil && e.on() {
		l.strike = true
	}
	if e := rPr.child("u"); e != nil {
		l.underline = e.on()
	}
	if e := rPr.child("vanish"); e != nil {
		l.hidden = e.on()
	}
	if e := rPr.child("vertAlign"); e != nil {
		v := e.val()
		l.shifted = v == "superscript" || v == "subscript"
	}
	if e := rPr.child("rFonts"); e != nil && (e.attr("ascii") != "" || e.attr("hAnsi") != "") {
		l.mono = monospace(e)
	}
}

// look is a run's look: its character style's, then its own. The paragraph's
// style is left out, since a heading's boldness is the heading's.
func (s styleSheet) look(rPr *elem) runLook {
	var l runLook
	chain := s.chain(rPr.child("rStyle").val())
	for i := len(chain) - 1; i >= 0; i-- {
		l.apply(chain[i].rPr)
		if !l.mono && s.codeish(chain[i].id) {
			l.mono = true
		}
	}
	l.apply(rPr)
	return l
}

// level is one level of a list's numbering.
type level struct {
	format string
	start  int
	indent int
}

type numbering struct {
	abstract map[string]map[int]level
	nums     map[string]numDef
}

type numDef struct {
	abstract string
	starts   map[int]int
}

func readNumbering(root *elem) numbering {
	n := numbering{abstract: map[string]map[int]level{}, nums: map[string]numDef{}}
	if root == nil {
		return n
	}
	for _, e := range root.children {
		switch e.name {
		case "abstractNum":
			levels := map[int]level{}
			for _, lvl := range e.children {
				if lvl.name != "lvl" {
					continue
				}
				ilvl, _ := strconv.Atoi(lvl.attr("ilvl"))
				start := 1
				if v, err := strconv.Atoi(lvl.child("start").val()); err == nil {
					start = v
				}
				indent, _ := strconv.Atoi(lvl.child("pPr").child("ind").attr("left"))
				if indent == 0 {
					indent, _ = strconv.Atoi(lvl.child("pPr").child("ind").attr("start"))
				}
				levels[ilvl] = level{format: lvl.child("numFmt").val(), start: start, indent: indent}
			}
			n.abstract[e.attr("abstractNumId")] = levels
		case "num":
			def := numDef{abstract: e.child("abstractNumId").val(), starts: map[int]int{}}
			for _, o := range e.children {
				if o.name != "lvlOverride" {
					continue
				}
				ilvl, _ := strconv.Atoi(o.attr("ilvl"))
				if v, err := strconv.Atoi(o.child("startOverride").val()); err == nil {
					def.starts[ilvl] = v
				}
			}
			n.nums[e.attr("numId")] = def
		}
	}
	return n
}

// level is how a list's level counts and how far in its text sits.
func (n numbering) level(numID string, ilvl int) level {
	def, ok := n.nums[numID]
	if !ok {
		return level{format: "bullet", start: 1, indent: listIndent * (ilvl + 1)}
	}
	l, ok := n.abstract[def.abstract][ilvl]
	if !ok {
		l = level{format: "decimal", start: 1}
	}
	if start, ok := def.starts[ilvl]; ok {
		l.start = start
	}
	if l.indent == 0 {
		l.indent = listIndent * (ilvl + 1)
	}
	return l
}

// numPr is the list a paragraph is an item of, from itself or its style.
func (s styleSheet) numPr(pPr *elem, styleID string) (string, int, bool) {
	num := pPr.child("numPr")
	if num == nil {
		for _, st := range s.chain(styleID) {
			if num = st.pPr.child("numPr"); num != nil {
				break
			}
		}
	}
	if num == nil {
		return "", 0, false
	}
	id := num.child("numId").val()
	ilvl, _ := strconv.Atoi(num.child("ilvl").val())
	// numId 0 is Word's way of taking a style's numbering off one paragraph.
	if id == "" || id == "0" {
		return "", 0, false
	}
	return id, min(max(ilvl, 0), maxListLevel), true
}
