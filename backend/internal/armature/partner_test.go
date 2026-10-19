package armature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// partnerArmature answers the partner webhook operations from a script of
// method and status pairs, in order, and records what was asked.
type partnerArmature struct {
	mu     sync.Mutex
	script []partnerStep
	asked  []string
}

type partnerStep struct {
	method string
	status int
	body   string
}

func (a *partnerArmature) serve(t *testing.T) string {
	t.Helper()
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.asked = append(a.asked, r.Method+" "+strings.TrimPrefix(r.URL.Path, APIPath))
		if len(a.script) == 0 || a.script[0].method != r.Method {
			t.Errorf("Armature was asked %s %s out of turn", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		step := a.script[0]
		a.script = a.script[1:]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_, _ = w.Write([]byte(step.body))
	})
	return srv.URL
}

const (
	madeWebhook    = `{"webhook":{"id":"0199e1c2-0000-7000-8000-000000000001","url":"https://stator.example/api/v1/armature/webhook","topics":["issue.updated"],"enabled":true,"version":1,"secret":"armature_whs_new"}}`
	foundWebhook   = `{"webhook":{"id":"0199e1c2-0000-7000-8000-000000000001","url":"https://old.example/api/v1/armature/webhook","topics":["issue.updated"],"enabled":false,"version":1}}`
	noWebhook      = `{"webhook":null}`
	webhookExists  = `{"error":{"code":"partner_webhook_exists","message":"Stator has a webhook here already."}}`
	webhookMissing = `{"error":{"code":"not_found","message":"That webhook was not found."}}`
)

func TestStatorMakesItsWebhookOrBringsBackTheOneArmatureHas(t *testing.T) {
	in := partnerInput{URL: "https://stator.example/api/v1/armature/webhook", Topics: WebhookTopics, Version: WebhookVersion}
	for _, tc := range []struct {
		name   string
		script []partnerStep
		asked  string
	}{
		{"none yet", []partnerStep{{"POST", 201, madeWebhook}}, "[POST /webhooks/partner]"},
		{"one already, changed and given a new secret", []partnerStep{
			{"POST", 409, webhookExists}, {"GET", 200, foundWebhook}, {"PATCH", 200, foundWebhook}, {"POST", 200, madeWebhook},
		}, "[POST /webhooks/partner GET /webhooks/partner PATCH /webhooks/partner POST /webhooks/partner/rotate-secret]"},
		{"one that went before it was found", []partnerStep{
			{"POST", 409, webhookExists}, {"GET", 200, noWebhook}, {"POST", 201, madeWebhook},
		}, "[POST /webhooks/partner GET /webhooks/partner POST /webhooks/partner]"},
		{"one that went before it was changed", []partnerStep{
			{"POST", 409, webhookExists}, {"GET", 200, foundWebhook}, {"PATCH", 404, webhookMissing}, {"POST", 201, madeWebhook},
		}, "[POST /webhooks/partner GET /webhooks/partner PATCH /webhooks/partner POST /webhooks/partner]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &partnerArmature{script: tc.script}
			secret, err := makePartnerWebhook(context.Background(), loopback().As(a.serve(t), "armature_oat_admin"), in)
			if err != nil || secret != "armature_whs_new" {
				t.Fatalf("made %q, %v", secret, err)
			}
			if got := strings.Join(a.asked, " "); "["+got+"]" != tc.asked {
				t.Errorf("Armature was asked %s, want %s", got, tc.asked)
			}
		})
	}
}

func TestArmaturesRefusalsOfTheWebhookSayWhatToDo(t *testing.T) {
	in := partnerInput{URL: "http://stator.internal/api/v1/armature/webhook", Topics: WebhookTopics, Version: WebhookVersion}
	guard := "Armature may not reach stator.internal, which is inside its own network. Add stator.internal to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW) and try again."
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
		sentence   string
	}{
		{"the outbound guard", `{"error":{"code":"outbound_refused","message":"` + guard + `","fields":{"url":"Add stator.internal to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)."}}}`, 422, nil, guard},
		{"an address that does not resolve", `{"error":{"code":"invalid","message":"stator.internal does not resolve from Armature. Check the address, or Armature's DNS.","fields":{"url":"stator.internal does not resolve from Armature. Check the address, or Armature's DNS."}}}`, 422, nil,
			"stator.internal does not resolve from Armature. Check the address, or Armature's DNS."},
		{"a name taken by an administrator's webhook", `{"error":{"code":"conflict","message":"A webhook called Stator (connected app) is already here. Rename it in Settings, Webhooks, and connect again."}}`, 409, nil,
			"A webhook called Stator (connected app) is already here. Rename it in Settings, Webhooks, and connect again."},
		{"a grant without webhooks.partner", `{"error":{"code":"insufficient_scope","message":"Stator was not allowed to manage its webhook."}}`, 403, ErrWebhookAdmin, ""},
		{"a person no longer administering Armature", `{"error":{"code":"forbidden","message":"Ada Lovelace is no longer an Armature administrator."}}`, 403, ErrWebhookAdmin, ""},
		{"a grant that ended", `{"error":{"code":"unauthorized","message":"Sign in."}}`, 401, ErrWebhookAdmin, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &partnerArmature{script: []partnerStep{{"POST", tc.status, tc.body}}}
			_, err := makePartnerWebhook(context.Background(), loopback().As(a.serve(t), "armature_oat_admin"), in)
			var refused *WebhookRefusedError
			switch {
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Errorf("err = %v, want %v", err, tc.want)
			case tc.want == nil && (!errors.As(err, &refused) || refused.Message != tc.sentence):
				t.Errorf("err = %v, want Armature's sentence %q", err, tc.sentence)
			}
		})
	}
}

func TestOnlyAnArmatureWithBothCapabilitiesOffersTheOneStepConnect(t *testing.T) {
	both := Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerTokens, CapabilityPartnerWebhooks}}
	tokensOnly := Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerTokens}}
	webhooksOnly := Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerWebhooks}}
	unknown := Partner{Status: StatusUnreachable, Capabilities: []string{CapabilityPartnerTokens, CapabilityPartnerWebhooks}}
	if !both.OffersPartnerWebhook() || tokensOnly.OffersPartnerWebhook() || webhooksOnly.OffersPartnerWebhook() || unknown.OffersPartnerWebhook() {
		t.Error("the one-step connect is offered without both capabilities from an Armature that answered")
	}
	if !strings.Contains(" "+AdminScopes+" ", " "+ScopeWebhooks+" ") || !strings.HasPrefix(AdminScopes, AppScopes+" ") {
		t.Errorf("an administrator is asked for %q, which is not every scope a member is asked for and webhooks.partner", AdminScopes)
	}
}
