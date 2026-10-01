// Package armature reaches the Armature instance an organization connects, as
// the person asking with their own token. See docs/api-contract-m3.md.
package armature

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	// KeyPattern is an issue key as Stator stores it: the project key, a hyphen,
	// then the number, upper case although Armature reads lower case too.
	KeyPattern = `^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$`
	// TokenPrefix starts every Armature personal access token.
	TokenPrefix = "armature_pat_"
	// WebhookSecretPrefix starts every secret Armature shows for a webhook.
	WebhookSecretPrefix = "armature_whs_"
	// SignatureHeader carries sha256= and the hex HMAC of the raw body.
	SignatureHeader = "X-Armature-Signature-256"
	// LinkSource is what a remote link Stator puts on an issue names as its
	// application.
	LinkSource = "Stator"

	// MaxLookupKeys bounds the keys one lookup asks for.
	MaxLookupKeys = 50
	// DefaultListLimit and MaxListLimit bound the rows of an issue list block
	// and of one search.
	DefaultListLimit = 20
	MaxListLimit     = 100
	// MaxQueryLength bounds an NQL query a list block stores.
	MaxQueryLength = 2000
	// MaxColumns bounds the columns a list block shows.
	MaxColumns = 10
	// MaxCreateItems bounds the issues one create makes from a selection.
	MaxCreateItems = 50
	// MaxSummaryLength is Armature's own bound on a summary.
	MaxSummaryLength = 255

	// IssueCacheTTL and SearchCacheTTL are how long an answer is served to
	// the same person without asking Armature again; webhooks end it sooner.
	IssueCacheTTL  = 60 * time.Second
	SearchCacheTTL = 60 * time.Second
	// MetaCacheTTL covers projects and issue types, which no webhook announces.
	MetaCacheTTL = 5 * time.Minute
	// ThemeCacheTTL covers a followed Armature theme.
	ThemeCacheTTL = 5 * time.Minute
	// CallTimeout bounds one call to Armature from a request.
	CallTimeout = 5 * time.Second
	// ThemeTimeout is shorter, because the theme is read on every page load.
	ThemeTimeout = 2 * time.Second
	// MaxResponseBytes bounds how much of one Armature answer is read.
	MaxResponseBytes = 4 << 20
	// WebhookMaxBytes bounds a delivery's body.
	WebhookMaxBytes = 1 << 20
	// WebhookReplayWindow is how long a delivered event id is remembered, so
	// a replay of it changes nothing.
	WebhookReplayWindow = 24 * time.Hour
)

// Document nodes that hold Armature issues. #28, #29 and #30 add them to the
// allowlist in internal/document.
const (
	// NodeIssue is the inline smart link; its one attribute is key.
	NodeIssue = "armatureIssue"
	// NodeIssueBlock embeds one issue with its details; its one attribute is key.
	NodeIssueBlock = "armatureIssueBlock"
	// NodeIssueList is a table from an NQL query, with query, columns and limit.
	NodeIssueList = "armatureIssueList"
)

// Status says whether an answer came from Armature, and if not, why.
type Status string

const (
	StatusOK Status = "ok"
	// StatusNotConfigured is an organization with no Armature connection.
	StatusNotConfigured Status = "not_configured"
	// StatusNotConnected is a person who stored no token.
	StatusNotConnected Status = "not_connected"
	// StatusRejected is a stored token Armature no longer accepts.
	StatusRejected Status = "rejected"
	// StatusUnreachable is Armature not answering, or answering an error.
	StatusUnreachable Status = "unreachable"
)

// Statuses lists every Status, for the API document.
var Statuses = []Status{StatusOK, StatusNotConfigured, StatusNotConnected, StatusRejected, StatusUnreachable}

// Column is a column an issue list block may show.
type Column string

const (
	ColumnKey      Column = "key"
	ColumnSummary  Column = "summary"
	ColumnType     Column = "type"
	ColumnStatus   Column = "status"
	ColumnPriority Column = "priority"
	ColumnAssignee Column = "assignee"
	ColumnReporter Column = "reporter"
	ColumnCreated  Column = "created"
	ColumnUpdated  Column = "updated"
	ColumnDue      Column = "due"
)

// Columns lists every Column, in the order the insert dialog offers them.
var Columns = []Column{ColumnKey, ColumnSummary, ColumnType, ColumnStatus, ColumnPriority, ColumnAssignee, ColumnReporter, ColumnCreated, ColumnUpdated, ColumnDue}

// DefaultColumns are what a new list block shows.
var DefaultColumns = []Column{ColumnKey, ColumnSummary, ColumnStatus, ColumnAssignee}

// Priorities are Armature's, lowest first.
var Priorities = []string{"lowest", "low", "medium", "high", "highest"}

// StatusCategories are Armature's, in workflow order.
var StatusCategories = []string{"todo", "in_progress", "done"}

// WebhookTopics are the Armature topics the receiver acts on; the admin
// screen tells an administrator to subscribe to these.
var WebhookTopics = []string{"issue.created", "issue.updated", "issue.transitioned", "comment.added"}

