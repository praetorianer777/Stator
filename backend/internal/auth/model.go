package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role is a member's standing on the site.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	// RoleGuest is somebody from outside, let into one space and nothing else.
	RoleGuest Role = "guest"
)

// Roles lists every role, in the order the API documents them.
var Roles = []Role{RoleOwner, RoleAdmin, RoleMember, RoleGuest}

// CanAdminister reports whether the role may change the site's settings.
func (r Role) CanAdminister() bool { return r == RoleOwner || r == RoleAdmin }

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
	Role   Role      `json:"role"`
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

// Standing is the organization's name and the caller's place in it.
type Standing struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
	// GuestSpace is where a guest lands and all they reach; null for everybody else.
	GuestSpace *SpaceRef `json:"guestSpace"`
}

// Admitted is somebody let in from the list of who waits, with their role.
type Admitted struct {
	UserID uuid.UUID `json:"userId"`
	Role   Role      `json:"role"`
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
