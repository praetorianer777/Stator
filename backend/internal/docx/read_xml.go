package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// elem is an element of a part, read whole: Word's structures are read by
// looking ahead and around, which a stream of tokens makes awkward.
// Names are local; the reader reads transitional and strict documents alike.
type elem struct {
	name     string
	attrs    []xml.Attr
	children []*elem
	text     string
}

// maxXMLDepth bounds how deep a part nests, so a crafted one cannot drive
// the walks that follow past their stack.
const maxXMLDepth = 200

var errTooDeep = errors.New("it nests deeper than any document Word writes")

// parseXML reads a part into its tree.
func parseXML(data []byte) (*elem, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	root := &elem{}
	stack := []*elem{root}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) > maxXMLDepth {
				return nil, errTooDeep
			}
			e := &elem{name: t.Name.Local, attrs: t.Attr}
			top.children = append(top.children, e)
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if textual[top.name] {
				top.text += string(t)
			}
		}
	}
	if len(root.children) == 0 {
		return nil, errors.New("the part is empty")
	}
	return root.children[0], nil
}

// textual are the elements whose characters are read; elsewhere they are
// only the whitespace between elements.
var textual = map[string]bool{
	"t": true, "instrText": true, "delText": true, "title": true, "Application": true, "Target": true,
}

func (e *elem) attr(local string) string {
	if e == nil {
		return ""
	}
	for _, a := range e.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// attrNS reads an attribute by local name within a namespace whose URI
// ends as given, where two of the same local name may sit on one element.
func (e *elem) attrNS(suffix, local string) string {
	if e == nil {
		return ""
	}
	for _, a := range e.attrs {
		if a.Name.Local == local && strings.HasSuffix(a.Name.Space, suffix) {
			return a.Value
		}
	}
	return ""
}

func (e *elem) child(local string) *elem {
	if e == nil {
		return nil
	}
	for _, c := range e.children {
		if c.name == local {
			return c
		}
	}
	return nil
}

// find is the first element below e of the name, depth first.
func (e *elem) find(local string) *elem {
	if e == nil {
		return nil
	}
	for _, c := range e.children {
		if c.name == local {
			return c
		}
		if found := c.find(local); found != nil {
			return found
		}
	}
	return nil
}

// findAll is every element below e of the name, not looking inside one found.
func (e *elem) findAll(local string, into []*elem) []*elem {
	if e == nil {
		return into
	}
	for _, c := range e.children {
		if c.name == local {
			into = append(into, c)
			continue
		}
		into = c.findAll(local, into)
	}
	return into
}

// val is an element's w:val, the way Word writes most of its settings.
func (e *elem) val() string { return e.attr("val") }

// on reads a toggle such as w:b: present and not switched off.
func (e *elem) on() bool {
	if e == nil {
		return false
	}
	switch strings.ToLower(e.val()) {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

// textOf is every w:t below e, as it reads.
func (e *elem) textOf() string {
	var b strings.Builder
	var walk func(*elem)
	walk = func(x *elem) {
		for _, c := range x.children {
			if c.name == "t" {
				b.WriteString(c.text)
				continue
			}
			if c.name == "del" || c.name == "moveFrom" {
				continue
			}
			walk(c)
		}
	}
	if e != nil {
		walk(e)
	}
	return b.String()
}
