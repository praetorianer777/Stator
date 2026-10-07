package space

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// CopyInput names the space to copy permissions from and how. Fingerprint
// is the preview's, which applying requires.
type CopyInput struct {
	From        string        `json:"from"`
	Mode        perm.CopyMode `json:"mode"`
	Fingerprint string        `json:"fingerprint,omitempty"`
}

// CopySpace names one side of a copy.
type CopySpace struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// CopyPreview is what copying would change in the target, as the copy that
// applies it computes it.
type CopyPreview struct {
	Source  CopySpace         `json:"source"`
	Target  CopySpace         `json:"target"`
	Mode    perm.CopyMode     `json:"mode"`
	Changes []perm.CopyChange `json:"changes"`
	// Skipped are the source's grants the copy leaves behind, and why.
	Skipped []perm.CopySkip `json:"skipped"`
	// Kept are the target's guests, whom a replace leaves as they are.
	Kept   []perm.CopyGrant `json:"kept"`
	Counts perm.CopyCounts  `json:"counts"`
	// LeavesNoAdministrator says the copy would take administer from
	// everybody in the target, which applying refuses.
	LeavesNoAdministrator bool `json:"leavesNoAdministrator"`
	// Fingerprint names the two tables the preview was computed from;
	// applying it refuses once either has changed.
	Fingerprint string `json:"fingerprint"`
}

// CopyConflictError refuses a copy whose preview no longer holds, or whose
// result the space may not have, in a sentence saying what to do.
type CopyConflictError struct {
	Code    string
	Message string
}

func (e *CopyConflictError) Error() string { return e.Message }

// Why a copy is refused as a whole.
const (
	CopyChangedCode         = "copy_changed"
	CopyNoAdministratorCode = "no_administrator"
	CopyPersonalCode        = "personal_space"
)

var (
	errCopyNoAdministrator = &CopyConflictError{Code: CopyNoAdministratorCode,
		Message: "This copy would leave the space without an administrator. Merge instead, or give somebody administer in the source space first."}
	errCopyIntoPersonal = &CopyConflictError{Code: CopyPersonalCode,
		Message: "A personal space's permissions are its owner's own sharing, so nothing is copied into it. Change them in its permission table instead."}
)

// keepsAdministrator is the database's refusal of a space left without one.
const keepsAdministrator = "space_grant_keeps_administrator"

// ErrNoAdministrator refuses a permission table that names no administrator
// where the old one did, for a caller who does not administer the organization.
var ErrNoAdministrator = &FieldError{Field: "grants",
	Message: "A space keeps at least one administrator. Give administer to another person or group before you take it from the last one."}

// PreviewCopy says what copying another space's permissions onto this one
// would change, without changing anything.
func (s *Service) PreviewCopy(ctx context.Context, actor perm.Actor, key string, in CopyInput) (*CopyPreview, error) {
	if err := checkCopyInput(in, false); err != nil {
		return nil, err
	}
	var out *CopyPreview
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := loadCopy(ctx, tx, actor, key, in, false)
		if err != nil {
			return err
		}
		out = c.preview
		return nil
	})
	return out, err
}

