package httpapi

import (
	"slices"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/events"
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
