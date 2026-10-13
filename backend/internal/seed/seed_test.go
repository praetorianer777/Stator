package seed

import (
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheDemoIsWhatTheBootstrapOrganizationDefaultsTo(t *testing.T) {
	if config.DefaultBootstrapOrgSlug != DemoOrgSlug || config.DefaultBootstrapOrgName != DemoOrgName {
		t.Fatalf("config defaults %q %q, seed %q %q",
			config.DefaultBootstrapOrgSlug, config.DefaultBootstrapOrgName, DemoOrgSlug, DemoOrgName)
	}
}

// The refusal is what an operator reads in the bootstrap Job's log, so it
// names the organization there is and what to do instead.
func TestASecondOrganizationIsRefusedWithASentence(t *testing.T) {
	err := error(anotherOrganization{have: "acme", asked: "globex"})
	if !errors.Is(err, ErrAnotherOrganization) {
		t.Fatalf("%v is not ErrAnotherOrganization", err)
	}
	msg := err.Error()
	for _, want := range []string{"acme", "globex", "one organization per instance", "fresh instance"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q does not say %q", msg, want)
		}
	}
	if !strings.HasSuffix(msg, ".") {
		t.Errorf("%q is not a sentence", msg)
	}
}
