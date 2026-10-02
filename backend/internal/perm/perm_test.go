package perm

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// A space as it starts: everyone views, adds pages, comments and deletes.
var openSpace = []SpacePermission{SpaceView, SpaceAddPages, SpaceAddComments, SpaceDelete}

func member(space ...SpacePermission) Facts {
	return Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{UseStator}, Space: space}
}

func TestSpacePermissionsImplyAsTheContractSays(t *testing.T) {
	admin := Facts{Member: true, Role: auth.RoleAdmin}
	owner := Facts{Member: true, Role: auth.RoleOwner}
	for _, tt := range []struct {
		name   string
		facts  Facts
		action Action
		want   bool
	}{
		{"a member of an open space views", member(openSpace...), ViewSpace, true},
		{"a member of an open space edits", member(openSpace...), EditPages, true},
		{"a member of an open space trashes", member(openSpace...), DeletePages, true},
		{"a member of an open space comments", member(openSpace...), AddComments, true},
		{"a member of an open space does not administer it", member(openSpace...), AdministerSpace, false},
		{"a member of an open space does not purge", member(openSpace...), PurgeTrash, false},
		{"any permission implies view", member(SpaceAddComments), ViewSpace, true},
		{"commenting is not editing", member(SpaceAddComments), EditPages, false},
		{"viewing is not editing", member(SpaceView), EditPages, false},
		{"administer implies editing", member(SpaceAdminister), EditPages, true},
		{"administer implies deleting the space", member(SpaceAdminister), DeleteSpace, true},
		{"administer implies purging", member(SpaceAdminister), PurgeTrash, true},
		{"a member of an open space does not review its stale pages", member(openSpace...), ReviewStale, false},
		{"an administrator of the space reviews its stale pages", member(SpaceAdminister), ReviewStale, true},
		{"an organization admin reviews every space's stale pages", admin, ReviewStale, true},
		{"a member of an open space does not change its shortcuts", member(openSpace...), ManageShortcuts, false},
		{"an administrator of the space changes its shortcuts", member(SpaceAdminister), ManageShortcuts, true},
		{"an organization admin changes every space's shortcuts", admin, ManageShortcuts, true},
		{"no grant is no view", member(), ViewSpace, false},
		{"without use nothing holds", Facts{Member: true, Role: auth.RoleMember, Space: openSpace}, ViewSpace, false},
		{"a stranger holds nothing", Facts{Space: openSpace, Global: []GlobalPermission{UseStator}}, ViewSpace, false},
		{"an organization admin holds every space permission", admin, AdministerSpace, true},
		{"an owner holds every space permission", owner, PurgeTrash, true},
		{"an admin without use still uses", admin, ViewSpace, true},
		{"members do not create spaces by default", member(), CreateSpace, false},
		{"a member granted createSpace creates", Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{UseStator, CreateSpaces}}, CreateSpace, true},
		{"createSpace without use is nothing", Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{CreateSpaces}}, CreateSpace, false},
		{"admins create spaces", admin, CreateSpace, true},
		{"an unknown action is refused", admin, Action("space.unknown"), false},
	} {
		if got := Decide(tt.facts, tt.action); got != tt.want {
			t.Errorf("%s: %s = %v, want %v", tt.name, tt.action, got, tt.want)
		}
	}
}

func TestGlobalAdministerFollowsTheRoles(t *testing.T) {
	granted := Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{UseStator, AdministerOrg}}
	if granted.HoldsGlobal(AdministerOrg) {
		t.Error("a member holds administer through a grant")
	}
	if !(Facts{Member: true, Role: auth.RoleAdmin}).GlobalCan().Administer {
		t.Error("an admin does not administer")
	}
	if got := member().GlobalCan(); got != (GlobalCan{Use: true}) {
		t.Errorf("a member is offered %+v", got)
	}
}

func TestSpaceCanFollowsTheRules(t *testing.T) {
	if got := member(openSpace...).Can(); got != (Can{EditPages: true, AddComments: true, DeletePages: true}) {
		t.Errorf("a member of an open space is offered %+v", got)
	}
	all := Can{EditPages: true, Administer: true, Delete: true, PurgeTrash: true, AddComments: true, DeletePages: true}
	if got := member(SpaceAdminister).Can(); got != all {
		t.Errorf("a space administrator is offered %+v", got)
	}
	if got := member(SpaceView).Can(); got != (Can{}) {
		t.Errorf("a reader is offered %+v", got)
	}
}

func link(viewListed, onView, editListed, onEdit bool) ChainLink {
	return ChainLink{PageID: uuid.New(), ViewListed: viewListed, OnViewList: onView, EditListed: editListed, OnEditList: onEdit}
}

