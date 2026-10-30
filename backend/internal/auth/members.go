package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// Members lists the people of the installation, members before guests, then
// by name, each with the roles they hold across it. It reads through row
// level security like any other data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	out := []Member{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, m.role, u.is_active, m.created_at, gs.id, gs.key, gs.name,
			       COALESCE((SELECT jsonb_agg(jsonb_build_object('role', r.key, 'name', r.name, 'groupId', g.id, 'groupName', g.name)
			                                  ORDER BY r.sort_order, lower(r.name), g.name NULLS FIRST)
			                 FROM role_assignment a
			                 JOIN role r ON r.key = a.role
			                 LEFT JOIN groups g ON g.id = a.group_id
			                 WHERE a.space_id IS NULL AND m.role <> 'guest'
			                   AND (a.user_id = m.user_id OR EXISTS (
			                       SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = m.user_id))), '[]')
			FROM member m JOIN app_user u ON u.id = m.user_id
			LEFT JOIN space gs ON gs.id = m.guest_space_id
			ORDER BY m.role = 'guest', lower(u.name), u.email`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				m     Member
				guest guestSpaceColumns
			)
			if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.Active, &m.JoinedAt, &guest.id, &guest.key, &guest.name, &m.Roles); err != nil {
				return err
			}
			m.GuestSpace = guest.ref()
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// RemoveMember takes somebody and their groups out of the installation; their
// open sessions stop reaching it at once, as every request checks. The last
// active administrator stays, which the database holds to.
func (s *Service) RemoveMember(ctx context.Context, userID, actor uuid.UUID, ip string) (db.LSN, error) {
	if userID == actor {
		return 0, ErrRemoveSelf
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			role       Role
			guestSpace *uuid.UUID
		)
		err := tx.QueryRow(ctx, `SELECT role, guest_space_id FROM member WHERE user_id = $1 FOR UPDATE`, userID).Scan(&role, &guestSpace)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchMember
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, userID); err != nil {
			return err
		}
		// The group rows would outlive the membership otherwise: their guard
		// checks membership only when a row is written. Everyone's went with it.
		if _, err := tx.Exec(ctx, `DELETE FROM group_member WHERE user_id = $1`, userID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberRemoved, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: removedData(role, guestSpace)})
	})
}

// removedData is what the record keeps of a membership taken away: its role,
// and a guest's space.
func removedData(role Role, guestSpace *uuid.UUID) map[string]any {
	data := map[string]any{"role": role}
	if guestSpace != nil {
		data["spaceId"] = *guestSpace
	}
	return data
}

// GrantAdministrator grants somebody the built-in administrator role across
// the installation, in the caller's transaction; a grant there already is kept.
func GrantAdministrator(ctx context.Context, tx db.DBTX, userID uuid.UUID, by *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_assignment (role, user_id, created_by) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, AdministratorRole, userID, by); err != nil {
		return fmt.Errorf("make the person an administrator: %w", err)
	}
	return nil
}
