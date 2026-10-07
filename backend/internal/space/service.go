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
	"github.com/praetorianer777/stator/backend/internal/events"
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
SELECT s.id, s.key, s.name, s.description, s.home_page_id, s.created_at, s.updated_at,
       s.archived_at, COALESCE((SELECT u.name FROM app_user u WHERE u.id = s.archived_by), ''),
       EXISTS (SELECT 1 FROM watch w WHERE w.space_id = s.id AND w.user_id = current_actor_id() AND w.kind = 'space'),
       EXISTS (SELECT 1 FROM star st WHERE st.space_id = s.id AND st.user_id = current_actor_id()),
       s.owner_id, COALESCE((SELECT u.name FROM app_user u WHERE u.id = s.owner_id), '')
FROM space s`

func scan(row pgx.Row) (*Space, error) {
	var (
		s         Space
		home      *uuid.UUID
		owner     *uuid.UUID
		ownerName string
	)
	err := row.Scan(&s.ID, &s.Key, &s.Name, &s.Description, &home, &s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt, &s.ArchivedByName, &s.Watching, &s.Starred, &owner, &ownerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if home != nil {
		s.HomePageID = *home
	}
	if owner != nil {
		s.Owner = &SpaceOwner{ID: *owner, Name: ownerName}
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
	f, err := perm.LoadFacts(ctx, tx, actor, s.ID)
	if err != nil {
		return nil, err
	}
	if !perm.Decide(f, perm.ViewSpace) {
		return nil, ErrNotFound
	}
	s.Can = can(f, s)
	return s, nil
}

// can is what the facts offer in a space, which in an archived one is reading
// and administering it.
func can(f perm.Facts, s *Space) perm.Can {
	c := f.Can()
	if s.ArchivedAt != nil {
		c.EditPages, c.AddComments, c.DeletePages = false, false, false
	}
	return c
}

// ByKey is the where clause of Load for a key, which it normalizes.
const ByKey = `s.key = upper(btrim($1))`

// ByID is the where clause of Load for an id.
const ByID = `s.id = $1`

// List is every space the actor may see, by name, archived ones only when
// asked for, as Armature lists its projects.
func (s *Service) List(ctx context.Context, actor perm.Actor, includeArchived bool) ([]Space, error) {
	out := []Space{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectSpaces+` WHERE `+perm.ViewableSpace("s", 1)+` AND ($2 OR s.archived_at IS NULL)
			ORDER BY lower(s.name), s.key`, actor.UserID, includeArchived)
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
			f, err := perm.LoadFacts(ctx, tx, actor, one.ID)
			if err != nil {
				return err
			}
			if perm.Decide(f, perm.ViewSpace) {
				one.Can = can(f, one)
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
	tpl, err := chooseTemplate(in)
	if err != nil {
		return nil, 0, err
	}
	var out *Space
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		action, owner := perm.CreateSpace, (*uuid.UUID)(nil)
		if in.Personal {
			action, owner = perm.CreatePersonalSpace, &actor.UserID
		}
		if err := perm.Check(ctx, tx, actor, action, uuid.Nil); err != nil {
			return err
		}
		if in.Personal {
			var mine string
			err := tx.QueryRow(ctx, `SELECT key FROM space WHERE owner_id = $1 AND org_id = current_org_id()`, actor.UserID).Scan(&mine)
			if err == nil {
				return &PersonalTakenError{Key: mine}
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("look for the personal space: %w", err)
			}
		}
		// The ids are made here rather than returned: a row the statement
		// writes is not yet one its own snapshot lets the policies see.
		id, home := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `
			INSERT INTO space (id, org_id, key, name, description, created_by, owner_id)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6)`, id, key, name, description, actor.UserID, owner)
		// Made a moment ago by another request, which the look above missed.
		if constraint(err) == "space_one_personal" {
			return &PersonalTakenError{}
		}
		if isUnique(err) {
			return &FieldError{Field: "key", Message: fmt.Sprintf("The key %s is taken by another space. Choose another.", key)}
		}
		if err != nil {
			return fmt.Errorf("save the space: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page (id, org_id, space_id, rank, title, created_by, updated_by)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $5)`, home, id, rank.Initial(), name, actor.UserID); err != nil {
			return fmt.Errorf("make the home page: %w", err)
		}
		if tpl != nil {
			body, err := forSpace(tpl.Home, key)
			if err != nil {
				return fmt.Errorf("fill the home page: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE page SET body = $2 WHERE id = $1`, home, body); err != nil {
				return fmt.Errorf("fill the home page: %w", err)
			}
		}
		// Everybody who sees the space sees its home page, so it starts published.
		if err := publishFirst(ctx, tx, home); err != nil {
			return fmt.Errorf("publish the home page: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE space SET home_page_id = $2 WHERE id = $1`, id, home); err != nil {
			return fmt.Errorf("name the home page: %w", err)
		}
		logged := map[string]any{"key": key, "name": name, "personal": in.Personal}
		if tpl != nil {
			if err := seed(ctx, tx, actor, id, home, key, tpl); err != nil {
				return err
			}
			logged["template"] = tpl.Key
		}
		if err := record(ctx, tx, actor, audit.ActionSpaceCreated, id, logged); err != nil {
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

// Archive archives a space, or with archived false unarchives it. Archiving
// it twice, or unarchiving one that is not, is no change.
func (s *Service) Archive(ctx context.Context, actor perm.Actor, key string, archived bool) (*Space, db.LSN, error) {
	var out *Space
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := Load(ctx, tx, actor, ByKey+` FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.ArchiveSpace, current.ID); err != nil {
			return err
		}
		if (current.ArchivedAt != nil) != archived {
			// The database stamps when and by whom from the change itself.
			if _, err := tx.Exec(ctx, `UPDATE space SET archived_at = CASE WHEN $2 THEN now() END WHERE id = $1`, current.ID, archived); err != nil {
				return fmt.Errorf("archive the space: %w", err)
			}
			action := audit.ActionSpaceArchived
			if !archived {
				action = audit.ActionSpaceUnarchived
			}
			if err := record(ctx, tx, actor, action, current.ID, map[string]any{"key": current.Key, "name": current.Name}); err != nil {
				return err
			}
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
		if err := deleteSpace(ctx, tx, current.ID); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionSpaceDeleted, current.ID, map[string]any{"key": current.Key, "name": current.Name})
	})
}

func deleteSpace(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	// Before the pages go: the sync finds them gone and takes their links
	// off the issues they named.
	if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, false, $2)`, id, events.TraceParent(ctx)); err != nil {
		return fmt.Errorf("sync the space's Armature links: %w", err)
	}
	_, err := tx.Exec(ctx, `DELETE FROM space WHERE id = $1`, id)
	return err
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

// constraint names the constraint a unique violation broke, or "".
func constraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
