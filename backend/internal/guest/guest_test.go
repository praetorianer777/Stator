package guest

import (
	"slices"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

func TestEachRoleGrantsWhatItSaysAndNeverAdminister(t *testing.T) {
	for role, want := range map[Role][]perm.SpacePermission{
		RoleViewer:    {perm.SpaceView},
		RoleCommenter: {perm.SpaceView, perm.SpaceAddComments},
		RoleEditor:    {perm.SpaceView, perm.SpaceAddPages, perm.SpaceAddComments, perm.SpaceDelete},
		RoleNone:      nil,
	} {
		got := role.Permissions()
		if !slices.Equal(got, want) {
			t.Errorf("%s grants %v, want %v", role, got, want)
		}
		if slices.Contains(got, perm.SpaceAdminister) {
			t.Errorf("%s administers the space", role)
		}
		if role != RoleNone && RoleOf(got) != role {
			t.Errorf("the grants of %s read back as %s", role, RoleOf(got))
		}
	}
}

func TestARoleIsReadFromWhatTheGrantsAllow(t *testing.T) {
	for _, tc := range []struct {
		held []perm.SpacePermission
		want Role
	}{
		{nil, RoleNone},
		{[]perm.SpacePermission{perm.SpaceView}, RoleViewer},
		{[]perm.SpacePermission{perm.SpaceDelete}, RoleViewer},
		{[]perm.SpacePermission{perm.SpaceAddComments}, RoleCommenter},
		{[]perm.SpacePermission{perm.SpaceView, perm.SpaceAddPages}, RoleEditor},
	} {
		if got := RoleOf(tc.held); got != tc.want {
			t.Errorf("RoleOf(%v) = %s, want %s", tc.held, got, tc.want)
		}
	}
}

func TestOnlyThreeRolesAreInvited(t *testing.T) {
	if slices.Contains(Invitable, RoleNone) || len(Invitable) != 3 {
		t.Errorf("the invitable roles are %v", Invitable)
	}
}

func TestAnAddressNeedsItsShape(t *testing.T) {
	for email, want := range map[string]bool{
		"ada@example.com":        true,
		"ada.lovelace@sub.ex.io": true,
		"":                       false,
		"ada":                    false,
		"@example.com":           false,
		"ada@localhost":          false,
		"ada@ex@ample.com":       false,
		"ada lovelace@ex.com":    false,
		"ada@ex.com, bob@ex.com": false,
	} {
		if got := plausibleEmail(email); got != want {
			t.Errorf("plausibleEmail(%q) = %v, want %v", email, got, want)
		}
	}
}
