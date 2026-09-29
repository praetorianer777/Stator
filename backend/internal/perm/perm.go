// Package perm decides who may do what to spaces and pages. Every service asks
// it, inside the transaction that acts, and nothing else decides; space and
// page permissions (#19) replace the rules here without touching the callers.
package perm

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// Action is something a person asks to do.
type Action string

const (
	// CreateSpace makes a new space in the organization.
	CreateSpace Action = "space.create"
	// ViewSpace reads a space and its pages.
	ViewSpace Action = "space.view"
	// AdministerSpace renames and describes a space.
	AdministerSpace Action = "space.administer"
	// DeleteSpace removes a space with every page in it.
	DeleteSpace Action = "space.delete"
	// EditPages adds and changes pages.
	EditPages Action = "page.edit"
)

// Actor is who asks: a person and their standing in the organization the
// request acts in.
type Actor struct {
	UserID uuid.UUID
	Role   auth.OrgRole
}

// ActorOf is the caller behind a request.
func ActorOf(p *auth.Principal) Actor {
	if p == nil {
		return Actor{}
	}
	return Actor{UserID: p.UserID, Role: p.Role}
}

// ErrDenied is what every refusal wraps.
var ErrDenied = errors.New("permission denied")

// DeniedError says, as a sentence, what the person may not do and whom to ask.
type DeniedError struct{ Action Action }

func (e *DeniedError) Error() string {
	switch e.Action {
	case CreateSpace:
		return "Only an administrator of the organization can create spaces. Ask one of them to make it for you."
	case AdministerSpace:
		return "Only an administrator of the organization can change a space's details. Ask one of them."
	case DeleteSpace:
		return "Only an administrator of the organization can delete a space. Ask one of them."
	case EditPages:
		return "You may read this space but not change its pages. Ask an administrator for access."
	}
	return "You do not have permission to do that. Ask an administrator of the organization."
}

func (e *DeniedError) Is(target error) bool { return target == ErrDenied }

// Check refuses the action with a DeniedError unless the actor may take it.
// space is the space it concerns, or uuid.Nil for CreateSpace. It runs in the
// caller's transaction, so rules that read the database see what it sees.
func Check(ctx context.Context, tx db.DBTX, actor Actor, action Action, space uuid.UUID) error {
	if allowed(actor, action) {
		return nil
	}
	return &DeniedError{Action: action}
}

// Allowed is Check as a yes or no, for telling the interface what to offer.
func Allowed(ctx context.Context, tx db.DBTX, actor Actor, action Action, space uuid.UUID) bool {
	return Check(ctx, tx, actor, action, space) == nil
}

// Until #19 every member views and edits every space of their organization,
// and its owners and administrators alone make, change, delete and purge.
func allowed(actor Actor, action Action) bool {
	if actor.UserID == uuid.Nil || actor.Role == "" {
		return false
	}
	switch action {
	case ViewSpace, EditPages:
		return true
	case CreateSpace, AdministerSpace, DeleteSpace:
		return actor.Role.CanAdminister()
	}
	return false
}

// Can is what the interface offers on a space, answered by the same rules.
type Can struct {
	EditPages  bool `json:"editPages"`
	Administer bool `json:"administer"`
	Delete     bool `json:"delete"`
}

// On says what the actor may do in a space.
func On(ctx context.Context, tx db.DBTX, actor Actor, space uuid.UUID) Can {
	return Can{
		EditPages:  Allowed(ctx, tx, actor, EditPages, space),
		Administer: Allowed(ctx, tx, actor, AdministerSpace, space),
		Delete:     Allowed(ctx, tx, actor, DeleteSpace, space),
	}
}
