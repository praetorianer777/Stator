// Package document holds which ProseMirror documents a page may store, and
// what search and the table of contents read from them.
package document

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// AttrKind is the JSON type an attribute's value has.
type AttrKind string

const (
	KindString   AttrKind = "string"
	KindInteger  AttrKind = "integer"
	KindBoolean  AttrKind = "boolean"
	KindIntegers AttrKind = "integers"
	// KindStrings is a list of distinct strings, each one of Enum.
	KindStrings AttrKind = "strings"
	// KindNull is an attribute the editor always writes and always leaves empty.
	KindNull AttrKind = "null"
)

// Attr is what one attribute may hold. Min and Max bound each integer;
// MinLength and MaxLength count a string's characters or a list's items.
type Attr struct {
	Kind      AttrKind `json:"kind"`
	Nullable  bool     `json:"nullable,omitempty"`
	Enum      []string `json:"enum,omitempty"`
	Min       int      `json:"min,omitempty"`
	Max       int      `json:"max,omitempty"`
	MinLength int      `json:"minLength,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	URL       bool     `json:"url,omitempty"`
	// Date makes a string a calendar day that exists, written YYYY-MM-DD.
	Date bool `json:"date,omitempty"`
}

// NodeSpec is one node type: its attributes, the types it may contain (none
// makes it a leaf), how many when that is bounded, and whether it or its
// inline children take marks.
type NodeSpec struct {
	Attrs       map[string]Attr `json:"attrs,omitempty"`
	Content     []string        `json:"content,omitempty"`
	MinContent  int             `json:"minContent,omitempty"`
	MaxContent  int             `json:"maxContent,omitempty"`
	Inline      bool            `json:"inline,omitempty"`
	AllowsMarks bool            `json:"allowsMarks,omitempty"`
}

// MarkSpec is one mark type and its attributes. A repeatable mark may sit on
// one piece of text several times, each time with other attributes.
type MarkSpec struct {
	Attrs      map[string]Attr `json:"attrs,omitempty"`
	Repeatable bool            `json:"repeatable,omitempty"`
}

// Allowlist is every node and mark a stored document may hold.
type Allowlist struct {
	Nodes map[string]NodeSpec `json:"nodes"`
	Marks map[string]MarkSpec `json:"marks"`
}

const (
	// MaxBytes caps a document; a page longer than this is several pages.
	MaxBytes = 2 << 20
	// MaxDepth stops a nest of lists, quotes and panels from being a way to
	// exhaust the server.
	MaxDepth = 40
	// MaxHeadingLevel is the deepest heading; the page title is above level 1.
	MaxHeadingLevel = 3
	// MaxAnchorLength leaves room for a dedupe suffix after a slug.
	MaxAnchorLength = 80
	// MaxSlugLength is where a heading's slug is cut before deduplication.
	MaxSlugLength = 64
	// MaxHrefLength matches what browsers and proxies reliably carry.
	MaxHrefLength = 2048
	// MaxTableSpan bounds how many rows or columns one cell may cover.
	MaxTableSpan = 100
	// MaxColumnWidth bounds a stored column width, in pixels.
	MaxColumnWidth = 4000
	// MaxListStart bounds where a numbered list may start counting.
	MaxListStart = 1_000_000
	// maxLabelLength bounds a mention's shown name and a link's title.
	maxLabelLength = 256
	// maxLanguageLength bounds a code block's language name.
	maxLanguageLength = 32
	// MaxAltLength bounds an image's description.
	MaxAltLength = 500
	// MaxImageWidth bounds an image's stored width, in pixels, as a column's.
	MaxImageWidth = 4000
	// maxFileNameLength matches the longest file name an upload keeps.
	maxFileNameLength = 200
	// MaxChildPagesDepth is the most levels a child pages block names; a null
	// depth is every level.
	MaxChildPagesDepth = 10
	// MaxExpandTitleLength bounds an expand block's title, one line on its toggle.
	MaxExpandTitleLength = 200
	// MinColumns and MaxColumns bound a column layout: one column is a page,
	// and a fourth is too narrow to read on most screens.
	MinColumns = 2
	MaxColumns = 3
	// MinColumnShare and MaxColumnShare bound a column's share of its row, in
	// percent, so that no column of three is squeezed to nothing.
	MinColumnShare = 10
	MaxColumnShare = 80
	// MaxStatusLength keeps a status label short enough to sit in a line of text.
	MaxStatusLength = 40
	// MaxMathLength bounds a formula's TeX source, so one formula cannot keep
	// every reader's browser typesetting.
	MaxMathLength = 4000
	// MaxDiagramLength bounds a diagram's source, room for a sketch of a whole
	// system while one diagram cannot keep a reader's browser drawing.
	MaxDiagramLength = 20000
	// MaxExcerptNameLength keeps an excerpt's name to what fits in a picker's line.
	MaxExcerptNameLength = 80
)

// The states of a decision item.
const (
	DecisionDecided   = "decided"
	DecisionUndecided = "undecided"
)

// NodeDecision is a decision item: one line of text and whether it is decided.
const NodeDecision = "decision"

// NodeStatus and NodeDate are the inline status label and the inline date.
const (
	NodeStatus = "status"
	NodeDate   = "date"
)

// NodeMathInline and NodeMathBlock are a formula in running text and one on
// a line of its own, each stored as its TeX source.
const (
	NodeMathInline = "mathInline"
	NodeMathBlock  = "mathBlock"
)

// NodeExcerpt is a named part of a page that other pages include. Its id
// stays when it is renamed, so an include keeps finding it.
const NodeExcerpt = "excerpt"

// NodeLinkCard is a link shown as a card with what its page says about
// itself, or as the player of an allowlisted site.
const NodeLinkCard = "linkCard"

// LinkCardViews are how a link card shows its page.
var LinkCardViews = []string{"card", "embed"}

// NodeDiagram is a diagram written as Mermaid text, drawn by each reader's
// browser.
const NodeDiagram = "diagram"

// DatePattern is a day as a date node stores it; the validator also checks
// that the day exists.
const DatePattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`

// AnchorMark names the mark an inline thread's passage carries in a page body.
const AnchorMark = "inlineComment"

// UUIDPattern is an id as the API writes it, in lower case.
const UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`

// AnchorPattern is what a heading anchor looks like: lowercase words of
// letters and digits joined by single hyphens.
const AnchorPattern = `^[\p{Ll}\p{Lo}\p{Lm}\p{N}]+(?:-[\p{Ll}\p{Lo}\p{Lm}\p{N}]+)*$`

var (
	blockNodes  = []string{"paragraph", "heading", "bulletList", "orderedList", "taskList", "blockquote", "codeBlock", "horizontalRule", "table", "panel", "expand", "columns", NodeDecision, NodeMathBlock, NodeDiagram, NodeLinkCard, NodeExcerpt, "image", "tableOfContents", "childPages", armature.NodeIssueBlock, armature.NodeIssueList}
	inlineNodes = []string{"text", "hardBreak", "mention", "attachment", armature.NodeIssue, NodeStatus, NodeDate, NodeMathInline}
	mathAttrs   = map[string]Attr{"latex": {Kind: KindString, MaxLength: MaxMathLength, Pattern: `\S`}}
	cellAttrs   = map[string]Attr{
		"colspan":    {Kind: KindInteger, Min: 1, Max: MaxTableSpan},
		"rowspan":    {Kind: KindInteger, Min: 1, Max: MaxTableSpan},
		"colwidth":   {Kind: KindIntegers, Nullable: true, Max: MaxColumnWidth, MaxLength: MaxTableSpan},
		"background": {Kind: KindString, Nullable: true, Enum: CellBackgrounds},
		"align":      {Kind: KindString, Nullable: true, Enum: []string{"left", "center", "right"}},
	}
)

// PanelKinds and CellBackgrounds name theme roles rather than colours, so a
// custom theme recolours them.
var (
	PanelKinds = []string{"info", "note", "success", "warning", "error"}
	// DecisionStates is whether a decision item has been made yet.
	DecisionStates  = []string{DecisionDecided, DecisionUndecided}
	CellBackgrounds = []string{"neutral", "accent", "success", "warning", "danger"}
	StatusColors    = []string{"neutral", "accent", "success", "warning", "danger"}
)

// ChildPagesScopes and ChildPagesSorts are what a child pages block lists and
// in which order; the API's list of the pages below a page takes the same.
var (
	ChildPagesScopes = []string{"children", "subtree"}
	ChildPagesSorts  = []string{"tree", "title", "updated"}
)

func issueColumns() []string {
	out := make([]string, len(armature.Columns))
	for i, c := range armature.Columns {
		out[i] = string(c)
	}
	return out
}

// Allowed is the one table the validator, the generated
// api/document-allowlist.json and the web editor's test all read.
var Allowed = Allowlist{
	Nodes: map[string]NodeSpec{
		"doc":       {Content: blockNodes},
		"paragraph": {Content: inlineNodes, AllowsMarks: true},
		// A decision is a line of its own, so the log can quote it whole.
		NodeDecision: {Content: inlineNodes, AllowsMarks: true, Attrs: map[string]Attr{"state": {Kind: KindString, Enum: DecisionStates}}},
		"heading": {
			Content:     inlineNodes,
			AllowsMarks: true,
			Attrs: map[string]Attr{
				"level": {Kind: KindInteger, Min: 1, Max: MaxHeadingLevel},
				"id":    {Kind: KindString, Nullable: true, MaxLength: MaxAnchorLength, Pattern: AnchorPattern},
			},
		},
		"bulletList": {Content: []string{"listItem"}},
		"orderedList": {
			Content: []string{"listItem"},
			Attrs: map[string]Attr{
				"start": {Kind: KindInteger, Min: 0, Max: MaxListStart},
				"type":  {Kind: KindString, Nullable: true, Enum: []string{"1", "a", "A", "i", "I"}},
			},
		},
		"listItem":   {Content: blockNodes},
		"taskList":   {Content: []string{"taskItem"}},
		"taskItem":   {Content: blockNodes, Attrs: map[string]Attr{"checked": {Kind: KindBoolean}}},
		"blockquote": {Content: blockNodes},
		"codeBlock": {
			Content: []string{"text"},
			Attrs: map[string]Attr{
				"language": {Kind: KindString, Nullable: true, MaxLength: maxLanguageLength, Pattern: `^[a-z0-9][a-z0-9+#-]*$`},
			},
		},
		"horizontalRule": {},
		"table":          {Content: []string{"tableRow"}},
		"tableRow":       {Content: []string{"tableCell", "tableHeader"}},
		"tableCell":      {Content: blockNodes, Attrs: cellAttrs},
		"tableHeader":    {Content: blockNodes, Attrs: cellAttrs},
		"panel":          {Content: blockNodes, Attrs: map[string]Attr{"kind": {Kind: KindString, Enum: PanelKinds}}},
		// Whether it is open is each reader's own, so only its title is stored;
		// an empty title reads as a stock label.
		"expand": {Content: blockNodes, Attrs: map[string]Attr{"title": {Kind: KindString, MaxLength: MaxExpandTitleLength}}},
		// A column's width is its share of the row; the shares need not add up,
		// since they are read as proportions, and none set is an even split.
		"columns":   {Content: []string{"column"}, MinContent: MinColumns, MaxContent: MaxColumns},
		"column":    {Content: blockNodes, Attrs: map[string]Attr{"width": {Kind: KindInteger, Nullable: true, Min: MinColumnShare, Max: MaxColumnShare}}},
		"text":      {Inline: true},
		"hardBreak": {Inline: true},
		"mention": {
			Inline: true,
			Attrs: map[string]Attr{
				// id is a member's id; a mention of somebody who left stays in
				// the text and tells nobody.
				"id":                    {Kind: KindString, Pattern: UUIDPattern},
				"label":                 {Kind: KindString, MaxLength: maxLabelLength, Pattern: `\S`},
				"mentionSuggestionChar": {Kind: KindString, Nullable: true, Enum: []string{"@"}},
			},
		},
		"image": {
			Attrs: map[string]Attr{
				"attachmentId": {Kind: KindString, Pattern: UUIDPattern},
				"alt":          {Kind: KindString, Nullable: true, MaxLength: MaxAltLength},
				"width":        {Kind: KindInteger, Nullable: true, Min: 1, Max: MaxImageWidth},
			},
		},
		"tableOfContents": {
			Attrs: map[string]Attr{"maxLevel": {Kind: KindInteger, Min: 1, Max: MaxHeadingLevel}},
		},
		"childPages": {
			Attrs: map[string]Attr{
				"scope": {Kind: KindString, Enum: ChildPagesScopes},
				"depth": {Kind: KindInteger, Nullable: true, Min: 1, Max: MaxChildPagesDepth},
				"sort":  {Kind: KindString, Enum: ChildPagesSorts},
			},
		},
		// Only the key: a summary in the body would be readable by anybody who
		// reads the page, and would go stale.
		armature.NodeIssue: {
			Inline: true,
			Attrs:  map[string]Attr{"key": {Kind: KindString, Pattern: armature.KeyPattern}},
		},
		armature.NodeIssueBlock: {
			Attrs: map[string]Attr{"key": {Kind: KindString, Pattern: armature.KeyPattern}},
		},
		// The query and how to show it, never its rows: each reader's view
		// asks Armature for the rows that reader may see.
		armature.NodeIssueList: {
			Attrs: map[string]Attr{
				"query":   {Kind: KindString, MaxLength: armature.MaxQueryLength, Pattern: `\S`},
				"columns": {Kind: KindStrings, Enum: issueColumns(), MinLength: 1, MaxLength: armature.MaxColumns},
				"limit":   {Kind: KindInteger, Min: 1, Max: armature.MaxListLimit},
			},
		},
		// The colour names a theme role, so a custom theme recolours it.
		NodeStatus: {
			Inline: true,
			Attrs: map[string]Attr{
				"label": {Kind: KindString, MaxLength: MaxStatusLength, Pattern: `\S`},
				"color": {Kind: KindString, Enum: StatusColors},
			},
		},
		// A day rather than an instant, so every reader sees the same day,
		// in their own locale's words, wherever they are.
		NodeDate: {
			Inline: true,
			Attrs:  map[string]Attr{"date": {Kind: KindString, Pattern: DatePattern, Date: true}},
		},
		// Only the source: each reader's browser typesets it, so a formula is
		// never stored as markup a reader's browser would run.
		NodeMathInline: {Inline: true, Attrs: mathAttrs},
		NodeMathBlock:  {Attrs: mathAttrs},
		// Only the text, for the same reason: the drawing is made from it.
		// Only the address and the view: what the page says is read for each
		// reader, so it is never stale in the body.
		// Any blocks but another excerpt, which the validator refuses at any depth.
		NodeExcerpt: {Content: blockNodes, Attrs: map[string]Attr{
			"id":   {Kind: KindString, Pattern: UUIDPattern},
			"name": {Kind: KindString, MaxLength: MaxExcerptNameLength, Pattern: `\S`},
		}},
		NodeLinkCard: {Attrs: map[string]Attr{
			"url":  {Kind: KindString, MaxLength: MaxHrefLength, URL: true, Pattern: `^[Hh][Tt][Tt][Pp][Ss]?://`},
			"view": {Kind: KindString, Enum: LinkCardViews},
		}},
		NodeDiagram: {Attrs: map[string]Attr{"source": {Kind: KindString, MaxLength: MaxDiagramLength, Pattern: `\S`}}},
		"attachment": {
			Inline: true,
			Attrs: map[string]Attr{
				"attachmentId": {Kind: KindString, Pattern: UUIDPattern},
				"fileName":     {Kind: KindString, MaxLength: maxFileNameLength, Pattern: `\S`},
			},
		},
	},
	Marks: map[string]MarkSpec{
		"bold":   {},
		"italic": {},
		"strike": {},
		"code":   {},
		// hint is a template's placeholder text: shown muted, replaced on the
		// first keystroke, and stripped by the database from what is published.
		"hint": {},
		// The passage an inline thread is about. Passages may overlap, and
		// the database strips the mark from every version.
		AnchorMark: {Repeatable: true, Attrs: map[string]Attr{
			"threadId": {Kind: KindString, Pattern: UUIDPattern},
		}},
		"link": {Attrs: map[string]Attr{
			"href":   {Kind: KindString, MaxLength: MaxHrefLength, URL: true},
			"target": {Kind: KindString, Nullable: true, Enum: []string{"_blank"}},
			"rel":    {Kind: KindString, Nullable: true, Enum: []string{"noopener noreferrer nofollow"}},
			"class":  {Kind: KindNull},
			"title":  {Kind: KindString, Nullable: true, MaxLength: maxLabelLength},
		}},
	},
}