// Connection is the organization's Armature instance, for its administrators.
type Connection struct {
	// BaseURL is the origin people open Armature at, without /api/v1.
	BaseURL string `json:"baseUrl"`
	// OrgSlug is the Armature organization every stored token must belong to.
	OrgSlug string `json:"orgSlug"`
	// ArmatureOrgID is learned from the first token that checks out; a
	// webhook naming another organization is refused.
	ArmatureOrgID *uuid.UUID `json:"armatureOrgId"`
	// WebhookSecretSet says a secret is stored; the secret is never answered.
	WebhookSecretSet bool `json:"webhookSecretSet"`
	// WebhookURL is the address to enter in Armature's webhook settings.
	WebhookURL string `json:"webhookUrl"`
	// WebhookTopics are the topics to subscribe that address to.
	WebhookTopics []string `json:"webhookTopics"`
	// Connected counts the members who stored a token.
	Connected int       `json:"connected"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConnectionInput sets the instance. Changing baseUrl or orgSlug forgets
// every stored token, so none is ever sent to another host.
type ConnectionInput struct {
	BaseURL string `json:"baseUrl"`
	OrgSlug string `json:"orgSlug"`
	// WebhookSecret is the secret Armature showed; absent keeps the stored
	// one, empty removes it.
	WebhookSecret *string `json:"webhookSecret,omitempty"`
}

// Account is the caller's own link to Armature.
type Account struct {
	// Configured says the organization has an Armature connection.
	Configured bool `json:"configured"`
	// BaseURL is where issues open, null until configured.
	BaseURL *string `json:"baseUrl"`
	// Connected says the caller stored a token.
	Connected bool   `json:"connected"`
	Status    Status `json:"status"`
	// User is who the token acts as in Armature, as the last check found.
	User      *AccountUser `json:"user"`
	CheckedAt *time.Time   `json:"checkedAt"`
}

// AccountUser is a person in Armature.
type AccountUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// TokenInput is a personal access token made in Armature. It is sealed with
// STATOR_SECRET_KEY and never answered again.
type TokenInput struct {
	Token string `json:"token"`
}

// Issue is an Armature issue as a chip, a block and a list row draw it.
type Issue struct {
	Key string `json:"key"`
	// URL opens the issue in Armature.
	URL        string       `json:"url"`
	ProjectKey string       `json:"projectKey"`
	Summary    string       `json:"summary"`
	Type       IssueType    `json:"type"`
	Status     IssueStatus  `json:"status"`
	Priority   string       `json:"priority"`
	Assignee   *IssuePerson `json:"assignee"`
	Reporter   *IssuePerson `json:"reporter"`
	DueDate    *time.Time   `json:"dueDate"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
}

// IssueType is an Armature issue type; Icon is Armature's icon name.
type IssueType struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon"`
	Level int       `json:"level"`
}

// IssueStatus is where an issue stands in its workflow.
type IssueStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// IssuePerson is an assignee or a reporter.
type IssuePerson struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// IssueResult is one key of a lookup; Issue is null when Armature has no
// issue by that key that the caller may see.
type IssueResult struct {
	Key   string `json:"key"`
	Issue *Issue `json:"issue"`
}

// Project is an Armature project the caller may see.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// CanCreate says the caller may file issues in it.
	CanCreate bool `json:"canCreate"`
}

// CreateIssuesInput files one issue per item, in order, from a selection on
// a page the caller is editing.
type CreateIssuesInput struct {
	PageID     uuid.UUID `json:"pageId"`
	ProjectKey string    `json:"projectKey"`
	// TypeID is null for Armature's default type.
	TypeID *uuid.UUID   `json:"typeId,omitempty"`
	Items  []CreateItem `json:"items"`
}

// CreateItem is one issue to file.
type CreateItem struct {
	Summary string `json:"summary"`
}

// CreateFailure is the item Armature refused; the ones after it were not tried.
type CreateFailure struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LinkState is whether a page's link on an issue has reached Armature.
type LinkState string

const (
	LinkSynced  LinkState = "synced"
	LinkPending LinkState = "pending"
	// LinkFailed is a link Armature refused; the next publish tries again.
	LinkFailed LinkState = "failed"
)

// LinkStates lists every LinkState, for the API document.
var LinkStates = []LinkState{LinkSynced, LinkPending, LinkFailed}

// Link is one issue a page names, and how its remote link stands.
type Link struct {
	Key      string     `json:"key"`
	State    LinkState  `json:"state"`
	Error    *string    `json:"error"`
	SyncedAt *time.Time `json:"syncedAt"`
}

// ThemeFollow says whether the caller follows their Armature theme, and
// whether Armature answered the last time it was asked.
type ThemeFollow struct {
	Following bool   `json:"following"`
	Status    Status `json:"status"`
	// Error says why Armature's theme, though it answered, cannot be used.
	Error *string `json:"error"`
}

// WebhookEnvelope is what Armature posts, signed over the raw body.
type WebhookEnvelope struct {
	// ID is the event's, the same on every retry and redelivery.
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OrgID      uuid.UUID       `json:"orgId"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}