func TestRestrictionsInheritAndNarrow(t *testing.T) {
	free := link(false, false, false, false)
	for _, tt := range []struct {
		name  string
		facts Facts
		chain []ChainLink
		want  PageAccess
	}{
		{"an unrestricted page is the space's", member(openSpace...), []ChainLink{free, free},
			PageAccess{View: true, Edit: true, Delete: true, Comment: true}},
		{"a view list above hides the page", member(openSpace...), []ChainLink{free, link(true, false, false, false), free},
			PageAccess{ViewRestricted: true}},
		{"passing every view list shows it", member(openSpace...), []ChainLink{link(true, true, false, false), link(true, true, false, false)},
			PageAccess{View: true, Edit: true, Delete: true, Comment: true, ViewRestricted: true}},
		{"one list failed among several hides it", member(openSpace...), []ChainLink{link(true, true, false, false), link(true, false, false, false)},
			PageAccess{ViewRestricted: true}},
		{"an edit list above stops editing and deleting", member(openSpace...), []ChainLink{free, link(false, false, true, false)},
			PageAccess{View: true, Comment: true, EditRestricted: true}},
		{"an edit list does not let anybody past a view list", member(openSpace...), []ChainLink{link(true, false, true, true)},
			PageAccess{ViewRestricted: true, EditRestricted: true}},
		{"restrictions only narrow the space", member(SpaceView), []ChainLink{link(true, true, true, true)},
			PageAccess{View: true, ViewRestricted: true, EditRestricted: true}},
		{"a space administrator is not bound", member(SpaceAdminister), []ChainLink{link(true, false, true, false)},
			PageAccess{View: true, Edit: true, Delete: true, Comment: true, Archive: true, ViewRestricted: true, EditRestricted: true}},
		{"an organization administrator is not bound", Facts{Member: true, Role: auth.RoleAdmin}, []ChainLink{free, link(true, false, true, false)},
			PageAccess{View: true, Edit: true, Delete: true, Comment: true, Archive: true, ViewRestricted: true, EditRestricted: true}},
		{"nobody sees another's unpublished page", Facts{Member: true, Role: auth.RoleOwner}, []ChainLink{free, {HiddenDraft: true}},
			PageAccess{}},
		{"no chain is no page", member(openSpace...), nil, PageAccess{}},
	} {
		if got := PageRules(tt.facts, tt.chain); got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestRestrictMeansEdit(t *testing.T) {
	got := PageAccess{View: true, Edit: true, Comment: true}.Can()
	if got != (PageCan{Edit: true, Restrict: true, Comment: true}) {
		t.Errorf("page.can is %+v", got)
	}
}

func TestASaveMayNotLockItsSaverOut(t *testing.T) {
	if !LocksOut(false, PageAccess{View: true}) {
		t.Error("losing edit is not a lock-out")
	}
	if !LocksOut(false, PageAccess{}) {
		t.Error("losing view is not a lock-out")
	}
	if LocksOut(false, PageAccess{View: true, Edit: true}) {
		t.Error("keeping both is a lock-out")
	}
	if LocksOut(true, PageAccess{}) {
		t.Error("a space administrator is locked out")
	}
}

func TestRefusalsAreSentences(t *testing.T) {
	for _, a := range []Action{CreateSpace, ViewSpace, AdministerSpace, DeleteSpace, EditPages, DeletePages, AddComments, PurgeTrash, InspectAccess, ReviewStale, ListReaders, ManageShortcuts, Action("x")} {
		err := error(&DeniedError{Action: a})
		if !errors.Is(err, ErrDenied) {
			t.Errorf("the refusal of %s does not wrap ErrDenied", a)
		}
		if msg := err.Error(); !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
			t.Errorf("the refusal of %s is not a sentence: %q", a, msg)
		}
	}
}

func TestTheViewablePredicateNamesItsParameter(t *testing.T) {
	if got := ViewablePage("p", 3); got != "perm_page_viewable(p.id, $3::uuid)" {
		t.Errorf("ViewablePage = %s", got)
	}
	if got := ViewableSpace("s", 1); got != "perm_space_holds($1::uuid, s.id, 'view')" {
		t.Errorf("ViewableSpace = %s", got)
	}
}

func TestPickerLimits(t *testing.T) {
	for in, want := range map[int]int{0: DefaultPickerLimit, -1: DefaultPickerLimit, 7: 7, 500: MaxPickerLimit} {
		if got := PickerLimit(in); got != want {
			t.Errorf("PickerLimit(%d) = %d, want %d", in, got, want)
		}
	}
	if got := LikePrefix(`50%_a\`); got != `50\%\_a\\%` {
		t.Errorf("LikePrefix escapes to %q", got)
	}
}

// A token limited to spaces is handed facts only for a space it reaches, so
// within one its owner's role holds as ever, and the organization never.
func TestATokenLimitedToSpacesHoldsNothingOfTheOrganization(t *testing.T) {
	admin := Facts{Member: true, Role: auth.RoleAdmin, SpacesOnly: true}
	creator := Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{UseStator, CreateSpaces}, SpacesOnly: true}
	for _, tt := range []struct {
		name   string
		facts  Facts
		action Action
		want   bool
	}{
		{"an admin's token still administers a space it reaches", admin, AdministerSpace, true},
		{"an admin's token still purges in a space it reaches", admin, PurgeTrash, true},
		{"an admin's token creates no space", admin, CreateSpace, false},
		{"a creator's token creates no space", creator, CreateSpace, false},
		{"a member's token edits where the member may", Facts{Member: true, Role: auth.RoleMember, Global: []GlobalPermission{UseStator}, Space: openSpace, SpacesOnly: true}, EditPages, true},
		{"a space it does not reach comes as no facts at all", Facts{}, ViewSpace, false},
	} {
		if got := Decide(tt.facts, tt.action); got != tt.want {
			t.Errorf("%s: %s = %v, want %v", tt.name, tt.action, got, tt.want)
		}
	}
	if admin.OrgAdmin() || admin.GlobalCan().Administer || admin.GlobalCan().CreateSpace || !admin.GlobalCan().Use {
		t.Errorf("an admin's limited token = %+v, want use and nothing more", admin.GlobalCan())
	}
	if !(Facts{Member: true, Role: auth.RoleAdmin}).OrgAdmin() {
		t.Error("an admin without a limited token stopped administering the organization")
	}
}
