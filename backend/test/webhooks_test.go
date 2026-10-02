//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// Outbound webhooks (#110): administrators keep them, every event they take
// reaches a receiver signed, read as their owner when it is sent, retried,
// redelivered and turned off, and the database holds the app role to all of
// it. The stack's worker sends what the outbox queues; the receiver is the
// armature-stub's hook bins.

// webhooks is the service as cmd/api builds it, with the stack's key, so the
// stack's worker opens what this process seals.
func (h *harness) webhooks(t *testing.T) *webhook.Service {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return webhook.NewService(h.cluster, box, webhook.Options{AppURL: os.Getenv("STATOR_TEST_APP_URL"), Allow: netguard.ParseAllow(armatureStubHost), Log: discard()})
}

// hookBin is a fresh bin on the stub and the address the api posts to.
func hookBin(t *testing.T) (name, address string) {
	t.Helper()
	stub := os.Getenv(envArmatureStub)
	if stub == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envArmatureStub)
	}
	name = "bin-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, stub+"/_hooks/"+name, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	return name, stub + "/_hooks/" + name
}

// binAnswers makes a bin answer status from now on.
func binAnswers(t *testing.T, bin string, status int) {
	t.Helper()
	body, _ := json.Marshal(map[string]int{"status": status})
	req, _ := http.NewRequest(http.MethodPut, os.Getenv(envArmatureStub)+"/_hooks/"+bin+"/status", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set the bin's status: %v %v", resp, err)
	}
	resp.Body.Close()
}

// received is one delivery a bin took, with its envelope read.
type received struct {
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	env     map[string]any
}

func (r received) payload() map[string]any {
	p, _ := r.env["payload"].(map[string]any)
	return p
}

func binDeliveries(t *testing.T, bin string) []received {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_hooks/" + bin)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Deliveries []received `json:"deliveries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the bin answered %s", raw)
	}
	for i := range out.Deliveries {
		_ = json.Unmarshal([]byte(out.Deliveries[i].Body), &out.Deliveries[i].env)
	}
	return out.Deliveries
}

// awaitTopic waits until the bin holds n deliveries of a topic about a page,
// and returns them.
func awaitTopic(t *testing.T, bin, topic, pageID string, n int) []received {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		var found []received
		for _, d := range binDeliveries(t, bin) {
			page, _ := d.payload()["page"].(map[string]any)
			if d.env["topic"] == topic && (pageID == "" || page["id"] == pageID) {
				found = append(found, d)
			}
		}
		if len(found) >= n {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s about %s reached the bin %d times in %s, want %d: %v", topic, pageID, len(found), deliveryWait, n, binDeliveries(t, bin))
		}
		time.Sleep(workerIdle * 4)
	}
}

// deliveryRow is a logged attempt as the database holds it.
type deliveryRow struct {
	ID      uuid.UUID
	Attempt int
	State   string
	Error   string
	Manual  bool
	Event   string
}

// awaitState waits until the webhook holds at least attempts attempts at an
// event of the topic about the page and the latest is in the state wanted,
// and returns the log.
func (h *harness) awaitState(t *testing.T, webhookID, topic, pageID, want string, attempts int) []deliveryRow {
	t.Helper()
	deadline := time.Now().Add(deliveryWait)
	for {
		rows, err := h.super.Query(context.Background(), `
			SELECT id, attempt, state, error, manual, event::text FROM webhook_delivery
			WHERE endpoint_id = $1 AND topic = $2 AND ($3 = '' OR event ->> 'pageId' = $3)
			ORDER BY attempt`, webhookID, topic, pageID)
		if err != nil {
			t.Fatal(err)
		}
		var log []deliveryRow
		for rows.Next() {
			var d deliveryRow
			if err := rows.Scan(&d.ID, &d.Attempt, &d.State, &d.Error, &d.Manual, &d.Event); err != nil {
				t.Fatal(err)
			}
			log = append(log, d)
		}
		rows.Close()
		if len(log) >= attempts && log[len(log)-1].State == want {
			return log
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s attempts about %s read %+v after %s, want the last %s", topic, pageID, log, deliveryWait, want)
		}
		time.Sleep(workerIdle * 4)
	}
}

