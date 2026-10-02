// Package audit is the organization's record of who did what to it, written in
// the same transaction as the act so the two cannot disagree.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ActionSSOProviderSaved  = "sso.provider_saved"
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
	// ActionCommentDeleted is somebody else's comment deleted with the
	// space's delete permission; one's own is not recorded.
	ActionCommentDeleted            = "comment.deleted"
	ActionThemeDefaultSet           = "theme.default_set"
	ActionArmatureConnectionSaved   = "armature.connection_saved"
	ActionArmatureConnectionRemoved = "armature.connection_removed"
	// Exports: what left the wiki as a file, and the record itself.
	ActionPageExported  = "page.exported"
	ActionAuditExported = "audit.exported"
	// Stewardship: who answers for a page, and whether it was checked.
	ActionPageOwnerSet     = "page.owner_set"
	ActionPageOwnerRemoved = "page.owner_removed"
	ActionPageVerified     = "page.verified"
	ActionPageUnverified   = "page.unverified"
	// Archiving: a page with the pages below it, or a whole space.
	ActionPageArchived    = "page.archived"
	ActionPageUnarchived  = "page.unarchived"
	ActionSpaceArchived   = "space.archived"
	ActionSpaceUnarchived = "space.unarchived"
	// Webhooks: where the organization's events are posted. A webhook the
	// worker turns off for failing is recorded with nobody as its actor.
	ActionWebhookCreated       = "webhook.created"
	ActionWebhookUpdated       = "webhook.updated"
	ActionWebhookDeleted       = "webhook.deleted"
	ActionWebhookSecretRotated = "webhook.secret_rotated"
	ActionWebhookDisabled      = "webhook.disabled"
	// ActionPageShared is a page sent to people with a note; the note is not kept.
	ActionPageShared = "page.shared"
	// Templates of the organization's own, for every space or for one.
	ActionTemplateCreated = "template.created"
	ActionTemplateUpdated = "template.updated"
	ActionTemplateDeleted = "template.deleted"
)

// Actions is every action the log may hold, for a filter to offer and a
// client to name.
var Actions = []string{
	ActionMemberAdmitted, ActionMemberDeclined, ActionMemberRemoved, ActionMemberJoined, ActionMemberRoleChanged,
	ActionSSOProviderSaved, ActionGroupRoleSet, ActionGroupRoleRemoved,
	ActionTokenCreated, ActionTokenRevoked,
	ActionSpaceCreated, ActionSpaceUpdated, ActionSpaceDeleted, ActionPagePurged, ActionTrashEmptied,
	ActionOrgPermissionSet, ActionSpacePermissionsSet, ActionPageRestrictionsSet,
	ActionCommentDeleted, ActionThemeDefaultSet,
	ActionArmatureConnectionSaved, ActionArmatureConnectionRemoved,
	ActionPageExported, ActionAuditExported,
	ActionPageOwnerSet, ActionPageOwnerRemoved, ActionPageVerified, ActionPageUnverified,
	ActionPageArchived, ActionPageUnarchived, ActionSpaceArchived, ActionSpaceUnarchived,
	ActionWebhookCreated, ActionWebhookUpdated, ActionWebhookDeleted, ActionWebhookSecretRotated, ActionWebhookDisabled,
	ActionPageShared,
	ActionTemplateCreated, ActionTemplateUpdated, ActionTemplateDeleted,
}

// Redacted stands in the record for a value that looked like a credential.
const Redacted = "[redacted]"

// SecretPrefixes start every credential Stator issues or stores, so a value
// that starts with one never reaches the record whatever key it hides under.
var SecretPrefixes = []string{"stator_pat_", "stator_whs_", "armature_pat_", "armature_whs_"}

// secretWords mark a key whose value would be a credential if it were text.
var secretWords = []string{"secret", "password", "token", "credential"}

// secretStates are the words a secret's key may carry instead of the secret.
var secretStates = map[string]bool{"set": true, "kept": true, "removed": true}

// Entry is one act to record. A blank IP takes the one WithIP put on the context.
type Entry struct {
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Actor      uuid.UUID
	Data       map[string]any
	IP         string
}

type ipKey struct{}

// WithIP carries the caller's address to every entry written for them.
func WithIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

func ipFrom(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey{}).(string)
	return ip
}

// Write records an entry for an organization inside the caller's transaction.
func Write(ctx context.Context, tx db.DBTX, orgID uuid.UUID, e Entry) error {
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("an audit entry needs an action")
	}
	data := []byte("{}")
	if e.Data != nil {
		var err error
		if data, err = Scrub(e.Data); err != nil {
			return fmt.Errorf("%s: %w", e.Action, err)
		}
	}
	var actor *uuid.UUID
	if e.Actor != uuid.Nil {
		actor = &e.Actor
	}
	ip := e.IP
	if ip == "" {
		ip = ipFrom(ctx)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, data, ip)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet)`,
		orgID, actor, e.Action, e.TargetType, e.TargetID, data, ip)
	return err
}

// Scrub writes an entry's data as JSON with every value that looks like a
// credential replaced by Redacted, so an act never fails over its record.
func Scrub(data map[string]any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(scrub("", v))
}

func scrub(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			v[k] = scrub(k, inner)
		}
		return v
	case []any:
		for i, inner := range v {
			v[i] = scrub(key, inner)
		}
		return v
	case string:
		for _, prefix := range SecretPrefixes {
			if strings.HasPrefix(strings.TrimSpace(v), prefix) {
				return Redacted
			}
		}
		if secretKey(key) && v != "" && !secretStates[v] {
			return Redacted
		}
	}
	return v
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range secretWords {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}
