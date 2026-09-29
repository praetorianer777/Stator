package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Members lists the people of the organization on ctx, owners first, then by
// name. It reads through row level security like any other tenant data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	out := []Member{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, m.org_role, m.role_source, m.created_at
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = $1
			ORDER BY CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, lower(u.name), u.email`, org.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.RoleSource, &m.JoinedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// RemoveMember takes somebody and their groups out of the organization on ctx;
// their open sessions stop reaching it at once, as every request checks.
func (s *Service) RemoveMember(ctx context.Context, userID, actor uuid.UUID, ip string) (db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if userID == actor {
		return 0, ErrRemoveSelf
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var role OrgRole
		err := tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2 FOR UPDATE`, org.ID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchMember
		}
		if err != nil {
			return err
		}
		if role == RoleOwner {
			return ErrOwnerStays
		}
		// The group rows would outlive the membership otherwise: their guard
		// checks membership only when a row is written.
		if _, err := tx.Exec(ctx, `DELETE FROM group_member WHERE org_id = $1 AND user_id = $2`, org.ID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, org.ID, userID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionMemberRemoved, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"role": role}})
	})
}
