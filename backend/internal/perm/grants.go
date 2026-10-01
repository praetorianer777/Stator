package perm

import "github.com/google/uuid"

// SubjectType says whom a grant or a restriction names.
type SubjectType string

const (
	SubjectUser  SubjectType = "user"
	SubjectGroup SubjectType = "group"
	// SubjectEveryone is every member of the organization who may use it.
	SubjectEveryone SubjectType = "everyone"
)

// SubjectTypes lists every SubjectType, for the API document.
var SubjectTypes = []SubjectType{SubjectUser, SubjectGroup, SubjectEveryone}

// Subject is somebody a permission names, as a list shows them.
type Subject struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's, null for everyone.
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
}

// SubjectRef names a subject in a request.
type SubjectRef struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's, left out for everyone.
	ID *uuid.UUID `json:"id,omitempty"`
}

// GlobalPermission is something granted across the whole organization.
type GlobalPermission string

const (
	// UseStator is reading anything at all in the organization.
	UseStator GlobalPermission = "use"
	// CreateSpaces is making new spaces.
	CreateSpaces GlobalPermission = "createSpace"
	// AdministerOrg is the organization's settings, people and permissions.
	AdministerOrg GlobalPermission = "administer"
)

// GlobalPermissions lists every GlobalPermission, for the API document.
var GlobalPermissions = []GlobalPermission{UseStator, CreateSpaces, AdministerOrg}

// GlobalGrant is one global permission and whom it is granted to.
type GlobalGrant struct {
	Permission GlobalPermission `json:"permission"`
	Subjects   []Subject        `json:"subjects"`
	// Fixed grants follow the organization's roles and are changed under
	// Users rather than here.
	Fixed bool `json:"fixed"`
}

// GlobalGrantInput replaces whom a global permission is granted to.
type GlobalGrantInput struct {
	Subjects []SubjectRef `json:"subjects"`
}

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

// SpaceGrant is what one subject may do in a space.
type SpaceGrant struct {
	Subject     Subject           `json:"subject"`
	Permissions []SpacePermission `json:"permissions"`
}

// SpaceGrantInput is one row of the space's permission table.
type SpaceGrantInput struct {
	Subject     SubjectRef        `json:"subject"`
	Permissions []SpacePermission `json:"permissions"`
}

// SpaceGrantsInput replaces a space's whole permission table.
type SpaceGrantsInput struct {
	Grants []SpaceGrantInput `json:"grants"`
}

// PageCan is what the caller may do to one page, its restrictions included.
type PageCan struct {
	Edit     bool `json:"edit"`
	Delete   bool `json:"delete"`
	Restrict bool `json:"restrict"`
	Comment  bool `json:"comment"`
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

// Group is a group of the organization, as a picker offers it.
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	MemberCount int       `json:"memberCount"`
	// FromProvider groups mirror the identity provider's.
	FromProvider bool `json:"fromProvider"`
}
