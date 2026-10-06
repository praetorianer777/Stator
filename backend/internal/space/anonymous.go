package space

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// AnonymousAccess is whether anybody may read a space without signing in.
type AnonymousAccess struct {
	// View grants reading the space's published pages to anybody, which
	// counts only while the organization allows it.
	View bool `json:"view"`
	// OrgEnabled says whether the organization allows it at all.
	OrgEnabled bool `json:"orgEnabled"`
	// Personal spaces are never public.
	Personal bool `json:"personal"`
}

// AnonymousAccessInput grants reading without signing in, or takes it away.
type AnonymousAccessInput struct {
	View bool `json:"view"`
}

// ErrPersonalNotPublic refuses opening a personal space to anybody: it is
// named after its owner, and its pages are theirs.
var ErrPersonalNotPublic = &FieldError{Field: "view",
	Message: "A personal space cannot be read without signing in. Move the pages to a team space to publish them."}

// anonymousTeamSpace is the guard that keeps personal spaces private.
const anonymousTeamSpace = "space_grant_anonymous_team_space"

// AnonymousAccess says whether anybody may read the space. For whoever
// administers it.
func (s *Service) AnonymousAccess(ctx context.Context, actor perm.Actor, key string) (*AnonymousAccess, error) {
	var out *AnonymousAccess
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := Load(ctx, tx, actor, ByKey, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
			return err
		}
		out, err = anonymousAccess(ctx, tx, sp)
		return err
	})
	return out, err
}

func anonymousAccess(ctx context.Context, tx db.DBTX, sp *Space) (*AnonymousAccess, error) {
	out := AnonymousAccess{Personal: sp.Owner != nil}
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM space_grant WHERE space_id = $1 AND subject_type = 'anonymous' AND permission = 'view'),
		       (SELECT anonymous_access FROM org WHERE id = current_org_id())`, sp.ID).Scan(&out.View, &out.OrgEnabled)
	if err != nil {
		return nil, fmt.Errorf("read whether anybody may read the space: %w", err)
	}
	return &out, nil
}

// SetAnonymousAccess lets anybody read the space without signing in, or
// stops it; a personal space stays private.
func (s *Service) SetAnonymousAccess(ctx context.Context, actor perm.Actor, key string, in AnonymousAccessInput) (*AnonymousAccess, db.LSN, error) {
	var out *AnonymousAccess
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := Load(ctx, tx, actor, ByKey+` FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
			return err
		}
		before, err := anonymousAccess(ctx, tx, sp)
		if err != nil {
			return err
		}
		if in.View && sp.Owner != nil {
			return ErrPersonalNotPublic
		}
		if in.View {
			_, err = tx.Exec(ctx, `
				INSERT INTO space_grant (org_id, space_id, permission, subject_type)
				VALUES (current_org_id(), $1, 'view', 'anonymous') ON CONFLICT DO NOTHING`, sp.ID)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM space_grant WHERE space_id = $1 AND subject_type = 'anonymous'`, sp.ID)
		}
		if err != nil {
			return fmt.Errorf("change whether anybody may read the space: %w", err)
		}
		if out, err = anonymousAccess(ctx, tx, sp); err != nil {
			return err
		}
		if before.View == out.View {
			return nil
		}
		return record(ctx, tx, actor, audit.ActionSpaceAnonymousAccessSet, sp.ID, map[string]any{"key": sp.Key, "view": out.View})
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == anonymousTeamSpace {
		return nil, lsn, ErrPersonalNotPublic
	}
	return out, lsn, err
}
