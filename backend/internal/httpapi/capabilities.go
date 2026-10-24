package httpapi

import (
	"slices"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/oauth"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// capability is one integration feature a partner checks for by name at
// /meta, with the operations and topics it rests on; docs/integration.md
// lists them.
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
	// topics are what Stator emits for it, from events.Topics.
	topics []string
	// receives are Armature's topics it acts on, from armature.WebhookTopics.
	receives []string
}

// integrationCapabilities may only use names from Dynamo's shared list, in
// Armature's name@version scheme.
var integrationCapabilities = []capability{
	{
		name:       "page-links@1",
		promise:    "Each Armature issue a published page names as a chip or an issue block carries the page's remote link, put on and taken off as the page changes, and the page says how each stands.",
		operations: []string{"GET /pages/{pageID}/armature-links"},
		topics:     []string{events.TopicArmatureLinks},
	},
	{
		name:    "page-remote-links@1",
		promise: "Another tool puts links to its objects on a page as somebody who may edit it, a link with the same url replacing the one there, and hears through webhooks when somebody takes one off.",
		operations: []string{
			"GET /pages/{pageID}/remote-links",
			"POST /pages/{pageID}/remote-links",
			"DELETE /pages/{pageID}/remote-links/{linkID}",
		},
		topics: []string{events.TopicPageRemoteLinkRemoved},
	},
	{
		name:       "webhook-receiver@1",
		promise:    "Armature's signed webhook deliveries, envelope version 1, received for the issue topics, so pages show a change to an issue at once.",
		operations: []string{"POST /armature/webhook"},
		receives:   armature.WebhookTopics,
	},
	{
		name:    "webhooks@1",
		promise: "Endpoints subscribed to topics, their deliveries, a redelivery, a test delivery and a new secret, each delivery a signed envelope.",
		operations: []string{
			"GET /webhooks",
			"POST /webhooks",
			"PATCH /webhooks/{webhookID}",
			"DELETE /webhooks/{webhookID}",
			"GET /webhooks/{webhookID}/deliveries",
			"POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver",
			"POST /webhooks/{webhookID}/test",
			"POST /webhooks/{webhookID}/rotate-secret",
		},
		topics: webhook.Topics,
	},
	{
		name: oauth.CapabilityTokenExchange,
		promise: "POST /oauth/token with grant_type " + oauth.GrantTokenExchange + ": subject_token is an access token of the shared Keycloak realm whose azp is the partner app's Keycloak client id and whose aud holds Stator's client; " +
			"the app authenticates with its own credentials; the answer is an access and a refresh token within the person's rights, with no consent page.",
		operations: []string{"GET " + oauth.MetadataPath, "POST /oauth/token", "POST /oauth/revoke"},
	},
	{
		name: oauth.CapabilityPartnerTokens,
		promise: "An app registers itself at POST /oauth/register (RFC 7591) and is pending until a Stator administrator allows it by connecting it once; " +
			"a person connects it by authorization code with PKCE S256 on Stator's consent page, choosing read or write and the spaces it reaches; " +
			"the access token lasts at most an hour and works at the MCP endpoint only, within the person's rights; the refresh token rotates at each use and one used twice ends the grant; " +
			"a 401 at the MCP endpoint names the RFC 9728 metadata, and RFC 7009 revokes a grant.",
		operations: []string{"GET " + oauth.MetadataPath, "GET " + oauth.ResourceMetadataPath + APIPrefix + oauth.MCPPath, "POST /oauth/register", "GET /oauth/authorize",
			"GET /oauth/consent", "POST /oauth/consent", "POST /oauth/token", "POST /oauth/revoke", "POST /mcp"},
	},
	{
		name:    "theme-export@1",
		promise: "A theme exported as one armature-theme/1 file with its pictures and fonts, and such a file imported as a theme of the caller's own.",
		operations: []string{
			"GET /themes/{themeID}/export",
			"POST /themes/import",
		},
	},
}

// capabilities is what this build announces beyond its API version; a
// partner checks for a name here rather than comparing versions.
func capabilities() []string {
	names := make([]string, 0, len(integrationCapabilities))
	for _, c := range integrationCapabilities {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}