// CopyPermissions applies the preview whose fingerprint in carries, in one
// transaction, and answers the target's table as written.
func (s *Service) CopyPermissions(ctx context.Context, actor perm.Actor, key string, in CopyInput) ([]perm.SpaceGrant, *CopyPreview, db.LSN, error) {
	if err := checkCopyInput(in, true); err != nil {
		return nil, nil, 0, err
	}
	var (
		out     []perm.SpaceGrant
		applied *CopyPreview
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := loadCopy(ctx, tx, actor, key, in, true)
		if err != nil {
			return err
		}
		if c.preview.Fingerprint != strings.TrimSpace(in.Fingerprint) {
			return &CopyConflictError{Code: CopyChangedCode, Message: fmt.Sprintf(
				"The permissions of %s or %s changed since the preview was made. Look at the new preview, then copy again.", c.source.Key, c.target.Key)}
		}
		if c.preview.LeavesNoAdministrator {
			return errCopyNoAdministrator
		}
		applied = c.preview
		out = gridOf(c.plan.Result)
		if len(c.plan.Changes) == 0 {
			return nil
		}
		if err := writeGrants(ctx, tx, c.target.ID, c.plan.Result); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, false, $2)`, c.target.ID, events.TraceParent(ctx)); err != nil {
			return fmt.Errorf("sync the space's Armature links: %w", err)
		}
		n := c.plan.Counts
		return record(ctx, tx, actor, audit.ActionSpacePermissionsCopied, c.target.ID, map[string]any{
			"key": c.target.Key, "from": map[string]any{"id": c.source.ID, "key": c.source.Key}, "mode": c.preview.Mode,
			"added": n.Added, "widened": n.Widened, "narrowed": n.Narrowed, "changed": n.Changed, "removed": n.Removed, "skipped": n.Skipped,
		})
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.ConstraintName == keepsAdministrator:
		return nil, nil, lsn, errCopyNoAdministrator
	case err != nil:
		if msg, ok := perm.GuestRefusal(err); ok {
			return nil, nil, lsn, &FieldError{Field: "from", Message: msg}
		}
		return nil, nil, lsn, err
	}
	return out, applied, lsn, nil
}

func checkCopyInput(in CopyInput, applying bool) error {
	if strings.TrimSpace(in.From) == "" {
		return &FieldError{Field: "from", Message: "Choose the space to copy permissions from."}
	}
	if !slices.Contains(perm.CopyModes, in.Mode) {
		return &FieldError{Field: "mode", Message: "Choose replace, to make this space's permissions the other's, or merge, to add the other's to these."}
	}
	if applying && strings.TrimSpace(in.Fingerprint) == "" {
		return &FieldError{Field: "fingerprint", Message: "Preview the copy first, then apply the preview you looked at."}
	}
	return nil
}

type copyCase struct {
	source, target *Space
	plan           perm.CopyPlan
	preview        *CopyPreview
}

// loadCopy reads both spaces and both tables and plans the copy. Applying
// locks the target, and the source against changes of its table, which
// lock the space's row too, until the copy commits.
func loadCopy(ctx context.Context, tx db.DBTX, actor perm.Actor, key string, in CopyInput, applying bool) (*copyCase, error) {
	targetLock, sourceLock := "", ""
	if applying {
		targetLock, sourceLock = ` FOR UPDATE`, ` FOR SHARE`
	}
	target, err := Load(ctx, tx, actor, ByKey+targetLock, key)
	if err != nil {
		return nil, err
	}
	if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, target.ID); err != nil {
		return nil, err
	}
	if target.Owner != nil {
		return nil, errCopyIntoPersonal
	}
	from := NormalizeKey(in.From)
	if from == target.Key {
		return nil, &FieldError{Field: "from", Message: "Choose another space than this one to copy permissions from."}
	}
	source, err := Load(ctx, tx, actor, ByKey+sourceLock, from)
	if errors.Is(err, ErrNotFound) {
		return nil, &FieldError{Field: "from", Message: fmt.Sprintf("There is no space %s that you may see. Choose one from the list.", from)}
	}
	if err != nil {
		return nil, err
	}
	if source.Owner != nil {
		return nil, &FieldError{Field: "from", Message: "A personal space's permissions are its owner's own sharing and are not copied. Choose a team space."}
	}
	if err := perm.Check(ctx, tx, actor, perm.CopyPermissionsFrom, source.ID); err != nil {
		return nil, err
	}
	sourceGrants, err := copyGrants(ctx, tx, source.ID)
	if err != nil {
		return nil, err
	}
	targetGrants, err := copyGrants(ctx, tx, target.ID)
	if err != nil {
		return nil, err
	}
	plan := perm.PlanCopy(sourceGrants, targetGrants, in.Mode)
	return &copyCase{source: source, target: target, plan: plan, preview: &CopyPreview{
		Source:                CopySpace{Key: source.Key, Name: source.Name},
		Target:                CopySpace{Key: target.Key, Name: target.Name},
		Mode:                  in.Mode,
		Changes:               plan.Changes,
		Skipped:               plan.Skipped,
		Kept:                  plan.Kept,
		Counts:                plan.Counts,
		LeavesNoAdministrator: plan.LeavesNoAdministrator,
		Fingerprint:           perm.CopyFingerprint(in.Mode, source.ID, target.ID, sourceGrants, targetGrants),
	}}, nil
}

// copyGrants reads a space's whole table, reading without signing in and
// guests included, a row per subject.
func copyGrants(ctx context.Context, tx db.DBTX, space uuid.UUID) ([]perm.CopyGrant, error) {
	rows, err := tx.Query(ctx, `
		SELECT g.subject_type, COALESCE(g.user_id, g.group_id),
		       CASE g.subject_type WHEN 'everyone' THEN $2 WHEN 'anonymous' THEN $3 ELSE COALESCE(u.name, gr.name, '') END,
		       COALESCE(m.org_role = 'guest', false), g.permission
		FROM space_grant g
		LEFT JOIN app_user u ON u.id = g.user_id
		LEFT JOIN groups gr ON gr.id = g.group_id
		LEFT JOIN org_member m ON m.org_id = g.org_id AND m.user_id = g.user_id
		WHERE g.space_id = $1
		ORDER BY CASE g.subject_type WHEN 'anonymous' THEN 0 WHEN 'everyone' THEN 1 WHEN 'group' THEN 2 ELSE 3 END,
		         lower(COALESCE(u.name, gr.name, '')), COALESCE(g.user_id, g.group_id)`,
		space, perm.EveryoneName, perm.AnonymousName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []perm.CopyGrant{}
	for rows.Next() {
		var (
			sub perm.CopySubject
			p   perm.SpacePermission
		)
		if err := rows.Scan(&sub.Type, &sub.ID, &sub.Name, &sub.Guest, &p); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].Subject.Key() == sub.Key() {
			out[n-1].Permissions = perm.Ordered(append(out[n-1].Permissions, p))
			continue
		}
		out = append(out, perm.CopyGrant{Subject: sub, Permissions: []perm.SpacePermission{p}})
	}
	return out, rows.Err()
}

// writeGrants makes a space's table the one given, reading without signing
// in included.
func writeGrants(ctx context.Context, tx db.DBTX, space uuid.UUID, table []perm.CopyGrant) error {
	// New rows go in before old ones go, as in SetPermissions, so whoever
	// administers the space still does while the policy asks.
	var perms, types, users, groups []string
	for _, g := range table {
		var user, group *uuid.UUID
		switch g.Subject.Type {
		case perm.SubjectUser:
			user = g.Subject.ID
		case perm.SubjectGroup:
			group = g.Subject.ID
		}
		for _, p := range g.Permissions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id, group_id)
				VALUES (current_org_id(), $1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
				space, string(p), string(g.Subject.Type), user, group); err != nil {
				return fmt.Errorf("grant %s: %w", p, err)
			}
			perms = append(perms, string(p))
			types = append(types, string(g.Subject.Type))
			users = append(users, idText(user))
			groups = append(groups, idText(group))
		}
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM space_grant g WHERE g.space_id = $1 AND NOT EXISTS (
			SELECT 1 FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS w (permission, subject_type, user_id, group_id)
			WHERE w.permission = g.permission AND w.subject_type = g.subject_type
			  AND NULLIF(w.user_id, '')::uuid IS NOT DISTINCT FROM g.user_id
			  AND NULLIF(w.group_id, '')::uuid IS NOT DISTINCT FROM g.group_id)`,
		space, perms, types, users, groups)
	return err
}

// gridOf is a table as the permission grid shows it, without reading
// without signing in, which has a switch of its own.
func gridOf(table []perm.CopyGrant) []perm.SpaceGrant {
	out := []perm.SpaceGrant{}
	for _, g := range table {
		if g.Subject.Type == perm.SubjectAnonymous {
			continue
		}
		out = append(out, perm.SpaceGrant{
			Subject:     perm.Subject{Type: g.Subject.Type, ID: g.Subject.ID, Name: g.Subject.Name},
			Permissions: g.Permissions,
		})
	}
	slices.SortStableFunc(out, func(a, b perm.SpaceGrant) int {
		if d := subjectRank(a.Subject) - subjectRank(b.Subject); d != 0 {
			return d
		}
		return strings.Compare(strings.ToLower(a.Subject.Name), strings.ToLower(b.Subject.Name))
	})
	return out
}
