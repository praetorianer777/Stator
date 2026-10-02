package auth

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestAPITokensArePrefixedAndHashedWhole(t *testing.T) {
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "stator_pat_") || len(secret) != len("stator_pat_")+43 {
		t.Fatalf("token %q is not stator_pat_ and 43 characters", secret)
	}
	if !bytes.Equal(digest, HashToken(secret)) {
		t.Fatal("the digest is not the SHA-256 of the whole token, prefix included")
	}
	if !IsAPIToken(secret) || ParseAPIToken(secret) != nil {
		t.Fatalf("a token just made does not parse: %q", secret)
	}
}

func TestParseAPITokenRefusesAnythingElse(t *testing.T) {
	good, _, _ := GenerateAPIToken()
	session, _, _ := GenerateToken()
	for name, secret := range map[string]string{
		"empty":           "",
		"a session":       session,
		"no body":         APITokenPrefix,
		"short":           good[:len(good)-1],
		"long":            good + "A",
		"padded":          good + "=",
		"standard base64": APITokenPrefix + "+/" + good[len(APITokenPrefix)+2:],
		"another prefix":  "armature_pat_" + strings.TrimPrefix(good, APITokenPrefix),
		"upper prefix":    strings.ToUpper(APITokenPrefix) + strings.TrimPrefix(good, APITokenPrefix),
	} {
		if err := ParseAPIToken(secret); err != ErrInvalidToken {
			t.Errorf("%s %q parsed: %v", name, secret, err)
		}
	}
	if IsAPIToken(session) {
		t.Error("a session secret was taken for a personal access token")
	}
}

func TestOnlyTheReadScopeExists(t *testing.T) {
	if err := ValidateScopes(nil); err != nil {
		t.Errorf("no scope: %v", err)
	}
	if err := ValidateScopes([]string{ScopeRead, ScopeRead}); err != nil {
		t.Errorf("read: %v", err)
	}
	for _, scope := range []string{"write", "admin", "READ", ""} {
		if err := ValidateScopes([]string{ScopeRead, scope}); err != ErrTokenScope {
			t.Errorf("%q was accepted: %v", scope, err)
		}
	}
}

func TestOnlyATokenMadeToReadIsReadOnly(t *testing.T) {
	id := uuid.New()
	for name, tc := range map[string]struct {
		p    *Principal
		want bool
	}{
		"nobody":            {nil, false},
		"a session":         {&Principal{SessionID: &id}, false},
		"a full token":      {&Principal{TokenID: &id}, false},
		"a reading token":   {&Principal{TokenID: &id, Scopes: []string{ScopeRead}}, true},
		"a session claimed": {&Principal{SessionID: &id, Scopes: []string{ScopeRead}}, false},
	} {
		if got := tc.p.ReadOnly(); got != tc.want {
			t.Errorf("%s: ReadOnly = %v, want %v", name, got, tc.want)
		}
	}
}

func TestANewTokenIsValidatedBeforeItIsMade(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	s := &Service{now: func() time.Time { return now }}
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	for name, tc := range map[string]struct {
		in   NewAPIToken
		want error
	}{
		"fine":             {NewAPIToken{Name: " deploy ", Scopes: []string{ScopeRead, ScopeRead}, ExpiresAt: &future}, nil},
		"forever":          {NewAPIToken{Name: "deploy"}, nil},
		"no name":          {NewAPIToken{Name: "  "}, ErrTokenName},
		"a long name":      {NewAPIToken{Name: strings.Repeat("x", MaxTokenNameLength+1)}, ErrTokenName},
		"a long name fits": {NewAPIToken{Name: strings.Repeat("ü", MaxTokenNameLength)}, nil},
		"an unknown scope": {NewAPIToken{Name: "deploy", Scopes: []string{"write"}}, ErrTokenScope},
		"already expired":  {NewAPIToken{Name: "deploy", ExpiresAt: &past}, ErrTokenExpiry},
		"expiring now":     {NewAPIToken{Name: "deploy", ExpiresAt: &now}, ErrTokenExpiry},
	} {
		in := tc.in
		if err := s.validateNewToken(&in); err != tc.want {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	in := NewAPIToken{Name: " deploy ", Scopes: []string{ScopeRead, ScopeRead}}
	_ = s.validateNewToken(&in)
	if in.Name != "deploy" || len(in.Scopes) != 1 {
		t.Errorf("the name is trimmed and the scopes deduplicated: %+v", in)
	}
}

func TestATokenNamesItsSpacesOnceEach(t *testing.T) {
	s := &Service{now: time.Now}
	in := NewAPIToken{Name: "deploy", Spaces: []string{" docs", "DOCS", "ops", " "}}
	if err := s.validateNewToken(&in); err != nil {
		t.Fatal(err)
	}
	if strings.Join(in.Spaces, ",") != "DOCS,OPS" {
		t.Errorf("the keys are %v, want DOCS and OPS once each", in.Spaces)
	}
	all := NewAPIToken{Name: "deploy"}
	if err := s.validateNewToken(&all); err != nil || all.Spaces == nil || len(all.Spaces) != 0 {
		t.Errorf("a token naming no space = %v %v, want an empty list", all.Spaces, err)
	}
	many := NewAPIToken{Name: "deploy"}
	for i := range MaxTokenSpaces + 1 {
		many.Spaces = append(many.Spaces, fmt.Sprintf("S%d", i))
	}
	if err := s.validateNewToken(&many); err != ErrTokenSpaces {
		t.Errorf("%d spaces = %v, want %v", len(many.Spaces), err, ErrTokenSpaces)
	}
}

func TestATokenLimitedToSpacesAdministersNoOrganization(t *testing.T) {
	org := &tenant.Org{ID: uuid.New(), Slug: "acme"}
	id := uuid.New()
	limited := &Principal{Org: org, Role: RoleOwner, TokenID: &id, SpacesOnly: true}
	if !limited.InSpacesOnly() || limited.CanAdminister() {
		t.Error("an owner's token limited to spaces administers the organization")
	}
	if (&Principal{Org: org, Role: RoleOwner, TokenID: &id}).InSpacesOnly() {
		t.Error("a token without spaces is limited")
	}
	if (&Principal{Org: org, Role: RoleOwner, SessionID: &id, SpacesOnly: true}).InSpacesOnly() {
		t.Error("a session is limited to spaces")
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
