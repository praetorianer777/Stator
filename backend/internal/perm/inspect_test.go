package perm

import (
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// inspected builds the facts the database would answer from the Go mirror of
// its rules, which the integration suite holds to the SQL.
func inspected(f Facts, published bool, chain ...InspectLink) InspectFacts {
	links := make([]ChainLink, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		l := chain[i]
		links = append(links, ChainLink{PageID: l.Page.ID, HiddenDraft: l.Hidden,
			ViewListed: len(l.View.Listed) > 0, OnViewList: l.View.On, EditListed: len(l.Edit.Listed) > 0, OnEditList: l.Edit.On})
	}
	a := PageRules(f, links)
	trashable := !chain[len(chain)-1].Page.Home
	out := InspectFacts{OrgAdmin: f.OrgAdmin(), Use: f.HoldsGlobal(UseStator), Space: map[SpacePermission]bool{},
		Chain: chain, Published: published, Trashable: trashable,
		Verdict: map[Right]bool{RightView: a.View, RightEdit: a.Edit, RightDelete: a.Delete && trashable, RightComment: a.Comment && published}}
	for _, p := range SpacePermissions {
		out.Space[p] = f.HoldsSpace(p)
	}
	for _, p := range f.Space {
		out.Grants = append(out.Grants, SpaceGrant{Subject: everyoneSubject, Permissions: []SpacePermission{p}})
	}
	return out
}

var everyoneSubject = Subject{Type: SubjectEveryone, Name: EveryoneName}

func chainPage(title string, home bool) InspectLink {
	return InspectLink{Page: AccessPage{ID: uuid.New(), Title: title, Home: home}}
}

func listed(on bool, names ...string) ListFacts {
	l := ListFacts{On: on}
	for _, n := range names {
		id := uuid.New()
		l.Listed = append(l.Listed, Subject{Type: SubjectUser, ID: &id, Name: n})
	}
	if on {
		l.Via = l.Listed[:1]
	}
	return l
}

// decider is the first step a right does not pass, which is its reason.
func decider(r AccessRight) *AccessStep {
	for i := range r.Steps {
		if !r.Steps[i].Passed {
			return &r.Steps[i]
		}
	}
	return nil
}

func TestEveryRefusalNamesTheStepThatDecidesIt(t *testing.T) {
	home := chainPage("Home", true)
	secret := chainPage("Secret", false)
	secret.View = listed(false, "Ann")
	locked := chainPage("Locked", false)
	locked.Edit = listed(false, "Ann")
	onList := chainPage("Team", false)
	onList.View = listed(true, "Bob", "Ann")
	draft := chainPage("Draft", false)
	draft.Hidden = true
	admin := Facts{Member: true, Role: auth.RoleAdmin}
	noUse := Facts{Member: true, Role: auth.RoleMember, Space: openSpace}

	type verdict struct {
		allowed bool
		kind    StepKind
	}
	for _, tt := range []struct {
		name  string
		facts InspectFacts
		want  map[Right]verdict
	}{
		{"an open space lets a member do everything", inspected(member(openSpace...), true, home, chainPage("Page", false)), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}, RightDelete: {true, ""}, RightComment: {true, ""}}},
		{"a view list above hides the page", inspected(member(openSpace...), true, home, secret, chainPage("Below", false)), map[Right]verdict{
			RightView: {false, StepList}, RightEdit: {false, StepView}, RightDelete: {false, StepView}, RightComment: {false, StepView}}},
		{"being on a list lets one through", inspected(member(openSpace...), true, home, onList), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}}},
		{"an edit list leaves reading and commenting", inspected(member(openSpace...), true, home, locked), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {false, StepList}, RightDelete: {false, StepList}, RightComment: {true, ""}}},
		{"a read only space refuses editing at its grant", inspected(member(SpaceView), true, home, chainPage("Page", false)), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {false, StepSpace}, RightDelete: {false, StepSpace}, RightComment: {false, StepSpace}}},
		{"an administrator of the space passes every list", inspected(member(SpaceAdminister), true, home, secret, locked), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}, RightDelete: {true, ""}}},
		{"an administrator of the organization passes every list", inspected(admin, true, home, secret, locked), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}}},
		{"without use nothing is seen", inspected(noUse, true, home, chainPage("Page", false)), map[Right]verdict{
			RightView: {false, StepUse}, RightEdit: {false, StepView}}},
		{"somebody else's unpublished page hides what is below it", inspected(admin, true, home, draft, chainPage("Below", false)), map[Right]verdict{
			RightView: {false, StepUnpublished}}},
		{"an unpublished page takes no comments", inspected(member(openSpace...), false, home, chainPage("Mine", false)), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}, RightComment: {false, StepPublished}}},
		{"a home page never goes to the trash", inspected(admin, true, home), map[Right]verdict{
			RightView: {true, ""}, RightEdit: {true, ""}, RightDelete: {false, StepHome}, RightComment: {true, ""}}},
		{"nor does it for somebody who cannot see it", inspected(noUse, true, home), map[Right]verdict{
			RightDelete: {false, StepView}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rights := Explain(tt.facts)
			if len(rights) != len(Rights) {
				t.Fatalf("%d rights explained", len(rights))
			}
			for i, r := range rights {
				if r.Right != Rights[i] {
					t.Errorf("right %d is %s", i, r.Right)
				}
				if r.Allowed != tt.facts.Verdict[r.Right] {
					t.Errorf("%s: allowed %v, the database said %v", r.Right, r.Allowed, tt.facts.Verdict[r.Right])
				}
				if d := decider(r); (d == nil) != r.Allowed {
					t.Errorf("%s: allowed %v but the steps say %+v", r.Right, r.Allowed, d)
				}
				want, ok := tt.want[r.Right]
				if !ok {
					continue
				}
				if r.Allowed != want.allowed {
					t.Errorf("%s: allowed %v, want %v", r.Right, r.Allowed, want.allowed)
				}
				if d := decider(r); d != nil && d.Kind != want.kind {
					t.Errorf("%s: decided by %s, want %s", r.Right, d.Kind, want.kind)
				}
			}
		})
	}
}

