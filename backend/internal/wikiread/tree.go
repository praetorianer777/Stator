package wikiread

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// node is an element or a piece of text of either format, read into one
// shape so one converter serves both.
type node struct {
	// tag is the element's lower-case name, its namespace prefix kept as
	// in "ns:name"; empty for text.
	tag   string
	attrs map[string]string
	text  string
	kids  []*node
	up    *node
}

// maxTreeDepth bounds how deep a page's elements are kept as elements;
// below it only their words are, which no page nests so deep anyway.
const maxTreeDepth = 200

func (n *node) attr(name string) string { return n.attrs[name] }

// classes are the element's class names, lower case.
func (n *node) classes() []string { return strings.Fields(strings.ToLower(n.attrs["class"])) }

func (n *node) hasClass(name string) bool {
	for _, c := range n.classes() {
		if c == name {
			return true
		}
	}
	return false
}

// classWords are the words of the element's class names, split at hyphens
// and underscores, so "box-warning" says warning.
func (n *node) classWords() map[string]bool {
	out := map[string]bool{}
	for _, c := range n.classes() {
		out[c] = true
		for _, w := range strings.FieldsFunc(c, func(r rune) bool { return r == '-' || r == '_' }) {
			out[w] = true
		}
	}
	return out
}

// find is the first element below n, in document order, that match picks.
func (n *node) find(match func(*node) bool) *node {
	for _, k := range n.kids {
		if k.tag == "" {
			continue
		}
		if match(k) {
			return k
		}
		if found := k.find(match); found != nil {
			return found
		}
	}
	return nil
}

// findAll is every element below n that match picks, not looking inside one picked.
func (n *node) findAll(match func(*node) bool) []*node {
	var out []*node
	for _, k := range n.kids {
		if k.tag == "" {
			continue
		}
		if match(k) {
			out = append(out, k)
			continue
		}
		out = append(out, k.findAll(match)...)
	}
	return out
}

// child is the first element directly below n named tag.
func (n *node) child(tag string) *node {
	for _, k := range n.kids {
		if k.tag == tag {
			return k
		}
	}
	return nil
}

// within says whether n sits inside an element match picks.
func (n *node) within(match func(*node) bool) bool {
	for up := n.up; up != nil; up = up.up {
		if match(up) {
			return true
		}
	}
	return false
}

// detach takes n out of its parent.
func (n *node) detach() {
	if n.up == nil {
		return
	}
	kids := n.up.kids[:0]
	for _, k := range n.up.kids {
		if k != n {
			kids = append(kids, k)
		}
	}
	n.up.kids = kids
	n.up = nil
}

// words are the text of n and everything in it, a line break as a space.
func (n *node) words() string {
	var b strings.Builder
	var walk func(*node, bool)
	walk = func(n *node, root bool) {
		switch {
		case n.tag == "":
			b.WriteString(n.text)
		case n.tag == "br":
			b.WriteByte(' ')
		case skipped[n.tag] && !root:
		default:
			for _, k := range n.kids {
				walk(k, false)
			}
		}
	}
	walk(n, true)
	return strings.Join(strings.Fields(b.String()), " ")
}

// skipped are elements whose text is no content: scripts, styles, a
// macro's settings.
var skipped = map[string]bool{"script": true, "style": true, "head": true, "title": true, "template": true, "noscript": true, "ac:parameter": true}

func byID(id string) func(*node) bool {
	return func(n *node) bool { return n.attrs["id"] == id }
}

func byTag(tag string) func(*node) bool {
	return func(n *node) bool { return n.tag == tag }
}

func byClass(name string) func(*node) bool {
	return func(n *node) bool { return n.hasClass(name) }
}

// parseHTML reads an HTML document into the tree.
func parseHTML(data []byte) (*node, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return fromHTML(doc, nil, 0), nil
}

func fromHTML(h *html.Node, up *node, depth int) *node {
	n := &node{up: up}
	switch h.Type {
	case html.TextNode:
		n.text = h.Data
		return n
	case html.ElementNode:
		n.tag = strings.ToLower(h.Data)
		n.attrs = make(map[string]string, len(h.Attr))
		for _, a := range h.Attr {
			key := strings.ToLower(a.Key)
			if a.Namespace != "" {
				key = a.Namespace + ":" + key
			}
			n.attrs[key] = a.Val
		}
	case html.DocumentNode:
		n.tag = "#document"
	default:
		return nil
	}
	if depth > maxTreeDepth {
		n.kids = []*node{{text: textOf(h), up: n}}
		return n
	}
	for c := h.FirstChild; c != nil; c = c.NextSibling {
		if k := fromHTML(c, n, depth+1); k != nil {
			n.kids = append(n.kids, k)
		}
	}
	return n
}

// textOf is every word below h, read without recursion.
func textOf(h *html.Node) string {
	var b strings.Builder
	stack := []*html.Node{h}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.Type == html.TextNode {
			b.WriteString(top.Data)
			b.WriteByte(' ')
		}
		var kids []*html.Node
		for c := top.FirstChild; c != nil; c = c.NextSibling {
			kids = append(kids, c)
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, kids[i])
		}
	}
	return b.String()
}

// parseXHTML reads a page body of the XML export: XHTML with elements of
// the other wiki's own in namespaces it never declares.
func parseXHTML(body string) (*node, error) {
	dec := xml.NewDecoder(strings.NewReader("<body>" + body + "</body>"))
	dec.Strict = false
	dec.AutoClose = voidElements
	dec.Entity = xml.HTMLEntity
	root := &node{tag: "#document"}
	cur := root
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{tag: xmlName(t.Name), attrs: make(map[string]string, len(t.Attr)), up: cur}
			for _, a := range t.Attr {
				n.attrs[xmlName(a.Name)] = a.Value
			}
			if depth > maxTreeDepth {
				// Past the depth kept, an element's words go to the deepest one kept.
				depth++
				continue
			}
			cur.kids = append(cur.kids, n)
			cur = n
			depth++
		case xml.EndElement:
			depth--
			if depth > maxTreeDepth {
				continue
			}
			if cur.up != nil {
				cur = cur.up
			}
		case xml.CharData:
			cur.kids = append(cur.kids, &node{text: string(t), up: cur})
		}
	}
	return root, nil
}

// voidElements are the HTML elements a body may leave open; link and param
// are left out, as the decoder would match the other wiki's own by local name.
var voidElements = []string{"br", "hr", "img", "area", "input", "col", "wbr", "basefont", "isindex"}

func xmlName(n xml.Name) string {
	if n.Space != "" {
		return strings.ToLower(n.Space + ":" + n.Local)
	}
	return strings.ToLower(n.Local)
}
