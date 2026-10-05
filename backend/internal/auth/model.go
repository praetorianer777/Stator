package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrgRole is a member's standing within one organization.
type OrgRole string

const (
	RoleOwner  OrgRole = "owner"
	RoleAdmin  OrgRole = "admin"
	RoleMember OrgRole = "member"
	// RoleGuest is somebody from outside, let into one space and nothing else.
	RoleGuest OrgRole = "guest"
)

// OrgRoles lists every role, in the order the API documents them.
var OrgRoles = []OrgRole{RoleOwner, RoleAdmin, RoleMember, RoleGuest}

// CanAdminister reports whether the role may change organization settings.
func (r OrgRole) CanAdminister() bool { return r == RoleOwner || r == RoleAdmin }

// RoleSource says who decided a member's role.
type RoleSource string

const (
	// RoleSourceManual is a role somebody here chose, which the identity
	// provider leaves alone while none of the person's groups is mapped.
	RoleSourceManual RoleSource = "manual"
	// RoleSourceProvider is a role the provider's groups gave, which a later
	// sign-in takes back when the group goes.
	RoleSourceProvider RoleSource = "oidc"
)

// RoleSources lists every source, in the order the API documents them.
var RoleSources = []RoleSource{RoleSourceManual, RoleSourceProvider}

// Member is somebody in the organization, as its administrators see them.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   OrgRole   `json:"role"`
	// RoleSource says whether the role follows the identity provider's groups.
	RoleSource RoleSource `json:"roleSource"`
	JoinedAt   time.Time  `json:"joinedAt"`
	// GuestSpace is the one space a guest belongs to; null for everybody else.
	GuestSpace *SpaceRef `json:"guestSpace"`
}

// SpaceRef names a space by its id, key and name.
type SpaceRef struct {
	ID   uuid.UUID `json:"id"`
	Key  string    `json:"key"`
	Name string    `json:"name"`
}

// Locale is a language the interface speaks.
type Locale string

const (
	// LocaleBrowser is no choice: the interface follows the browser.
	LocaleBrowser Locale = ""
	LocaleEnglish Locale = "en"
	LocaleGerman  Locale = "de"
)

// Locales lists every value a person may choose, in the order the API
// documents them.
var Locales = []Locale{LocaleBrowser, LocaleEnglish, LocaleGerman}

// User is a person, global across organizations.
type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
	// Locale is the interface language the person chose, "en" or "de"; empty
	// means the browser's.
	Locale Locale `json:"locale"`
	// ShowInReaders says editors of the pages the person reads see their
	// name among its readers; they are counted either way.
	ShowInReaders bool `json:"showInReaders"`
}

// CurrentOrg is the organization a session is acting in, with the caller's
// standing there.
type CurrentOrg struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
	Role OrgRole   `json:"role"`
	// GuestSpace is where a guest lands and all they reach; null for everybody else.
	GuestSpace *SpaceRef `json:"guestSpace"`
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
	// ErrBadLocale is returned for a language the interface does not speak.
	ErrBadLocale = errors.New("choose a language from the list")
	// ErrNoSuchMember is returned when the person named is not a member here.
	ErrNoSuchMember = errors.New("that person is not a member of this organization")
	// ErrOwnerStays is returned when somebody tries to remove the owner, who
	// is how the organization is always entered again.
	ErrOwnerStays = errors.New("the owner of an organization cannot be removed")
	// ErrRemoveSelf is returned when an administrator tries to remove themselves.
	ErrRemoveSelf = errors.New("you cannot remove yourself from the organization")
)
