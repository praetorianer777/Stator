package guest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// MaxEmailLength bounds an address as the mail standards do.
const MaxEmailLength = 254

// Service invites guests into spaces and takes them out, for the
// organization's administrators.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// List is the guests of a space, by name.
func (s *Service) List(ctx context.Context, actor perm.Actor, spaceKey string) ([]Guest, error) {
	var out []Guest
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := adminSpace(ctx, tx, actor, spaceKey)
		if err != nil {
			return err
		}
		out, err = list(ctx, tx, sp.ID, nil)
		return err
	})
	return out, err
}

// Invite lets the person with the address into the space with the role
// given, making their account if they have none. They sign in through the
// organization's provider with that address.
func (s *Service) Invite(ctx context.Context, actor perm.Actor, spaceKey string, in InviteInput) (*Guest, db.LSN, error) {
	email := auth.NormalizeEmail(in.Email)
	if !plausibleEmail(email) {
		return nil, 0, &FieldError{Field: "email", Message: "Enter the guest's email address, such as ada@example.com."}
	}
	if !slices.Contains(Invitable, in.Role) {
		return nil, 0, &FieldError{Field: "role", Message: "Choose what the guest may do: viewer, commenter or editor."}
	}
	// Checked before anybody's account is made, so a refusal leaves nothing behind.
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := adminSpace(ctx, tx, actor, spaceKey)
		return err
	}); err != nil {
		return nil, 0, err
	}
	// Making a person is signup's business, which no tenant's policies cover.
	var userID uuid.UUID
	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		name, _, _ := strings.Cut(email, "@")
		return tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name) VALUES ($1, $2)
			ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
			RETURNING id`, email, name).Scan(&userID)
	}); err != nil {
		return nil, 0, fmt.Errorf("find or make the account of %s: %w", email, err)
	}

	var out Guest
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := adminSpace(ctx, tx, actor, spaceKey)
		if err != nil {
			return err
		}
		if err := notYetHere(ctx, tx, userID, email, sp); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role, guest_space_id)
			VALUES (current_org_id(), $1, 'guest', $2)`, userID, sp.ID); err != nil {
			return fmt.Errorf("make the guest's membership: %w", err)
		}
		for _, p := range in.Role.Permissions() {
			if _, err := tx.Exec(ctx, `
				INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
				VALUES (current_org_id(), $1, $2, 'user', $3)`, sp.ID, string(p), userID); err != nil {
				return fmt.Errorf("grant the guest %s: %w", p, err)
			}
		}
		// Somebody who asked to join and is now invited has had their answer.
		if _, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE user_id = $1`, userID); err != nil {
			return err
		}
		guests, err := list(ctx, tx, sp.ID, &userID)
		if err != nil {
			return err
		}
		if len(guests) != 1 {
			return fmt.Errorf("read the new guest back: %d rows", len(guests))
		}
		out = guests[0]
		return perm.Record(ctx, tx, actor, audit.ActionGuestInvited, "user", &userID, map[string]any{
			"email": email, "role": in.Role, "space": sp.Key, "spaceId": sp.ID})
	})
	if err != nil {
		return nil, lsn, refusal(err)
	}
	return &out, lsn, nil
}

// Remove takes a guest out of the space and so out of the organization; their
// open sessions stop reaching it at once, as every request checks.
func (s *Service) Remove(ctx context.Context, actor perm.Actor, spaceKey string, userID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := adminSpace(ctx, tx, actor, spaceKey)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM org_member WHERE org_id = current_org_id() AND user_id = $1
			  AND org_role = 'guest' AND guest_space_id = $2`, userID, sp.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotGuest
		}
		return perm.Record(ctx, tx, actor, audit.ActionMemberRemoved, "user", &userID, map[string]any{
			"role": auth.RoleGuest, "space": sp.Key, "spaceId": sp.ID})
	})
}

// adminSpace reads the space for an administrator of the organization, the
// only people who let guests in, and refuses a personal one.
func adminSpace(ctx context.Context, tx db.DBTX, actor perm.Actor, key string) (*space.Space, error) {
	f, err := perm.LoadFacts(ctx, tx, actor, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if !f.OrgAdmin() {
		return nil, &perm.DeniedError{}
	}
	sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
	if err != nil {
		return nil, err
	}
	if sp.Owner != nil {
		return nil, ErrPersonalSpace
	}
	return sp, nil
}

// notYetHere refuses somebody already in the organization, saying what to do
// instead, since a guest is made only by an invitation.
func notYetHere(ctx context.Context, tx db.DBTX, userID uuid.UUID, email string, sp *space.Space) error {
	var (
		role     auth.OrgRole
		inSpace  *uuid.UUID
		named    *string
		spaceKey *string
	)
	err := tx.QueryRow(ctx, `
		SELECT m.org_role, m.guest_space_id, gs.name, gs.key FROM org_member m
		LEFT JOIN space gs ON gs.id = m.guest_space_id
		WHERE m.org_id = current_org_id() AND m.user_id = $1`, userID).Scan(&role, &inSpace, &named, &spaceKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	case role == auth.RoleGuest && inSpace != nil && *inSpace == sp.ID:
		return &FieldError{Field: "email", Message: fmt.Sprintf("%s is a guest of this space already. Change what they may do under the space's permissions.", email)}
	case role == auth.RoleGuest && named != nil:
		return &FieldError{Field: "email", Message: fmt.Sprintf("%s is a guest of %s (%s) already, and a guest belongs to one space. Remove them there first.", email, *named, *spaceKey)}
	}
	return &FieldError{Field: "email", Message: fmt.Sprintf("%s is a member of this organization already. Grant them this space under its permissions instead.", email)}
}

// list reads the guests of a space with their grants there, or one of them.
func list(ctx context.Context, tx db.DBTX, spaceID uuid.UUID, only *uuid.UUID) ([]Guest, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.email::text, u.name, m.created_at,
		       ARRAY(SELECT g.permission FROM space_grant g
		             WHERE g.org_id = m.org_id AND g.space_id = m.guest_space_id AND g.subject_type = 'user' AND g.user_id = m.user_id)
		FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = current_org_id() AND m.org_role = 'guest' AND m.guest_space_id = $1
		  AND ($2::uuid IS NULL OR m.user_id = $2)
		ORDER BY lower(u.name), u.email`, spaceID, only)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Guest, error) {
		var (
			g    Guest
			held []string
		)
		if err := row.Scan(&g.UserID, &g.Email, &g.Name, &g.InvitedAt, &held); err != nil {
			return g, err
		}
		g.Permissions = inOrder(held)
		g.Role = RoleOf(g.Permissions)
		return g, nil
	})
	if out == nil {
		out = []Guest{}
	}
	return out, err
}

// inOrder sorts permissions as the API lists them, from view to administer.
func inOrder(held []string) []perm.SpacePermission {
	out := []perm.SpacePermission{}
	for _, p := range perm.SpacePermissions {
		if slices.Contains(held, string(p)) {
			out = append(out, p)
		}
	}
	return out
}

// plausibleEmail is the shape of an address and no more: the provider proves it.
func plausibleEmail(email string) bool {
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && strings.Contains(domain, ".") && !strings.ContainsAny(email, " \t\r\n,;<>") &&
		len(email) <= MaxEmailLength && !strings.Contains(domain, "@")
}

// refusal reads the database's refusal of a guest as the sentence it carries.
func refusal(err error) error {
	if msg, ok := perm.GuestRefusal(err); ok {
		return &FieldError{Field: "email", Message: msg}
	}
	return err
}
