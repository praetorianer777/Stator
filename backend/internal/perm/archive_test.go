package perm

import (
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// archive lays an archive over facts as the database would answer them: no
// right that changes the page holds.
func archive(f InspectFacts, how Archived, with *AccessPage) InspectFacts {
	f.Archived, f.ArchivedWith = how, with
	for _, r := range []Right{RightEdit, RightDelete, RightComment} {
		f.Verdict[r] = false
	}
	return f
}

func TestAnArchivedPageIsExplainedBeforeAnyGrant(t *testing.T) {
	home := chainPage("Home", true)
	old := chainPage("Old plans", false)
	for _, tt := range []struct {
		name  string
		facts InspectFacts
		with  *AccessPage
	}{
		{"an archived page", archive(inspected(member(SpaceAdminister), true, home, old), ArchivedPage, &old.Page), &old.Page},
		{"a page of an archived space", archive(inspected(Facts{Member: true, Role: auth.RoleOwner}, true, home, old), ArchivedSpace, nil), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rights := Explain(tt.facts)
			if !rights[0].Allowed {
				t.Errorf("an archived page cannot be read")
			}
			for _, r := range rights[1:] {
				d := decider(r)
				if r.Allowed || d == nil || d.Kind != StepArchived || d.Page != tt.with {
					t.Errorf("%s: allowed %v, decided by %+v", r.Right, r.Allowed, d)
				}
			}
		})
	}
	if steps := Explain(inspected(member(openSpace...), true, home, old))[1].Steps; len(steps) == 0 || steps[len(steps)-1].Kind == StepArchived {
		t.Errorf("a page that is not archived explains %+v", steps)
	}
}

func TestAnArchivedPageIsReadOnlyForEverybody(t *testing.T) {
	admin := PageRules(Facts{Member: true, Role: auth.RoleOwner}, []ChainLink{{}})
	for _, how := range []Archived{ArchivedPage, ArchivedSpace} {
		a := admin
		a.Archived = how
		got := a.Frozen()
		if !got.View || got.Edit || got.Delete || got.Comment || !got.Archive {
			t.Errorf("%s: an owner may %+v", how, got)
		}
		if can := got.Can(); can.Edit || can.Restrict || can.Comment || can.Archive != (how == ArchivedPage) {
			t.Errorf("%s: page.can is %+v", how, can)
		}
	}
	if got := admin.Frozen(); got != admin {
		t.Errorf("a page that is not archived changed to %+v", got)
	}
	if reader := PageRules(member(openSpace...), []ChainLink{{}}); reader.Archive {
		t.Errorf("a member who does not administer the space may archive")
	}
}

func TestARefusalSaysWhatIsArchived(t *testing.T) {
	for how, want := range map[Archived]string{
		ArchivedPage:  "This page is archived",
		ArchivedSpace: "This space is archived",
		NotArchived:   "You may read this page but not change it",
	} {
		err := Refuse(EditPages, how)
		if !errors.Is(err, ErrDenied) || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "Ask ") {
			t.Errorf("%q: %v", how, err)
		}
	}
	if !Decide(member(SpaceAdminister), ArchivePages) || !Decide(member(SpaceAdminister), ArchiveSpace) || Decide(member(openSpace...), ArchivePages) {
		t.Errorf("archiving is not the space administrators'")
	}
}
