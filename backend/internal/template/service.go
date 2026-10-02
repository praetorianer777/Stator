package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// MaxNameLength, MaxDescriptionLength and MaxTitleLength match the
	// database's checks; a title is a page's title.
	MaxNameLength        = 100
	MaxDescriptionLength = 500
	MaxTitleLength       = 255
)

// nameIndex is the unique index that keeps two templates of one scope from
// sharing a name.
const nameIndex = "page_template_name_idx"

// Input is what one of the organization's templates is made of.
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Title is what a page made from it is called; DateToken is the day it
	// is made and a variable's name in braces is its value.
	Title     string          `json:"title"`
	Body      json.RawMessage `json:"body"`
	Variables []Variable      `json:"variables"`
}

// CreateInput makes a template for the space SpaceKey names, or for every
// space of the organization when it is empty.
type CreateInput struct {
	SpaceKey string `json:"spaceKey,omitempty"`
	Input
}

// Service keeps the organization's own templates.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// clean tidies an input and refuses what a page could not be made from.
func (in Input) clean() (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Name == "":
		return in, fieldError("name", "Give the template a name.")
	case utf8.RuneCountInString(in.Name) > MaxNameLength:
		return in, fieldError("name", "Keep the name to %d characters.", MaxNameLength)
	case utf8.RuneCountInString(in.Description) > MaxDescriptionLength:
		return in, fieldError("description", "Keep the description to %d characters.", MaxDescriptionLength)
	case utf8.RuneCountInString(in.Title) > MaxTitleLength:
		return in, fieldError("title", "Keep the title to %d characters.", MaxTitleLength)
	case len(in.Body) == 0 || string(in.Body) == "null":
		return in, fieldError("body", "Give the template a body; start from an empty paragraph if it has nothing yet.")
	}
	vars, err := cleanVariables(in.Variables)
	if err != nil {
		return in, err
	}
	in.Variables = vars
	root, err := document.ParseTemplate(in.Body)
	if err != nil {
		return in, err
	}
	if err := checkBody(root, vars); err != nil {
		return in, err
	}
	return in, nil
}

const columns = `t.id, t.space_id, COALESCE(s.key, ''), t.name, t.description, t.title, t.body, t.variables`

const from = ` FROM page_template t LEFT JOIN space s ON s.org_id = t.org_id AND s.id = t.space_id`

func scan(row pgx.Row) (Template, error) {
	var (
		t    Template
		id   uuid.UUID
		vars []byte
	)
	if err := row.Scan(&id, &t.spaceID, &t.SpaceKey, &t.Name, &t.Description, &t.Title, &t.Body, &vars); err != nil {
		return t, err
	}
	t.Key = id.String()
	t.Scope = ScopeOrganization
	if t.spaceID != nil {
		t.Scope = ScopeSpace
	}
	if err := json.Unmarshal(vars, &t.Variables); err != nil {
		return t, fmt.Errorf("read the variables of template %s: %w", t.Key, err)
	}
	if t.Variables == nil {
		t.Variables = []Variable{}
	}
	return t, nil
}

