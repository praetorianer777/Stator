package page

import "github.com/praetorianer777/stator/backend/internal/perm"

// Restricted flags a page whose view or edit is narrowed, here or above.
type Restricted struct {
	View bool `json:"view"`
	Edit bool `json:"edit"`
}

// Restrictions are who may view and edit a page beyond the space's own
// permissions, and why: its own lists and those of the pages above it.
type Restrictions struct {
	// View and Edit are the page's own lists; empty is no restriction here.
	View []perm.Subject `json:"view"`
	Edit []perm.Subject `json:"edit"`
	// Inherited are the restricted pages above this one, the nearest last.
	Inherited []InheritedRestriction `json:"inherited"`
}

// InheritedRestriction is a restriction on a page above, which binds this one too.
type InheritedRestriction struct {
	Page Ref            `json:"page"`
	View []perm.Subject `json:"view"`
	Edit []perm.Subject `json:"edit"`
}

// RestrictionsInput replaces a page's own lists; an empty list lifts one.
type RestrictionsInput struct {
	View []perm.SubjectRef `json:"view"`
	Edit []perm.SubjectRef `json:"edit"`
}
