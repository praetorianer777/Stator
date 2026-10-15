package httpapi

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/events"
)

// capabilityName is name@version, as Armature and Dynamo's shared list spell them.
var capabilityName = regexp.MustCompile(`^[a-z][a-z-]*@[1-9][0-9]*$`)

// A partner offers a feature on seeing its name at /meta, so the list is the
// one docs/integration.md describes, sorted, and nothing else.
func TestMetaReportsTheIntegrationCapabilities(t *testing.T) {
	want := []string{"page-links@1", "page-remote-links@1", "theme-export@1", "webhook-receiver@1", "webhooks@1"}
	if got := capabilities(); !slices.Equal(got, want) {
		t.Errorf("capabilities() = %v, want %v", got, want)
	}
}

// A capability rests on operations and topics; renaming or dropping one of
// them must fail here, not at the partner that relied on it.
func TestEveryCapabilityRestsOnWhatTheBuildHas(t *testing.T) {
	routes := map[string]Route{}
	for _, r := range Catalog() {
		routes[r.Method+" "+r.Path] = r
	}
	for _, c := range integrationCapabilities {
		if len(c.operations) == 0 {
			t.Errorf("%s names no operation", c.name)
		}
		for _, op := range c.operations {
			route, ok := routes[op]
			switch {
			case !ok:
				t.Errorf("%s relies on %q, which is not in the operations table", c.name, op)
			case route.Pending:
				t.Errorf("%s relies on %q, which answers 501 until it is built", c.name, op)
			}
		}
		for _, topic := range c.topics {
			if !slices.Contains(events.Topics, topic) {
				t.Errorf("%s relies on the topic %q, which events.Topics does not list", c.name, topic)
			}
		}
		for _, topic := range c.receives {
			if !slices.Contains(armature.WebhookTopics, topic) {
				t.Errorf("%s promises to act on %q, which the receiver does not", c.name, topic)
			}
		}
	}
}

// A partner matches names exactly, and a breaking change raises the number.
func TestCapabilityNamesAreVersionedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range integrationCapabilities {
		if !capabilityName.MatchString(c.name) {
			t.Errorf("%q is not name@version, such as remote-links@1", c.name)
		}
		if seen[c.name] {
			t.Errorf("%q is listed twice", c.name)
		}
		seen[c.name] = true
		if strings.TrimSpace(c.promise) == "" {
			t.Errorf("%s says nothing of what it promises", c.name)
		}
	}
}

// What Stator needs of Armature is spelled as Armature reports it.
func TestEveryFeatureNeedsAVersionedCapability(t *testing.T) {
	for _, f := range armature.Features {
		if c := armature.FeatureCapabilities[f]; !capabilityName.MatchString(c) {
			t.Errorf("%s needs %q, which is not name@version", f, c)
		}
	}
	if len(armature.FeatureCapabilities) != len(armature.Features) {
		t.Errorf("%d features name a capability, of %d", len(armature.FeatureCapabilities), len(armature.Features))
	}
}
