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
	// KindStrings is a list of distinct strings, each one of Enum, or matching
	// Pattern when it has one.
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
	// MaxPropertyKeyLength keeps a property's name to a report column's heading.
	MaxPropertyKeyLength = 60
	// MaxProperties bounds one properties block, a page's metadata rather than its body.
	MaxProperties = 50
	// MaxReportLabels and MaxReportColumns bound what a properties report asks for.
	MaxReportLabels  = 5
	MaxReportColumns = 10
	// DefaultListedPages and MaxListedPages are how many pages a block that
	// lists them shows when it names no number, and the most it may.
	DefaultListedPages = 10
	MaxListedPages     = 50
	// DefaultReportedTasks and MaxReportedTasks are how many tasks a task
	// report shows when it names no number, and the most it may.
	DefaultReportedTasks = 20
	MaxReportedTasks     = 100
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

// NodeInclude shows another page, or one excerpt of it, as each reader may
// read it; it holds only what it points at.
const NodeInclude = "include"

// NodeLinkCard is a link shown as a card with what its page says about
// itself, or as the player of an allowlisted site.
const NodeLinkCard = "linkCard"

// LinkCardViews are how a link card shows its page.
var LinkCardViews = []string{"card", "embed"}

// NodeProperties is a page's metadata as a two-column table, one
// NodePropertyRow per name; a properties report gathers them across pages.
const (
	NodeProperties  = "properties"
	NodePropertyRow = "propertyRow"
)

// NodePropertiesReport lists the properties of the pages carrying labels; it
// holds what to list, and each reader's view asks for the pages they may read.
const NodePropertiesReport = "propertiesReport"

// NodeLabelledPages lists the published pages carrying labels, and
// NodeRecentlyUpdated those published last; each holds what to list, and each
// reader's view asks for the pages they may read.
const (
	NodeLabelledPages   = "labelledPages"
	NodeRecentlyUpdated = "recentlyUpdated"
)

// NodeBlogPosts lists the newest blog posts of a space, or of every space;
// it holds which and how many, and each reader's view asks for the posts they may read.
const NodeBlogPosts = "blogPosts"

// NodeTableChart draws the one table it holds as a chart, so the chart is
// always the table's: edit the table and the chart follows.
const NodeTableChart = "tableChart"

// The charts a table becomes.
const (
	ChartBar  = "bar"
	ChartLine = "line"
	ChartPie  = "pie"
)

// TableCharts is what a chart from a table may draw.
var TableCharts = []string{ChartBar, ChartLine, ChartPie}

// NodeAttachmentList lists the files of the page it is on, the latest
// version of each name first; it holds nothing, the page's files are its own.
const NodeAttachmentList = "attachmentList"

// NodeGallery shows pictures of the page side by side, each a NodeGalleryImage
// naming one version of a file by id, as an image does, with its caption.
const (
	NodeGallery      = "gallery"
	NodeGalleryImage = "galleryImage"
)

const (
	// MinGalleryColumns and MaxGalleryColumns bound how many pictures a
	// gallery's row holds on a wide screen; a narrow one shows fewer.
	MinGalleryColumns = 2
	MaxGalleryColumns = 4
	// DefaultGalleryColumns is what a gallery read from elsewhere starts with
	// when it says nothing.
	DefaultGalleryColumns = 3
	// MaxGalleryImages keeps a gallery to what one page can load.
	MaxGalleryImages = 60
)

// NodeCalendar draws a month of one of a space's calendars, with the due
// issues of an Armature project beside its events; it holds which, never them.
const NodeCalendar = "calendar"

// NodeTemplateButton makes a page from a template under a parent, in one
// click; it holds the template's key, where the page goes and what it is called.
const NodeTemplateButton = "templateButton"

