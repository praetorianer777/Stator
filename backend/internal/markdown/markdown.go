// Package markdown turns a page's document into Markdown and back into one
// the allowlist accepts; docs/markdown.md lists how each node is written.
package markdown

import (
	"encoding/json"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// MaxSourceBytes caps one Markdown file. A page's document is held to
// document.MaxBytes, which a file this size already comes close to.
const MaxSourceBytes = 1 << 20

// statorAttr names the attribute on the span and div elements that carry what
// Markdown has no syntax for; its value says which node the element is.
const statorAttr = "data-stator"

// The values of statorAttr. An issue is a span inline and a div as a block.
const (
	kindMention    = "mention"
	kindIssue      = "issue"
	kindStatus     = "status"
	kindDate       = "date"
	kindIssueList  = "issues"
	kindTOC        = "toc"
	kindChildPages = "child-pages"
	kindInclude    = "include"
	kindChart      = "issue-chart"
	kindRoadmap    = "issue-roadmap"
	kindReport     = "properties-report"
	kindLabelled   = "labelled-pages"
	kindUpdated    = "updated-pages"
)

// panelAlerts pairs each panel kind with the alert a quote opens with, one
// to one, so a panel comes back as the kind it left as.
var panelAlerts = map[string]string{
	"info":    "NOTE",
	"note":    "IMPORTANT",
	"success": "TIP",
	"warning": "WARNING",
	"error":   "CAUTION",
}

// markOrder is the order marks open in, outermost first: code is innermost
// because nothing inside a code span is read as Markdown.
var markOrder = []string{"link", "bold", "italic", "strike", "code"}

func markRank(t string) int {
	for i, m := range markOrder {
		if m == t {
			return i
		}
	}
	return -1
}

// validates says whether a node passes the allowlist where it would stand,
// inline in a paragraph or as a block of its own.
func validates(n document.Node, inline bool) bool {
	doc := document.Node{Type: "doc", Content: []document.Node{n}}
	if inline {
		doc.Content = []document.Node{{Type: "paragraph", Content: []document.Node{n}}}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return false
	}
	return document.Validate(body) == nil
}
