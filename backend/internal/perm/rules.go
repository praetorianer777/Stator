package perm

import (
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// The rules, as plain functions of what the database says, so each can be
// read and tested alone. The SQL functions of migration 00090 are the same
// rules for the policies; the integration suite holds the two to each other.

// Facts are what one person is granted in the organization and in a space.
type Facts struct {
	// Member is false for somebody who is not in the organization at all.
	Member bool
	Role   auth.OrgRole
	// Global and Space are the grants as stored, before any implication.
	Global []GlobalPermission
	Space  []SpacePermission
	// SpacesOnly says the person acts with a token limited to spaces, which
	// reaches nothing of the organization as a whole. Facts about a space it
	// does not reach are empty.
	SpacesOnly bool
}

// roleAdmin reports whether the person is an owner or administrator, who
// holds every permission in every space so nothing can be orphaned.
func (f Facts) roleAdmin() bool { return f.Member && f.Role.CanAdminister() }

// OrgAdmin reports whether the person administers the organization itself,
// which a token limited to spaces never does.
func (f Facts) OrgAdmin() bool { return f.roleAdmin() && !f.SpacesOnly }

// HoldsGlobal reports whether a global permission applies. Without use a
// person holds nothing, administer follows the roles alone, and a token
// limited to spaces holds use and nothing more.
func (f Facts) HoldsGlobal(p GlobalPermission) bool {
	switch {
	case f.SpacesOnly && p != UseStator:
		return false
	case f.roleAdmin():
		return true
	case !f.Member, p == AdministerOrg:
		return false
	}
	return slices.Contains(f.Global, UseStator) && slices.Contains(f.Global, p)
}

// HoldsSpace reports whether a space permission applies: every one implies
// view, and administer implies all.
func (f Facts) HoldsSpace(p SpacePermission) bool {
	if f.roleAdmin() {
		return true
	}
	if !f.HoldsGlobal(UseStator) {
		return false
	}
	for _, g := range f.Space {
		if g == p || g == SpaceAdminister || p == SpaceView {
			return true
		}
	}
	return false
}

// Decide answers whether the facts allow an action on a space.
func Decide(f Facts, action Action) bool {
	switch action {
	case CreateSpace:
		return f.HoldsGlobal(CreateSpaces)
	case CreatePersonalSpace:
		return f.HoldsGlobal(UseStator)
	case ViewSpace:
		return f.HoldsSpace(SpaceView)
	case EditPages:
		return f.HoldsSpace(SpaceAddPages)
	case AddComments:
		return f.HoldsSpace(SpaceAddComments)
	case DeletePages:
		return f.HoldsSpace(SpaceDelete)
	case AdministerSpace, DeleteSpace, PurgeTrash, InspectAccess, ArchivePages, ArchiveSpace, ReviewStale, ManageShortcuts:
		return f.HoldsSpace(SpaceAdminister)
	}
	return false
}

// Can is what the facts offer on a space.
func (f Facts) Can() Can {
	return Can{
		EditPages:   Decide(f, EditPages),
		Administer:  Decide(f, AdministerSpace),
		Delete:      Decide(f, DeleteSpace),
		PurgeTrash:  Decide(f, PurgeTrash),
		AddComments: Decide(f, AddComments),
		DeletePages: Decide(f, DeletePages),
	}
}

// GlobalCan is what the facts offer across the organization.
func (f Facts) GlobalCan() GlobalCan {
	return GlobalCan{
		Use:         f.HoldsGlobal(UseStator),
		CreateSpace: f.HoldsGlobal(CreateSpaces),
		Administer:  f.HoldsGlobal(AdministerOrg),
	}
}

// ChainLink is one page on the way from a page up to its home page, as far
// as its restrictions go for one person.
type ChainLink struct {
	PageID uuid.UUID
	// HiddenDraft is a page nobody has published yet, made by somebody else.
	HiddenDraft bool
	// ViewListed says the page has a view list; OnViewList, that the person
	// is on it. The same for edit.
	ViewListed, OnViewList bool
	EditListed, OnEditList bool
}

// PageAccess is what one person may do to one page.
type PageAccess struct {
	View, Edit, Delete, Comment bool
	// Archive is archiving the page and unarchiving it, which its space's
	// administrators may; Archived says whether it is, and how.
	Archive  bool
	Archived Archived
	// ViewRestricted and EditRestricted say whether any list of that kind
	// applies, whether or not the person passes it.
	ViewRestricted, EditRestricted bool
}

// Can is the access as the interface is told it.
func (a PageAccess) Can() PageCan {
	return PageCan{Edit: a.Edit, Delete: a.Delete, Restrict: a.Edit, Comment: a.Comment,
		Archive: a.Archive && a.Archived != ArchivedSpace}
}

// PageRules decides a page from the facts of its space and its chain, the
// page first. A person must pass every list on the page and above it;
// administrators of the space pass them all, but see nobody's unpublished page.
func PageRules(f Facts, chain []ChainLink) PageAccess {
	var a PageAccess
	if len(chain) == 0 {
		return a
	}
	viewLists, editLists := true, true
	hidden := false
	for _, l := range chain {
		hidden = hidden || l.HiddenDraft
		a.ViewRestricted = a.ViewRestricted || l.ViewListed
		a.EditRestricted = a.EditRestricted || l.EditListed
		viewLists = viewLists && (!l.ViewListed || l.OnViewList)
		editLists = editLists && (!l.EditListed || l.OnEditList)
	}
	if f.HoldsSpace(SpaceAdminister) {
		viewLists, editLists = true, true
	}
	a.View = !hidden && f.HoldsSpace(SpaceView) && viewLists
	a.Edit = a.View && f.HoldsSpace(SpaceAddPages) && editLists
	a.Delete = a.View && f.HoldsSpace(SpaceDelete) && editLists
	a.Comment = a.View && f.HoldsSpace(SpaceAddComments)
	a.Archive = a.View && f.HoldsSpace(SpaceAdminister)
	return a
}

// LocksOut reports whether restrictions just saved leave their saver unable
// to view or edit the page, which only an administrator of the space may do.
func LocksOut(administers bool, after PageAccess) bool {
	return !administers && !(after.View && after.Edit)
}
