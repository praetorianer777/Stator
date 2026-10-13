package seed

import (
	"testing"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheDemoIsWhatTheBootstrapOrganizationDefaultsTo(t *testing.T) {
	if config.DefaultBootstrapOrgName != DemoOrgName {
		t.Fatalf("config default %q, seed %q", config.DefaultBootstrapOrgName, DemoOrgName)
	}
}
