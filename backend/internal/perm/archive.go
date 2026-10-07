package perm

// Archived says whether a page is archived, and by what: an archived page
// stays readable and nothing on it changes until it is unarchived.
type Archived string

const (
	NotArchived Archived = ""
	// ArchivedPage is the page archived, alone or with a page above it.
	ArchivedPage Archived = "page"
	// ArchivedSpace is the page's whole space archived.
	ArchivedSpace Archived = "space"
)

// Frozen takes away every right that would change an archived page.
func (a PageAccess) Frozen() PageAccess {
	if a.Archived != NotArchived {
		a.Edit, a.Delete, a.Comment, a.Add, a.GrantEdit = false, false, false, false, false
	}
	return a
}

// Refuse is the refusal of an action on a page: its being archived when it
// is, a missing permission otherwise.
func Refuse(action Action, archived Archived) error {
	return &DeniedError{Action: action, Archived: archived}
}
