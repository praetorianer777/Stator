package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
)

// Limits of a page of grants.
const (
	DefaultAssignmentLimit = 50
	MaxAssignmentLimit     = 200
)

// Assignment is one role held by one subject in one scope, as a listing shows
// it: with the names filled in, because a page of identifiers explains nothing.
type Assignment struct {
	ID       uuid.UUID `json:"id"`
	Role     string    `json:"role"`
	RoleName string    `json:"roleName"`
	// SpaceID is nil for a role held across the whole installation.
	SpaceID   *uuid.UUID `json:"spaceId,omitempty"`
	SpaceKey  string     `json:"spaceKey,omitempty"`
	SpaceName string     `json:"spaceName,omitempty"`
	// Exactly one of the person and the group is set.
	UserID    *uuid.UUID `json:"userId,omitempty"`
	UserName  string     `json:"userName,omitempty"`
	UserEmail string     `json:"userEmail,omitempty"`
	// Guest marks a person from outside, let into the space this grant is in.
	Guest     bool       `json:"guest,omitempty"`
	GroupID   *uuid.UUID `json:"groupId,omitempty"`
	GroupName string     `json:"groupName,omitempty"`
	// GroupBuiltin is the group's mark when the installation keeps it, "everyone".
	GroupBuiltin string    `json:"groupBuiltin,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AssignmentPage is a page of grants and how many matched in all.
type AssignmentPage struct {
	Assignments []Assignment `json:"assignments"`
	Total       int          `json:"total"`
	Limit       int          `json:"limit"`
	Offset      int          `json:"offset"`
}

// AssignmentFilter narrows the grants listed.
type AssignmentFilter struct {
	// Q is text the person, the group, the role or the space holds.
	Q string
	// Space keeps the grants in this space, by key.
	Space string
	// Installation keeps the grants across the whole installation.
	Installation bool
	// Role keeps the grants of this role, by key.
	Role          string
	Limit, Offset int
}

// GrantInput grants a role to a person or a group, in a space or, with no
// space, across the whole installation.
type GrantInput struct {
	Role     string     `json:"role"`
	SpaceKey string     `json:"spaceKey"`
	UserID   *uuid.UUID `json:"userId"`
	GroupID  *uuid.UUID `json:"groupId"`
}

// ErrNoSuchAssignment is returned for a grant that is not there.
var ErrNoSuchAssignment = errors.New("there is no such grant")

const selectAssignment = `
SELECT a.id, a.role, r.name, a.space_id, COALESCE(s.key, ''), COALESCE(s.name, ''),
       a.user_id, COALESCE(u.name, ''), COALESCE(u.email::text, ''), COALESCE(m.role = 'guest', false),
       a.group_id, COALESCE(g.name, ''), COALESCE(g.builtin, ''), a.created_at
FROM role_assignment a
JOIN role r ON r.key = a.role
LEFT JOIN space s ON s.id = a.space_id
LEFT JOIN app_user u ON u.id = a.user_id
LEFT JOIN member m ON m.user_id = a.user_id
LEFT JOIN groups g ON g.id = a.group_id`

// assignmentOrder lists the installation first, then spaces by key; in each,
// the most powerful role first, Everyone and groups before people.
const assignmentOrder = `
ORDER BY a.space_id IS NOT NULL, s.key, r.sort_order, lower(r.name),
         g.builtin IS NULL, a.group_id IS NULL, lower(COALESCE(g.name, u.name, '')), a.id`

func scanAssignment(row pgx.CollectableRow) (Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.Role, &a.RoleName, &a.SpaceID, &a.SpaceKey, &a.SpaceName,
		&a.UserID, &a.UserName, &a.UserEmail, &a.Guest, &a.GroupID, &a.GroupName, &a.GroupBuiltin, &a.CreatedAt)
	return a, err
}

// SpaceAssignments are a space's own grants, in the caller's transaction.
func SpaceAssignments(ctx context.Context, tx db.DBTX, space uuid.UUID) ([]Assignment, error) {
	rows, err := tx.Query(ctx, selectAssignment+` WHERE a.space_id = $1`+assignmentOrder, space)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, scanAssignment)
	return nonNil(out), err
}

// Assignments lists who holds what, a page at a time. Grants across the
// installation are its administrators' to read; a space's, the space's too.
func (s *Service) Assignments(ctx context.Context, actor Actor, in AssignmentFilter) (*AssignmentPage, error) {
	switch {
	case in.Limit <= 0:
		in.Limit = DefaultAssignmentLimit
	case in.Limit > MaxAssignmentLimit:
		in.Limit = MaxAssignmentLimit
	}
	in.Offset = max(in.Offset, 0)
	out := AssignmentPage{Assignments: []Assignment{}, Limit: in.Limit, Offset: in.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var space *uuid.UUID
		if key := strings.ToUpper(strings.TrimSpace(in.Space)); key != "" {
			id, err := administeredSpace(ctx, tx, actor, key)
			if err != nil {
				return err
			}
			space = &id
		} else if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		where := `
			WHERE ($1::uuid IS NULL OR a.space_id = $1)
			  AND (NOT $2 OR a.space_id IS NULL)
			  AND ($3 = '' OR a.role = $3)
			  AND ($4 = '' OR r.name ILIKE $4 OR a.role ILIKE $4 OR u.name ILIKE $4 OR u.email::text ILIKE $4
			       OR g.name ILIKE $4 OR s.key ILIKE $4 OR s.name ILIKE $4)`
		q := ""
		if text := strings.TrimSpace(in.Q); text != "" {
			q = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
		}
		args := []any{space, in.Installation, strings.TrimSpace(in.Role), q}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM (`+selectAssignment+where+`) AS counted`, args...).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAssignment+where+assignmentOrder+` LIMIT $5 OFFSET $6`, append(args, in.Limit, in.Offset)...)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, scanAssignment)
		out.Assignments = nonNil(list)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// administeredSpace finds a space the actor administers by key, refusing
