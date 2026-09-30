package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Limits of the people and group pickers.
const (
	DefaultPickerLimit = 20
	MaxPickerLimit     = 50
)

// EveryoneName is how an answer names the everyone subject.
const EveryoneName = "Everyone"

var (
	// ErrFixed refuses changing administer, which follows the roles.
	ErrFixed = errors.New("administering the organization follows the owner and admin roles; change those under Users")
	// ErrUnknownPermission is the answer for a permission that does not exist.
	ErrUnknownPermission = errors.New("there is no such permission")
)

// FieldError refuses one field of a request, in a sentence shown beside it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Service answers the permission screens: the caller's own rights, the
// global grants, and the pickers that name people and groups.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// Mine is what the caller may do across the organization.
func (s *Service) Mine(ctx context.Context, actor Actor) (GlobalCan, error) {
	var out GlobalCan
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = Global(ctx, tx, actor)
		return err
	})
	return out, err
}

// requireAdmin refuses anybody but an owner or administrator.
func requireAdmin(ctx context.Context, tx db.DBTX, actor Actor) error {
	f, err := LoadFacts(ctx, tx, actor, uuid.Nil)
	if err != nil {
		return err
	}
	if !f.OrgAdmin() {
		return &DeniedError{}
	}
	return nil
}

// GlobalGrants lists every global permission and whom it is granted to.
func (s *Service) GlobalGrants(ctx context.Context, actor Actor) ([]GlobalGrant, error) {
	var out []GlobalGrant
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		out = make([]GlobalGrant, 0, len(GlobalPermissions))
		for _, p := range GlobalPermissions {
			g, err := globalGrant(ctx, tx, p)
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return nil
	})
	return out, err
}

func globalGrant(ctx context.Context, tx db.DBTX, p GlobalPermission) (GlobalGrant, error) {
	if p == AdministerOrg {
		rows, err := tx.Query(ctx, `
			SELECT 'user', u.id, u.name FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND m.org_role IN ('owner', 'admin')
			ORDER BY lower(u.name), u.id`)
		if err != nil {
			return GlobalGrant{}, err
		}
		subjects, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Subject])
		return GlobalGrant{Permission: p, Subjects: nonNil(subjects), Fixed: true}, err
	}
	rows, err := tx.Query(ctx, `SELECT `+SubjectColumns+` FROM global_grant g`+SubjectJoins+`
		WHERE g.permission = $1 ORDER BY `+SubjectOrder, string(p))
	if err != nil {
		return GlobalGrant{}, err
	}
	subjects, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Subject])
	return GlobalGrant{Permission: p, Subjects: nonNil(subjects)}, err
}

