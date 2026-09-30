// Package document holds which ProseMirror documents a page may store, and
// what search and the table of contents read from them.
package document

import (
	"bytes"
	"encoding/json"
)

// AttrKind is the JSON type an attribute's value has.
type AttrKind string

const (
	KindString   AttrKind = "string"
	KindInteger  AttrKind = "integer"
	KindBoolean  AttrKind = "boolean"
	KindIntegers AttrKind = "integers"
	// KindNull is an attribute the editor always writes and always leaves empty.
	KindNull AttrKind = "null"
)

// Attr is what one attribute may hold. Min and Max bound each integer;
// MaxLength counts a string's characters or a list's items.
type Attr struct {
	Kind      AttrKind `json:"kind"`
	Nullable  bool     `json:"nullable,omitempty"`
	Enum      []string `json:"enum,omitempty"`
	Min       int      `json:"min,omitempty"`
	Max       int      `json:"max,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	URL       bool     `json:"url,omitempty"`
}

// NodeSpec is one node type: its attributes, the types it may contain (none
// makes it a leaf), and whether it or its inline children take marks.
type NodeSpec struct {
	Attrs       map[string]Attr `json:"attrs,omitempty"`
	Content     []string        `json:"content,omitempty"`
	Inline      bool            `json:"inline,omitempty"`
	AllowsMarks bool            `json:"allowsMarks,omitempty"`
}

// MarkSpec is one mark type and its attributes.
type MarkSpec struct {
	Attrs map[string]Attr `json:"attrs,omitempty"`
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
	// maxIDLength bounds a mentioned person's id.
	maxIDLength = 128
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
)

// UUIDPattern is an id as the API writes it, in lower case.
const UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`

// AnchorPattern is what a heading anchor looks like: lowercase words of
// letters and digits joined by single hyphens.
const AnchorPattern = `^[\p{Ll}\p{Lo}\p{Lm}\p{N}]+(?:-[\p{Ll}\p{Lo}\p{Lm}\p{N}]+)*$`

var (
	blockNodes  = []string{"paragraph", "heading", "bulletList", "orderedList", "taskList", "blockquote", "codeBlock", "horizontalRule", "table", "panel", "image", "tableOfContents", "childPages"}
	inlineNodes = []string{"text", "hardBreak", "mention", "attachment"}
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
	PanelKinds      = []string{"info", "note", "success", "warning", "error"}
	CellBackgrounds = []string{"neutral", "accent", "success", "warning", "danger"}
)

// ChildPagesScopes and ChildPagesSorts are what a child pages block lists and
// in which order; the API's list of the pages below a page takes the same.
var (
	ChildPagesScopes = []string{"children", "subtree"}
	ChildPagesSorts  = []string{"tree", "title", "updated"}
)

// Allowed is the one table the validator, the generated
// api/document-allowlist.json and the web editor's test all read.
var Allowed = Allowlist{
	Nodes: map[string]NodeSpec{
		"doc":       {Content: blockNodes},
		"paragraph": {Content: inlineNodes, AllowsMarks: true},
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
		"text":           {Inline: true},
		"hardBreak":      {Inline: true},
		"mention": {
			Inline: true,
			Attrs: map[string]Attr{
				"id":                    {Kind: KindString, MaxLength: maxIDLength, Pattern: `^\S+$`},
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
		"link": {Attrs: map[string]Attr{
			"href":   {Kind: KindString, MaxLength: MaxHrefLength, URL: true},
			"target": {Kind: KindString, Nullable: true, Enum: []string{"_blank"}},
			"rel":    {Kind: KindString, Nullable: true, Enum: []string{"noopener noreferrer nofollow"}},
			"class":  {Kind: KindNull},
			"title":  {Kind: KindString, Nullable: true, MaxLength: maxLabelLength},
		}},
	},
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
