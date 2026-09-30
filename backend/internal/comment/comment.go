// Package comment keeps the discussion on pages: threads below a page, and
// inline threads anchored to a passage of its body by a mark.
package comment

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxBodyBytes caps a comment's document; anything longer is a page.
	MaxBodyBytes = 64 << 10
	// AnchorMark is the mark an inline thread's passage carries in the page
	// body; its threadId attribute names the thread.
	AnchorMark = "inlineComment"
	// MaxQuoteLength bounds the text of the passage an inline thread keeps.
	MaxQuoteLength = 500
)

// Kind says where a thread sits: below the page or on a passage of it.
type Kind string

const (
	KindPage   Kind = "page"
	KindInline Kind = "inline"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindPage, KindInline}

// AnchorState says whether an inline thread's passage is still in the page.
type AnchorState string

const (
	AnchorAnchored AnchorState = "anchored"
	// AnchorDetached is a thread whose passage a publish removed.
	AnchorDetached AnchorState = "detached"
)

// AnchorStates lists every AnchorState, for the API document.
var AnchorStates = []AnchorState{AnchorAnchored, AnchorDetached}

// Anchor is where an inline thread points in the page body.
type Anchor struct {
	State AnchorState `json:"state"`
	// Quote is the passage's text as it was when the thread began.
	Quote string `json:"quote"`
}

// Thread is a first comment and the replies to it, oldest first.
type Thread struct {
	// ID is the first comment's id, and the mark's threadId for an inline one.
	ID     uuid.UUID `json:"id"`
	PageID uuid.UUID `json:"pageId"`
	Kind   Kind      `json:"kind"`
	// Anchor is null for a thread below the page.
	Anchor         *Anchor    `json:"anchor"`
	Resolved       bool       `json:"resolved"`
	ResolvedByName string     `json:"resolvedByName"`
	ResolvedAt     *time.Time `json:"resolvedAt"`
	Comments       []Comment  `json:"comments"`
	Can            ThreadCan  `json:"can"`
}

// ThreadCan is what the caller may do to a thread.
type ThreadCan struct {
	Reply   bool `json:"reply"`
	Resolve bool `json:"resolve"`
}

// Comment is one comment of a thread.
type Comment struct {
	ID       uuid.UUID `json:"id"`
	ThreadID uuid.UUID `json:"threadId"`
	// AuthorID is null once the author is gone.
	AuthorID   *uuid.UUID `json:"authorId"`
	AuthorName string     `json:"authorName"`
	// Body is the document, null once the comment is deleted.
	Body      json.RawMessage `json:"body"`
	Deleted   bool            `json:"deleted"`
	CreatedAt time.Time       `json:"createdAt"`
	EditedAt  *time.Time      `json:"editedAt"`
	Can       CommentCan      `json:"can"`
}

// CommentCan is what the caller may do to one comment.
type CommentCan struct {
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
}

// Counts are a page's comments as its header shows them.
type Counts struct {
	// Page counts the comments below the page, replies included.
	Page int `json:"page"`
	// Inline and Detached count open inline threads, anchored or not.
	Inline   int `json:"inline"`
	Detached int `json:"detached"`
}

// ThreadInput starts a thread below a page.
type ThreadInput struct {
	Body json.RawMessage `json:"body"`
}

// InlineThreadInput starts a thread on a passage: the page's published body
// with the passage marked, which must differ from it by that mark alone.
type InlineThreadInput struct {
	// ThreadID is chosen by the client, since the mark has to name it.
	ThreadID uuid.UUID       `json:"threadId"`
	Body     json.RawMessage `json:"body"`
	PageBody json.RawMessage `json:"pageBody"`
}

// BodyInput is a reply, or a comment's new body.
type BodyInput struct {
	Body json.RawMessage `json:"body"`
}
