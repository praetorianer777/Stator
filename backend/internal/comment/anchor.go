package comment

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// ErrAnchorConflict refuses a passage marked on a body that is no longer the
// page's: somebody published or anchored meanwhile.
var ErrAnchorConflict = errors.New("the page changed since you selected the passage")

func anchorOf(m document.Mark) (uuid.UUID, bool) {
	if m.Type != AnchorMark {
		return uuid.Nil, false
	}
	raw, _ := m.Attrs["threadId"].(string)
	id, err := uuid.Parse(raw)
	return id, err == nil
}

func anchorMark(id uuid.UUID) document.Mark {
	return document.Mark{Type: AnchorMark, Attrs: map[string]any{"threadId": id.String()}}
}

func marked(n document.Node, id uuid.UUID) bool {
	return slices.ContainsFunc(n.Marks, func(m document.Mark) bool {
		got, ok := anchorOf(m)
		return ok && got == id
	})
}

// textblocks gathers the blocks that hold text directly, in reading order,
// so a passage can be changed in place.
func textblocks(n *document.Node, out *[]*document.Node, depth int) {
	if document.IsTextblock(*n) {
		*out = append(*out, n)
		return
	}
	if depth > document.MaxDepth {
		return
	}
	for i := range n.Content {
		textblocks(&n.Content[i], out, depth+1)
	}
}

// quoteOf is the text a thread's passage covers, cut at MaxQuoteLength.
func quoteOf(text string) string {
	if utf8.RuneCountInString(text) <= MaxQuoteLength {
		return text
	}
	runes := []rune(text)
	return string(runes[:MaxQuoteLength])
}

// NewAnchor answers the quote of a new thread's passage, when taking its mark
// out of the body sent leaves the stored body.
func NewAnchor(stored, sent json.RawMessage, id uuid.UUID) (string, error) {
	quote, root, err := passage(sent, id)
	if err != nil {
		return "", err
	}
	current, err := document.Parse(stored)
	if err != nil {
		return "", err
	}
	unmarked := document.DropMarks(root, func(m document.Mark) bool {
		got, ok := anchorOf(m)
		return ok && got == id
	})
	if !reflect.DeepEqual(unmarked, document.Normalize(current)) {
		return "", ErrAnchorConflict
	}
	return quote, nil
}

// passage finds the one run of text the thread's mark covers in a body.
func passage(sent json.RawMessage, id uuid.UUID) (string, document.Node, error) {
	field := func(message string) error { return &FieldError{Field: "pageBody", Message: message} }
	if len(sent) == 0 || string(sent) == "null" {
		return "", document.Node{}, field("Send the page's body with the passage marked.")
	}
	root, err := document.Parse(sent)
	if err == nil {
		err = document.ValidateNode(root)
	}
	if err != nil {
		var bad *document.InvalidError
		if errors.As(err, &bad) {
			return "", document.Node{}, field(bad.Message)
		}
		return "", document.Node{}, err
	}
	var blocks []*document.Node
	textblocks(&root, &blocks, 0)
	var (
		holder *document.Node
		first  = -1
		last   = -1
	)
	for _, block := range blocks {
		for i, child := range block.Content {
			if !marked(child, id) {
				continue
			}
			if holder != nil && holder != block {
				return "", document.Node{}, field("Select text within one paragraph or heading; a passage cannot span blocks.")
			}
			holder = block
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if holder == nil {
		return "", document.Node{}, field("Mark the passage you are commenting on with the thread's id.")
	}
	var text strings.Builder
	for _, child := range holder.Content[first : last+1] {
		if !marked(child, id) {
			return "", document.Node{}, field("Select one passage; the text you comment on cannot have gaps.")
		}
		text.WriteString(child.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", document.Node{}, field("Select some text to comment on.")
	}
	return quoteOf(text.String()), root, nil
}

// Settlement is what a publish did to the page's inline threads.
type Settlement struct {
	// Body is the body to publish, the one given when nothing changed.
	Body json.RawMessage
	// Moved are threads put back where their quote occurs once.
	Moved []uuid.UUID
	// Detached are threads whose passage could not be found.
	Detached []uuid.UUID
}

// Settle anchors the live threads, each by its quote, in a body about to be
// published; docs/api-contract-m2.md gives the rules.
func Settle(body json.RawMessage, live map[uuid.UUID]string) (Settlement, error) {
	out := Settlement{Body: body}
	if len(live) == 0 && !bytes.Contains(body, []byte(`"`+AnchorMark+`"`)) {
		return out, nil
	}
	root, err := document.Parse(body)
	if err != nil {
		return out, err
	}
	found := map[uuid.UUID]bool{}
	changed := false
	root = document.DropMarks(root, func(m document.Mark) bool {
		if m.Type != AnchorMark {
			return false
		}
		id, ok := anchorOf(m)
		if _, alive := live[id]; ok && alive {
			found[id] = true
			return false
		}
		changed = true
		return true
	})
	var missing []uuid.UUID
	for id := range live {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	slices.SortFunc(missing, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range missing {
		if reanchor(&root, id, live[id]) {
			out.Moved = append(out.Moved, id)
			changed = true
		} else {
			out.Detached = append(out.Detached, id)
		}
	}
	if changed {
		if out.Body, err = json.Marshal(root); err != nil {
			return out, err
		}
	}
	return out, nil
}

// reanchor marks the one place a quote occurs, and reports whether there was
// exactly one.
func reanchor(root *document.Node, id uuid.UUID, quote string) bool {
	if quote == "" {
		return false
	}
	var blocks []*document.Node
	textblocks(root, &blocks, 0)
	var (
		at    *document.Node
		start int
		count int
	)
	for _, block := range blocks {
		text := blockText(*block)
		for from := 0; from <= len(text); {
			i := strings.Index(text[from:], quote)
			if i < 0 {
				break
			}
			count++
			if count > 1 {
				return false
			}
			at, start = block, from+i
			_, size := utf8.DecodeRuneInString(text[from+i:])
			from += i + max(size, 1)
		}
	}
	if count != 1 {
		return false
	}
	markRange(at, start, start+len(quote), anchorMark(id))
	return true
}

// blockText is a text block's text alone; the offsets markRange takes count
// its bytes, and other inline nodes take up none.
func blockText(block document.Node) string {
	var b strings.Builder
	for _, child := range block.Content {
		if child.Type == "text" {
			b.WriteString(child.Text)
		}
	}
	return b.String()
}

// markRange adds a mark to the text between two offsets of a block,
// splitting the text nodes it starts and ends in.
func markRange(block *document.Node, start, end int, mark document.Mark) {
	var content []document.Node
	offset := 0
	for _, child := range block.Content {
		if child.Type != "text" {
			content = append(content, child)
			continue
		}
		from, to := offset, offset+len(child.Text)
		offset = to
		if to <= start || from >= end {
			content = append(content, child)
			continue
		}
		cut := func(a, b int) document.Node {
			part := child
			part.Text = child.Text[a-from : b-from]
			return part
		}
		if from < start {
			content = append(content, cut(from, start))
		}
		inside := cut(max(from, start), min(to, end))
		inside.Marks = append(slices.Clone(child.Marks), mark)
		content = append(content, inside)
		if to > end {
			content = append(content, cut(end, to))
		}
	}
	block.Content = content
}
