package webhook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/netguard"
)

func TestSignAndVerify(t *testing.T) {
	body := []byte(`{"id":"x"}`)
	sig := Sign(body, "secret")
	if len(sig) != len("sha256=")+64 || !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature = %q", sig)
	}
	// RFC 4231, test case 2: any receiver's HMAC-SHA256 agrees.
	if got := Sign([]byte("what do ya want for nothing?"), "Jefe"); got != "sha256=5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Fatalf("the RFC's vector signs as %s", got)
	}
	if !Verify(body, "secret", sig) || Verify(body, "other", sig) || Verify([]byte(`{}`), "secret", sig) || Verify(body, "secret", strings.ToUpper(sig)) {
		t.Fatal("verify disagrees with sign")
	}
}

func TestBackoffCoversEveryRetryAndGrows(t *testing.T) {
	if len(Backoff) != MaxAttempts-1 {
		t.Fatalf("%d waits for %d attempts", len(Backoff), MaxAttempts)
	}
	for i := 1; i < len(Backoff); i++ {
		if Backoff[i] <= Backoff[i-1] || Backoff[i] > 24*time.Hour {
			t.Errorf("backoff %d = %s", i, Backoff[i])
		}
	}
	for attempt := 1; attempt < MaxAttempts; attempt++ {
		if NextWait(attempt) != Backoff[attempt-1] {
			t.Errorf("after attempt %d the wait is %s", attempt, NextWait(attempt))
		}
	}
	if NextWait(0) != Backoff[0] || NextWait(MaxAttempts+3) != Backoff[len(Backoff)-1] {
		t.Error("a wait out of range is not clamped")
	}
}

func TestAnEndpointIsTurnedOffOnlyAfterFailingLongAndOften(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	long := now.Add(-DisableAfter - time.Minute)
	cases := []struct {
		failures int
		since    time.Time
		want     bool
	}{
		{DisableAfterFailures, long, true},
		{DisableAfterFailures + 10, now.Add(-DisableAfter), true},
		{DisableAfterFailures - 1, long, false},
		{100, now.Add(-time.Hour), false},
	}
	for _, c := range cases {
		if got := ShouldDisable(c.failures, c.since, now); got != c.want {
			t.Errorf("%d failures since %s: %v, want %v", c.failures, now.Sub(c.since), got, c.want)
		}
	}
}

func TestEveryTopicIsAnEventTheProductEmits(t *testing.T) {
	for _, topic := range Topics {
		found := false
		for _, emitted := range events.Topics {
			found = found || emitted == topic
		}
		if !found {
			t.Errorf("%s is offered but never emitted", topic)
		}
	}
	if Subscribable[0] != TopicAny {
		t.Error("the wildcard leads the subscribable topics")
	}
}

func TestTheSecretIsOneTheAuditLogNeverKeeps(t *testing.T) {
	plain, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, SecretPrefix) || len(plain) < len(SecretPrefix)+40 {
		t.Fatalf("secret %q", plain)
	}
	raw, err := audit.Scrub(map[string]any{"name": "Chat", "note": plain})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), plain) {
		t.Fatalf("the audit log kept a webhook secret: %s", raw)
	}
	other, _ := newSecret()
	if other == plain {
		t.Fatal("two secrets are the same")
	}
}

func TestAnInputIsCleanedOrRefusedWithASentence(t *testing.T) {
	s := &Service{opts: Options{Allow: netguard.ParseAllow("receiver.internal,10.9.0.0/16")}}
	got, err := s.clean(WebhookInput{Name: "  Chat  ", URL: " https://hooks.example.com/x?token=1 ", Topics: []string{"comment.created", "page.published", "comment.created"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Chat" || got.URL != "https://hooks.example.com/x?token=1" || strings.Join(got.Topics, ",") != "page.published,comment.created" {
		t.Fatalf("cleaned to %+v", got)
	}
	for _, allowed := range []string{"http://receiver.internal/hook", "http://10.9.1.2:8080/hook"} {
		if _, err := s.clean(WebhookInput{Name: "In", URL: allowed, Topics: []string{TopicAny}}); err != nil {
			t.Errorf("%s is named by the operator and was refused: %v", allowed, err)
		}
	}
	refused := []struct {
		in    WebhookInput
		field string
	}{
		{WebhookInput{Name: " ", URL: "https://x.example", Topics: []string{TopicAny}}, "name"},
		{WebhookInput{Name: strings.Repeat("n", MaxName+1), URL: "https://x.example", Topics: []string{TopicAny}}, "name"},
		{WebhookInput{Name: "A", URL: "ftp://x.example", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "https://user:pw@x.example", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "https://x.example/#frag", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "x.example", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "http://127.0.0.1:8080/hook", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "http://[::1]/hook", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "http://169.254.169.254/latest", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "http://localhost/hook", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "https://" + strings.Repeat("a", MaxURL) + ".example", Topics: []string{TopicAny}}, "url"},
		{WebhookInput{Name: "A", URL: "https://x.example", Topics: nil}, "topics"},
		{WebhookInput{Name: "A", URL: "https://x.example", Topics: []string{"page.verification_lapsed"}}, "topics"},
		{WebhookInput{Name: "A", URL: "https://x.example", Topics: []string{"armature.links"}}, "topics"},
	}
	for _, c := range refused {
		_, err := s.clean(c.in)
		var field *FieldError
		if !errors.As(err, &field) || field.Field != c.field {
			t.Errorf("%+v: %v, want a refusal of %s", c.in, err, c.field)
			continue
		}
		if !strings.HasSuffix(field.Message, ".") || field.Message[0] < 'A' || field.Message[0] > 'Z' {
			t.Errorf("%q is not a sentence", field.Message)
		}
	}
}

func TestTheAuditLogKeepsOnlyTheHostOfAnAddress(t *testing.T) {
	if got := hostOf("https://hooks.example.com:8443/services/T0/B0/secretpart?token=x"); got != "hooks.example.com:8443" {
		t.Fatalf("host %q", got)
	}
}

func TestASealedSecretOpensOnlyForItsEndpoint(t *testing.T) {
	org, a, b := uuid.New(), uuid.New(), uuid.New()
	if string(sealContext(org, a)) == string(sealContext(org, b)) || string(sealContext(org, a)) == string(sealContext(uuid.New(), a)) {
		t.Fatal("two endpoints share a seal context")
	}
}

func TestAPingEnvelopeIsArmaturesShape(t *testing.T) {
	s := &Service{}
	id, org := uuid.New(), uuid.New()
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	raw, err := s.body(t.Context(), claimed{eventID: id, orgID: org, topic: TopicPing, occurred: at})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != id.String() || got["topic"] != TopicPing || got["orgId"] != org.String() || got["occurredAt"] != "2026-10-01T06:00:00Z" {
		t.Fatalf("envelope %s", raw)
	}
	if got["payload"].(map[string]any)["message"] != pingMessage {
		t.Fatalf("payload %s", raw)
	}
}

func TestAnEventWithoutAnOwnerIsWithheld(t *testing.T) {
	s := &Service{}
	_, err := s.body(t.Context(), claimed{eventID: uuid.New(), orgID: uuid.New(), topic: events.TopicPagePublished, event: json.RawMessage(`{}`)})
	if !errors.Is(err, errWithheld) {
		t.Fatalf("an endpoint without an owner sent a payload: %v", err)
	}
}