// SubjectColumns, SubjectJoins and SubjectOrder read a Subject from a table
// aliased g with subject_type, user_id and group_id: everyone first, then
// groups, then people, each by name.
const (
	SubjectColumns = `g.subject_type, COALESCE(g.user_id, g.group_id), COALESCE(u.name, gr.name, '` + EveryoneName + `')`
	SubjectJoins   = `
		LEFT JOIN app_user u ON u.id = g.user_id
		LEFT JOIN groups gr ON gr.id = g.group_id`
	SubjectOrder = `CASE g.subject_type WHEN 'everyone' THEN 0 WHEN 'group' THEN 1 ELSE 2 END,
		lower(COALESCE(u.name, gr.name, '')), COALESCE(g.user_id, g.group_id)`
)

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// SetGlobalGrant replaces whom a global permission is granted to.
func (s *Service) SetGlobalGrant(ctx context.Context, actor Actor, permission GlobalPermission, in GlobalGrantInput) (*GlobalGrant, db.LSN, error) {
	if !slices.Contains(GlobalPermissions, permission) {
		return nil, 0, ErrUnknownPermission
	}
	if permission == AdministerOrg {
		return nil, 0, ErrFixed
	}
	var out GlobalGrant
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		subjects, err := ResolveSubjects(ctx, tx, "subjects", in.Subjects, true)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM global_grant WHERE permission = $1`, string(permission)); err != nil {
			return err
		}
		for _, sub := range subjects {
			if _, err := tx.Exec(ctx, `
				INSERT INTO global_grant (org_id, permission, subject_type, user_id, group_id)
				VALUES (current_org_id(), $1, $2, $3, $4)`, string(permission), string(sub.Type), sub.UserID(), sub.GroupID()); err != nil {
				return fmt.Errorf("grant %s: %w", permission, err)
			}
		}
		if out, err = globalGrant(ctx, tx, permission); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionOrgPermissionSet, "org", nil, map[string]any{
			"permission": permission, "subjects": SubjectLog(out.Subjects)})
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// UserID and GroupID are the columns a stored subject fills.
func (s Subject) UserID() *uuid.UUID {
	if s.Type == SubjectUser {
		return s.ID
	}
	return nil
}

func (s Subject) GroupID() *uuid.UUID {
	if s.Type == SubjectGroup {
		return s.ID
	}
	return nil
}

// ResolveSubjects checks each subject exists in the organization and names
// it, dropping repeats; field is the request field a refusal names.
func ResolveSubjects(ctx context.Context, tx db.DBTX, field string, refs []SubjectRef, everyone bool) ([]Subject, error) {
	out := make([]Subject, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		sub, err := resolve(ctx, tx, field, ref, everyone)
		if err != nil {
			return nil, err
		}
		key := string(sub.Type)
		if sub.ID != nil {
			key += ":" + sub.ID.String()
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, sub)
		}
	}
	return out, nil
}

func resolve(ctx context.Context, tx db.DBTX, field string, ref SubjectRef, everyone bool) (Subject, error) {
	switch ref.Type {
	case SubjectEveryone:
		if !everyone {
			return Subject{}, &FieldError{Field: field, Message: "A restriction names people and groups. To open a page to everyone, remove its restriction instead."}
		}
		return Subject{Type: SubjectEveryone, Name: EveryoneName}, nil
	case SubjectUser, SubjectGroup:
	default:
		return Subject{}, &FieldError{Field: field, Message: "Name each subject as a user, a group or everyone."}
	}
	if ref.ID == nil {
		return Subject{}, &FieldError{Field: field, Message: "Give the id of each person or group you name."}
	}
	var (
		name string
		err  error
	)
	if ref.Type == SubjectUser {
		err = tx.QueryRow(ctx, `
			SELECT u.name FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND m.user_id = $1`, *ref.ID).Scan(&name)
	} else {
		err = tx.QueryRow(ctx, `SELECT name FROM groups WHERE id = $1`, *ref.ID).Scan(&name)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, &FieldError{Field: field, Message: fmt.Sprintf("There is no %s with the id %s in this organization. Pick one from the list.", ref.Type, *ref.ID)}
	}
	if err != nil {
		return Subject{}, err
	}
	id := *ref.ID
	return Subject{Type: ref.Type, ID: &id, Name: name}, nil
}

// SubjectLog is how the audit log keeps who was named: ids and names as
// they were, since either may change later.
func SubjectLog(subjects []Subject) []map[string]any {
	out := make([]map[string]any, 0, len(subjects))
	for _, s := range subjects {
		out = append(out, map[string]any{"type": s.Type, "id": s.ID, "name": s.Name})
	}
	return out
}

// Record notes a change of permissions in the organization's audit log.
func Record(ctx context.Context, tx db.DBTX, actor Actor, action, targetType string, target *uuid.UUID, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: targetType, TargetID: target, Actor: actor.UserID, Data: data})
}

// PickerLimit clamps a picker's limit, 0 standing for the default.
func PickerLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPickerLimit
	case limit > MaxPickerLimit:
		return MaxPickerLimit
	}
	return limit
}

// likePrefix is a LIKE pattern matching what starts with the text typed.
func likePrefix(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(strings.TrimSpace(q)) + "%"
}

// People are members whose name, a word of it, or email starts with q.
func (s *Service) People(ctx context.Context, actor Actor, q string, limit int) ([]Person, error) {
	out := []Person{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id()
			  AND (u.name ILIKE $1 OR u.name ILIKE '% ' || $1 OR u.email::text ILIKE $1)
			ORDER BY lower(u.name), u.email LIMIT $2`, likePrefix(q), PickerLimit(limit))
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Person])
		out = nonNil(out)
		return err
	})
	return out, err
}

// Groups are the organization's groups whose name, or a word of it, starts with q.
func (s *Service) Groups(ctx context.Context, actor Actor, q string, limit int) ([]Group, error) {
	out := []Group{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT g.id, g.name, (SELECT count(*)::int FROM group_member gm WHERE gm.group_id = g.id), g.source = 'oidc'
			FROM groups g
			WHERE g.name ILIKE $1 OR g.name ILIKE '% ' || $1
			ORDER BY lower(g.name), g.id LIMIT $2`, likePrefix(q), PickerLimit(limit))
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Group])
		out = nonNil(out)
		return err
	})
	return out, err
}
