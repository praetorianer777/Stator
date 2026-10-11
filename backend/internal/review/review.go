// Package review holds pages to a review before they are published, and asks
// their readers to confirm they read them (#313); docs/decisions.md says how.
package review

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// MaxRequired bounds how many approvals a rule may ask for.
const MaxRequired = 10

// PageRef names a page a rule or a review is about.
type PageRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// Rule says who reviews what is published in a space, or in a branch of its
// pages. A branch's rule takes the place of the space's for the pages in it.
type Rule struct {
	ID       uuid.UUID `json:"id"`
	SpaceKey string    `json:"spaceKey"`
	// Page is the top of the branch the rule covers, with every page below
	// it; null for the whole space, its blog included.
	Page *PageRef `json:"page"`
	// Reviewers are the people and groups who may approve; a group's
	// members each count once.
	Reviewers []perm.Subject `json:"reviewers"`
	// Required is how many of them must approve before a draft goes out.
	Required      int       `json:"required"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// RuleInput sets a rule: people and groups, never everyone, and how many
// approvals it takes, from 1 to MaxRequired.
type RuleInput struct {
	Reviewers []perm.SubjectRef `json:"reviewers"`
	Required  int               `json:"required"`
}

// State is where a review stands.
type State string

const (
	// StatePending waits for its reviewers; the page is not published
	// around it.
	StatePending State = "pending"
	// StateChanges is a review a reviewer sent back with what to change.
	StateChanges State = "changes_requested"
	// StateWithdrawn is a review its author took back.
	StateWithdrawn State = "withdrawn"
	// StatePublished is a review whose approvals came in, published as its
	// author's version.
	StatePublished State = "published"
	// StateSkipped is a review an administrator published without its
	// approvals, with a reason.
	StateSkipped State = "skipped"
)

// States is every state, for the API's description.
var States = []State{StatePending, StateChanges, StateWithdrawn, StatePublished, StateSkipped}

// Decision is what a reviewer says of a draft.
type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionChanges Decision = "changes"
)

// Decisions is every decision, for the API's description.
var Decisions = []Decision{DecisionApprove, DecisionChanges}

// Review is a draft of a page put to its reviewers, as it was when submitted.
type Review struct {
	ID     uuid.UUID `json:"id"`
	PageID uuid.UUID `json:"pageId"`
	State  State     `json:"state"`
	// AuthorID is null once the author is gone.
	AuthorID   *uuid.UUID `json:"authorId"`
	AuthorName string     `json:"authorName"`
	// BaseVersion is the published version the draft was made from.
	BaseVersion int    `json:"baseVersion"`
	Title       string `json:"title"`
	// Comment is what the author says changed, for the history.
	Comment string `json:"comment"`
	// Reviewers and Required are the rule's when the draft was submitted.
	Reviewers []perm.Subject `json:"reviewers"`
	Required  int            `json:"required"`
	// Approvals counts the reviewers who approved.
	Approvals int             `json:"approvals"`
	Decisions []DecisionEntry `json:"decisions"`
	// Can says what the caller may do with the review.
	Can         ReviewCan  `json:"can"`
	SubmittedAt time.Time  `json:"submittedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	// Version is the version it was published as; null until then.
	Version *int `json:"version"`
	// SkipReason and SkippedByName say why and by whom it was published
	// without its approvals; empty unless it was skipped.
	SkipReason    string `json:"skipReason"`
	SkippedByName string `json:"skippedByName"`
	// Body is the draft's document, given when one review is read; the
	// comparison with the published version is GET /pages/{id}/compare
	// with review:{id} as a side.
	Body json.RawMessage `json:"body,omitempty"`
}

// ReviewCan says what the caller may do with a review.
type ReviewCan struct {
	// Decide is approving or sending back: a named reviewer, not the
	// author, while it is pending.
	Decide bool `json:"decide"`
	// Withdraw is the author's, while it is pending.
	Withdraw bool `json:"withdraw"`
	// Skip is publishing it without its approvals, with a reason: an
	// administrator of the space, while it is pending.
	Skip bool `json:"skip"`
}

// DecisionEntry is one reviewer's word on a review.
type DecisionEntry struct {
	// ReviewerID is null once the reviewer is gone.
	ReviewerID   *uuid.UUID `json:"reviewerId"`
	ReviewerName string     `json:"reviewerName"`
	Decision     Decision   `json:"decision"`
	Comment      string     `json:"comment"`
	DecidedAt    time.Time  `json:"decidedAt"`
}

// SubmitInput puts the caller's draft of the page to its reviewers; the
// comment and the watchers are those the publish will have.
type SubmitInput struct {
	Comment        string `json:"comment,omitempty"`
	NotifyWatchers bool   `json:"notifyWatchers,omitempty"`
}

// DecisionInput is a reviewer's word; asking for changes needs a comment.
type DecisionInput struct {
	Decision Decision `json:"decision"`
	Comment  string   `json:"comment,omitempty"`
}

// SkipInput publishes a pending review without its approvals.
type SkipInput struct {
	Reason string `json:"reason"`
}

// Status is a page's standing with reviews, as the page shows it.
type Status struct {
	// Rule is the rule the page is held to, its branch's or its space's;
	// null when publishing it needs no review.
	Rule *Rule `json:"rule"`
	// Pending is the review waiting on the page, null when none is.
	Pending *Review `json:"pending"`
}

// Approval is how a version got past its review, as the history shows it.
type Approval struct {
	ReviewID uuid.UUID `json:"reviewId"`
	// ApprovedBy names the reviewers who approved it.
	ApprovedBy []string `json:"approvedBy"`
	// SkippedByName and SkipReason say it was published without its
	// approvals; empty otherwise.
	SkippedByName string `json:"skippedByName"`
	SkipReason    string `json:"skipReason"`
}

// ReadAsk is a page asking its readers to confirm they read it.
type ReadAsk struct {
	// Since is the version from which a confirmation counts; null when
	// the page does not ask.
	Since *int `json:"since"`
	// ConfirmedAt is when the caller confirmed reading it since; null
	// when they have not.
	ConfirmedAt *time.Time `json:"confirmedAt"`
}

// ReadAskInput asks readers to confirm from the current version, again
// when the page asked before, or stops asking.
type ReadAskInput struct {
	Ask bool `json:"ask"`
}

// Readers is who confirmed reading a page since it asked, and who of those
// who may read it has not, for its editors.
type Readers struct {
	Since     int           `json:"since"`
	Confirmed []ReaderEntry `json:"confirmed"`
	Pending   []perm.Person `json:"pending"`
	// More counts those who have not confirmed beyond Pending.
	More int `json:"more"`
}

// ReaderEntry is one confirmation.
type ReaderEntry struct {
	Person      perm.Person `json:"person"`
	Version     int         `json:"version"`
	ConfirmedAt time.Time   `json:"confirmedAt"`
}
