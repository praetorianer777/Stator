package httpapi

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/via"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// A receiver is generated from the document like any client, so what a
// delivery carries, body and headers, is described there, as Armature does.
func TestTheDocumentDescribesAWebhookDelivery(t *testing.T) {
	doc := Spec()
	delivery := doc.Webhooks["event"]["post"]
	if delivery == nil {
		t.Fatal("the document describes no webhook delivery")
	}
	body, err := doc.Resolve(delivery.RequestBody.Content["application/json"].Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "id", "source", "topic", "occurredAt", "payload"} {
		if !slices.Contains(body.Required, field) {
			t.Errorf("the envelope does not promise %s", field)
		}
	}
	if len(body.Properties["payload"].AnyOf) == 0 {
		t.Error("the envelope does not say what a payload holds")
	}
	headers := map[string]bool{}
	for _, p := range delivery.Parameters {
		if p.In == "header" && p.Required {
			headers[p.Name] = true
		}
	}
	for _, name := range []string{webhook.EventHeader, webhook.DeliveryHeader, webhook.VersionHeader,
		webhook.SignatureHeader, webhook.TimestampHeader, webhook.TimestampedSignatureHeader} {
		if !headers[name] {
			t.Errorf("the delivery does not list the header %s", name)
		}
	}
}

// Every payload the sender builds fits the document; the integration suite
// holds the bodies it actually posts to the same schema.
func TestEveryPayloadFitsTheDocumentedEnvelope(t *testing.T) {
	doc := Spec()
	schema := doc.Webhooks["event"]["post"].RequestBody.Content["application/json"].Schema
	page := webhook.PageRef{ID: uuid.New(), Title: "Plans", Version: 2, URL: "https://wiki.example/s/DOC/p/x", Space: webhook.SpaceRef{Key: "DOC", Name: "Docs"}, Kind: "page"}
	actor := &webhook.Person{ID: uuid.New(), Name: "Ada"}
	// An actor who acted through a connected app says so (#498).
	through := &webhook.Person{ID: uuid.New(), Name: "Ada", Via: &via.Via{Kind: via.KindOAuth, Name: "Rotor"}}
	parent := uuid.New()
	payloads := map[string]any{
		webhook.TopicPing:                 webhook.Ping{Message: "Stator can reach this address."},
		events.TopicPagePublished:         webhook.PagePublished{Page: page, Actor: actor},
		events.TopicPageMoved:             webhook.PageMoved{Page: page, From: webhook.Place{Space: &page.Space, ParentID: &parent}, To: webhook.Place{}, Actor: nil},
		events.TopicPageDeleted:           webhook.PageDeleted{Page: page, Actor: actor},
		events.TopicPageRemoteLinkRemoved: webhook.RemoteLinkRemoved{Page: page, RemoteLink: webhook.RemoteLinkRef{ID: uuid.New(), URL: "https://exciter.example/r/x", Title: "REQ-1 Sign in (specifies)", Source: "Exciter"}, Actor: actor},
		events.TopicCommentCreated:        webhook.CommentCreated{Comment: webhook.CommentRef{ID: uuid.New(), ThreadID: uuid.New(), Text: "Ship it", URL: page.URL + "?thread=x"}, Page: page, Actor: through},
	}
	for _, topic := range append([]string{webhook.TopicPing}, webhook.Topics...) {
		payload, ok := payloads[topic]
		if !ok {
			t.Errorf("no payload of %s is checked against the document", topic)
			continue
		}
		raw, err := json.Marshal(webhook.EnvelopeV1{Version: webhook.CurrentVersion, ID: uuid.New(), Source: webhook.Source, Topic: topic, OccurredAt: time.Now().UTC(), Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.ValidateJSON(schema, raw); err != nil {
			t.Errorf("%s does not fit the document: %v", topic, err)
		}
	}
	if err := doc.ValidateJSON(schema, []byte(`{"version":1,"id":"x","source":"elsewhere","topic":"ping","occurredAt":"2026-10-15T09:30:00Z","payload":{"message":"hi"}}`)); err == nil {
		t.Error("an envelope from another source fits the document")
	}
	if err := doc.ValidateJSON(schema, []byte(`{"id":"x","source":"stator","topic":"ping","occurredAt":"2026-10-15T09:30:00Z","payload":{"message":"hi"}}`)); err == nil {
		t.Error("an envelope without a version fits the document")
	}
	if err := doc.ValidateJSON(schema, []byte(`{"version":1,"id":"x","source":"stator","topic":"page.published","occurredAt":"2026-10-15T09:30:00Z","payload":{"page":{}}}`)); err == nil {
		t.Error("a payload that is no topic's fits the document")
	}
}
