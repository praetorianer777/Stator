// Package armature reaches the Armature instance the site connects, as the
// person asking with their own token. See docs/api-contract-m3.md.
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
	// TimestampHeader carries when Armature sent the attempt, in Unix
	// seconds, and TimestampedSignatureHeader the HMAC of that time, a dot
	// and the body. An Armature from before #242 sends neither.
	TimestampHeader            = "X-Armature-Timestamp"
	TimestampedSignatureHeader = "X-Armature-Signature-Timestamped-256"
	// WebhookVersionHeader names the body's envelope version, so a delivery in
	// one the receiver does not read is refused before it is parsed.
	WebhookVersionHeader = "X-Armature-Webhook-Version"
	// WebhookTimestampTolerance is how far a delivery's signed time may be
	// from this server's clock, either way, before it is refused as a replay;
	// Armature signs each attempt afresh, so a genuine retry is never stale.
	WebhookTimestampTolerance = 5 * time.Minute
	// WebhookReceiptTTL is how long the last delivery taken is remembered for
	// the settings page.
	WebhookReceiptTTL = 30 * 24 * time.Hour
	// WebhookVersion is the one envelope version the receiver reads. An
	// endpoint registered in Armature without a version gets the current one,
	// so a newer Armature may send another, which is refused rather than misread.
	WebhookVersion = 1
	// LinkSource is what a remote link Stator puts on an issue names as its
	// application.
	LinkSource = "Stator"

	// MaxLookupKeys bounds the keys one lookup asks for.
	MaxLookupKeys = 50
	// DefaultListLimit and MaxListLimit bound the rows of an issue list block
	// and of one search.
	DefaultListLimit = 20
	MaxListLimit     = 100
	// ProjectPageSize is the most projects Armature answers at once, and
	// MaxProjectPages bounds the pages one listing reads.
	ProjectPageSize = 200
	MaxProjectPages = 10
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
	// NodeChart draws a count of an NQL query's issues, with project, query,
	// chart, groupBy and days.
	NodeChart = "armatureChart"
	// NodeRoadmap draws an NQL query's issues on a timeline, with project,
	// query and groupBy.
	NodeRoadmap = "armatureRoadmap"
)

// Status says whether an answer came from Armature, and if not, why.
type Status string

const (
	StatusOK Status = "ok"
	// StatusNotConfigured is a site with no Armature connection.
	StatusNotConfigured Status = "not_configured"
	// StatusNotConnected is a person who stored no token.
	StatusNotConnected Status = "not_connected"
	// StatusRejected is a stored token Armature no longer accepts.
	StatusRejected Status = "rejected"
	// StatusUnreachable is Armature not answering, or answering an error.
	StatusUnreachable Status = "unreachable"
	// StatusOutdated is an Armature that does not report the capability the
	// read needs; Account lists which.
	StatusOutdated Status = "outdated"
)

// Statuses lists every Status, for the API document.
var Statuses = []Status{StatusOK, StatusNotConfigured, StatusNotConnected, StatusRejected, StatusUnreachable, StatusOutdated}

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

