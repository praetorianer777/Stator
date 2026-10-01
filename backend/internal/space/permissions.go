package space

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Permissions is the space's permission table, a row per subject.
func (s *Service) Permissions(ctx context.Context, actor perm.Actor, key string) ([]perm.SpaceGrant, error) {
	var out []perm.SpaceGrant
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := Load(ctx, tx, actor, ByKey, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
			return err
		}
		out, err = grants(ctx, tx, sp.ID)
		return err
	})
	return out, err
}

func grants(ctx context.Context, tx db.DBTX, space uuid.UUID) ([]perm.SpaceGrant, error) {
	rows, err := tx.Query(ctx, `SELECT `+perm.SubjectColumns+`, g.permission FROM space_grant g`+perm.SubjectJoins+`
		WHERE g.space_id = $1 ORDER BY `+perm.SubjectOrder, space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []perm.SpaceGrant{}
	for rows.Next() {
		var (
			sub perm.Subject
			p   perm.SpacePermission
		)
		if err := rows.Scan(&sub.Type, &sub.ID, &sub.Name, &p); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && sameSubject(out[n-1].Subject, sub) {
			out[n-1].Permissions = append(out[n-1].Permissions, p)
			continue
		}
		out = append(out, perm.SpaceGrant{Subject: sub, Permissions: []perm.SpacePermission{p}})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Permissions = inOrder(out[i].Permissions)
	}
	return out, nil
}

func sameSubject(a, b perm.Subject) bool {
	return a.Type == b.Type && (a.ID == nil) == (b.ID == nil) && (a.ID == nil || *a.ID == *b.ID)
}

// inOrder sorts permissions as the API lists them, from view to administer.
func inOrder(ps []perm.SpacePermission) []perm.SpacePermission {
	out := make([]perm.SpacePermission, 0, len(ps))
	for _, p := range perm.SpacePermissions {
		if slices.Contains(ps, p) {
			out = append(out, p)
		}
	}
	return out
}

// SetPermissions replaces the space's whole permission table.
func (s *Service) SetPermissions(ctx context.Context, actor perm.Actor, key string, in perm.SpaceGrantsInput) ([]perm.SpaceGrant, db.LSN, error) {
	for _, row := range in.Grants {
		if len(row.Permissions) == 0 {
			return nil, 0, &FieldError{Field: "grants", Message: "Each row grants at least one permission. Remove a row to take everything away."}
		}
		for _, p := range row.Permissions {
			if !slices.Contains(perm.SpacePermissions, p) {
				return nil, 0, &FieldError{Field: "grants", Message: fmt.Sprintf("There is no space permission %q. Choose view, addPages, addComments, delete or administer.", p)}
			}
		}
	}
	var out []perm.SpaceGrant
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := Load(ctx, tx, actor, ByKey+` FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
			return err
		}
		type row struct {
			subject     perm.Subject
			permissions []perm.SpacePermission
		}
		var table []row
		for _, in := range in.Grants {
			subjects, err := perm.ResolveSubjects(ctx, tx, "grants", []perm.SubjectRef{in.Subject}, true)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(table, func(r row) bool { return sameSubject(r.subject, subjects[0]) })
			if i < 0 {
				table = append(table, row{subject: subjects[0]})
				i = len(table) - 1
			}
			table[i].permissions = inOrder(append(table[i].permissions, in.Permissions...))
		}
		// New rows go in before old ones go, so a space administrator who is
		// not an organization one still administers it while the policy asks.
		var perms, types, users, groups []string
		for _, r := range table {
			for _, p := range r.permissions {
				if _, err := tx.Exec(ctx, `
					INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id, group_id)
					VALUES (current_org_id(), $1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
					sp.ID, string(p), string(r.subject.Type), r.subject.UserID(), r.subject.GroupID()); err != nil {
					return fmt.Errorf("grant %s: %w", p, err)
				}
				perms = append(perms, string(p))
				types = append(types, string(r.subject.Type))
				users = append(users, idText(r.subject.UserID()))
				groups = append(groups, idText(r.subject.GroupID()))
			}
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM space_grant g WHERE g.space_id = $1 AND NOT EXISTS (
				SELECT 1 FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS w (permission, subject_type, user_id, group_id)
				WHERE w.permission = g.permission AND w.subject_type = g.subject_type
				  AND NULLIF(w.user_id, '')::uuid IS NOT DISTINCT FROM g.user_id
				  AND NULLIF(w.group_id, '')::uuid IS NOT DISTINCT FROM g.group_id)`,
			sp.ID, perms, types, users, groups); err != nil {
			return err
		}
		// Whether everyone may view the space decides how its pages are titled
		// on the Armature issues they name.
		if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, false, $2)`, sp.ID, events.TraceParent(ctx)); err != nil {
			return fmt.Errorf("sync the space's Armature links: %w", err)
		}
		// The answer is the table as written: somebody who just gave up
		// administering the space may no longer read it back.
		out = make([]perm.SpaceGrant, 0, len(table))
		for _, r := range table {
			out = append(out, perm.SpaceGrant{Subject: r.subject, Permissions: r.permissions})
		}
		slices.SortStableFunc(out, func(a, b perm.SpaceGrant) int { return subjectRank(a.Subject) - subjectRank(b.Subject) })
		logged := make([]map[string]any, 0, len(out))
		for _, g := range out {
			logged = append(logged, map[string]any{"subject": perm.SubjectLog([]perm.Subject{g.Subject})[0], "permissions": g.Permissions})
		}
		return record(ctx, tx, actor, audit.ActionSpacePermissionsSet, sp.ID, map[string]any{"key": sp.Key, "grants": logged})
	})
	return out, lsn, err
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// subjectRank orders everyone first, then groups, then people, as reads do.
func subjectRank(s perm.Subject) int {
	switch s.Type {
	case perm.SubjectEveryone:
		return 0
	case perm.SubjectGroup:
		return 1
	}
	return 2
}
