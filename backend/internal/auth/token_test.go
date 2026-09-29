package auth

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/tenant"
)

func TestGenerateTokenIsUniqueAndHashed(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		secret, digest, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if seen[secret] {
			t.Fatal("GenerateToken returned a duplicate secret")
		}
		seen[secret] = true

		if len(digest) != 32 {
			t.Fatalf("digest length = %d, want 32", len(digest))
		}
		if len(secret) < 43 {
			t.Fatalf("secret %q carries fewer than 32 bytes", secret)
		}
		if strings.Contains(string(digest), secret) {
			t.Fatal("digest contains the secret verbatim")
		}
		if !bytes.Equal(digest, HashToken(secret)) {
			t.Fatal("HashToken does not reproduce the digest returned by GenerateToken")
		}
	}
}

func TestOnlyOwnersAndAdminsAdminister(t *testing.T) {
	org := &tenant.Org{ID: uuid.New(), Slug: "acme"}
	for role, want := range map[OrgRole]bool{RoleOwner: true, RoleAdmin: true, RoleMember: false, "": false} {
		if got := (&Principal{Org: org, Role: role}).CanAdminister(); got != want {
			t.Errorf("%q administers = %v, want %v", role, got, want)
		}
	}
	if (&Principal{Role: RoleOwner}).CanAdminister() {
		t.Error("an owner outside any organization administers nothing")
	}
	var nobody *Principal
	if nobody.CanAdminister() || nobody.InOrg() {
		t.Error("an anonymous caller administers nothing")
	}
}