func makeHook(t *testing.T, c *client, name, address string, topics ...string) (id, secretValue string) {
	t.Helper()
	got := obj(t, want(t, c.post(t, "/api/v1/webhooks", map[string]any{"name": name, "url": address, "topics": topics}), http.StatusCreated, "make "+name), "webhook")
	secretValue, _ = got["secret"].(string)
	if !strings.HasPrefix(secretValue, webhook.SecretPrefix) {
		t.Fatalf("a new webhook came without its secret: %v", got)
	}
	return got["id"].(string), secretValue
}

func TestAnAdministratorKeepsWebhooksAndSeesTheSecretOnce(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-keep")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	adminID := h.addPerson(t, home.org, "admin")
	admin := api.as(t, adminID, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	bin, address := hookBin(t)

	id, plain := makeHook(t, owner, "Chat", address, events.TopicPagePublished)
	path := "/api/v1/webhooks/" + id
	me := &home.user

	t.Run("the secret is in the answer that made it and nowhere after", func(t *testing.T) {
		got := want(t, owner.get(t, "/api/v1/webhooks"), http.StatusOK, "list the webhooks")
		if strings.Contains(string(got.Raw), plain) || strings.Contains(string(got.Raw), `"secret"`) {
			t.Errorf("the list carries a secret: %s", got.Raw)
		}
		hooks := list(t, got, "webhooks")
		if len(hooks) != 1 || hooks[0].(map[string]any)["owner"].(map[string]any)["id"] != home.user.String() {
			t.Errorf("the list reads %s", got.Raw)
		}
		var sealed []byte
		if err := h.super.QueryRow(context.Background(), `SELECT secret_sealed FROM webhook_endpoint WHERE id = $1`, id).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(sealed, []byte(plain)) || bytes.Contains(sealed, []byte(webhook.SecretPrefix)) {
			t.Error("the secret is stored as it was shown")
		}
		if data := h.recordedOnce(t, home.org, audit.ActionWebhookCreated, me, id); strings.Contains(data, plain) || strings.Contains(data, "/_hooks/") {
			t.Errorf("the record of a new webhook holds its secret or its path: %s", data)
		}
	})

	t.Run("a ping reaches the receiver signed with that secret", func(t *testing.T) {
		sent := obj(t, want(t, owner.post(t, path+"/test", nil), http.StatusOK, "send a ping"), "delivery")
		if sent["state"] != "delivered" || number(sent["status"]) != http.StatusNoContent || sent["manual"] != true {
			t.Fatalf("the ping reads %v", sent)
		}
		got := binDeliveries(t, bin)
		if len(got) != 1 {
			t.Fatalf("the bin holds %d deliveries", len(got))
		}
		d := got[0]
		if !webhook.Verify([]byte(d.Body), plain, d.Headers["X-Stator-Signature-256"]) {
			t.Errorf("the signature %q does not match the body", d.Headers["X-Stator-Signature-256"])
		}
		if d.Headers["X-Stator-Event"] != webhook.TopicPing || d.Headers["X-Stator-Delivery"] != sent["id"] || d.Headers["Content-Type"] != "application/json" {
			t.Errorf("the headers read %v", d.Headers)
		}
		if d.env["topic"] != webhook.TopicPing || d.env["orgId"] != home.org.String() || d.payload()["message"] == "" {
			t.Errorf("the envelope reads %s", d.Body)
		}
		log := list(t, want(t, owner.get(t, path+"/deliveries"), http.StatusOK, "read the log"), "deliveries")
		if len(log) != 1 || log[0].(map[string]any)["id"] != sent["id"] {
			t.Errorf("the log reads %v", log)
		}
		want(t, owner.get(t, path+"/deliveries?limit=0"), http.StatusUnprocessableEntity, "a window of none")
	})

	t.Run("a rotated secret signs from then on, and another administrator becomes the owner by saving", func(t *testing.T) {
		rotated := obj(t, want(t, owner.post(t, path+"/rotate-secret", nil), http.StatusOK, "rotate"), "webhook")
		fresh, _ := rotated["secret"].(string)
		if fresh == "" || fresh == plain {
			t.Fatalf("the rotation answered %v", rotated)
		}
		h.recordedOnce(t, home.org, audit.ActionWebhookSecretRotated, me, id)
		want(t, admin.post(t, path+"/test", nil), http.StatusOK, "ping again")
		last := binDeliveries(t, bin)
		if d := last[len(last)-1]; !webhook.Verify([]byte(d.Body), fresh, d.Headers["X-Stator-Signature-256"]) || webhook.Verify([]byte(d.Body), plain, d.Headers["X-Stator-Signature-256"]) {
			t.Error("the ping after a rotation is not signed with the new secret alone")
		}
		saved := obj(t, want(t, admin.patch(t, path, map[string]any{"name": "Chat room", "url": address, "topics": []string{"*"}, "enabled": false}), http.StatusOK, "save as the other administrator"), "webhook")
		if saved["owner"].(map[string]any)["id"] != adminID.String() || saved["enabled"] != false || saved["topics"].([]any)[0] != "*" {
			t.Errorf("the saved webhook reads %v", saved)
		}
		if data := h.recordedOnce(t, home.org, audit.ActionWebhookUpdated, &adminID, id); !strings.Contains(data, `"enabled": false`) {
			t.Errorf("the update is recorded as %s", data)
		}
	})

	t.Run("what cannot be a webhook is refused with a sentence", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"url":    {"name": "Loop", "url": "http://127.0.0.1:9/hook", "topics": []string{"*"}},
			"topics": {"name": "Lapse", "url": address, "topics": []string{"page.verification_lapsed"}},
			"name":   {"name": "chat ROOM", "url": address, "topics": []string{"*"}},
		} {
			if msg := fieldError(t, want(t, owner.post(t, "/api/v1/webhooks", body), http.StatusUnprocessableEntity, "refuse "+name), name); msg == "" {
				t.Errorf("no sentence for %s", name)
			}
		}
		want(t, owner.patch(t, path, map[string]any{"name": "Chat", "url": "ftp://x", "topics": []string{"*"}}), http.StatusUnprocessableEntity, "a bad address on save")
	})

	t.Run("members and strangers are refused every operation", func(t *testing.T) {
		delivery := list(t, want(t, owner.get(t, path+"/deliveries"), http.StatusOK, "the log"), "deliveries")[0].(map[string]any)["id"].(string)
		calls := map[string]func() response{
			"list": func() response { return member.get(t, "/api/v1/webhooks") },
			"create": func() response {
				return member.post(t, "/api/v1/webhooks", map[string]any{"name": "Mine", "url": address, "topics": []string{"*"}})
			},
			"update": func() response {
				return member.patch(t, path, map[string]any{"name": "Mine", "url": address, "topics": []string{"*"}})
			},
			"rotate":     func() response { return member.post(t, path+"/rotate-secret", nil) },
			"test":       func() response { return member.post(t, path+"/test", nil) },
			"deliveries": func() response { return member.get(t, path+"/deliveries") },
			"redeliver":  func() response { return member.post(t, path+"/deliveries/"+delivery+"/redeliver", nil) },
			"delete":     func() response { return member.delete(t, path) },
		}
		for what, call := range calls {
			if code := errorCode(t, want(t, call(), http.StatusForbidden, "a member may not "+what)); code != "forbidden" {
				t.Errorf("a member's %s is refused with %s", what, code)
			}
		}
		want(t, api.anonymous().get(t, "/api/v1/webhooks"), http.StatusUnauthorized, "nobody lists webhooks")
		want(t, owner.post(t, "/api/v1/webhooks/"+uuid.NewString()+"/test", nil), http.StatusNotFound, "a ping to no webhook")
		want(t, owner.post(t, path+"/deliveries/"+uuid.NewString()+"/redeliver", nil), http.StatusNotFound, "a redelivery of nothing")
		want(t, owner.patch(t, "/api/v1/webhooks/"+uuid.NewString(), map[string]any{"name": "X", "url": address, "topics": []string{"*"}}), http.StatusNotFound, "a save of no webhook")
		want(t, owner.post(t, "/api/v1/webhooks/"+uuid.NewString()+"/rotate-secret", nil), http.StatusNotFound, "a rotation of no webhook")
	})

	t.Run("deleting takes the log with it", func(t *testing.T) {
		want(t, owner.delete(t, path), http.StatusNoContent, "delete the webhook")
		h.recordedOnce(t, home.org, audit.ActionWebhookDeleted, me, id)
		want(t, owner.get(t, path+"/deliveries"), http.StatusNotFound, "the log of a deleted webhook")
		want(t, owner.delete(t, path), http.StatusNotFound, "delete it again")
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1`, id); n != 0 {
			t.Errorf("%d attempts outlived their webhook", n)
		}
	})
}

func TestContentEventsReachTheReceiverAsTheOwnerMayReadThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-events")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	docs := newTree(t, owner, "HOOK", "Hooks")
	elsewhere := newTree(t, owner, "ELSE", "Elsewhere")
	bin, address := hookBin(t)
	id, plain := makeHook(t, owner, "Everything", address, "*")
	path := "/api/v1/webhooks/" + id

	page := docs.add(docs.homeID, "Plans")
	published := awaitTopic(t, bin, events.TopicPagePublished, page, 1)[0]
	if !webhook.Verify([]byte(published.Body), plain, published.Headers["X-Stator-Signature-256"]) {
		t.Error("a published page's delivery is not signed with the secret")
	}
	p := published.payload()["page"].(map[string]any)
	if p["title"] != "Plans" || p["space"].(map[string]any)["key"] != "HOOK" || !strings.HasSuffix(p["url"].(string), "/s/HOOK/p/"+page) {
		t.Errorf("the published page reads %s", published.Body)
	}
	if actor := published.payload()["actor"].(map[string]any); actor["id"] != home.user.String() {
		t.Errorf("the actor reads %v", actor)
	}

	thread := startThread(t, owner, page, "Shall we ship on Friday?")
	commented := awaitTopic(t, bin, events.TopicCommentCreated, page, 1)[0]
	if c := commented.payload()["comment"].(map[string]any); c["text"] != "Shall we ship on Friday?" || c["id"] != thread["id"] || c["reply"] != false {
		t.Errorf("the comment reads %s", commented.Body)
	}

	want(t, owner.post(t, pagePath(page, "/move"), map[string]any{"parentId": elsewhere.homeID}), http.StatusOK, "move Plans")
	moved := awaitTopic(t, bin, events.TopicPageMoved, page, 1)[0].payload()
	from, to := moved["from"].(map[string]any), moved["to"].(map[string]any)
	if from["space"].(map[string]any)["key"] != "HOOK" || from["parentId"] != docs.homeID || to["space"].(map[string]any)["key"] != "ELSE" || to["parentId"] != elsewhere.homeID {
		t.Errorf("the move reads %v", moved)
	}

	want(t, owner.delete(t, pagePath(page)), http.StatusNoContent, "trash Plans")
	deleted := awaitTopic(t, bin, events.TopicPageDeleted, page, 1)[0].payload()
	if deleted["page"].(map[string]any)["title"] != "Plans" {
		t.Errorf("the deletion reads %v", deleted)
	}

	t.Run("a redelivery is the same event again, as the next attempt", func(t *testing.T) {
		log := h.awaitState(t, id, events.TopicPagePublished, page, "delivered", 1)
		again := obj(t, want(t, owner.post(t, path+"/deliveries/"+log[0].ID.String()+"/redeliver", nil), http.StatusOK, "redeliver"), "delivery")
		if again["state"] != "delivered" || number(again["attempt"]) != 2 || again["manual"] != true || again["eventId"] != published.env["id"] {
			t.Fatalf("the redelivery reads %v", again)
		}
		both := awaitTopic(t, bin, events.TopicPagePublished, page, 2)
		if both[0].env["id"] != both[1].env["id"] || both[0].Headers["X-Stator-Delivery"] == both[1].Headers["X-Stator-Delivery"] {
			t.Error("a redelivery does not repeat the event under a delivery of its own")
		}
	})
}

func TestAFailingReceiverIsRetriedThenTurnedOff(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-retry")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	docs := newTree(t, owner, "RTY", "Retries")
	bin, address := hookBin(t)
	binAnswers(t, bin, http.StatusServiceUnavailable)
	id, _ := makeHook(t, owner, "Flaky", address, events.TopicPagePublished)

	first := docs.add(docs.homeID, "First")
	log := h.awaitState(t, id, events.TopicPagePublished, first, "pending", 2)
	if len(log) != 2 || log[0].State != "failed" || !strings.Contains(log[0].Error, "503") || log[1].Attempt != 2 {
		t.Fatalf("after one failure the log reads %+v", log)
	}
	var seconds float64
	if err := h.super.QueryRow(context.Background(), `
		SELECT extract(epoch FROM next_attempt_at - (SELECT attempted_at FROM webhook_delivery WHERE id = $2))::float8
		FROM webhook_delivery WHERE id = $1`, log[1].ID, log[0].ID).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	wait := time.Duration(seconds * float64(time.Second))
	if wait < webhook.Backoff[0]-5*time.Second || wait > webhook.Backoff[0]+5*time.Second {
		t.Errorf("the second attempt is due %s after the first, want %s", wait, webhook.Backoff[0])
	}

	binAnswers(t, bin, http.StatusOK)
	h.cleanupExec(t, h.super, `UPDATE webhook_delivery SET next_attempt_at = now() WHERE id = $1`, log[1].ID)
	log = h.awaitState(t, id, events.TopicPagePublished, first, "delivered", 1)
	if len(log) != 2 {
		t.Fatalf("the retry reads %+v", log)
	}
	got := awaitTopic(t, bin, events.TopicPagePublished, first, 2)
	if got[0].env["id"] != got[1].env["id"] {
		t.Error("a retry is not the same event")
	}
	if n := h.countRows(t, `SELECT failures FROM webhook_endpoint WHERE id = $1`, id); n != 0 {
		t.Errorf("a delivery left %d failures counted", n)
	}

	t.Run("a webhook failing for a day is turned off, and turning it on starts again", func(t *testing.T) {
		binAnswers(t, bin, http.StatusInternalServerError)
		h.cleanupExec(t, h.super, `UPDATE webhook_endpoint SET failures = $2, failing_since = now() - interval '25 hours' WHERE id = $1`,
			id, webhook.DisableAfterFailures-1)
		second := docs.add(docs.homeID, "Second")
		log := h.awaitState(t, id, events.TopicPagePublished, second, "failed", 1)
		if len(log) != 1 {
			t.Errorf("a webhook turned off still tries again: %+v", log)
		}
		// The worker wrote it, so no read of the caller's waits for it.
		h.settle(t)
		hook := list(t, want(t, owner.get(t, "/api/v1/webhooks"), http.StatusOK, "the list"), "webhooks")[0].(map[string]any)
		if hook["enabled"] != false || hook["disabledReason"] != webhook.ReasonFailing || number(hook["failures"]) != webhook.DisableAfterFailures {
			t.Fatalf("the failing webhook reads %v", hook)
		}
		if data := h.recordedOnce(t, home.org, audit.ActionWebhookDisabled, nil, id); !strings.Contains(data, webhook.ReasonFailing) {
			t.Errorf("the worker's turning it off is recorded as %s", data)
		}

		third := docs.add(docs.homeID, "Third")
		h.drained(t, home.org)
		if n := h.countRows(t, `SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1 AND event ->> 'pageId' = $2`, id, third); n != 0 {
			t.Errorf("a webhook that is off was queued %d attempts", n)
		}

		binAnswers(t, bin, http.StatusNoContent)
		on := obj(t, want(t, owner.patch(t, "/api/v1/webhooks/"+id, map[string]any{"name": "Flaky", "url": address, "topics": []string{events.TopicPagePublished}, "enabled": true}),
			http.StatusOK, "turn it on again"), "webhook")
		if on["enabled"] != true || on["disabledReason"] != nil || number(on["failures"]) != 0 {
			t.Errorf("turned on again it reads %v", on)
		}
	})
}

func TestAPayloadIsWithheldWhatItsOwnerMayNotViewWhenItIsSent(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-perm")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "admin")
	ann := api.as(t, annID, home.org, slug)
	docs := newTree(t, owner, "PRM", "Permissions")
	want(t, owner.put(t, "/api/v1/spaces/PRM/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments"}},
		map[string]any{"subject": user(home.user), "permissions": []any{"administer"}},
	}}), http.StatusOK, "everybody reads the space")
	bin, address := hookBin(t)
	id, _ := makeHook(t, ann, "Ann's", address, events.TopicPagePublished)

	// Ann made it while an administrator; she is a member now, and the
	// payloads are read as she may read them when they are sent.
	h.cleanupExec(t, h.super, `UPDATE org_member SET org_role = 'member' WHERE org_id = $1 AND user_id = $2`, home.org, annID)
	h.settle(t)

	open := docs.add(docs.homeID, "Open")
	awaitTopic(t, bin, events.TopicPagePublished, open, 1)

	vault := docs.add(docs.homeID, "Vault")
	want(t, restrict(t, owner, vault, []any{user(home.user)}, nil), http.StatusOK, "only the owner reads the Vault")
	closed := docs.add(vault, "Secret plans")
	log := h.awaitState(t, id, events.TopicPagePublished, closed, "withheld", 1)
	if !strings.Contains(log[len(log)-1].Error, "may not view") {
		t.Errorf("a withheld delivery says %q", log[len(log)-1].Error)
	}
	for _, d := range binDeliveries(t, bin) {
		if strings.Contains(d.Body, closed) || strings.Contains(d.Body, "Secret plans") {
			t.Fatalf("the receiver got a page its owner may not view: %s", d.Body)
		}
	}

	t.Run("a redelivery reads the page afresh", func(t *testing.T) {
		delivered := h.awaitState(t, id, events.TopicPagePublished, open, "delivered", 1)
		want(t, restrict(t, owner, open, []any{user(home.user)}, nil), http.StatusOK, "Open closes")
		again := obj(t, want(t, owner.post(t, "/api/v1/webhooks/"+id+"/deliveries/"+delivered[0].ID.String()+"/redeliver", nil), http.StatusOK, "redeliver Open"), "delivery")
		if again["state"] != "withheld" {
			t.Errorf("a redelivery of a page closed since reads %v", again)
		}
		if n := len(awaitTopic(t, bin, events.TopicPagePublished, open, 1)); n != 1 {
			t.Errorf("the closed page reached the receiver %d times", n)
		}
	})

	t.Run("the owner leaving withholds everything but a ping", func(t *testing.T) {
		h.cleanupExec(t, h.super, `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, home.org, annID)
		h.settle(t)
		hook := list(t, want(t, owner.get(t, "/api/v1/webhooks"), http.StatusOK, "the list"), "webhooks")[0].(map[string]any)
		if hook["owner"] != nil {
			t.Fatalf("a webhook keeps an owner who left: %v", hook)
		}
		late := docs.add(docs.homeID, "Late")
		log := h.awaitState(t, id, events.TopicPagePublished, late, "withheld", 1)
		if !strings.Contains(log[0].Error, "left") {
			t.Errorf("a webhook without an owner says %q", log[0].Error)
		}
		if sent := obj(t, want(t, owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "ping"), "delivery"); sent["state"] != "delivered" {
			t.Errorf("a ping without an owner reads %v", sent)
		}
	})
}

