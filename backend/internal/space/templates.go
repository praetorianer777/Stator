package space

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/template"
)

// spaceScoped are the blocks a template leaves without a space, meaning the
// one it makes; they are given its key, since a block reads null as every space.
var spaceScoped = []string{document.NodeLabelledPages, document.NodeRecentlyUpdated}

// chooseTemplate finds the template a new space asks for, nil for a blank one.
func chooseTemplate(in CreateInput) (*template.SpaceTemplate, error) {
	if in.Template == "" {
		return nil, nil
	}
	if in.Personal {
		return nil, &FieldError{Field: "template", Message: "A personal space starts blank. Leave the template out, or make a shared space from it."}
	}
	found, err := template.SpaceByKey(in.Template)
	if err != nil {
		return nil, &FieldError{Field: "template", Message: fmt.Sprintf("There is no space template %q. Pick one from the list of space templates, or leave it out for a blank space.", in.Template)}
	}
	return &found, nil
}

// forSpace is a template body as it is stored in the space with this key.
func forSpace(body json.RawMessage, key string) (json.RawMessage, error) {
	root, err := document.Parse(body)
	if err != nil {
		return nil, err
	}
	var fill func(n *document.Node)
	fill = func(n *document.Node) {
		if slices.Contains(spaceScoped, n.Type) && n.Attrs != nil && n.Attrs["space"] == nil {
			n.Attrs["space"] = key
		}
		for i := range n.Content {
			fill(&n.Content[i])
		}
	}
	fill(&root)
	return json.Marshal(root)
}

// seed fills a space just made from a template, its home page already
// published: the pages below it with their labels, and what everyone may do.
func seed(ctx context.Context, tx db.DBTX, actor perm.Actor, sp uuid.UUID, home uuid.UUID, key string, tpl *template.SpaceTemplate) error {
	if err := seedPages(ctx, tx, actor, sp, home, key, tpl.Pages); err != nil {
		return err
	}
	return presetGrants(ctx, tx, sp, tpl.Permissions.Everyone)
}

func seedPages(ctx context.Context, tx db.DBTX, actor perm.Actor, sp, parent uuid.UUID, key string, pages []template.SpacePage) error {
	ranks, err := rank.Sequence(len(pages))
	if err != nil {
		return err
	}
	for i, p := range pages {
		body, err := forSpace(p.Body, key)
		if err != nil {
			return fmt.Errorf("fill %q: %w", p.Title, err)
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO page (id, org_id, space_id, parent_id, rank, title, body, created_by, updated_by)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $7, $7)`, id, sp, parent, ranks[i], p.Title, body, actor.UserID); err != nil {
			return fmt.Errorf("make %q: %w", p.Title, err)
		}
		if err := publishFirst(ctx, tx, id); err != nil {
			return fmt.Errorf("publish %q: %w", p.Title, err)
		}
		for _, name := range p.Labels {
			if _, err := tx.Exec(ctx, `
				INSERT INTO page_label (org_id, page_id, name, created_by) VALUES (current_org_id(), $1, $2, $3)`, id, name, actor.UserID); err != nil {
				return fmt.Errorf("label %q: %w", p.Title, err)
			}
		}
		if err := seedPages(ctx, tx, actor, sp, id, key, p.Children); err != nil {
			return err
		}
	}
	return nil
}

// publishFirst makes a page's content its first version, as everybody who
// sees the space should see what it starts with.
func publishFirst(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_version (org_id, page_id, number, title, body, created_by)
		SELECT org_id, id, 1, title, body, created_by FROM page WHERE id = $1`, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE page SET version = 1 WHERE id = $1`, id)
	return err
}

// presetGrants replaces what everyone may do in a new space with the
// template's; the creator's own grant, which the database made, stays.
func presetGrants(ctx context.Context, tx db.DBTX, sp uuid.UUID, preset []perm.SpacePermission) error {
	everyone := make([]string, 0, len(preset))
	for _, p := range preset {
		everyone = append(everyone, string(p))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO space_grant (org_id, space_id, permission, subject_type)
		SELECT current_org_id(), $1, p, 'everyone' FROM unnest($2::text[]) AS p
		ON CONFLICT DO NOTHING`, sp, everyone); err != nil {
		return fmt.Errorf("grant the template's permissions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM space_grant WHERE space_id = $1 AND subject_type = 'everyone' AND NOT permission = ANY ($2::text[])`, sp, everyone); err != nil {
		return fmt.Errorf("take back what the template does not grant: %w", err)
	}
	return nil
}