func TestTheStepsNameTheirPagesListsAndGrants(t *testing.T) {
	home := chainPage("Home", true)
	secret := chainPage("Secret", false)
	secret.View = listed(false, "Ann", "Carl")
	rights := Explain(inspected(member(openSpace...), true, home, secret))
	d := decider(rights[0])
	if d == nil || d.Page == nil || d.Page.Title != "Secret" || d.List == nil || *d.List != ListView || len(d.Listed) != 2 || len(d.Via) != 0 {
		t.Fatalf("the view list step is %+v", d)
	}

	view := rights[0].Steps
	if view[0].Kind != StepUse || len(view[0].Via) != 0 || view[1].Kind != StepSpace || *view[1].Permission != SpaceView {
		t.Fatalf("view starts with %+v", view[:2])
	}
	if len(view[1].Grants) != len(openSpace) {
		t.Errorf("every grant gives view, but %d are named", len(view[1].Grants))
	}
	edit := rights[1].Steps
	if edit[1].Kind != StepSpace || len(edit[1].Grants) != 1 || edit[1].Grants[0].Permissions[0] != SpaceAddPages {
		t.Errorf("edit is given by %+v", edit[1].Grants)
	}

	bypassed := Explain(inspected(member(SpaceView, SpaceAdminister), true, home, secret))[0].Steps
	last := bypassed[len(bypassed)-1]
	if !last.Bypassed || !last.Passed {
		t.Errorf("a space administrator's list step is %+v", last)
	}
	if before := bypassed[len(bypassed)-2]; before.Kind != StepSpace || *before.Permission != SpaceAdminister {
		t.Errorf("the grant that lets an administrator through is not named: %+v", before)
	}
	orgAdmin := Explain(inspected(Facts{Member: true, Role: auth.RoleOwner}, true, home, secret))[0].Steps
	if orgAdmin[0].Kind != StepOrgAdmin || len(orgAdmin) != 2 || !orgAdmin[1].Bypassed {
		t.Errorf("an owner's view is %+v", orgAdmin)
	}
	for _, r := range rights {
		for _, s := range r.Steps {
			if s.Via == nil || s.Grants == nil || s.Listed == nil {
				t.Errorf("%s: a %s step answers null for a list", r.Right, s.Kind)
			}
		}
	}
}
