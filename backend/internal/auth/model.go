package auth

import (
	"errors"

	"github.com/google/uuid"
)

// OrgRole is a member's standing within one organization.
type OrgRole string

const (
	RoleOwner  OrgRole = "owner"
	RoleAdmin  OrgRole = "admin"
	RoleMember OrgRole = "member"
)

// OrgRoles lists every role, in the order the API documents them.
var OrgRoles = []OrgRole{RoleOwner, RoleAdmin, RoleMember}

// CanAdminister reports whether the role may change organization settings.
func (r OrgRole) CanAdminister() bool { return r == RoleOwner || r == RoleAdmin }

// User is a person, global across organizations.
type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// CurrentOrg is the organization a session is acting in, with the caller's
// standing there.
type CurrentOrg struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
	Role OrgRole   `json:"role"`
}

// Membership is a user's place in one organization.
type Membership struct {
	OrgID   uuid.UUID `json:"orgId"`
	OrgSlug string    `json:"orgSlug"`
	OrgName string    `json:"orgName"`
	Role    OrgRole   `json:"role"`
}

// Proof is how a session was opened, which decides how far it reaches.
type Proof string

const (
	ProofPassword Proof = "password"
	ProofOIDC     Proof = "oidc"
)

var (
	// ErrInvalidCredentials is returned for an unknown email and a wrong
	// password alike, so the login form cannot enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrUserInactive is returned when a deactivated account signs in.
	ErrUserInactive = errors.New("account is deactivated")
	// ErrNotAMember is returned when somebody tries to act in an organization
	// they do not belong to, or names one that does not exist.
	ErrNotAMember = errors.New("not a member of that organization")
	// ErrSessionStaysHome is returned when a session proven for one
	// organization tries to act where that proof does not vouch for it.
	ErrSessionStaysHome = errors.New("this sign-in does not reach that organization")
)
