package seed

import (
	"testing"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheDemoIsWhatTheBootstrapOrganizationDefaultsTo(t *testing.T) {
	if config.DefaultBootstrapOrgSlug != DemoOrgSlug || config.DefaultBootstrapOrgName != DemoOrgName {
		t.Fatalf("config defaults %q %q, seed %q %q",
			config.DefaultBootstrapOrgSlug, config.DefaultBootstrapOrgName, DemoOrgSlug, DemoOrgName)
	}
}
