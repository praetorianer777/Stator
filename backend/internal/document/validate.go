package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrInvalid is what every refusal wraps, so a handler can answer 422.
var ErrInvalid = errors.New("invalid document")

// InvalidError carries a sentence the author can act on.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Node is one ProseMirror node as the editor stores it.
type Node struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
	Content []Node         `json:"content,omitempty"`
}

// Mark is one mark on an inline node.
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Parse decodes a document without judging it; Validate is the gate.
func Parse(body json.RawMessage) (Node, error) {
	if len(body) > MaxBytes {
		return Node{}, invalid("This page is too long; keep it under %d MB, or split it into several pages.", MaxBytes>>20)
	}
	return decode(body, "page")
}

func decode(body json.RawMessage, noun string) (Node, error) {
	var root Node
	dec := json.NewDecoder(bytes.NewReader(body))
	// A field the schema does not name would be stored and served back
	// unread, so it is refused rather than carried along.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&root); err != nil {
		return Node{}, invalid("This %s is not a document the editor can read; reload the editor and save again.", noun)
	}
	if dec.More() {
		return Node{}, invalid("This %s is not a document the editor can read; reload the editor and save again.", noun)
	}
	return root, nil
}

// Validate accepts exactly what the editor and the reader can show, and
// refuses everything else in a sentence that says what to change.
func Validate(body json.RawMessage) error {
	root, err := Parse(body)
	if err != nil {
		return err
	}
	return ValidateNode(root)
}

// ValidateNode checks an already decoded document against the allowlist.
func ValidateNode(root Node) error {
	if err := (validator{list: &Allowed, noun: "page", anchors: map[string]bool{}}).check(root); err != nil {
		return err
	}
	return checkExcerpts(root)
}

// ParseTemplate decodes a template's body and holds it to TemplateAllowed,
// answering the decoded node so the caller can check its variables.
func ParseTemplate(body json.RawMessage) (Node, error) {
	if len(body) > MaxBytes {
		return Node{}, invalid("This template is too long; keep it under %d MB.", MaxBytes>>20)
	}
	root, err := decode(body, "template")
	if err != nil {
		return Node{}, err
	}
	return root, validator{list: &TemplateAllowed, noun: "template", anchors: map[string]bool{}}.check(root)
}

// ParseComment decodes a comment's document and holds it to CommentAllowed,
// answering the decoded node so the caller can read its words.
func ParseComment(body json.RawMessage) (Node, error) {
	if len(body) > MaxCommentBytes {
		return Node{}, invalid("This comment is too long; keep it under %d KB, or put the text on a page.", MaxCommentBytes>>10)
	}
	root, err := decode(body, "comment")
	if err != nil {
		return Node{}, err
	}
	return root, validator{list: &CommentAllowed, noun: "comment", anchors: map[string]bool{}}.check(root)
}

type validator struct {
	list    *Allowlist
	noun    string
	anchors map[string]bool
}

func (v validator) check(root Node) error {
	if root.Type != "doc" {
		return v.invalid(`This %s must be a document with "type":"doc".`)
	}
	return v.node(root, NodeSpec{Content: []string{"doc"}}, 0)
}

// invalid words a refusal about this kind of document; the format's first
// verb is the noun.
func (v validator) invalid(format string, args ...any) error {
	return invalid(format, append([]any{v.noun}, args...)...)
}

func (v validator) node(n Node, parent NodeSpec, depth int) error {
	if depth > MaxDepth {
		return v.invalid("This %s is nested too deeply; flatten its lists, quotes and panels.")
	}
	spec, ok := v.list.Nodes[n.Type]
	if !ok {
		return v.invalid("This %s holds a %q block, which the editor cannot show; take it out.", n.Type)
	}
	if !slices.Contains(parent.Content, n.Type) {
		return v.invalid("This %s puts a %q where it cannot go; move it out.", n.Type)
	}
	if n.Type == "text" {
		if n.Text == "" {
			return v.invalid("This %s holds an empty piece of text; save it again from the editor.")
		}
	} else if n.Text != "" {
		return v.invalid("This %s gives a %q text of its own, which only text may have.", n.Type)
	}
	if len(spec.Content) == 0 && len(n.Content) > 0 {
		return v.invalid("This %s puts content inside a %q, which holds none.", n.Type)
	}
	if (spec.MinContent > 0 && len(n.Content) < spec.MinContent) || (spec.MaxContent > 0 && len(n.Content) > spec.MaxContent) {
		return v.invalid("This %s has a %q holding %d, which takes %d to %d; add or take some out.", n.Type, len(n.Content), spec.MinContent, spec.MaxContent)
	}
	if err := v.checkAttrs(n.Attrs, spec.Attrs, fmt.Sprintf("a %q", n.Type)); err != nil {
		return err
	}
	if n.Type == NodeSketch {
		if err := checkSketch(n.Attrs); err != nil {
			return err
		}
	}
	if n.Type == "heading" {
		if id, _ := n.Attrs["id"].(string); id != "" {
			if v.anchors[id] {
				return invalid("Two headings share the anchor %q; rename one of them.", id)
			}
			v.anchors[id] = true
		}
	}
	if len(n.Marks) > 0 && !(spec.Inline && parent.AllowsMarks) {
		return v.invalid("This %s styles a %q where styles cannot go; remove the formatting.", n.Type)
	}
	seen := map[string]bool{}
	for _, m := range n.Marks {
		ms, ok := v.list.Marks[m.Type]
		if !ok {
			return v.invalid("This %s uses a %q style, which the editor cannot show; take it out.", m.Type)
		}
		key := m.Type
		if ms.Repeatable {
			attrs, _ := json.Marshal(m.Attrs)
			key += string(attrs)
		}
		if seen[key] {
			return v.invalid("This %s applies the %q style twice to the same text.", m.Type)
		}
		seen[key] = true
		if err := v.checkAttrs(m.Attrs, ms.Attrs, fmt.Sprintf("the %q style", m.Type)); err != nil {
			return err
		}
	}
	for _, child := range n.Content {
		if err := v.node(child, spec, depth+1); err != nil {
			return err
		}
	}
	return nil
}

