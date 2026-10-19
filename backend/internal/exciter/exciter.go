// Package exciter reaches the Exciter installation the site connects, as the
// person asking with their own token, the way package armature reaches
// Armature. See docs/integration.md and Exciter's docs/api-contract-backlog.md,
// section #43.
package exciter

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

const (
	// KeyPattern is Exciter's requirement.KeyPattern: the project key, -R, then
	// the number.
	KeyPattern = `^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`
	// MaxKeyLength bounds a key Stator stores or asks for: a ten character
	// project key, -R and eighteen digits, more than Exciter ever numbers.
	MaxKeyLength = 30
	// TokenPrefix starts every Exciter personal access token.
	TokenPrefix = "exciter_pat_"
	// WebhookSecretPrefix starts every secret Exciter shows for a webhook.
	WebhookSecretPrefix = "exciter_whs_"
	// SignatureHeader carries sha256= and the hex HMAC of the raw body;
	// TimestampHeader when Exciter sent the attempt, in Unix seconds; and
	// TimestampedSignatureHeader the HMAC of that time, a dot and the body.
	SignatureHeader            = "X-Exciter-Signature-256"
	TimestampHeader            = "X-Exciter-Timestamp"
	TimestampedSignatureHeader = "X-Exciter-Signature-Timestamped-256"
	// WebhookVersionHeader names the body's envelope version.
	WebhookVersionHeader = "X-Exciter-Webhook-Version"
	// WebhookVersion is the one envelope version the receiver reads.
	WebhookVersion = 1
	// WebhookSource is what every envelope Exciter posts names as its sender.
	WebhookSource = "exciter"
	// MetaProduct is how an Exciter names itself at /meta.
	MetaProduct = "exciter"

	// MaxLookupKeys bounds the keys one lookup asks for.
	MaxLookupKeys = 50
	// LookupParallel bounds the reads one lookup makes at once.
	LookupParallel = 8
	// RequirementCacheTTL is how long an answer is served to the same person
	// without asking Exciter again; webhooks end it sooner.
	RequirementCacheTTL = 60 * time.Second
	// CachePrefix starts every key the cache writes.
	CachePrefix = "stator:exciter:"
)

// The nodes a page names Exciter requirements with; internal/document
// allows them, and a page stores neither a requirement nor a row.
const (
	// NodeRequirement is the inline chip of a requirement; its one attribute is key.
	NodeRequirement = "exciterRequirement"
	// NodeRequirementBlock shows one requirement with its statement and
	// rationale; its one attribute is key.
	NodeRequirementBlock = "exciterRequirementBlock"
	// NodeRequirementList is a table of what a query finds in one project,
	// with project, query and limit.
	NodeRequirementList = "exciterRequirementList"
)

const (
	// ProjectKeyPattern is the project part of KeyPattern.
	ProjectKeyPattern = `^[A-Z][A-Z0-9]{1,9}$`
	// MaxQueryLength is the longest query Exciter's language reads.
	MaxQueryLength = 2000
	// DefaultListLimit is the rows a list block shows at first, and
	// MaxListLimit the most Exciter answers in one window.
	DefaultListLimit = 20
	MaxListLimit     = 500
	// SearchLimit is the most requirements the picker offers, and
	// MaxSearchLength bounds what it looks for.
	SearchLimit     = 10
	MaxSearchLength = 200
	// SuggestLimit is the most words and requirements one completion offers,
	// as many as Exciter answers.
	SuggestLimit = 20
)

// Status says whether an answer came from Exciter, and if not, why; the
// words are Armature's, so a client reads both alike.
type Status = armature.Status

const (
	StatusOK            = armature.StatusOK
	StatusNotConfigured = armature.StatusNotConfigured
	StatusNotConnected  = armature.StatusNotConnected
	StatusRejected      = armature.StatusRejected
	StatusUnreachable   = armature.StatusUnreachable
	StatusOutdated      = armature.StatusOutdated
)

// WebhookTopics are the Exciter topics the receiver acts on; the admin
// screen tells an administrator to subscribe to these.
var WebhookTopics = []string{
	"requirement.created", "requirement.revised", "requirement.changed", "requirement.trashed",
	"requirement.restored", "requirement.moved", "review.decided",
}

