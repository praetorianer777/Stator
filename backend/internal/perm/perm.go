// Package perm decides who may do what to spaces and pages. Every service asks
// it, inside the transaction that acts, and nothing else decides; the database
// holds raw SQL to the same rules through the functions its policies call.
package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	// AdministerSpace changes a space's details and its permissions.
	AdministerSpace Action = "space.administer"
	// DeleteSpace removes a space with every page in it.
	DeleteSpace Action = "space.delete"
	// EditPages adds, changes, moves and copies pages, and restricts them.
	EditPages Action = "page.edit"
	// DeletePages moves pages to the trash and back.
	DeletePages Action = "page.delete"
	// AddComments comments on pages.
	AddComments Action = "page.comment"
	// PurgeTrash deletes trashed pages for good.
	PurgeTrash Action = "trash.purge"
	// InspectAccess shows what somebody may do to a page of the space, and why.
	InspectAccess Action = "space.inspect"
	// ArchivePages archives pages of the space and unarchives them.
	ArchivePages Action = "page.archive"
	// ArchiveSpace archives the whole space and unarchives it.
	ArchiveSpace Action = "space.archive"
	// ReviewStale reads which pages of the space nobody opened or published
	// for a while.
	ReviewStale Action = "space.review"
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
// Archived names what is archived when that, not a permission, is the reason.
type DeniedError struct {
	Action   Action
	Archived Archived
}

func (e *DeniedError) Error() string {
	switch e.Archived {
	case ArchivedPage:
		return "This page is archived, so nothing on it changes. Ask an administrator of the space to unarchive it first."
	case ArchivedSpace:
		return "This space is archived, so nothing in it changes. Ask an administrator of the space to unarchive the space first."
	}
	switch e.Action {
	case CreateSpace:
		return "You may not create spaces. Ask an administrator of the organization to let you, or to make the space for you."
	case AdministerSpace:
		return "Only an administrator of this space can change its details and permissions. Ask one of them."
	case DeleteSpace:
		return "Only an administrator of this space can delete it. Ask one of them."
	case PurgeTrash:
		return "Only an administrator of this space can delete pages for good. Restore the page instead, or ask one of them."
	case EditPages:
		return "You may read this page but not change it. Ask an administrator of the space for access."
	case DeletePages:
		return "You may not move pages of this space to the trash or back. Ask an administrator of the space for access."
	case AddComments:
		return "You may not comment in this space. Ask an administrator of the space for access."
	case InspectAccess:
		return "Only an administrator of this space can check what somebody may do here. Ask one of them to check it for you."
	case ArchivePages:
		return "Only an administrator of this space can archive pages and unarchive them. Ask one of them."
	case ArchiveSpace:
		return "Only an administrator of this space can archive it and unarchive it. Ask one of them."
	case ReviewStale:
		return "Only administrators of a space, or of the organization, can read which of its pages went stale. Ask one of them to check the space."
	}
	return "You do not have permission to do that. Ask an administrator of the organization."
}

func (e *DeniedError) Is(target error) bool { return target == ErrDenied }

// Check refuses the action with a DeniedError unless the actor may take it.
// space is the space it concerns, or uuid.Nil for CreateSpace. It runs in the
// caller's transaction, so the rules read what it sees.
func Check(ctx context.Context, tx db.DBTX, actor Actor, action Action, space uuid.UUID) error {
	f, err := LoadFacts(ctx, tx, actor, space)
	if err != nil {
		return err
	}
	if !Decide(f, action) {
		return &DeniedError{Action: action}
	}
	return nil
}

// Allowed is Check as a yes or no, for telling the interface what to offer. A
// failed lookup answers no.
func Allowed(ctx context.Context, tx db.DBTX, actor Actor, action Action, space uuid.UUID) bool {
	return Check(ctx, tx, actor, action, space) == nil
}

// Can is what the interface offers on a space, answered by the same rules.
type Can struct {
	EditPages  bool `json:"editPages"`
	Administer bool `json:"administer"`
	Delete     bool `json:"delete"`
	PurgeTrash bool `json:"purgeTrash"`
	// AddComments and DeletePages are the space permissions of the same
	// names; DeletePages moves pages to the trash and back.
	AddComments bool `json:"addComments"`
	DeletePages bool `json:"deletePages"`
}

// On says what the actor may do in a space.
func On(ctx context.Context, tx db.DBTX, actor Actor, space uuid.UUID) (Can, error) {
	f, err := LoadFacts(ctx, tx, actor, space)
	if err != nil {
		return Can{}, err
	}
	return f.Can(), nil
}

// LoadFacts reads what the database grants the actor in the organization and
// in one space, uuid.Nil for none.
func LoadFacts(ctx context.Context, tx db.DBTX, actor Actor, space uuid.UUID) (Facts, error) {
	if actor.UserID == uuid.Nil {
		return Facts{}, nil
	}
	var (
		role   *string
		global []string
		held   []string
	)
	err := tx.QueryRow(ctx, `
		SELECT (SELECT org_role FROM org_member WHERE org_id = current_org_id() AND user_id = $1),
		       perm_global_grants($1), perm_space_grants($1, $2)`, actor.UserID, space).Scan(&role, &global, &held)
	if err != nil {
		return Facts{}, fmt.Errorf("read permissions: %w", err)
	}
	if role == nil {
		return Facts{}, nil
	}
	f := Facts{Member: true, Role: auth.OrgRole(*role)}
	for _, g := range global {
		f.Global = append(f.Global, GlobalPermission(g))
	}
	for _, s := range held {
		f.Space = append(f.Space, SpacePermission(s))
	}
	return f, nil
}

// Global says what the actor may do across the organization.
func Global(ctx context.Context, tx db.DBTX, actor Actor) (GlobalCan, error) {
	f, err := LoadFacts(ctx, tx, actor, uuid.Nil)
	if err != nil {
		return GlobalCan{}, err
	}
	return f.GlobalCan(), nil
}

// ForPage says what the actor may do to one page, its restrictions and those
// above it included, and which space it is in. A page that does not exist is
// one nobody may view.
func ForPage(ctx context.Context, tx db.DBTX, actor Actor, page uuid.UUID) (PageAccess, uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, space_id, hidden, view_listed, on_view_list, edit_listed, on_edit_list
		FROM perm_page_lists($1, $2)`, page, actor.UserID)
	if err != nil {
		return PageAccess{}, uuid.Nil, err
	}
	var space uuid.UUID
	chain, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChainLink, error) {
		var l ChainLink
		err := row.Scan(&l.PageID, &space, &l.HiddenDraft, &l.ViewListed, &l.OnViewList, &l.EditListed, &l.OnEditList)
		return l, err
	})
	if err != nil {
		return PageAccess{}, uuid.Nil, err
	}
	if len(chain) == 0 {
		return PageAccess{}, uuid.Nil, nil
	}
	f, err := LoadFacts(ctx, tx, actor, space)
	if err != nil {
		return PageAccess{}, uuid.Nil, err
	}
	a := PageRules(f, chain)
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT CASE WHEN s.archived_at IS NOT NULL THEN 'space' WHEN p.archived_at IS NOT NULL THEN 'page' ELSE '' END
		                 FROM page p JOIN space s ON s.id = p.space_id WHERE p.id = $1), '')`, page).Scan(&a.Archived); err != nil {
		return PageAccess{}, uuid.Nil, fmt.Errorf("read whether the page is archived: %w", err)
	}
	return a.Frozen(), space, nil
}