var patterns = map[string]*regexp.Regexp{}

func init() {
	compile := func(attrs map[string]Attr) {
		for _, a := range attrs {
			if a.Pattern != "" {
				patterns[a.Pattern] = regexp.MustCompile(a.Pattern)
			}
		}
	}
	for _, n := range TemplateAllowed.Nodes {
		compile(n.Attrs)
	}
	for _, m := range Allowed.Marks {
		compile(m.Attrs)
	}
}

func (v validator) checkAttrs(attrs map[string]any, allowed map[string]Attr, owner string) error {
	for name, value := range attrs {
		rule, ok := allowed[name]
		if !ok {
			return v.invalid("This %s gives %s an attribute %q, which the editor does not know; take it out.", owner, name)
		}
		if !attrValid(rule, value) {
			return v.invalid("This %s gives %s the attribute %s=%s, which is not allowed; pick it again in the editor.", owner, name, shortJSON(value))
		}
	}
	return nil
}

func attrValid(rule Attr, value any) bool {
	if value == nil {
		return rule.Nullable || rule.Kind == KindNull
	}
	switch rule.Kind {
	case KindNull:
		return false
	case KindBoolean:
		_, ok := value.(bool)
		return ok
	case KindInteger:
		return integerIn(value, rule)
	case KindIntegers:
		list, ok := value.([]any)
		if !ok || len(list) > rule.MaxLength {
			return false
		}
		for _, item := range list {
			if !integerIn(item, rule) {
				return false
			}
		}
		return true
	case KindStrings:
		list, ok := value.([]any)
		if !ok || len(list) < rule.MinLength || len(list) > rule.MaxLength {
			return false
		}
		seen := make(map[string]bool, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok || seen[s] || (rule.Pattern == "" && !slices.Contains(rule.Enum, s)) || (rule.Pattern != "" && !patterns[rule.Pattern].MatchString(s)) {
				return false
			}
			seen[s] = true
		}
		return true
	case KindString:
		s, ok := value.(string)
		if !ok {
			return false
		}
		if rule.MaxLength > 0 && utf8.RuneCountInString(s) > rule.MaxLength {
			return false
		}
		if rule.Enum != nil && !slices.Contains(rule.Enum, s) {
			return false
		}
		if rule.Pattern != "" && !patterns[rule.Pattern].MatchString(s) {
			return false
		}
		if rule.URL && !SafeHref(s) {
			return false
		}
		if rule.Date {
			if _, err := time.Parse(time.DateOnly, s); err != nil {
				return false
			}
		}
		return true
	}
	return false
}

func integerIn(value any, rule Attr) bool {
	f, ok := value.(float64)
	return ok && f == math.Trunc(f) && f >= float64(rule.Min) && f <= float64(rule.Max)
}

func shortJSON(value any) string {
	out, _ := json.Marshal(value)
	const limit = 60
	if len(out) > limit {
		return string(out[:limit]) + "..."
	}
	return string(out)
}

var allowedSchemes = []string{"http", "https", "mailto"}

// SafeHref admits web and mail addresses and links within the site, and
// nothing a browser would run or send somewhere unexpected.
func SafeHref(href string) bool {
	if href == "" || utf8.RuneCountInString(href) > MaxHrefLength || !utf8.ValidString(href) {
		return false
	}
	for _, r := range href {
		// Browsers drop tabs and newlines inside a URL, so "java\tscript:"
		// would run; a backslash reads as a slash and makes "/\host" leave the site.
		if r < 0x20 || r == 0x7f || r == '\\' || r == ' ' {
			return false
		}
	}
	if strings.HasPrefix(href, "//") {
		return false
	}
	end := strings.IndexAny(href, "/?#")
	colon := strings.IndexByte(href, ':')
	if colon < 0 || (end >= 0 && end < colon) {
		return true
	}
	scheme := strings.ToLower(href[:colon])
	if !slices.Contains(allowedSchemes, scheme) {
		return false
	}
	if scheme == "mailto" {
		return len(href) > colon+1
	}
	u, err := url.Parse(href)
	return err == nil && u.Host != ""
}