// load reads one of the organization's templates the caller may read.
func load(ctx context.Context, tx db.DBTX, id uuid.UUID) (Template, error) {
	t, err := scan(tx.QueryRow(ctx, `SELECT `+columns+from+` WHERE t.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrUnknown
	}
	return t, err
}

// spaceID finds a space the caller may view by its key.
func spaceID(ctx context.Context, tx db.DBTX, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM space WHERE key = $1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, ErrUnknownSpace
	}
	return id, err
}

// keeper says whether the actor may change templates of the scope given: a
// space's id, or nil for the organization's.
type keeper struct {
	ctx   context.Context
	tx    db.DBTX
	actor perm.Actor
	seen  map[uuid.UUID]bool
}

func (k *keeper) may(space *uuid.UUID) bool {
	key := uuid.Nil
	if space != nil {
		key = *space
	}
	if may, ok := k.seen[key]; ok {
		return may
	}
	action := perm.KeepOrgTemplates
	if space != nil {
		action = perm.KeepTemplates
	}
	may := perm.Allowed(k.ctx, k.tx, k.actor, action, key)
	k.seen[key] = may
	return may
}

func (k *keeper) check(space *uuid.UUID) error {
	if k.may(space) {
		return nil
	}
	action := perm.KeepOrgTemplates
	if space != nil {
		action = perm.KeepTemplates
	}
	return &perm.DeniedError{Action: action}
}

func newKeeper(ctx context.Context, tx db.DBTX, actor perm.Actor) *keeper {
	return &keeper{ctx: ctx, tx: tx, actor: actor, seen: map[uuid.UUID]bool{}}
}

// List is what a new page can start from: the space's own templates when a
// space is named, then the organization's, then the built-ins, each by name.
func (s *Service) List(ctx context.Context, actor perm.Actor, spaceKey string) ([]Template, error) {
	var out []Template
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		space := uuid.Nil
		if spaceKey != "" {
			var err error
			if space, err = spaceID(ctx, tx, spaceKey); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT `+columns+from+`
			WHERE t.space_id IS NULL OR t.space_id = $1
			ORDER BY t.space_id IS NULL, lower(t.name), t.id`, space)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Template, error) { return scan(row) })
		if err != nil {
			return err
		}
		k := newKeeper(ctx, tx, actor)
		for i := range out {
			out[i].CanEdit = k.may(out[i].spaceID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return append(out, BuiltIns()...), nil
}

// Get reads one template by its key: a built-in's name or one of the
// organization's ids.
func (s *Service) Get(ctx context.Context, actor perm.Actor, key string) (Template, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return ByKey(key)
	}
	var out Template
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if out, err = load(ctx, tx, id); err != nil {
			return err
		}
		out.CanEdit = newKeeper(ctx, tx, actor).may(out.spaceID)
		return nil
	})
	return out, err
}