// Connection is the site's Exciter installation, for its administrators.
type Connection struct {
	// BaseURL is the origin people open Exciter at, without /api/v1.
	BaseURL string `json:"baseUrl"`
	// WebhookSecretSet says a secret is stored; the secret is never answered.
	WebhookSecretSet bool `json:"webhookSecretSet"`
	// WebhookURL is the address to enter in Exciter's webhook settings.
	WebhookURL string `json:"webhookUrl"`
	// WebhookTopics are the topics to subscribe that address to.
	WebhookTopics []string `json:"webhookTopics"`
	// LastDelivery is the last webhook delivery Stator took, null when none
	// was taken lately or there is no Valkey to remember it.
	LastDelivery *armature.WebhookReceipt `json:"lastDelivery"`
	// Connected counts the members who stored a token.
	Connected int       `json:"connected"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Partner is what Exciter said of itself at /meta, asked afresh.
	Partner Partner `json:"partner"`
	// App is Stator as a partner app of Exciter, which connects people at
	// sign-in by token exchange (#462).
	App ConnectionApp `json:"app"`
}

// ConnectionApp is the partner app an Exciter administrator registered for
// Stator, as a Stator administrator stored it; the secret is never answered.
type ConnectionApp struct {
	// ClientID is null until one is stored.
	ClientID  *string `json:"clientId"`
	SecretSet bool    `json:"secretSet"`
	// Offered says Exciter reports oauth.token-exchange@1.
	Offered bool `json:"offered"`
	// Exchanging says people are connected at sign-in: the operator turned
	// it on, Exciter offers it and the app is stored.
	Exchanging bool `json:"exchanging"`
}

// AppInput is the partner app Exciter's administrator registered for
// Stator with Stator's Keycloak client: its client id and secret.
type AppInput struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// AccountVia says how the caller's account is connected.
type AccountVia string

const (
	// ViaToken is a pasted personal access token.
	ViaToken AccountVia = "token"
	// ViaSignIn is a grant Exciter answered for the caller's sign-in.
	ViaSignIn AccountVia = "sign_in"
)

// AccountVias lists every AccountVia, for the API document.
var AccountVias = []AccountVia{ViaToken, ViaSignIn}

// ConnectionInput sets the installation. Changing baseUrl forgets every
// stored token, so none is ever sent to another host.
type ConnectionInput struct {
	BaseURL string `json:"baseUrl"`
	// WebhookSecret is the secret Exciter showed; absent keeps the stored
	// one, empty removes it.
	WebhookSecret *string `json:"webhookSecret,omitempty"`
}

// ConnectionCheckInput is an address the administrator is about to save.
type ConnectionCheckInput struct {
	BaseURL string `json:"baseUrl"`
}

// ConnectionCheck is what answers at an address, before it is saved.
type ConnectionCheck struct {
	// BaseURL is the address as it would be stored.
	BaseURL string `json:"baseUrl"`
	// Partner is what the Exciter there said of itself at /meta.
	Partner Partner `json:"partner"`
}

// Account is the caller's own link to Exciter.
type Account struct {
	// Configured says the site has an Exciter connection.
	Configured bool `json:"configured"`
	// BaseURL is where requirements open, null until configured.
	BaseURL *string `json:"baseUrl"`
	// Connected says the caller stored a token or was connected at sign-in.
	Connected bool `json:"connected"`
	// Via is how, null while not connected.
	Via    *AccountVia `json:"via"`
	Status Status      `json:"status"`
	// SignInRefusal says why Exciter did not connect the caller when they
	// last signed in through the shared identity provider, null otherwise.
	SignInRefusal *string `json:"signInRefusal"`
	// User is who the token acts as in Exciter, as the last check found.
	User      *armature.AccountUser `json:"user"`
	CheckedAt *time.Time            `json:"checkedAt"`
	// Missing are the features the connected Exciter does not offer.
	Missing []MissingFeature `json:"missing"`
}

// TokenInput is a personal access token made in Exciter. It is sealed with
// STATOR_SECRET_KEY and never answered again.
type TokenInput struct {
	Token string `json:"token"`
}

// Requirement is an Exciter requirement as a chip draws it.
type Requirement struct {
	ID uuid.UUID `json:"id"`
	// Key is the requirement's current key, which may differ from the one a
	// page names it by after a move.
	Key string `json:"key"`
	// URL opens the requirement in Exciter.
	URL        string             `json:"url"`
	ProjectKey string             `json:"projectKey"`
	Title      string             `json:"title"`
	Status     *RequirementStatus `json:"status"`
	Revision   int                `json:"revision"`
	// TrashedAt is set for a requirement in Exciter's trash that the reader
	// may still find there.
	TrashedAt *time.Time `json:"trashedAt"`
}

// RequirementDetail is a requirement as a requirement block draws it: the
// chip's fields with its statement and rationale.
type RequirementDetail struct {
	Requirement
	// Statement and Rationale are ProseMirror documents in Exciter's schema,
	// null when the requirement has none; a block renders the nodes it
	// knows and the rest as text.
	Statement json.RawMessage `json:"statement"`
	Rationale json.RawMessage `json:"rationale"`
}

// RequirementStatus is where a requirement stands in its workflow.
type RequirementStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// StatusCategories are Exciter's, in workflow order.
var StatusCategories = []string{"draft", "review", "approved", "done", "closed"}

// RequirementResult is one key of a lookup; Requirement is null when Exciter
// has none by that key the caller may see.
type RequirementResult struct {
	Key         string       `json:"key"`
	Requirement *Requirement `json:"requirement"`
}

// WebhookHeaders are the headers a delivery is signed and described in.
type WebhookHeaders struct {
	Signature, Timestamp, TimestampedSignature, Version string
}

// WebhookEnvelope is what Exciter posts in version 1, signed over the raw body.
type WebhookEnvelope struct {
	Version int `json:"version"`
	// ID is the event's, the same on every retry and redelivery.
	ID         uuid.UUID       `json:"id"`
	Source     string          `json:"source"`
	Topic      string          `json:"topic"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}
