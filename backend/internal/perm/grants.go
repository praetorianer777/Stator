package perm

import "github.com/google/uuid"

// SubjectType says whom a grant or a restriction names.
type SubjectType string

const (
	SubjectUser  SubjectType = "user"
	SubjectGroup SubjectType = "group"
)

// SubjectTypes lists every SubjectType, for the API document.
var SubjectTypes = []SubjectType{SubjectUser, SubjectGroup}

// Subject is somebody a permission names, as a list shows them.
type Subject struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's.
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
}

// SubjectRef names a subject in a request.
type SubjectRef struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's.
	ID *uuid.UUID `json:"id,omitempty"`
}

// GlobalPermission is something a grant across the whole installation holds.
type GlobalPermission string

const (
	// UseStator is reading anything at all in the installation.
	UseStator GlobalPermission = "use"
	// CreateSpaces is making new spaces.
	CreateSpaces GlobalPermission = "createSpace"
	// AdministerSite is the installation's settings, people and access.
	AdministerSite GlobalPermission = "administerSite"
)

// GlobalPermissions lists every GlobalPermission, for the API document.
var GlobalPermissions = []GlobalPermission{UseStator, CreateSpaces, AdministerSite}

// GlobalCan is what the caller may do across the organization.
type GlobalCan struct {
	Use         bool `json:"use"`
	CreateSpace bool `json:"createSpace"`
	Administer  bool `json:"administer"`
}

// SpacePermission is something granted in one space.
type SpacePermission string

const (
	// SpaceView is reading the space and its pages.
	SpaceView SpacePermission = "view"
	// SpaceAddPages is adding, editing, moving and copying pages, and
	// attaching files.
	SpaceAddPages SpacePermission = "addPages"
	// SpaceAddComments is commenting on pages.
	SpaceAddComments SpacePermission = "addComments"
	// SpaceDelete is moving pages to the trash and back, and deleting
	// attachments and comments of others.
	SpaceDelete SpacePermission = "delete"
	// SpaceAdminister is the space's settings, permissions, trash and deletion.
	SpaceAdminister SpacePermission = "administer"
)

// SpacePermissions lists every SpacePermission, for the API document.
var SpacePermissions = []SpacePermission{SpaceView, SpaceAddPages, SpaceAddComments, SpaceDelete, SpaceAdminister}

// PageCan is what the caller may do to one page, its restrictions included.
type PageCan struct {
	Edit     bool `json:"edit"`
	Delete   bool `json:"delete"`
	Restrict bool `json:"restrict"`
	Comment  bool `json:"comment"`
	// Add is adding pages below this one, and moving or copying it, which
	// takes add pages in the space besides editing the page.
	Add bool `json:"add"`
	// GrantEdit is changing who else may edit the page and the pages below
	// it, which the space's administrators may.
	GrantEdit bool `json:"grantEdit"`
	// Archive is archiving the page with the pages below it, or unarchiving it.
	Archive bool `json:"archive"`
}

// Person is a member of the organization, as a picker offers them.
type Person struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Mentionable is a person the mention picker offers, and whether they may
// view the page once it is published; only those who may are told.
type Mentionable struct {
	Person
	CanView bool `json:"canView"`
}

// Group is a named set of people, as a picker and the Groups tab show it.
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Source says who keeps the members: "local" by hand, "oidc" the
	// identity provider, whose memberships every sign-in replaces.
	Source string `json:"source"`
	// ExternalRef is the name in the provider's groups claim that fills the
	// group; empty for one kept by hand.
	ExternalRef string `json:"externalRef"`
	// Builtin marks a group the installation keeps itself: "everyone" holds
	// every member. Its name and members are not edited.
	Builtin     string `json:"builtin,omitempty"`
	MemberCount int    `json:"memberCount"`
	// RoleCount is how many grants name the group.
	RoleCount int `json:"roleCount"`
	// FromProvider groups mirror the identity provider's.
	FromProvider bool `json:"fromProvider"`
	// Members are the people in it, when one group is read.
	Members []Person `json:"members,omitempty"`
}
