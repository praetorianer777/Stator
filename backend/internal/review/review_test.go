package review

import (
	"testing"

	"github.com/google/uuid"
)

func TestTheNearestBranchRuleHoldsAPage(t *testing.T) {
	page, parent, top, home := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	chain := []uuid.UUID{page, parent, top, home}
	space := &Rule{ID: uuid.New(), Required: 1}
	atTop := &Rule{ID: uuid.New(), Required: 2}
	atParent := &Rule{ID: uuid.New(), Required: 3}
	atPage := &Rule{ID: uuid.New(), Required: 4}

	for _, c := range []struct {
		what     string
		branches map[uuid.UUID]*Rule
		space    *Rule
		want     *Rule
	}{
		{"no rule anywhere", nil, nil, nil},
		{"the space's alone", nil, space, space},
		{"a branch above in place of the space's", map[uuid.UUID]*Rule{top: atTop}, space, atTop},
		{"the nearer of two branches", map[uuid.UUID]*Rule{top: atTop, parent: atParent}, space, atParent},
		{"the page's own", map[uuid.UUID]*Rule{top: atTop, page: atPage}, space, atPage},
		{"a branch without a space rule", map[uuid.UUID]*Rule{home: atTop}, nil, atTop},
		{"a branch elsewhere holds nothing here", map[uuid.UUID]*Rule{uuid.New(): atTop}, nil, nil},
	} {
		if got := Nearest(chain, c.branches, c.space); got != c.want {
			t.Errorf("%s: got %v, want %v", c.what, got, c.want)
		}
	}
	// A post has no branch: only the space's rule holds it.
	if got := Nearest(nil, map[uuid.UUID]*Rule{page: atPage}, space); got != space {
		t.Errorf("a post is held by %v", got)
	}
}

func TestDecisionsAreCountedByEachReviewersLastWord(t *testing.T) {
	approve := DecisionEntry{Decision: DecisionApprove}
	changes := DecisionEntry{Decision: DecisionChanges, Comment: "Fix the dates"}
	for _, c := range []struct {
		what      string
		decisions []DecisionEntry
		required  int
		approvals int
		changes   bool
		complete  bool
	}{
		{"nobody yet", nil, 1, 0, false, false},
		{"one of one", []DecisionEntry{approve}, 1, 1, false, true},
		{"one of two", []DecisionEntry{approve}, 2, 1, false, false},
		{"two of two", []DecisionEntry{approve, approve}, 2, 2, false, true},
		{"more than asked", []DecisionEntry{approve, approve, approve}, 2, 3, false, true},
		{"changes beside an approval", []DecisionEntry{approve, changes}, 1, 1, true, true},
		{"changes alone", []DecisionEntry{changes}, 1, 0, true, false},
		{"a rule asking for none is never met", []DecisionEntry{approve}, 0, 1, false, false},
	} {
		approvals, asked := Tally(c.decisions)
		if approvals != c.approvals || asked != c.changes {
			t.Errorf("%s: tallied %d approvals and changes %v", c.what, approvals, asked)
		}
		if got := Complete(approvals, c.required); got != c.complete {
			t.Errorf("%s: complete is %v", c.what, got)
		}
	}
}