// Create makes a template for a space or for the whole organization.
func (s *Service) Create(ctx context.Context, actor perm.Actor, in CreateInput) (*Template, db.LSN, error) {
	clean, err := in.Input.clean()
	if err != nil {
		return nil, 0, err
	}
	vars, err := json.Marshal(clean.Variables)
	if err != nil {
		return nil, 0, err
	}
	var out Template
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var space *uuid.UUID
		if key := strings.TrimSpace(in.SpaceKey); key != "" {
			id, err := spaceID(ctx, tx, key)
			if err != nil {
				return err
			}
			space = &id
		}
		if err := newKeeper(ctx, tx, actor).check(space); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_template (id, org_id, space_id, name, description, title, body, variables)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $7)`,
			id, space, clean.Name, clean.Description, clean.Title, string(clean.Body), string(vars)); err != nil {
			return nameTaken(err)
		}
		if out, err = load(ctx, tx, id); err != nil {
			return err
		}
		out.CanEdit = true
		return record(ctx, tx, actor, audit.ActionTemplateCreated, out)
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// Update replaces what a template is made of; where it belongs stays.
func (s *Service) Update(ctx context.Context, actor perm.Actor, key string, in Input) (*Template, db.LSN, error) {
	id, err := customID(key)
	if err != nil {
		return nil, 0, err
	}
	clean, err := in.clean()
	if err != nil {
		return nil, 0, err
	}
	vars, err := json.Marshal(clean.Variables)
	if err != nil {
		return nil, 0, err
	}
	var out Template
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := load(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := newKeeper(ctx, tx, actor).check(current.spaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE page_template SET name = $2, description = $3, title = $4, body = $5, variables = $6 WHERE id = $1`,
			id, clean.Name, clean.Description, clean.Title, string(clean.Body), string(vars)); err != nil {
			return nameTaken(err)
		}
		if out, err = load(ctx, tx, id); err != nil {
			return err
		}
		out.CanEdit = true
		return record(ctx, tx, actor, audit.ActionTemplateUpdated, out)
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// Delete removes a template; pages made from it keep what they were given.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, key string) (db.LSN, error) {
	id, err := customID(key)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := load(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := newKeeper(ctx, tx, actor).check(current.spaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM page_template WHERE id = $1`, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionTemplateDeleted, current)
	})
}

// customID reads the key of one of the organization's templates; a
// built-in's is refused for what it is.
func customID(key string) (uuid.UUID, error) {
	id, err := uuid.Parse(key)
	if err == nil {
		return id, nil
	}
	if _, err := ByKey(key); err == nil {
		return uuid.Nil, ErrBuiltIn
	}
	return uuid.Nil, ErrUnknown
}

func nameTaken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == nameIndex {
		return fieldError("name", "There is a template by that name here already; choose another name.")
	}
	return err
}

func record(ctx context.Context, tx db.DBTX, actor perm.Actor, action string, t Template) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	id, _ := uuid.Parse(t.Key)
	data := map[string]any{"name": t.Name, "variables": len(t.Variables)}
	if t.SpaceKey != "" {
		data["space"] = t.SpaceKey
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "template", TargetID: &id, Actor: actor.UserID, Data: data})
}

// Instantiate fills a template in for a page made in space, in its transaction:
// the body held to the page allowlist, and the title, empty taking the template's.
func Instantiate(ctx context.Context, tx db.DBTX, space uuid.UUID, key string, values map[string]string, title string) (string, json.RawMessage, error) {
	tpl, err := forSpace(ctx, tx, space, key)
	if err != nil {
		return "", nil, err
	}
	today := time.Now().UTC().Format(time.DateOnly)
	resolved, err := Resolve(tpl.Variables, values, today)
	if err != nil {
		return "", nil, err
	}
	people, err := peopleIn(ctx, tx, space, tpl.Variables, resolved)
	if err != nil {
		return "", nil, err
	}
	root, err := document.ParseTemplate(tpl.Body)
	if err != nil {
		return "", nil, err
	}
	body, err := json.Marshal(Fill(root, tpl.Variables, resolved, people))
	if err != nil {
		return "", nil, err
	}
	if err := document.Validate(body); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(title) == "" {
		title = strings.ReplaceAll(tpl.Title, DateToken, today)
	}
	return FillTitle(title, tpl.Variables, resolved, people), body, nil
}

func forSpace(ctx context.Context, tx db.DBTX, space uuid.UUID, key string) (Template, error) {
	unknown := fieldError("template", "There is no such template; pick one from the list of templates.")
	id, err := uuid.Parse(key)
	if err != nil {
		tpl, err := ByKey(key)
		if errors.Is(err, ErrUnknown) {
			return tpl, unknown
		}
		return tpl, err
	}
	tpl, err := load(ctx, tx, id)
	if errors.Is(err, ErrUnknown) {
		return tpl, unknown
	}
	if err != nil {
		return tpl, err
	}
	if tpl.spaceID != nil && *tpl.spaceID != space {
		return tpl, fieldError("template", "That template belongs to another space; pick one this space offers.")
	}
	return tpl, nil
}

// peopleIn names each person a value gives, refusing anybody who is not a
// member who may view the space, whom a mention would only confuse.
func peopleIn(ctx context.Context, tx db.DBTX, space uuid.UUID, vars []Variable, values map[string]string) (People, error) {
	var ids []uuid.UUID
	for _, v := range vars {
		if v.Kind == Person && values[v.Name] != "" {
			ids = append(ids, uuid.MustParse(values[v.Name]))
		}
	}
	people := People{}
	if len(ids) == 0 {
		return people, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id, COALESCE(NULLIF(btrim(u.name), ''), u.email::text) FROM app_user u
		WHERE u.id = ANY($1::uuid[]) AND perm_is_member(u.id) AND perm_space_holds(u.id, $2, 'view')`, ids, space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		people[id.String()] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, v := range vars {
		if value := values[v.Name]; v.Kind == Person && value != "" {
			if _, ok := people[value]; !ok {
				return nil, valueError(v, "%s has to be somebody who may view this space; pick another person.", v.Label)
			}
		}
	}
	return people, nil
}