func TestTheServerNeverPostsIntoItsOwnNetwork(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-ssrf")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	for _, inside := range []string{"http://10.0.0.1/hook", "http://169.254.169.254/latest/meta-data", "http://localhost:8080/api/v1/auth/me"} {
		fieldError(t, want(t, owner.post(t, "/api/v1/webhooks", map[string]any{"name": "In", "url": inside, "topics": []string{"*"}}), http.StatusUnprocessableEntity, "save "+inside), "url")
	}
	// A name says nothing until it resolves; the guard refuses where it leads.
	id, _ := makeHook(t, owner, "Valkey", "http://valkey:6379/", "*")
	sent := obj(t, want(t, owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "ping valkey"), "delivery")
	if sent["state"] != "failed" || sent["status"] != nil || !strings.Contains(sent["error"].(string), "STATOR_OUTBOUND_ALLOW") || strings.Contains(sent["error"].(string), "6379") {
		t.Errorf("a ping into the network reads %v", sent)
	}
}

func TestWebhooksAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "hooks-rls")
	other := h.makeMember(t, "hooks-rls-other")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	memberID := h.addPerson(t, home.org, "member")
	_, address := hookBin(t)
	id, _ := makeHook(t, owner, "Walled", address, "*")
	want(t, owner.post(t, "/api/v1/webhooks/"+id+"/test", nil), http.StatusOK, "a ping for the log")
	h.settle(t)
	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("an administrator never reads a secret, names an owner or writes the log", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		if count(`SELECT count(*) FROM webhook_endpoint`) != 1 || count(`SELECT count(*) FROM webhook_delivery`) != 1 {
			t.Fatal("the owner does not read their webhook and its log")
		}
		denied(t, conn, "read a sealed secret", `SELECT secret_sealed FROM webhook_endpoint`)
		denied(t, conn, "name another owner", `UPDATE webhook_endpoint SET owner_id = $2 WHERE id = $1`, id, memberID)
		denied(t, conn, "clear the failures", `UPDATE webhook_endpoint SET failures = 0, failing_since = NULL WHERE id = $1`, id)
		denied(t, conn, "forge a delivered attempt", `INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, event, occurred_at, state) VALUES ($1, $2, $3, 'ping', '{}', now(), 'delivered')`, home.org, id, uuid.New())
		denied(t, conn, "rewrite the log", `UPDATE webhook_delivery SET state = 'delivered'`)
		denied(t, conn, "erase the log", `DELETE FROM webhook_delivery`)
		denied(t, conn, "take a topic nobody emits", `UPDATE webhook_endpoint SET topics = '{armature.links}' WHERE id = $1`, id)
		if _, err := conn.Exec(ctx, `UPDATE webhook_endpoint SET name = 'Renamed' WHERE id = $1`, id); err != nil {
			t.Fatalf("the owner may rename their webhook: %v", err)
		}
		var ownerID uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT owner_id FROM webhook_endpoint WHERE id = $1`, id).Scan(&ownerID); err != nil || ownerID != home.user {
			t.Errorf("a save through SQL leaves the owner %s (%v)", ownerID, err)
		}
	})

	t.Run("a member reads and writes none of it", func(t *testing.T) {
		actAs(t, conn, home.org, memberID)
		if n := count(`SELECT count(*) FROM webhook_endpoint`) + count(`SELECT count(*) FROM webhook_delivery`); n != 0 {
			t.Errorf("a member reads %d rows", n)
		}
		denied(t, conn, "a member adds a webhook", `INSERT INTO webhook_endpoint (org_id, name, url, secret_sealed, topics) VALUES ($1, 'Mine', 'https://x.example', '\x01', '{*}')`, home.org)
		untouched(t, conn, "a member deletes it", `DELETE FROM webhook_endpoint WHERE id = $1`, id)
		untouched(t, conn, "a member turns it off", `UPDATE webhook_endpoint SET enabled = false WHERE id = $1`, id)
	})

	t.Run("another organization's administrator never sees it", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM webhook_endpoint WHERE id = $1`, id) + count(`SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1`, id); n != 0 {
			t.Errorf("another organization reads %d rows", n)
		}
		denied(t, conn, "point a webhook into another organization", `INSERT INTO webhook_endpoint (org_id, name, url, secret_sealed, topics) VALUES ($1, 'Theirs', 'https://x.example', '\x01', '{*}')`, home.org)
	})
}
