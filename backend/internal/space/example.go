package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
)

// ErrExampleExists says the organization has its example space already.
var ErrExampleExists = errors.New("the example space exists")

// ExampleInput is the example space as the example package makes it: the
// key to try, its words, its home page's body and what everyone may do in it.
type ExampleInput struct {
	Key         string
	Name        string
	Description string
	Home        json.RawMessage
	Everyone    []perm.SpacePermission
	// Language is the one its pages are written in, for the audit log.
	Language string
}

// Example is the organization's example space, nil when there is none; only
// those who may make it ask.
func (s *Service) Example(ctx context.Context, actor perm.Actor) (*Space, error) {
	var out *Space
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		var err error
		out, err = exampleOf(ctx, tx, actor)
		return err
	})
	return out, err
}

func exampleOf(ctx context.Context, tx db.DBTX, actor perm.Actor) (*Space, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM space WHERE org_id = current_org_id() AND example`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found, err := Load(ctx, tx, actor, ByID, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return found, err
}

// CreateExample makes the example space and its published home page; a taken
// key is a FieldError on key, an existing example ErrExampleExists.
func (s *Service) CreateExample(ctx context.Context, actor perm.Actor, in ExampleInput) (*Space, db.LSN, error) {
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
	if err := document.Validate(in.Home); err != nil {
		return nil, 0, fmt.Errorf("the example's home page: %w", err)
	}
	var out *Space
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		id, home := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `
			INSERT INTO space (id, org_id, key, name, description, created_by, example)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, true)`, id, key, name, description, actor.UserID)
		if constraint(err) == "space_one_example" {
			return ErrExampleExists
		}
		if isUnique(err) {
			return &FieldError{Field: "key", Message: fmt.Sprintf("The key %s is taken by another space. Choose another.", key)}
		}
		if err != nil {
			return fmt.Errorf("save the example space: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page (id, org_id, space_id, rank, title, body, created_by, updated_by)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $6)`, home, id, rank.Initial(), name, in.Home, actor.UserID); err != nil {
			return fmt.Errorf("make the home page: %w", err)
		}
		if err := publishFirst(ctx, tx, home); err != nil {
			return fmt.Errorf("publish the home page: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE space SET home_page_id = $2 WHERE id = $1`, id, home); err != nil {
			return fmt.Errorf("name the home page: %w", err)
		}
		if err := presetGrants(ctx, tx, id, in.Everyone); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionExampleSpaceCreated, id, map[string]any{"key": key, "name": name, "language": in.Language}); err != nil {
			return err
		}
		out, err = Load(ctx, tx, actor, ByID, id)
		return err
	})
	return out, lsn, err
}
