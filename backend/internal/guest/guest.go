// Package guest lets people from outside an organization into one of its
// spaces. A guest is a member with the role guest and that space; the
// database holds them to it, and this package only invites and removes them.
package guest

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Role is what a guest may do in their space, as an invitation names it.
type Role string

const (
	// RoleViewer reads the space.
	RoleViewer Role = "viewer"
	// RoleCommenter reads it and comments.
	RoleCommenter Role = "commenter"
	// RoleEditor reads, comments, adds and changes pages and moves them to the trash.
	RoleEditor Role = "editor"
	// RoleNone is a guest whose grants were all taken away under the space's
	// permissions; they stay a guest and reach nothing.
	RoleNone Role = "none"
)

// Roles lists every Role, for the API document.
var Roles = []Role{RoleViewer, RoleCommenter, RoleEditor, RoleNone}

// Invitable lists the roles an invitation may give.
var Invitable = []Role{RoleViewer, RoleCommenter, RoleEditor}

// Permissions are the space permissions a role grants, from view up. A guest
// never administers a space: that would show them every grant in it.
func (r Role) Permissions() []perm.SpacePermission {
	switch r {
	case RoleViewer:
		return []perm.SpacePermission{perm.SpaceView}
	case RoleCommenter:
		return []perm.SpacePermission{perm.SpaceView, perm.SpaceAddComments}
	case RoleEditor:
		return []perm.SpacePermission{perm.SpaceView, perm.SpaceAddPages, perm.SpaceAddComments, perm.SpaceDelete}
	}
	return nil
}

// RoleOf names what a guest's grants amount to, which the space's permission
// table may have changed since the invitation: the most they allow.
func RoleOf(held []perm.SpacePermission) Role {
	switch {
	case slices.Contains(held, perm.SpaceAddPages):
		return RoleEditor
	case slices.Contains(held, perm.SpaceAddComments):
		return RoleCommenter
	case len(held) > 0:
		return RoleViewer
	}
	return RoleNone
}

// Guest is somebody let into one space, as its administrators see them.
type Guest struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   Role      `json:"role"`
	// Permissions are the grants naming the guest in the space.
	Permissions []perm.SpacePermission `json:"permissions"`
	InvitedAt   time.Time              `json:"invitedAt"`
}

// InviteInput names somebody to let into a space and what they may do there.
type InviteInput struct {
	// Email is the address the guest signs in with at the organization's provider.
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

// FieldError refuses one field of an invitation, in a sentence shown beside it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

var (
	// ErrNotGuest answers for somebody who is not a guest of the space named.
	ErrNotGuest = errors.New("that person is not a guest of this space")
	// ErrPersonalSpace refuses a guest in somebody's personal space.
	ErrPersonalSpace = errors.New("a personal space belongs to its owner alone; invite the guest to another space")
)
