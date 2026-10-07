package perm

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func person(name string, guest bool) CopySubject {
	id := uuid.NewSHA1(uuid.Nil, []byte(name))
	return CopySubject{Type: SubjectUser, ID: &id, Name: name, Guest: guest}
}

func team(name string) CopySubject {
	id := uuid.NewSHA1(uuid.Nil, []byte("group "+name))
	return CopySubject{Type: SubjectGroup, ID: &id, Name: name}
}

var (
	everybody = CopySubject{Type: SubjectEveryone, Name: EveryoneName}
	anybody   = CopySubject{Type: SubjectAnonymous, Name: "Anybody"}
	ann       = person("Ann", false)
	bob       = person("Bob", false)
	gwen      = person("Gwen", true)
	hank      = person("Hank", true)
	writers   = team("Writers")
)

func grant(s CopySubject, ps ...SpacePermission) CopyGrant {
	return CopyGrant{Subject: s, Permissions: ps}
}

func changeOf(t *testing.T, plan CopyPlan, s CopySubject) *CopyChange {
	t.Helper()
	for i := range plan.Changes {
		if plan.Changes[i].Subject.Key() == s.Key() {
			return &plan.Changes[i]
		}
	}
	return nil
}

func held(plan CopyPlan, s CopySubject) []SpacePermission {
	for _, g := range plan.Result {
		if g.Subject.Key() == s.Key() {
			return g.Permissions
		}
	}
	return nil
}

