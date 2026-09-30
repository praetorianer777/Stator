// Package audit is the organization's record of who did what to it, written in
// the same transaction as the act so the two cannot disagree.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// Actions recorded so far.
const (
	ActionMemberAdmitted = "member.admitted"
	ActionMemberDeclined = "member.declined"
	ActionMemberRemoved  = "member.removed"
	// ActionMemberJoined is somebody let in by a mapped group on their first
	// sign-in, with nobody here clicking.
	ActionMemberJoined = "member.joined"
	// ActionMemberRoleChanged is a role the identity provider's groups changed.
	ActionMemberRoleChanged = "member.role_changed"
	ActionGroupRoleSet      = "sso.group_role_set"
	ActionGroupRoleRemoved  = "sso.group_role_removed"
	ActionTokenCreated      = "token.created"
	ActionTokenRevoked      = "token.revoked"
	ActionSpaceCreated      = "space.created"
	ActionSpaceUpdated      = "space.updated"
	ActionSpaceDeleted      = "space.deleted"
	ActionPagePurged        = "page.purged"
	ActionTrashEmptied      = "trash.emptied"
	// Permissions: whom a global permission is granted to, a space's table,
	// and a page's own restrictions.
	ActionOrgPermissionSet    = "org.permission_set"
	ActionSpacePermissionsSet = "space.permissions_set"
	ActionPageRestrictionsSet = "page.restrictions_set"
)

// Entry is one act to record.
type Entry struct {
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Actor      uuid.UUID
	Data       map[string]any
	IP         string
}

// Write records an entry for an organization inside the caller's transaction.
func Write(ctx context.Context, tx db.DBTX, orgID uuid.UUID, e Entry) error {
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("an audit entry needs an action")
	}
	data := []byte("{}")
	if e.Data != nil {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return err
		}
	}
	var actor *uuid.UUID
	if e.Actor != uuid.Nil {
		actor = &e.Actor
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, data, ip)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet)`,
		orgID, actor, e.Action, e.TargetType, e.TargetID, data, e.IP)
	return err
}
