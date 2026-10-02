// Package auth handles identity: argon2 passwords, opaque session tokens,
// personal access tokens, and the organization a request is acting within.
package auth

import (
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID
	// Org is the organization the caller is acting in; nil until one is chosen.
	Org *tenant.Org

	Email     string
	Name      string
	AvatarURL string
	// Locale is the language the person chose for the interface; empty
	// means the browser's.
	Locale Locale
	// OrgName is Org's display name, empty when Org is nil.
	OrgName string
	// Role is the caller's standing in Org, empty when Org is nil.
	Role OrgRole
	// SessionID names the sign-in this request rides on, and keys its
	// read-your-writes position; nil for a caller without a session.
	SessionID *uuid.UUID
	// Proof is how that session was opened.
	Proof Proof
	// TokenID names the personal access token this request rides on, and
	// keys its read-your-writes position; nil for a session.
	TokenID *uuid.UUID
	// Scopes are the token's, empty for a session and for a token that may
	// do whatever its owner may.
	Scopes []string
	// SpacesOnly marks a token that reaches only TokenSpaces, which may by
	// now be none, and never the organization as a whole.
	SpacesOnly  bool
	TokenSpaces []uuid.UUID
}

// InSpacesOnly reports whether the caller came with a token limited to spaces.
func (p *Principal) InSpacesOnly() bool { return p != nil && p.TokenID != nil && p.SpacesOnly }

// ScopeRead marks a token that may read everything its owner can and change
// nothing, so a script can be handed a key that cannot act.
const ScopeRead = "read"

// HasScope reports whether the caller's token carries a scope.
func (p *Principal) HasScope(scope string) bool {
	return p != nil && p.TokenID != nil && slices.Contains(p.Scopes, scope)
}

// ReadOnly reports whether the caller came with a token made only to read.
func (p *Principal) ReadOnly() bool { return p.HasScope(ScopeRead) }

// InOrg reports whether the caller has chosen an organization to act in.
func (p *Principal) InOrg() bool { return p != nil && p.Org != nil && p.Org.ID != uuid.Nil }

// CanAdminister reports whether the caller may change the settings of the
// organization they are acting in, which a token limited to spaces may not.
func (p *Principal) CanAdminister() bool {
	return p.InOrg() && p.Role.CanAdminister() && !p.InSpacesOnly()
}
