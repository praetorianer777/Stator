package page

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// ErrLocksOut refuses restrictions that would leave their saver unable to view
// or edit the page.
var ErrLocksOut = errors.New("these restrictions would leave you unable to view or edit the page")

// Restricted flags a page whose view or edit is narrowed, here or above.
type Restricted struct {
	View bool `json:"view"`
	Edit bool `json:"edit"`
}

// Restrictions are who may view and edit a page beyond the space's own
// permissions, and why: its own lists and those of the pages above it.
type Restrictions struct {
	// View and Edit are the page's own lists; empty is no restriction here.
	View []perm.Subject `json:"view"`
	Edit []perm.Subject `json:"edit"`
	// Inherited are the restricted pages above this one, the home page first.
	Inherited []InheritedRestriction `json:"inherited"`
}

// InheritedRestriction is a restriction on a page above, which binds this one too.
type InheritedRestriction struct {
	Page Ref            `json:"page"`
	View []perm.Subject `json:"view"`
	Edit []perm.Subject `json:"edit"`
}

// RestrictionsInput replaces a page's own lists; an empty list lifts one.
type RestrictionsInput struct {
	View []perm.SubjectRef `json:"view"`
	Edit []perm.SubjectRef `json:"edit"`
}

// lists reads one page's own view and edit lists.
func lists(ctx context.Context, tx db.DBTX, id uuid.UUID) (view, edit []perm.Subject, err error) {
	rows, err := tx.Query(ctx, `SELECT `+perm.SubjectColumns+`, g.kind FROM page_restriction g`+perm.SubjectJoins+`
		WHERE g.page_id = $1 ORDER BY `+perm.SubjectOrder, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	view, edit = []perm.Subject{}, []perm.Subject{}
	for rows.Next() {
		var (
			s    perm.Subject
			kind string
		)
		if err := rows.Scan(&s.Type, &s.ID, &s.Name, &kind); err != nil {
			return nil, nil, err
		}
		if kind == "view" {
			view = append(view, s)
		} else {
			edit = append(edit, s)
		}
	}
	return view, edit, rows.Err()
}

func restrictionsOf(ctx context.Context, tx db.DBTX, p *Page) (*Restrictions, error) {
	view, edit, err := lists(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	out := &Restrictions{View: view, Edit: edit, Inherited: []InheritedRestriction{}}
	for _, above := range p.Ancestors {
		view, edit, err := lists(ctx, tx, above.ID)
		if err != nil {
			return nil, err
		}
		if len(view) > 0 || len(edit) > 0 {
			out.Inherited = append(out.Inherited, InheritedRestriction{Page: above, View: view, Edit: edit})
		}
	}
	return out, nil
}

// GetRestrictions is who may view and edit a page, and the restricted pages
// above it, which is what the lock dialog explains.
func (s *Service) GetRestrictions(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Restrictions, error) {
	var out *Restrictions
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		out, err = restrictionsOf(ctx, tx, p)
		return err
	})
	return out, err
}

// SetRestrictions replaces a page's own view and edit lists. The pages below
// inherit them.
func (s *Service) SetRestrictions(ctx context.Context, actor perm.Actor, id uuid.UUID, in RestrictionsInput) (*Restrictions, db.LSN, error) {
	var out *Restrictions
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		view, err := perm.ResolveSubjects(ctx, tx, "view", in.View, false)
		if err != nil {
			return err
		}
		edit, err := perm.ResolveSubjects(ctx, tx, "edit", in.Edit, false)
		if err != nil {
			return err
		}
		if p.Home && len(view) > 0 {
			return &FieldError{Field: "view", Message: "The home page cannot be hidden. Narrow who may view the space in its permissions instead."}
		}
		var kinds, types, users, groups []string
		for kind, subjects := range map[string][]perm.Subject{"view": view, "edit": edit} {
			for _, sub := range subjects {
				kinds = append(kinds, kind)
				types = append(types, string(sub.Type))
				users = append(users, idText(sub.UserID()))
				groups = append(groups, idText(sub.GroupID()))
			}
		}
		// One statement, so the policies judge every row by the page as it
		// was, and the rows it keeps are neither deleted nor written twice.
		if _, err := tx.Exec(ctx, `
			WITH wanted (kind, subject_type, user_id, group_id) AS (
				SELECT k, t, NULLIF(u, '')::uuid, NULLIF(g, '')::uuid
				FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS w (k, t, u, g)
			), gone AS (
				DELETE FROM page_restriction r WHERE r.page_id = $1 AND NOT EXISTS (
					SELECT 1 FROM wanted w WHERE w.kind = r.kind AND w.subject_type = r.subject_type
					  AND w.user_id IS NOT DISTINCT FROM r.user_id AND w.group_id IS NOT DISTINCT FROM r.group_id)
			)
			INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id, group_id)
			SELECT current_org_id(), $1, kind, subject_type, user_id, group_id FROM wanted
			ON CONFLICT DO NOTHING`, id, kinds, types, users, groups); err != nil {
			return fmt.Errorf("save the restrictions: %w", err)
		}
		after, _, err := perm.ForPage(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if perm.LocksOut(sp.Can.Administer, after) {
			return ErrLocksOut
		}
		if err := record(ctx, tx, actor, audit.ActionPageRestrictionsSet, id, map[string]any{
			"space": sp.Key, "title": p.Title, "view": perm.SubjectLog(view), "edit": perm.SubjectLog(edit)}); err != nil {
			return err
		}
		out, err = restrictionsOf(ctx, tx, p)
		return err
	})
	return out, lsn, err
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