// TemplateKeyPattern is a template's key: lower case words joined by hyphens.
const TemplateKeyPattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`

const (
	// MaxTemplateKeyLength bounds a template's key, a word or an id.
	MaxTemplateKeyLength = 64
	// MaxButtonLabelLength keeps a button's words to one line.
	MaxButtonLabelLength = 80
	// MaxButtonTitleLength is a page title's own bound, page.MaxTitleLength.
	MaxButtonTitleLength = 255
)

// NodeContributors names the people who published the page it is on, or the
// page and the pages below it; it holds which and how many, never the people.
const NodeContributors = "contributors"

// The pages a contributors block counts: the page alone, or the page and
// every page below it.
const (
	ContributorsPage = "page"
	ContributorsTree = "tree"
)

// ContributorScopes is what a contributors block may count.
var ContributorScopes = []string{ContributorsPage, ContributorsTree}

// DefaultContributors and MaxContributors are how many people a contributors
// block names when it says no number, and the most it may.
const (
	DefaultContributors = 10
	MaxContributors     = 50
)

// NodeTaskReport lists the tasks of published pages a filter picks; it holds
// the filter, and each reader's view asks for the tasks they may read.
const NodeTaskReport = "taskReport"

// Whom a task report's tasks are assigned to beside one person: whoever
// reads the report, or nobody.
const (
	AssigneeReader = "me"
	AssigneeNobody = "none"
)

// TaskAssigneePattern is a task report's assignee: the reader, nobody, or a person's id.
const TaskAssigneePattern = `^(?:me|none|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`

// The due days a task report picks, judged against today in UTC as a date
// node is: any, before today, today, from today for a week, or none.
const (
	DueAny     = "any"
	DueOverdue = "overdue"
	DueToday   = "today"
	DueWeek    = "week"
	DueNone    = "none"
)

// The states a task report picks.
const (
	TaskStateOpen = "open"
	TaskStateDone = "done"
	TaskStateAll  = "all"
)

// TaskReportDues and TaskReportStates are what a task report may choose.
var (
	TaskReportDues   = []string{DueAny, DueOverdue, DueToday, DueWeek, DueNone}
	TaskReportStates = []string{TaskStateOpen, TaskStateDone, TaskStateAll}
)

// How a content by label list matches its labels and orders its pages.
const (
	MatchAll    = "all"
	MatchAny    = "any"
	SortTitle   = "title"
	SortUpdated = "updated"
)

// ListMatches and ListSorts are what a content by label list may choose.
var (
	ListMatches = []string{MatchAll, MatchAny}
	ListSorts   = []string{SortUpdated, SortTitle}
)

// LabelPattern is a label as label.Normalize leaves it.
const LabelPattern = `^[\p{Ll}\p{Lo}\p{Lm}\p{N}][\p{Ll}\p{Lo}\p{Lm}\p{N}_.-]{0,39}$`

// PropertyKeyPattern is a property's name as a report column takes it: trimmed,
// within MaxPropertyKeyLength.
const PropertyKeyPattern = `^\S(?:.{0,58}\S)?$`

// SpaceKeyPattern is a space's key, as space.ValidKey reads it.
const SpaceKeyPattern = `^[A-Z][A-Z0-9]{1,9}$`

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
	blockNodes  = []string{"paragraph", "heading", "bulletList", "orderedList", "taskList", "blockquote", "codeBlock", "horizontalRule", "table", "panel", "expand", "columns", NodeDecision, NodeMathBlock, NodeDiagram, NodeLinkCard, NodeExcerpt, NodeInclude, NodeProperties, NodePropertiesReport, NodeLabelledPages, NodeRecentlyUpdated, NodeBlogPosts, NodeTaskReport, NodeAttachmentList, NodeTableChart, NodeCalendar, NodeTemplateButton, NodeContributors, "image", NodeGallery, "tableOfContents", "childPages", armature.NodeIssueBlock, armature.NodeIssueList, armature.NodeChart, armature.NodeRoadmap}
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

// strs is a typed enum as the allowlist writes it.
func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, k := range in {
		out[i] = string(k)
	}
	return out
}

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
		"listItem": {Content: blockNodes},
		"taskList": {Content: []string{"taskItem"}},
		// taskId keeps an item the same task from one version to the next; the
		// server gives one to every item that has none when it is published.
		"taskItem": {Content: blockNodes, Attrs: map[string]Attr{
			"checked":  {Kind: KindBoolean},
			AttrTaskID: {Kind: KindString, Nullable: true, Pattern: UUIDPattern},
		}},
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
		NodeGallery: {Content: []string{NodeGalleryImage}, MinContent: 1, MaxContent: MaxGalleryImages, Attrs: map[string]Attr{
			"columns": {Kind: KindInteger, Min: MinGalleryColumns, Max: MaxGalleryColumns},
		}},
		// A caption is the picture's alternative text too, so it shares its bound.
		NodeGalleryImage: {Attrs: map[string]Attr{
			"attachmentId": {Kind: KindString, Pattern: UUIDPattern},
			"caption":      {Kind: KindString, Nullable: true, MaxLength: MaxAltLength},
		}},
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
		// What to count and how to draw it, never the counts: each reader's
		// view asks Armature, with their own token, for what they may see.
		armature.NodeChart: {
			Attrs: map[string]Attr{
				"project": {Kind: KindString, Pattern: armature.ProjectPattern},
				"query":   {Kind: KindString, MaxLength: armature.MaxQueryLength, Pattern: `\S`},
				"chart":   {Kind: KindString, Enum: strs(armature.ChartKinds)},
				"groupBy": {Kind: KindString, Enum: armature.ChartGroupings},
				"days":    {Kind: KindInteger, Min: armature.MinChartDays, Max: armature.MaxChartDays},
			},
		},
		// What to draw, never the days: each reader's view asks Armature.
		armature.NodeRoadmap: {
			Attrs: map[string]Attr{
				"project": {Kind: KindString, Pattern: armature.ProjectPattern},
				"query":   {Kind: KindString, MaxLength: armature.MaxQueryLength, Pattern: `\S`},
				"groupBy": {Kind: KindString, Enum: strs(armature.RoadmapGroupings)},
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
		// Ids only: the words are read for each reader when the page is shown,
		// so the included page's restrictions and later versions hold.
		NodeInclude: {Attrs: map[string]Attr{
			"pageId":    {Kind: KindString, Pattern: UUIDPattern},
			"excerptId": {Kind: KindString, Nullable: true, Pattern: UUIDPattern},
		}},
		// A name may be empty while it is typed; a report leaves such rows out.
		NodeProperties: {Content: []string{NodePropertyRow}, MinContent: 1, MaxContent: MaxProperties},
		NodePropertyRow: {Content: inlineNodes, AllowsMarks: true, Attrs: map[string]Attr{
			"key": {Kind: KindString, MaxLength: MaxPropertyKeyLength},
		}},
		// What to gather, never the values: each reader's view asks for them.
		NodePropertiesReport: {Attrs: map[string]Attr{
			"labels":  {Kind: KindStrings, Pattern: LabelPattern, MinLength: 1, MaxLength: MaxReportLabels},
			"space":   {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"columns": {Kind: KindStrings, Pattern: PropertyKeyPattern, MaxLength: MaxReportColumns},
		}},
		NodeLabelledPages: {Attrs: map[string]Attr{
			"labels": {Kind: KindStrings, Pattern: LabelPattern, MinLength: 1, MaxLength: MaxReportLabels},
			"match":  {Kind: KindString, Enum: ListMatches},
			"space":  {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"sort":   {Kind: KindString, Enum: ListSorts},
			"limit":  {Kind: KindInteger, Min: 1, Max: MaxListedPages},
		}},
		NodeRecentlyUpdated: {Attrs: map[string]Attr{
			"space": {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"limit": {Kind: KindInteger, Min: 1, Max: MaxListedPages},
		}},
		// A space's key, or null for every space the reader may read.
		NodeBlogPosts: {Attrs: map[string]Attr{
			"space": {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"limit": {Kind: KindInteger, Min: 1, Max: MaxListedPages},
		}},
		NodeAttachmentList: {},
		NodeTableChart: {Content: []string{"table"}, MinContent: 1, MaxContent: 1, Attrs: map[string]Attr{
			"chart":     {Kind: KindString, Enum: TableCharts},
			"showTable": {Kind: KindBoolean},
		}},
		NodeCalendar: {Attrs: map[string]Attr{
			"calendarId": {Kind: KindString, Pattern: UUIDPattern},
			"project":    {Kind: KindString, Nullable: true, Pattern: armature.ProjectPattern},
		}},
		NodeTemplateButton: {Attrs: map[string]Attr{
			"template": {Kind: KindString, MaxLength: MaxTemplateKeyLength, Pattern: TemplateKeyPattern},
			"space":    {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"parent":   {Kind: KindString, Nullable: true, Pattern: UUIDPattern},
			"label":    {Kind: KindString, MaxLength: MaxButtonLabelLength},
			"title":    {Kind: KindString, MaxLength: MaxButtonTitleLength},
		}},
		NodeContributors: {Attrs: map[string]Attr{
			"scope": {Kind: KindString, Enum: ContributorScopes},
			"limit": {Kind: KindInteger, Min: 1, Max: MaxContributors},
		}},
		NodeTaskReport: {Attrs: map[string]Attr{
			"space":    {Kind: KindString, Nullable: true, Pattern: SpaceKeyPattern},
			"assignee": {Kind: KindString, Nullable: true, Pattern: TaskAssigneePattern},
			"due":      {Kind: KindString, Enum: TaskReportDues},
			"state":    {Kind: KindString, Enum: TaskReportStates},
			"limit":    {Kind: KindInteger, Min: 1, Max: MaxReportedTasks},
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
