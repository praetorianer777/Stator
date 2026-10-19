package exciter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/netguard"
)

func TestARequirementKeyIsExcitersShape(t *testing.T) {
	for raw, want := range map[string]string{"CHK-R42": "CHK-R42", " chk-r1 ": "CHK-R1", "A1-R7": "A1-R7"} {
		if got, ok := NormalizeKey(raw); !ok || got != want {
			t.Errorf("%q reads as %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"CHK-42", "CHK-R0", "CHK-R042", "C-R1", "CHECKOUTXYZ-R1", "1CHK-R1", "CHK-R", "CHK-R1x", "CHK-R" + strings.Repeat("9", MaxKeyLength)} {
		if key, ok := NormalizeKey(raw); ok {
			t.Errorf("%q reads as the key %q", raw, key)
		}
	}
}

func TestARequirementOpensUnderItsProject(t *testing.T) {
	if got := RequirementURL("https://exciter.example.com/", "CHK", "CHK-R4"); got != "https://exciter.example.com/p/CHK/r/CHK-R4" {
		t.Errorf("the address is %s", got)
	}
}

func TestAWebhookClearsWhatItNames(t *testing.T) {
	id := uuid.New()
	payload, _ := json.Marshal(map[string]any{"requirement": map[string]any{"id": id, "key": "chk-r4"}, "fromKey": "OLD-R7", "toKey": "CHK-R4"})
	keys, got := WebhookClears("requirement.moved", payload)
	if got != id || !slices.Equal(keys, []string{"CHK-R4", "OLD-R7"}) {
		t.Errorf("a move clears %v and %s", keys, got)
	}
	for _, topic := range WebhookTopics {
		if keys, got := WebhookClears(topic, payload); len(keys) == 0 || got != id {
			t.Errorf("%s clears nothing", topic)
		}
	}
	for _, topic := range []string{"link.created", "comment.created", "ping", ""} {
		if keys, got := WebhookClears(topic, payload); len(keys) != 0 || got != uuid.Nil {
			t.Errorf("%s, which the receiver does not act on, clears %v", topic, keys)
		}
	}
	if keys, got := WebhookClears("requirement.revised", json.RawMessage(`"no object"`)); len(keys) != 0 || got != uuid.Nil {
		t.Errorf("a payload of another shape clears %v", keys)
	}
}

func TestEachFeatureNeedsItsCapabilityByExactName(t *testing.T) {
	meta := &armature.Meta{Product: "exciter", Capabilities: []string{"requirement-embed@2", "requirement-list@1", "webhooks@1"}}
	p := partnerOf(meta, nil)
	if p.Offers(FeatureRequirements) || !p.Offers(FeatureLists) || p.Offers(FeatureProjects) || !p.Offers(FeatureWebhooks) {
		t.Errorf("offers judged wrongly: %+v", p)
	}
	var missing []string
	for _, m := range p.Missing {
		missing = append(missing, m.Capability)
	}
	if !slices.Equal(missing, []string{CapabilityRequirementEmbed, CapabilitySpecifications}) {
		t.Errorf("missing %v", missing)
	}
	if down := partnerOf(nil, armature.ErrUnreachable); !down.Offers(FeatureRequirements) || len(down.Missing) != 0 || down.Status != StatusUnreachable {
		t.Errorf("an Exciter that did not answer is judged %+v", down)
	}
}

func service(allow string) *Service {
	return NewService(nil, nil, armature.NewClient(netguard.ParseAllow(allow), nil), nil,
		Options{Development: true, Log: slog.New(slog.DiscardHandler)})
}

func TestAnAddressIsCheckedBeforeItIsSaved(t *testing.T) {
	answer := func(body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/meta" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	exciterURL := answer(`{"product":"exciter","build":{"version":"1.2.0"},"apiVersion":"1","capabilities":["requirement-embed@1","webhooks@1"]}`)
	armatureURL := answer(`{"product":"armature","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`)
	htmlURL := answer(`<html>a wiki of somebody else's</html>`)
	ctx := context.Background()

	check, err := service("127.0.0.0/8").CheckConnection(ctx, ConnectionCheckInput{BaseURL: exciterURL + "/"})
	if err != nil || check.BaseURL != exciterURL || check.Partner.Version == nil || *check.Partner.Version != "1.2.0" || len(check.Partner.Missing) != 2 {
		t.Fatalf("an Exciter checks out as %+v, %v", check, err)
	}
	for name, tc := range map[string]struct{ allow, url, says string }{
		"another product":                  {"127.0.0.0/8", armatureURL, "it is not Exciter"},
		"something that is not JSON":       {"127.0.0.0/8", htmlURL, "it is not Exciter"},
		"an address the guard refuses":     {"", exciterURL, "Use Exciter's public address"},
		"an address with a path":           {"127.0.0.0/8", exciterURL + "/api/v1", "without a path"},
		"an address where nothing listens": {"127.0.0.0/8", "http://127.0.0.1:1", "Exciter did not answer"},
	} {
		_, err := service(tc.allow).CheckConnection(ctx, ConnectionCheckInput{BaseURL: tc.url})
		var field *armature.FieldError
		if !errors.As(err, &field) || field.Field != "baseUrl" || !strings.Contains(field.Message, tc.says) {
			t.Errorf("%s: %v does not say %q", name, err, tc.says)
		}
	}
}
