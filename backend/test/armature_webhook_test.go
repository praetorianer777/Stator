//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Webhooks (#33): Armature signs a delivery with the organization's secret,
// and the receiver clears what it announces from every person's cache.

// delivery is one signed post to the receiver, as Armature's webhook service
// sends it; the stub's own sender is the Playwright spec's to use.
type delivery struct {
	ID      uuid.UUID
	Topic   string
	OrgID   uuid.UUID
	Payload map[string]any
	// Version is the envelope's; 0 leaves it out, as Armature did before it
	// numbered the envelope.
	Version int
	// Source is a field the receiver does not read; empty leaves it out.
	Source string
}

func (d delivery) body(t *testing.T) []byte {
	t.Helper()
	envelope := map[string]any{"id": d.ID, "topic": d.Topic, "orgId": d.OrgID, "occurredAt": time.Now().UTC(), "payload": d.Payload}
	if d.Version != 0 {
		envelope["version"] = d.Version
	}
	if d.Source != "" {
		envelope["source"] = d.Source
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// postWebhook posts to the receiver's address, or with oldSlug to the one
// it had while it named the organization.
func postWebhook(t *testing.T, api *apiServer, oldSlug string, body []byte, signature string) response {
	t.Helper()
	return postWebhookWith(t, api, oldSlug, body, map[string]string{armature.SignatureHeader: signature})
}

// timestamped are the headers Armature signs a delivery in since #242: the
// plain signature, the time it was sent and the signature over both.
func timestamped(body []byte, secret string, sent time.Time) map[string]string {
	return map[string]string{
		armature.SignatureHeader:            armature.Sign(body, secret),
		armature.TimestampHeader:            strconv.FormatInt(sent.Unix(), 10),
		armature.TimestampedSignatureHeader: armature.SignTimestamped(body, secret, sent),
		armature.WebhookVersionHeader:       strconv.Itoa(armature.WebhookVersion),
	}
}

// postWebhookWith posts with the headers given, an empty one left out.
func postWebhookWith(t *testing.T, api *apiServer, oldSlug string, body []byte, headers map[string]string) response {
	t.Helper()
	address := api.srv.URL + "/api/v1/armature/webhook"
	if oldSlug != "" {
		address += "/" + oldSlug
	}
	req, err := http.NewRequest(http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Armature-Webhook")
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := response{Status: resp.StatusCode}
	out.Raw = readAll(resp)
	if len(out.Raw) > 0 {
		_ = json.Unmarshal(out.Raw, &out.Body)
	}
	return out
}

func readAll(resp *http.Response) []byte {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes()
}

func TestArmatureWebhooksClearTheCachedIssuesTheyAnnounce(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-hooks")
	slug := h.site.Slug
	owner := api.as(t, home.user)

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects")
	h.settle(t)
	var armatureOrg uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT armature_org_id FROM armature_connection`).Scan(&armatureOrg); err != nil {
		t.Fatal(err)
	}
	tokenID := tokenRowOf(t, h, home.user)
	t.Cleanup(func() { stubControl(t, http.MethodDelete, slug, nil) })

	status := func(key string) string {
		t.Helper()
		r := want(t, owner.get(t, "/api/v1/armature/issues/"+key), http.StatusOK, "read "+key)
		issue, _ := r.Body["issue"].(map[string]any)
		st, _ := issue["status"].(map[string]any)
		category, _ := st["category"].(string)
		return category
	}
	send := func(d delivery, secret string) response {
		t.Helper()
		body := d.body(t)
		return postWebhook(t, api, "", body, armature.Sign(body, secret))
	}

	before := status("CP-2")
	stubControl(t, http.MethodPatch, slug+"/issues/CP-2", map[string]any{"statusCategory": "done"})
	if got := status("CP-2"); got != before {
		t.Fatalf("without a webhook the cache answers %s, want the cached %s", got, before)
	}

	t.Run("a signed delivery clears the issue, and the next read asks Armature", func(t *testing.T) {
		d := delivery{ID: uuid.New(), Topic: "issue.transitioned", OrgID: armatureOrg, Payload: map[string]any{"key": "cp-2", "actorId": uuid.New()}}
		r := send(d, webhookSecret)
		if r.Status != http.StatusNoContent || len(r.Raw) != 0 {
			t.Fatalf("a signed delivery is answered %d %s", r.Status, r.Raw)
		}
		if _, cached := cachedIssue(t, h, tokenID, "CP-2"); cached {
			t.Error("the issue is still cached after its webhook")
		}
		if got := status("CP-2"); got != "done" {
			t.Errorf("after the webhook the issue is %s, want done", got)
		}

		// The same event again is acknowledged and changes nothing.
		stubControl(t, http.MethodPatch, slug+"/issues/CP-2", map[string]any{"statusCategory": "in_progress"})
		if r := send(d, webhookSecret); r.Status != http.StatusNoContent {
			t.Errorf("a replay is answered %d", r.Status)
		}
		if got := status("CP-2"); got != "done" {
			t.Errorf("a replay cleared the cache: the issue is %s", got)
		}

		// Receivers set up before the address lost the slug post there still.
		old := delivery{ID: uuid.New(), Topic: "issue.transitioned", OrgID: armatureOrg, Payload: map[string]any{"key": "CP-2"}}.body(t)
		if r := postWebhook(t, api, slug, old, armature.Sign(old, webhookSecret)); r.Status != http.StatusNoContent {
			t.Errorf("a delivery to the old address is answered %d %s", r.Status, r.Raw)
		}
		if got := status("CP-2"); got != "in_progress" {
			t.Errorf("a delivery to the old address left the issue %s, want in_progress", got)
		}
		var remembered time.Duration
		remembered, _ = h.valkey(t).TTL(context.Background(), armature.EventKey(h.site.ID, d.ID)).Result()
		if remembered <= armature.WebhookReplayWindow-time.Minute || remembered > armature.WebhookReplayWindow {
			t.Errorf("the event is remembered for %s, want %s", remembered, armature.WebhookReplayWindow)
		}
	})

	t.Run("each topic clears what the contract says", func(t *testing.T) {
		search := armature.SearchKey(h.site.ID)
		for _, tc := range []struct {
			topic    string
			payload  map[string]any
			cleared  []string
			searches bool
		}{
			{"issue.created", map[string]any{"key": "CP-3"}, []string{"CP-3"}, true},
			{"issue.updated", map[string]any{"key": "CP-5", "movedFrom": "SEC-2"}, []string{"CP-5", "SEC-2"}, true},
			{"comment.added", map[string]any{"key": "CP-4", "commentId": uuid.New()}, []string{"CP-4"}, false},
			{"ping", map[string]any{}, nil, false},
			{"worklog.added", map[string]any{"key": "CP-1"}, nil, false},
		} {
			for _, key := range []string{"CP-1", "CP-3", "CP-4", "CP-5", "SEC-2"} {
				status(key)
			}
			want(t, owner.get(t, "/api/v1/armature/search?"+url.Values{"q": {"project = CP"}}.Encode()), http.StatusOK, "search")
			if r := send(delivery{ID: uuid.New(), Topic: tc.topic, OrgID: armatureOrg, Payload: tc.payload}, webhookSecret); r.Status != http.StatusNoContent {
				t.Fatalf("%s is answered %d %s", tc.topic, r.Status, r.Raw)
			}
			for _, key := range []string{"CP-1", "CP-3", "CP-4", "CP-5", "SEC-2"} {
				_, cached := cachedIssue(t, h, tokenID, key)
				if cleared := contains(tc.cleared, key); cached == cleared {
					t.Errorf("%s: %s cached %v, want cleared %v", tc.topic, key, cached, cleared)
				}
			}
			if n, _ := h.valkey(t).Exists(context.Background(), search).Result(); (n == 0) != tc.searches {
				t.Errorf("%s: the searches are cleared %v, want %v", tc.topic, n == 0, tc.searches)
			}
		}
	})

	t.Run("a delivery that cannot be trusted is refused, with one answer for every reason", func(t *testing.T) {
		status("CP-1")
		good := delivery{ID: uuid.New(), Topic: "issue.updated", OrgID: armatureOrg, Payload: map[string]any{"key": "CP-1"}}
		body := good.body(t)
		var answers []string
		for _, tc := range []struct {
			name, slug, signature string
			body                  []byte
		}{
			{"another secret", "", armature.Sign(body, webhookSecret+"x"), body},
			{"no signature", "", "", body},
			{"a body changed after signing", "", armature.Sign(body, webhookSecret), append(bytes.Clone(body[:len(body)-1]), []byte(`,"x":1}`)...)},
			{"another Armature organization", "", "", nil},
			{"an old address naming another organization", "no-such-org-" + strings.ToLower(uuid.NewString()[:8]), armature.Sign(body, webhookSecret), body},
		} {
			if tc.body == nil {
				foreign := delivery{ID: uuid.New(), Topic: "issue.updated", OrgID: uuid.New(), Payload: map[string]any{"key": "CP-1"}}.body(t)
				tc.body, tc.signature = foreign, armature.Sign(foreign, webhookSecret)
			}
			r := postWebhook(t, api, tc.slug, tc.body, tc.signature)
			if r.Status != http.StatusUnauthorized || errorCode(t, r) != "bad_signature" {
				t.Errorf("%s is answered %d %s", tc.name, r.Status, r.Raw)
			}
			// The request id is each request's own; everything else must match.
			e := obj(t, r, "error")
			delete(e, "requestId")
			same, _ := json.Marshal(e)
			answers = append(answers, string(same))
		}
		for _, each := range answers[1:] {
			if each != answers[0] {
				t.Errorf("the refusals differ, which tells an organization apart:\n%s\n%s", answers[0], each)
			}
		}
		if _, cached := cachedIssue(t, h, tokenID, "CP-1"); !cached {
			t.Error("a refused delivery cleared the cache")
		}
		if n, _ := h.valkey(t).Exists(context.Background(), armature.EventKey(h.site.ID, good.ID)).Result(); n != 0 {
			t.Error("a refused delivery's event was remembered, so the genuine one would be taken for a replay")
		}
	})

	t.Run("an envelope version the receiver does not read is refused, after the signature", func(t *testing.T) {
		status("CP-1")
		current := delivery{ID: uuid.New(), Topic: "issue.updated", OrgID: armatureOrg, Payload: map[string]any{"key": "CP-1"}, Version: armature.WebhookVersion}
		if r := send(current, webhookSecret); r.Status != http.StatusNoContent {
			t.Fatalf("version %d is answered %d %s", armature.WebhookVersion, r.Status, r.Raw)
		}
		status("CP-1")
		next := delivery{ID: uuid.New(), Topic: "issue.updated", Payload: map[string]any{"key": "CP-1"}, Version: armature.WebhookVersion + 1}
		r := send(next, webhookSecret)
		if r.Status != http.StatusUnprocessableEntity || errorCode(t, r) != "unsupported_version" {
			t.Errorf("a newer version is answered %d %s", r.Status, r.Raw)
		}
		if _, cached := cachedIssue(t, h, tokenID, "CP-1"); !cached {
			t.Error("a delivery in an unknown version cleared the cache")
		}
		unsigned := next.body(t)
		if r := postWebhook(t, api, "", unsigned, armature.Sign(unsigned, webhookSecret+"x")); r.Status != http.StatusUnauthorized {
			t.Errorf("an unknown version with a wrong signature is answered %d, which tells the version apart before the secret", r.Status)
		}
	})

	t.Run("a delivery that signs its time is held to it, and the connection shows the last one taken", func(t *testing.T) {
		status("CP-1")
		// source is one of the fields an envelope may gain without a new
		// version, which the receiver ignores.
		d := delivery{ID: uuid.New(), Topic: "issue.updated", OrgID: armatureOrg, Payload: map[string]any{"key": "CP-1"}, Version: armature.WebhookVersion, Source: "armature"}
		body := d.body(t)
		now := time.Now()
		forged := timestamped(body, webhookSecret, now)
		forged[armature.TimestampedSignatureHeader] = armature.Sign(body, webhookSecret)
		halfway := timestamped(body, webhookSecret, now)
		halfway[armature.TimestampedSignatureHeader] = ""
		moved := timestamped(body, webhookSecret, now)
		moved[armature.TimestampHeader] = strconv.FormatInt(now.Unix()+1, 10)
		for _, tc := range []struct {
			name    string
			headers map[string]string
			status  int
			code    string
		}{
			{"a replay later than the window", timestamped(body, webhookSecret, now.Add(-armature.WebhookTimestampTolerance-time.Minute)), http.StatusUnauthorized, "stale_delivery"},
			{"a time ahead of the window", timestamped(body, webhookSecret, now.Add(armature.WebhookTimestampTolerance+time.Minute)), http.StatusUnauthorized, "stale_delivery"},
			{"the plain signature as the timestamped one", forged, http.StatusUnauthorized, "bad_signature"},
			{"a time without its signature", halfway, http.StatusUnauthorized, "bad_signature"},
			{"a time changed after signing", moved, http.StatusUnauthorized, "bad_signature"},
		} {
			r := postWebhookWith(t, api, "", body, tc.headers)
			if r.Status != tc.status || errorCode(t, r) != tc.code {
				t.Errorf("%s is answered %d %s, want %d %s", tc.name, r.Status, r.Raw, tc.status, tc.code)
			}
		}
		if _, cached := cachedIssue(t, h, tokenID, "CP-1"); !cached {
			t.Error("a refused timestamped delivery cleared the cache")
		}
		if n, _ := h.valkey(t).Exists(context.Background(), armature.EventKey(h.site.ID, d.ID)).Result(); n != 0 {
			t.Error("a refused timestamped delivery's event was remembered")
		}

		// The version header is read before the body, after the signature.
		next := timestamped(body, webhookSecret, now)
		next[armature.WebhookVersionHeader] = strconv.Itoa(armature.WebhookVersion + 1)
		if r := postWebhookWith(t, api, "", body, next); r.Status != http.StatusUnprocessableEntity || errorCode(t, r) != "unsupported_version" {
			t.Errorf("a newer version header is answered %d %s", r.Status, r.Raw)
		}

		if r := postWebhookWith(t, api, "", body, timestamped(body, webhookSecret, now)); r.Status != http.StatusNoContent {
			t.Fatalf("a delivery signed with its time is answered %d %s", r.Status, r.Raw)
		}
		if _, cached := cachedIssue(t, h, tokenID, "CP-1"); cached {
			t.Error("the issue is still cached after a timestamped delivery")
		}
		last := func() map[string]any {
			t.Helper()
			c := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "read the connection"), "connection")
			got, _ := c["lastDelivery"].(map[string]any)
			return got
		}
		if got := last(); got == nil || got["topic"] != "issue.updated" || number(got["version"]) != armature.WebhookVersion || got["timestamped"] != true {
			t.Errorf("the last delivery reads %v, want version %d signed with its time", got, armature.WebhookVersion)
		}

		// An Armature from before #242 signs no time and names no version.
		if r := send(delivery{ID: uuid.New(), Topic: "ping", OrgID: armatureOrg, Payload: map[string]any{}}, webhookSecret); r.Status != http.StatusNoContent {
			t.Fatalf("a delivery without a time is answered %d %s", r.Status, r.Raw)
		}
		if got := last(); got == nil || got["topic"] != "ping" || got["version"] != nil || got["timestamped"] != false {
			t.Errorf("the last delivery reads %v, want one without a version or a time", got)
		}
	})

	t.Run("a body over the limit is refused before it is read whole", func(t *testing.T) {
		huge := bytes.Repeat([]byte("x"), armature.WebhookMaxBytes+1)
		r := postWebhook(t, api, "", huge, armature.Sign(huge, webhookSecret))
		if r.Status != http.StatusRequestEntityTooLarge || errorCode(t, r) != "too_large" {
			t.Errorf("a large body is answered %d %s", r.Status, r.Raw)
		}
	})

}

func contains(list []string, value string) bool {
	for _, each := range list {
		if each == value {
			return true
		}
	}
	return false
}
