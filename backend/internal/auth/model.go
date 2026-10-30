package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role is a member's standing: whether they are in, as a member or as the
// guest of one space. What they may do comes from grants only.
type Role string

const (
	RoleMember Role = "member"
	// RoleGuest is somebody from outside, let into one space and nothing else.
	RoleGuest Role = "guest"
	// RoleAdmin is no standing: letting somebody in as admin makes them a
	// member and grants them the built-in administrator role besides.
	RoleAdmin Role = "admin"
)

// Roles lists every standing, in the order the API documents them.
var Roles = []Role{RoleMember, RoleGuest}

// JoinRoles lists what letting somebody in may make of them.
var JoinRoles = []Role{RoleMember, RoleAdmin}

// AdministratorRole is the key of the built-in role that administers the
// installation, which letting somebody in as admin grants.
const AdministratorRole = "administrator"

// Member is somebody in the installation, as its administrators see them.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	// Role is the standing: member, or guest.
	Role Role `json:"role"`
	// Active is false for an account somebody switched off.
	Active   bool      `json:"active"`
	JoinedAt time.Time `json:"joinedAt"`
	// GuestSpace is the one space a guest belongs to; null for everybody else.
	GuestSpace *SpaceRef `json:"guestSpace"`
	// Roles are what the person holds across the installation, by name or
	// through a group.
	Roles []MemberRole `json:"roles"`
}

// MemberRole is a role somebody holds across the installation, and the group
// it comes through, if any.
type MemberRole struct {
	Role string `json:"role"`
	Name string `json:"name"`
	// GroupID and GroupName name the group the grant is to; null for one by name.
	GroupID   *uuid.UUID `json:"groupId"`
	GroupName *string    `json:"groupName"`
	// GroupBuiltin marks Everyone, "everyone"; null for any other group.
	GroupBuiltin *string `json:"groupBuiltin"`
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
	// Timezone is the IANA zone the person's dates are written in, such as
	// Europe/Berlin; empty means the browser's.
	Timezone string `json:"timezone"`
	// ShowInReaders says editors of the pages the person reads see their
	// name among its readers; they are counted either way.
	ShowInReaders bool `json:"showInReaders"`
}

// Standing is the organization's name and the caller's place in it.
type Standing struct {
	Name string `json:"name"`
	// Role is the standing, member or guest; what the caller may do is /access/me's.
	Role Role `json:"role"`
	// Administers says a grant lets the caller administer the installation,
	// so the web app draws its menus before /access/me answers.
	Administers bool `json:"administers"`
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
	// ErrBadTimezone is returned for a zone the database of zones does not know.
	ErrBadTimezone = errors.New("choose a time zone from the list")
	// ErrNoSuchMember is returned when the person named is not a member here.
	ErrNoSuchMember = errors.New("that person is not a member of this installation")
	// ErrRemoveSelf is returned when an administrator tries to remove themselves.
	ErrRemoveSelf = errors.New("you cannot remove yourself from the installation")
	// ErrSwitchSelfOff is returned when an administrator tries to switch their own account off.
	ErrSwitchSelfOff = errors.New("you cannot switch your own account off")
)
