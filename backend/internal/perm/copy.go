package perm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Copying one space's permissions onto another (#82). The plan is a plain
// function of both tables, so the preview and the copy that applies it are
// computed by the same code and can be tested alone.

// CopyMode says what a copy does with the grants the target has already.
type CopyMode string

const (
	// CopyReplace makes the target's grants the source's.
	CopyReplace CopyMode = "replace"
	// CopyMerge adds what the source grants that the target does not, and
	// takes nothing away.
	CopyMerge CopyMode = "merge"
)

// CopyModes lists every CopyMode, for the API document.
var CopyModes = []CopyMode{CopyReplace, CopyMerge}

// SubjectAnonymous is anybody who is not signed in, granted view on a space
// open to them; it is copied with the rest but never shown in the grid.
const SubjectAnonymous SubjectType = "anonymous"

// AnonymousName is how an answer names the anonymous subject.
const AnonymousName = "Anybody"

// CopySubjectTypes lists whom a copy may name, for the API document.
var CopySubjectTypes = []SubjectType{SubjectUser, SubjectGroup, SubjectEveryone, SubjectAnonymous}

// CopySubject is whom a grant of a copy names.
type CopySubject struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's, null for everyone and anybody.
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
	// Guest marks a person from outside, let into the one space whose grant
	// names them.
	Guest bool `json:"guest"`
}

// Key tells two subjects apart.
func (s CopySubject) Key() string {
	if s.ID == nil {
		return string(s.Type)
	}
	return string(s.Type) + ":" + s.ID.String()
}

// CopyGrant is what one subject holds in a space.
type CopyGrant struct {
	Subject     CopySubject       `json:"subject"`
	Permissions []SpacePermission `json:"permissions"`
}

// CopyChangeKind says how a copy changes what one subject holds.
type CopyChangeKind string

const (
	// CopyAdded is a subject the target did not name.
	CopyAdded CopyChangeKind = "added"
	// CopyWidened is a subject given more and nothing taken.
	CopyWidened CopyChangeKind = "widened"
	// CopyNarrowed is a subject losing some of what they hold.
	CopyNarrowed CopyChangeKind = "narrowed"
	// CopyChanged is a subject given some and losing some.
	CopyChanged CopyChangeKind = "changed"
	// CopyRemoved is a subject losing everything in the target.
	CopyRemoved CopyChangeKind = "removed"
)

// CopyChangeKinds lists every CopyChangeKind, for the API document.
var CopyChangeKinds = []CopyChangeKind{CopyAdded, CopyWidened, CopyNarrowed, CopyChanged, CopyRemoved}

// CopyChange is what one subject holds in the target before and after.
type CopyChange struct {
	Subject CopySubject       `json:"subject"`
	Kind    CopyChangeKind    `json:"kind"`
	Before  []SpacePermission `json:"before"`
	After   []SpacePermission `json:"after"`
}

// CopySkipReason says why a grant of the source is not copied.
type CopySkipReason string

// CopySkipGuest is a guest of the source, who belongs to that space alone.
const CopySkipGuest CopySkipReason = "guest"

// CopySkipReasons lists every CopySkipReason, for the API document.
var CopySkipReasons = []CopySkipReason{CopySkipGuest}

// CopySkip is a grant of the source that the copy leaves behind, and why.
type CopySkip struct {
	Subject     CopySubject       `json:"subject"`
	Permissions []SpacePermission `json:"permissions"`
	Reason      CopySkipReason    `json:"reason"`
}

// CopyCounts sums a copy up, as its audit entry keeps it.
type CopyCounts struct {
	Added     int `json:"added"`
	Widened   int `json:"widened"`
	Narrowed  int `json:"narrowed"`
	Changed   int `json:"changed"`
	Removed   int `json:"removed"`
	Skipped   int `json:"skipped"`
	Unchanged int `json:"unchanged"`
}

// CopyPlan is what a copy would make of the target's grants.
type CopyPlan struct {
	// Result is the target's grants after the copy.
	Result  []CopyGrant
	Changes []CopyChange
	Skipped []CopySkip
	// Kept are the target's own guests, whom a replace leaves as they are:
	// the source cannot name them, and they come and go by invitation.
	Kept   []CopyGrant
	Counts CopyCounts
	// LeavesNoAdministrator says the target names an administrator now and
	// would name none after, which a copy never does.
	LeavesNoAdministrator bool
}

