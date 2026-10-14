// Package auth handles identity: argon2 passwords, opaque session tokens,
// personal access tokens, and the standing a caller has on the site.
package auth

import (
	"errors"
	"slices"

	"github.com/google/uuid"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// ErrNotMember is a person signed in who is no member yet, for whom the
// database acts as nobody.
var ErrNotMember = errors.New("not a member of this instance")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID

	Email     string
	Name      string
	AvatarURL string
	// Locale is the language the person chose for the interface; empty
	// means the browser's.
	Locale Locale
	// ShowInReaders says the person's name appears among a page's readers.
	ShowInReaders bool
	// SiteName is the site's display name, empty for a person who is no member.
	SiteName string
	// Role is the caller's standing on the site, empty for a person who is no
	// member.
	Role Role
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
	// now be none, and never the site as a whole.
	SpacesOnly  bool
	TokenSpaces []uuid.UUID
	// GuestSpace is the one space a guest reaches; nil for everybody else.
	GuestSpace *SpaceRef
}

// Guest reports whether the caller is a guest of one space.
func (p *Principal) Guest() bool { return p != nil && p.Role == RoleGuest }

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

// IsMember reports whether the caller is a member of the site, a guest included.
func (p *Principal) IsMember() bool { return p != nil && p.Role != "" }

// CanAdminister reports whether the caller may change the settings of the
// site, which a token limited to spaces may not.
func (p *Principal) CanAdminister() bool {
	return p.IsMember() && p.Role.CanAdminister() && !p.InSpacesOnly()
}
