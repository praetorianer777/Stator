//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
}

func (d delivery) body(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": d.ID, "topic": d.Topic, "orgId": d.OrgID, "occurredAt": time.Now().UTC(), "payload": d.Payload})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func postWebhook(t *testing.T, api *apiServer, slug string, body []byte, signature string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, api.srv.URL+"/api/v1/armature/webhook/"+slug, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Armature-Webhook")
	if signature != "" {
		req.Header.Set(armature.SignatureHeader, signature)
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
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects")
	h.settle(t)
	var armatureOrg uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT armature_org_id FROM armature_connection WHERE org_id = $1`, home.org).Scan(&armatureOrg); err != nil {
		t.Fatal(err)
	}
	tokenID := tokenRowOf(t, h, home.org, home.user)
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
		return postWebhook(t, api, slug, body, armature.Sign(body, secret))
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
		if _, cached := cachedIssue(t, h, home.org, tokenID, "CP-2"); cached {
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
		var remembered time.Duration
		remembered, _ = h.valkey(t).TTL(context.Background(), armature.EventKey(home.org, d.ID)).Result()
		if remembered <= armature.WebhookReplayWindow-time.Minute || remembered > armature.WebhookReplayWindow {
			t.Errorf("the event is remembered for %s, want %s", remembered, armature.WebhookReplayWindow)
		}
	})

	t.Run("each topic clears what the contract says", func(t *testing.T) {
		search := armature.SearchKey(home.org)
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
				_, cached := cachedIssue(t, h, home.org, tokenID, key)
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
		other := h.makeMember(t, "arm-hooks-none")
		otherSlug := h.slugOf(t, other.org)
		var answers []string
		for _, tc := range []struct {
			name, slug, signature string
			body                  []byte
		}{
			{"another secret", slug, armature.Sign(body, webhookSecret+"x"), body},
			{"no signature", slug, "", body},
			{"a body changed after signing", slug, armature.Sign(body, webhookSecret), append(bytes.Clone(body[:len(body)-1]), []byte(`,"x":1}`)...)},
			{"another Armature organization", slug, "", nil},
			{"an organization without Armature", otherSlug, armature.Sign(body, webhookSecret), body},
			{"no such organization", "no-such-org-" + strings.ToLower(uuid.NewString()[:8]), armature.Sign(body, webhookSecret), body},
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
		if _, cached := cachedIssue(t, h, home.org, tokenID, "CP-1"); !cached {
			t.Error("a refused delivery cleared the cache")
		}
		if n, _ := h.valkey(t).Exists(context.Background(), armature.EventKey(home.org, good.ID)).Result(); n != 0 {
			t.Error("a refused delivery's event was remembered, so the genuine one would be taken for a replay")
		}
	})

	t.Run("a body over the limit is refused before it is read whole", func(t *testing.T) {
		huge := bytes.Repeat([]byte("x"), armature.WebhookMaxBytes+1)
		r := postWebhook(t, api, slug, huge, armature.Sign(huge, webhookSecret))
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