// PlanCopy works out what copying source's grants onto target in mode does.
// A guest of the source is never copied.
func PlanCopy(source, target []CopyGrant, mode CopyMode) CopyPlan {
	plan := CopyPlan{Result: []CopyGrant{}, Changes: []CopyChange{}, Skipped: []CopySkip{}, Kept: []CopyGrant{}}
	before := map[string]CopyGrant{}
	for _, g := range target {
		before[g.Subject.Key()] = g
	}
	after := map[string]CopyGrant{}
	var order []string
	put := func(g CopyGrant) {
		key := g.Subject.Key()
		if _, ok := after[key]; !ok {
			order = append(order, key)
		}
		after[key] = g
	}
	for _, g := range target {
		switch {
		case mode == CopyMerge:
			put(g)
		case g.Subject.Guest:
			put(g)
			plan.Kept = append(plan.Kept, g)
		}
	}
	for _, g := range source {
		if g.Subject.Guest {
			plan.Skipped = append(plan.Skipped, CopySkip{Subject: g.Subject, Permissions: Ordered(g.Permissions), Reason: CopySkipGuest})
			continue
		}
		held := g.Permissions
		if was, ok := after[g.Subject.Key()]; ok {
			held = append(slices.Clone(was.Permissions), held...)
		}
		put(CopyGrant{Subject: g.Subject, Permissions: Ordered(held)})
	}
	for _, key := range order {
		now := after[key]
		plan.Result = append(plan.Result, now)
		was, ok := before[key]
		if !ok {
			plan.Changes = append(plan.Changes, CopyChange{Subject: now.Subject, Kind: CopyAdded, Before: []SpacePermission{}, After: Ordered(now.Permissions)})
			continue
		}
		gained, lost := difference(now.Permissions, was.Permissions), difference(was.Permissions, now.Permissions)
		var kind CopyChangeKind
		switch {
		case len(gained) > 0 && len(lost) > 0:
			kind = CopyChanged
		case len(gained) > 0:
			kind = CopyWidened
		case len(lost) > 0:
			kind = CopyNarrowed
		default:
			plan.Counts.Unchanged++
			continue
		}
		plan.Changes = append(plan.Changes, CopyChange{Subject: now.Subject, Kind: kind, Before: Ordered(was.Permissions), After: Ordered(now.Permissions)})
	}
	for _, g := range target {
		if _, ok := after[g.Subject.Key()]; !ok {
			plan.Changes = append(plan.Changes, CopyChange{Subject: g.Subject, Kind: CopyRemoved, Before: Ordered(g.Permissions), After: []SpacePermission{}})
		}
	}
	for _, c := range plan.Changes {
		switch c.Kind {
		case CopyAdded:
			plan.Counts.Added++
		case CopyWidened:
			plan.Counts.Widened++
		case CopyNarrowed:
			plan.Counts.Narrowed++
		case CopyChanged:
			plan.Counts.Changed++
		case CopyRemoved:
			plan.Counts.Removed++
		}
	}
	plan.Counts.Skipped = len(plan.Skipped)
	plan.LeavesNoAdministrator = administered(target) && !administered(plan.Result)
	return plan
}

func administered(grants []CopyGrant) bool {
	return slices.ContainsFunc(grants, func(g CopyGrant) bool { return slices.Contains(g.Permissions, SpaceAdminister) })
}

func difference(a, b []SpacePermission) []SpacePermission {
	var out []SpacePermission
	for _, p := range a {
		if !slices.Contains(b, p) {
			out = append(out, p)
		}
	}
	return out
}

// Ordered is a set of permissions without repeats, from view to administer.
func Ordered(ps []SpacePermission) []SpacePermission {
	out := make([]SpacePermission, 0, len(ps))
	for _, p := range SpacePermissions {
		if slices.Contains(ps, p) {
			out = append(out, p)
		}
	}
	return out
}

// CopyFingerprint names the mode and the two tables a plan was computed
// from, so a copy applies only the plan its caller saw.
func CopyFingerprint(mode CopyMode, source, target uuid.UUID, sourceGrants, targetGrants []CopyGrant) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", mode, source, target)
	for _, side := range [][]CopyGrant{sourceGrants, targetGrants} {
		lines := make([]string, 0, len(side))
		for _, g := range side {
			perms := make([]string, 0, len(g.Permissions))
			for _, p := range Ordered(g.Permissions) {
				perms = append(perms, string(p))
			}
			lines = append(lines, fmt.Sprintf("%s|%t|%s", g.Subject.Key(), g.Subject.Guest, strings.Join(perms, ",")))
		}
		slices.Sort(lines)
		fmt.Fprintf(h, "%d\n%s\n", len(lines), strings.Join(lines, "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
