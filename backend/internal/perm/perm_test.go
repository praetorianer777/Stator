package perm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// Until space permissions arrive, members read and write every page, and only
// owners and administrators shape the spaces themselves.
func TestTheRulesUntilSpacePermissions(t *testing.T) {
	ctx := context.Background()
	space := uuid.New()
	member := Actor{UserID: uuid.New(), Role: auth.RoleMember}
	admin := Actor{UserID: uuid.New(), Role: auth.RoleAdmin}
	owner := Actor{UserID: uuid.New(), Role: auth.RoleOwner}
	nobody := Actor{}

	for _, tt := range []struct {
		actor  Actor
		action Action
		want   bool
	}{
		{member, ViewSpace, true},
		{member, EditPages, true},
		{member, CreateSpace, false},
		{member, AdministerSpace, false},
		{member, DeleteSpace, false},
		{admin, CreateSpace, true},
		{admin, AdministerSpace, true},
		{admin, DeleteSpace, true},
		{owner, DeleteSpace, true},
		{nobody, ViewSpace, false},
		{nobody, EditPages, false},
		{admin, Action("space.unknown"), false},
	} {
		err := Check(ctx, nil, tt.actor, tt.action, space)
		if got := err == nil; got != tt.want {
			t.Errorf("%q may %s = %v, want %v", tt.actor.Role, tt.action, got, tt.want)
		}
		if err != nil {
			if !errors.Is(err, ErrDenied) {
				t.Errorf("the refusal of %s does not wrap ErrDenied", tt.action)
			}
			if msg := err.Error(); !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
				t.Errorf("the refusal of %s is not a sentence: %q", tt.action, msg)
			}
		}
	}

	if got := On(ctx, nil, member, space); got != (Can{EditPages: true}) {
		t.Errorf("a member is offered %+v", got)
	}
	if got := On(ctx, nil, admin, space); got != (Can{EditPages: true, Administer: true, Delete: true}) {
		t.Errorf("an administrator is offered %+v", got)
	}
}
