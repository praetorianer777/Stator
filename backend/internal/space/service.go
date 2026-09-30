package space

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Service keeps spaces and makes each one's home page with it.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

const selectSpaces = `
SELECT s.id, s.key, s.name, s.description, s.home_page_id, s.created_at, s.updated_at
FROM space s`

func scan(row pgx.Row) (*Space, error) {
	var (
		s    Space
		home *uuid.UUID
	)
	err := row.Scan(&s.ID, &s.Key, &s.Name, &s.Description, &home, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if home != nil {
		s.HomePageID = *home
	}
	return &s, err
}

// Load reads one space in the caller's transaction, by id or by key, and
// answers ErrNotFound for one the actor may not see.
func Load(ctx context.Context, tx db.DBTX, actor perm.Actor, where string, arg any) (*Space, error) {
	s, err := scan(tx.QueryRow(ctx, selectSpaces+` WHERE `+where, arg))
	if err != nil {
		return nil, err
	}
	if !perm.Allowed(ctx, tx, actor, perm.ViewSpace, s.ID) {
		return nil, ErrNotFound
	}
	s.Can = perm.On(ctx, tx, actor, s.ID)
	return s, nil
}

// ByKey is the where clause of Load for a key, which it normalizes.
const ByKey = `s.key = upper(btrim($1))`

// ByID is the where clause of Load for an id.
const ByID = `s.id = $1`

// List is every space the actor may see, by name.
func (s *Service) List(ctx context.Context, actor perm.Actor) ([]Space, error) {
	out := []Space{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectSpaces+` ORDER BY lower(s.name), s.key`)
		if err != nil {
			return err
		}
		var all []*Space
		for rows.Next() {
			one, err := scan(rows)
			if err != nil {
				rows.Close()
				return err
			}
			all = append(all, one)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, one := range all {
			if perm.Allowed(ctx, tx, actor, perm.ViewSpace, one.ID) {
				one.Can = perm.On(ctx, tx, actor, one.ID)
				out = append(out, *one)
			}
		}
		return nil
	})
	return out, err
}

// Get is one space by its key.
func (s *Service) Get(ctx context.Context, actor perm.Actor, key string) (*Space, error) {
	var out *Space
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = Load(ctx, tx, actor, ByKey, key)
		return err
	})
	return out, err
}

// Create makes a space and its home page, titled like the space, in one
// transaction, so no space is ever without its root.
func (s *Service) Create(ctx context.Context, actor perm.Actor, in CreateInput) (*Space, db.LSN, error) {
	key := NormalizeKey(in.Key)
	if err := checkKey(key); err != nil {
		return nil, 0, err
	}
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	description, err := cleanDescription(in.Description)
	if err != nil {
		return nil, 0, err
	}
	var out *Space
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateSpace, uuid.Nil); err != nil {
			return err
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO space (org_id, key, name, description, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`, key, name, description, actor.UserID).Scan(&id)
		if isUnique(err) {
			return &FieldError{Field: "key", Message: fmt.Sprintf("The key %s is taken by another space. Choose another.", key)}
		}
		if err != nil {
			return fmt.Errorf("save the space: %w", err)
		}
		var home uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO page (org_id, space_id, rank, title, created_by, updated_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $4) RETURNING id`, id, rank.Initial(), name, actor.UserID).Scan(&home); err != nil {
			return fmt.Errorf("make the home page: %w", err)
		}
		// Everybody who sees the space sees its home page, so it starts published.
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
			SELECT org_id, id, 1, title, body, created_by FROM page WHERE id = $1`, home); err != nil {
			return fmt.Errorf("publish the home page: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET version = 1 WHERE id = $1`, home); err != nil {
			return fmt.Errorf("publish the home page: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE space SET home_page_id = $2 WHERE id = $1`, id, home); err != nil {
			return fmt.Errorf("name the home page: %w", err)
		}
		if err := record(ctx, tx, actor, audit.ActionSpaceCreated, id, map[string]any{"key": key, "name": name}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, actor, ByID, id)
		return err
	})
	return out, lsn, err
}

// Update changes a space's name or description.
func (s *Service) Update(ctx context.Context, actor perm.Actor, key string, in UpdateInput) (*Space, db.LSN, error) {
	var out *Space
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := Load(ctx, tx, actor, ByKey+` FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, current.ID); err != nil {
			return err
		}
		name, description := current.Name, current.Description
		if in.Name != nil {
			if name, err = cleanName(*in.Name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if description, err = cleanDescription(*in.Description); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE space SET name = $2, description = $3 WHERE id = $1`, current.ID, name, description); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionSpaceUpdated, current.ID, map[string]any{"key": current.Key, "name": name}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, actor, ByID, current.ID)
		return err
	})
	return out, lsn, err
}

// Delete removes a space and, by the cascade, every page in it.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := Load(ctx, tx, actor, ByKey+` FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.DeleteSpace, current.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM space WHERE id = $1`, current.ID); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionSpaceDeleted, current.ID, map[string]any{"key": current.Key, "name": current.Name})
	})
}

// record notes an administrator's act on a space in the organization's audit
// log, in the transaction that did it.
func record(ctx context.Context, tx db.DBTX, actor perm.Actor, action string, id uuid.UUID, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "space", TargetID: &id, Actor: actor.UserID, Data: data})
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