// MaxCommentBytes caps a comment's document; anything longer is a page.
const MaxCommentBytes = 64 << 10

// CommentNodes and CommentMarks are what a comment may hold, by name: text
// and its structure, never files, tables, panels, expand blocks, columns,
// generated blocks or hints.
var (
	CommentNodes = []string{"doc", "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock", "hardBreak", "text", "mention"}
	CommentMarks = []string{"bold", "italic", "strike", "code", "link"}
)

// CommentAllowed is the part of Allowed a comment may hold, written to
// api/comment-allowlist.json for the web client's comment editor.
var CommentAllowed = Allowed.Subset(CommentNodes, CommentMarks)

// Subset keeps the named nodes and marks, each as a allows it, and drops
// every other type from what the kept nodes may contain.
func (a Allowlist) Subset(nodes, marks []string) Allowlist {
	out := Allowlist{Nodes: map[string]NodeSpec{}, Marks: map[string]MarkSpec{}}
	for _, name := range nodes {
		spec, ok := a.Nodes[name]
		if !ok {
			panic("document: no node " + name + " to keep")
		}
		var content []string
		for _, child := range spec.Content {
			if slices.Contains(nodes, child) {
				content = append(content, child)
			}
		}
		spec.Content = content
		out.Nodes[name] = spec
	}
	for _, name := range marks {
		spec, ok := a.Marks[name]
		if !ok {
			panic("document: no mark " + name + " to keep")
		}
		out.Marks[name] = spec
	}
	return out
}

// JSON is the allowlist as api/document-allowlist.json holds it: indented,
// keys sorted, ending in a newline.
func (a Allowlist) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(a); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