// Connection is the site's Armature instance, for its administrators.
type Connection struct {
	// BaseURL is the origin people open Armature at, without /api/v1.
	BaseURL string `json:"baseUrl"`
	// InstallationID is the Armature installation, learned from the first
	// token that checks out; a token or webhook of another one is refused.
	InstallationID *uuid.UUID `json:"installationId"`
	// WebhookSecretSet says a secret is stored; the secret is never answered.
	WebhookSecretSet bool `json:"webhookSecretSet"`
	// WebhookURL is the address to enter in Armature's webhook settings.
	WebhookURL string `json:"webhookUrl"`
	// WebhookTopics are the topics to subscribe that address to.
	WebhookTopics []string `json:"webhookTopics"`
	// LastDelivery is the last webhook delivery Stator took, null when none
	// was taken in WebhookReceiptTTL or there is no Valkey to remember it.
	LastDelivery *WebhookReceipt `json:"lastDelivery"`
	// Connected counts the members who stored a token.
	Connected int       `json:"connected"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Partner is what Armature said of itself at /meta, asked afresh.
	Partner Partner `json:"partner"`
	// App is Stator as a connected app of this Armature.
	App ConnectionApp `json:"app"`
	// Webhook is the webhook Stator makes in Armature itself, where
	// Armature lets it, instead of a pasted secret.
	Webhook PartnerWebhook `json:"partnerWebhook"`
}

// AppStatus is where Stator stands as a connected app of Armature.
type AppStatus string

const (
	// AppNone is no app yet: the first member to connect registers it.
	AppNone AppStatus = "none"
	// AppPending is registered by Stator and not yet allowed by an Armature
	// administrator, as far as Stator knows: no grant was ever redeemed.
	AppPending AppStatus = "pending"
	AppActive  AppStatus = "active"
	// AppRefused is an Armature that would not register Stator; Refusal
	// holds its sentence, and an administrator pastes the app instead.
	AppRefused AppStatus = "refused"
)

// AppStatuses lists every AppStatus, for the API document.
var AppStatuses = []AppStatus{AppNone, AppPending, AppActive, AppRefused}

// AppRegistrar says who registered the app in Armature.
type AppRegistrar string

const (
	RegisteredByStator AppRegistrar = "stator"
	RegisteredByAdmin  AppRegistrar = "admin"
)

// AppRegistrars lists every AppRegistrar, for the API document.
var AppRegistrars = []AppRegistrar{RegisteredByStator, RegisteredByAdmin}

// ConnectionApp is Stator as a connected app, for administrators. The client
// secret is never answered.
type ConnectionApp struct {
	// Offered says Armature reports oauth.partner-tokens@1, so members see
	// Connect Armature instead of the token form.
	Offered      bool          `json:"offered"`
	Status       AppStatus     `json:"status"`
	ClientID     *string       `json:"clientId"`
	RegisteredBy *AppRegistrar `json:"registeredBy"`
	// Refusal is Armature's sentence when it would not register Stator.
	Refusal *string `json:"refusal"`
	// RedirectURI is the return address to register by hand.
	RedirectURI string `json:"redirectUri"`
	// Scopes are what Stator asks of Armature for each person.
	Scopes []string `json:"scopes"`
}

// AppInput is an app an Armature administrator registered for Stator by
// hand. A new client id forgets every grant of the old one.
type AppInput struct {
	ClientID string `json:"clientId"`
	// ClientSecret is empty for an app registered as public.
	ClientSecret string `json:"clientSecret"`
}

// AccountVia says how the caller's account is connected.
type AccountVia string

const (
	// ViaApp is a grant from Armature's consent page.
	ViaApp AccountVia = "app"
	// ViaToken is a pasted personal access token.
	ViaToken AccountVia = "token"
)

// AccountVias lists every AccountVia, for the API document.
var AccountVias = []AccountVia{ViaApp, ViaToken}

// AccountApp is what the caller needs to know of the connected app.
type AccountApp struct {
	// Offered says Connect Armature may be offered: Armature reports
	// oauth.partner-tokens@1 and has not refused Stator.
	Offered bool `json:"offered"`
	// Pending says an Armature administrator has to allow Stator first.
	Pending bool `json:"pending"`
	// Refusal is Armature's sentence when it would not register Stator.
	Refusal *string `json:"refusal"`
	// Scopes are what the caller's grant holds, empty without one.
	Scopes []string `json:"scopes"`
}

// ConnectionInput sets the instance. Changing baseUrl forgets every stored
// token, so none is ever sent to another host.
type ConnectionInput struct {
	BaseURL string `json:"baseUrl"`
	// WebhookSecret is the secret Armature showed; absent keeps the stored
	// one, empty removes it.
	WebhookSecret *string `json:"webhookSecret,omitempty"`
}

// Account is the caller's own link to Armature.
type Account struct {
	// Configured says the site has an Armature connection.
	Configured bool `json:"configured"`
	// BaseURL is where issues open, null until configured.
	BaseURL *string `json:"baseUrl"`
	// Connected says the caller stored a token or connected Stator's app.
	Connected bool `json:"connected"`
	// Via is how, null while not connected.
	Via    *AccountVia `json:"via"`
	Status Status      `json:"status"`
	// App is what the caller may do with the connected app.
	App AccountApp `json:"app"`
	// User is who the token acts as in Armature, as the last check found.
	User      *AccountUser `json:"user"`
	CheckedAt *time.Time   `json:"checkedAt"`
	// Missing are the features the connected Armature does not offer, which
	// pages show a sentence for instead of asking it.
	Missing []MissingFeature `json:"missing"`
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

// WebhookReceipt is what the receiver last took from Armature, so an
// administrator sees that the webhook works and in which shape it arrives.
type WebhookReceipt struct {
	ReceivedAt time.Time `json:"receivedAt"`
	Topic      string    `json:"topic"`
	// Version is the envelope version the body named, null when it named
	// none, as an Armature from before #242 sends it.
	Version *int `json:"version"`
	// Timestamped says the delivery carried a timestamped signature, which
	// was checked against this server's clock.
	Timestamped bool `json:"timestamped"`
}

// WebhookHeaders are the headers a delivery is signed and described in.
type WebhookHeaders struct {
	Signature, Timestamp, TimestampedSignature, Version string
}

// WebhookEnvelope is what Armature posts, signed over the raw body.
type WebhookEnvelope struct {
	// Version is the envelope's format; an Armature from before it was
	// numbered leaves it out, which reads as 0 and means 1.
	Version int `json:"version"`
	// ID is the event's, the same on every retry and redelivery.
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OrgID      uuid.UUID       `json:"orgId"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}