// one they do not, and saying so only to whoever may see it.
func administeredSpace(ctx context.Context, tx db.DBTX, actor Actor, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM space WHERE key = $1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, &FieldError{Field: "spaceKey", Message: fmt.Sprintf("There is no space %s that you may see. Pick one from the list.", key)}
	}
	if err != nil {
		return uuid.Nil, err
	}
	if err := Check(ctx, tx, actor, AdministerSpace, id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// Grant gives a role to a person or a group, in a space or across the
// installation. Granting the same twice is the one grant.
func (s *Service) Grant(ctx context.Context, actor Actor, in GrantInput) (*Assignment, db.LSN, error) {
	role := strings.TrimSpace(in.Role)
	if role == "" {
		return nil, 0, &FieldError{Field: "role", Message: "Choose the role to grant."}
	}
	if (in.UserID == nil) == (in.GroupID == nil) {
		return nil, 0, &FieldError{Field: "userId", Message: "Grant the role to one person or one group."}
	}
	var out Assignment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var space *uuid.UUID
		if key := strings.ToUpper(strings.TrimSpace(in.SpaceKey)); key != "" {
			id, err := administeredSpace(ctx, tx, actor, key)
			if err != nil {
				return err
			}
			space = &id
		} else if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var whole bool
		err := tx.QueryRow(ctx, `SELECT installation_wide_only FROM role WHERE key = $1`, role).Scan(&whole)
		if errors.Is(err, pgx.ErrNoRows) {
			return &FieldError{Field: "role", Message: fmt.Sprintf("There is no role %s. Pick one from the list.", role)}
		}
		if err != nil {
			return err
		}
		if whole && space != nil {
			return &FieldError{Field: "role", Message: "That role answers for the whole installation and cannot be granted in one space. Pick another role."}
		}
		ref := SubjectRef{Type: SubjectUser, ID: in.UserID}
		if in.GroupID != nil {
			ref = SubjectRef{Type: SubjectGroup, ID: in.GroupID}
		}
		field := "userId"
		if in.GroupID != nil {
			field = "groupId"
		}
		if _, err := resolve(ctx, tx, field, ref); err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO role_assignment (role, space_id, user_id, group_id, created_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (role, space_id, user_id, group_id) DO UPDATE SET role = EXCLUDED.role
			RETURNING id`, role, space, in.UserID, in.GroupID, actor.UserID).Scan(&id)
		if msg, ok := GuestRefusal(err); ok {
			return &FieldError{Field: field, Message: msg}
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAssignment+` WHERE a.id = $1`, id)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanAssignment); err != nil {
			return err
		}
		if space != nil {
			if err := linksFollow(ctx, tx, *space); err != nil {
				return err
			}
		}
		return Record(ctx, tx, actor, audit.ActionGrantAdded, "grant", &id, assignmentLog(out))
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// Revoke takes a grant away. The last that administers a space, or the last
// that leaves the installation an active administrator, stays.
func (s *Service) Revoke(ctx context.Context, actor Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectAssignment+` WHERE a.id = $1 FOR UPDATE OF a`, id)
		if err != nil {
			return err
		}
		a, err := pgx.CollectExactlyOneRow(rows, scanAssignment)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchAssignment
		}
		if err != nil {
			return err
		}
		if a.SpaceID != nil {
			if err := Check(ctx, tx, actor, AdministerSpace, *a.SpaceID); err != nil {
				return err
			}
		} else if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id); err != nil {
			return err
		}
		if a.SpaceID != nil {
			if err := linksFollow(ctx, tx, *a.SpaceID); err != nil {
				return err
			}
		}
		return Record(ctx, tx, actor, audit.ActionGrantRemoved, "grant", &id, assignmentLog(a))
	})
}

// linksFollow retitles the space's pages on the Armature issues they name,
// which follows whether every member may view the space.
func linksFollow(ctx context.Context, tx db.DBTX, space uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, false, $2)`, space, events.TraceParent(ctx)); err != nil {
		return fmt.Errorf("sync the space's Armature links: %w", err)
	}
	return nil
}

// assignmentLog is what the audit log keeps of a grant: ids and names as they
// were, since either may change later.
func assignmentLog(a Assignment) map[string]any {
	data := map[string]any{"role": a.Role, "roleName": a.RoleName}
	if a.SpaceID != nil {
		data["spaceId"], data["space"] = *a.SpaceID, a.SpaceKey
	}
	if a.UserID != nil {
		data["userId"], data["userName"] = *a.UserID, a.UserName
	}
	if a.GroupID != nil {
		data["groupId"], data["groupName"] = *a.GroupID, a.GroupName
	}
	return data
}