func TestReplaceMakesTheTargetTheSourceAndSaysHowEachSubjectChanges(t *testing.T) {
	source := []CopyGrant{
		grant(everybody, SpaceView),
		grant(ann, SpaceAdminister, SpaceView),
		grant(writers, SpaceView, SpaceAddPages),
		grant(anybody, SpaceView),
		grant(gwen, SpaceView, SpaceAddComments),
	}
	target := []CopyGrant{
		grant(everybody, SpaceView, SpaceAddPages, SpaceAddComments, SpaceDelete),
		grant(ann, SpaceView),
		grant(bob, SpaceAdminister),
		grant(writers, SpaceView, SpaceDelete),
		grant(hank, SpaceView),
	}
	plan := PlanCopy(source, target, CopyReplace)

	for s, want := range map[CopySubject]CopyChangeKind{everybody: CopyNarrowed, ann: CopyWidened, bob: CopyRemoved, writers: CopyChanged, anybody: CopyAdded} {
		c := changeOf(t, plan, s)
		if c == nil || c.Kind != want {
			t.Errorf("%s changes as %+v, want %s", s.Name, c, want)
		}
	}
	if c := changeOf(t, plan, ann); !slices.Equal(c.Before, []SpacePermission{SpaceView}) || !slices.Equal(c.After, []SpacePermission{SpaceView, SpaceAdminister}) {
		t.Errorf("ann goes from %v to %v", c.Before, c.After)
	}
	if held(plan, bob) != nil {
		t.Errorf("bob keeps %v after a replace", held(plan, bob))
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Subject.Key() != gwen.Key() || plan.Skipped[0].Reason != CopySkipGuest {
		t.Errorf("the skipped grants are %+v, want the source's guest", plan.Skipped)
	}
	if held(plan, gwen) != nil {
		t.Error("the source's guest was copied")
	}
	if !slices.Equal(held(plan, hank), []SpacePermission{SpaceView}) || len(plan.Kept) != 1 || changeOf(t, plan, hank) != nil {
		t.Errorf("the target's guest holds %v, kept %+v", held(plan, hank), plan.Kept)
	}
	want := CopyCounts{Added: 1, Widened: 1, Narrowed: 1, Changed: 1, Removed: 1, Skipped: 1, Unchanged: 1}
	if plan.Counts != want {
		t.Errorf("the counts are %+v, want %+v", plan.Counts, want)
	}
	if plan.LeavesNoAdministrator {
		t.Error("ann still administers, yet the plan says nobody does")
	}
}

func TestMergeOnlyAddsAndWidens(t *testing.T) {
	source := []CopyGrant{grant(everybody, SpaceView), grant(ann, SpaceAddPages), grant(writers, SpaceView, SpaceAddComments), grant(gwen, SpaceView)}
	target := []CopyGrant{grant(everybody, SpaceView, SpaceAddPages), grant(bob, SpaceAdminister), grant(writers, SpaceView), grant(hank, SpaceView)}
	plan := PlanCopy(source, target, CopyMerge)

	if c := changeOf(t, plan, everybody); c != nil {
		t.Errorf("merging less narrowed everyone: %+v", c)
	}
	if !slices.Equal(held(plan, everybody), []SpacePermission{SpaceView, SpaceAddPages}) {
		t.Errorf("everyone holds %v", held(plan, everybody))
	}
	if c := changeOf(t, plan, ann); c == nil || c.Kind != CopyAdded || !slices.Equal(c.After, []SpacePermission{SpaceAddPages}) {
		t.Errorf("ann is %+v, want added as the source grants her", c)
	}
	if c := changeOf(t, plan, writers); c == nil || c.Kind != CopyWidened {
		t.Errorf("the writers are %+v, want widened", c)
	}
	if !slices.Equal(held(plan, bob), []SpacePermission{SpaceAdminister}) || !slices.Equal(held(plan, hank), []SpacePermission{SpaceView}) {
		t.Errorf("a merge took from bob or hank: %v, %v", held(plan, bob), held(plan, hank))
	}
	if len(plan.Kept) != 0 {
		t.Errorf("a merge reports kept guests: %+v", plan.Kept)
	}
	for _, c := range plan.Changes {
		if c.Kind == CopyNarrowed || c.Kind == CopyRemoved || c.Kind == CopyChanged {
			t.Errorf("a merge %s %s", c.Kind, c.Subject.Name)
		}
	}
	if plan.Counts.Skipped != 1 || plan.Counts.Unchanged != 3 {
		t.Errorf("the counts are %+v", plan.Counts)
	}
}

func TestACopyThatLeavesNobodyAdministeringIsMarked(t *testing.T) {
	target := []CopyGrant{grant(everybody, SpaceView), grant(ann, SpaceAdminister)}
	if plan := PlanCopy([]CopyGrant{grant(everybody, SpaceView)}, target, CopyReplace); !plan.LeavesNoAdministrator {
		t.Error("replacing the only administrator is not marked")
	}
	if plan := PlanCopy([]CopyGrant{grant(everybody, SpaceView)}, target, CopyMerge); plan.LeavesNoAdministrator {
		t.Error("a merge is marked, though it takes nothing")
	}
	if plan := PlanCopy([]CopyGrant{grant(bob, SpaceAdminister)}, target, CopyReplace); plan.LeavesNoAdministrator {
		t.Error("handing administer to bob is marked")
	}
	// A space administered by the organization's administrators alone has no
	// administrator of its own to lose.
	if plan := PlanCopy([]CopyGrant{grant(everybody, SpaceView)}, []CopyGrant{grant(everybody, SpaceView, SpaceAddPages)}, CopyReplace); plan.LeavesNoAdministrator {
		t.Error("a space without administrators is marked")
	}
}

func TestCopyingASpaceOntoItsTwinChangesNothing(t *testing.T) {
	table := []CopyGrant{grant(everybody, SpaceView), grant(ann, SpaceAdminister)}
	for _, mode := range CopyModes {
		plan := PlanCopy(table, table, mode)
		if len(plan.Changes) != 0 || plan.Counts.Unchanged != 2 || len(plan.Result) != 2 {
			t.Errorf("%s of a twin plans %+v", mode, plan)
		}
	}
}

func TestTheFingerprintFollowsTheTablesAndTheMode(t *testing.T) {
	src, dst := uuid.New(), uuid.New()
	a := []CopyGrant{grant(everybody, SpaceView), grant(ann, SpaceAdminister)}
	b := []CopyGrant{grant(bob, SpaceView)}
	base := CopyFingerprint(CopyReplace, src, dst, a, b)
	reordered := []CopyGrant{grant(ann, SpaceAdminister), grant(everybody, SpaceView)}
	if CopyFingerprint(CopyReplace, src, dst, reordered, b) != base {
		t.Error("the order the rows were read in changes the fingerprint")
	}
	renamed := []CopyGrant{grant(CopySubject{Type: SubjectEveryone, Name: "All"}, SpaceView), grant(ann, SpaceAdminister)}
	if CopyFingerprint(CopyReplace, src, dst, renamed, b) != base {
		t.Error("a name changes the fingerprint")
	}
	for what, other := range map[string]string{
		"the mode":       CopyFingerprint(CopyMerge, src, dst, a, b),
		"the source":     CopyFingerprint(CopyReplace, uuid.New(), dst, a, b),
		"the sides":      CopyFingerprint(CopyReplace, src, dst, b, a),
		"a source grant": CopyFingerprint(CopyReplace, src, dst, []CopyGrant{grant(everybody, SpaceView, SpaceAddPages), grant(ann, SpaceAdminister)}, b),
		"a target grant": CopyFingerprint(CopyReplace, src, dst, a, []CopyGrant{grant(bob, SpaceView), grant(gwen, SpaceView)}),
	} {
		if other == base {
			t.Errorf("changing %s keeps the fingerprint", what)
		}
	}
}

func TestOrderedDropsRepeatsAndSortsFromViewToAdminister(t *testing.T) {
	got := Ordered([]SpacePermission{SpaceAdminister, SpaceView, SpaceAdminister, SpaceDelete})
	if !slices.Equal(got, []SpacePermission{SpaceView, SpaceDelete, SpaceAdminister}) {
		t.Errorf("Ordered = %v", got)
	}
}
