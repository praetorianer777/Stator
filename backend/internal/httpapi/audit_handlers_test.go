package httpapi

import (
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
)

func TestTheAuditFiltersAreRead(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/audit?action=space.created&actor=0199a000-0000-7000-8000-000000000001&targetType=space&target=0199a000-0000-7000-8000-000000000002&from=2026-09-01&to=2026-09-30", nil)
	f, apiErr := auditFilter(r)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if f.Action != audit.ActionSpaceCreated || f.ActorID == nil || f.TargetID == nil || f.TargetType != "space" {
		t.Errorf("the filter reads %+v", f)
	}
	if !f.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the days read %s to %s, want the whole of September in UTC", f.From, f.To)
	}

	r = httptest.NewRequest("GET", "/api/v1/audit?from=2026-09-01T00:00:00%2B02:00&to=2026-09-02T00:00:00%2B02:00", nil)
	if f, apiErr = auditFilter(r); apiErr != nil || !f.From.Equal(time.Date(2026, 8, 31, 22, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("instants read %v to %v, %v", f.From, f.To, apiErr)
	}
}

func TestABadAuditFilterIsRefusedFieldByField(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/audit?action=page.edited&actor=ann&target=x&from=yesterday", nil)
	_, apiErr := auditFilter(r)
	if apiErr == nil || apiErr.Code != "validation_failed" {
		t.Fatalf("a bad filter is answered %v", apiErr)
	}
	for _, field := range []string{"action", "actor", "target", "from"} {
		if apiErr.Fields[field] == "" {
			t.Errorf("%s is not named among %v", field, apiErr.Fields)
		}
	}
	r = httptest.NewRequest("GET", "/api/v1/audit?from=2026-09-30&to=2026-09-01", nil)
	if _, apiErr = auditFilter(r); apiErr == nil || apiErr.Fields["to"] == "" {
		t.Errorf("a range ending before it starts is answered %v", apiErr)
	}
}

// The audit package cannot import the ones that issue credentials, so it
// names their prefixes itself; this holds the copy to the originals.
func TestTheLogKnowsEveryCredentialsPrefix(t *testing.T) {
	for _, prefix := range []string{auth.APITokenPrefix, armature.TokenPrefix, armature.WebhookSecretPrefix} {
		if !slices.Contains(audit.SecretPrefixes, prefix) {
			t.Errorf("audit.SecretPrefixes leaves out %q", prefix)
		}
	}
}
