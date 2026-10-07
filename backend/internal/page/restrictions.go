package page

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	// EditGrant are who else may edit the page and the pages below it,
	// though the space does not let them add pages; it narrows nothing.
	EditGrant []perm.Subject `json:"editGrant"`
	// Inherited are the restricted pages above this one, the home page first.
	Inherited []InheritedRestriction `json:"inherited"`
}

// InheritedRestriction is a restriction on a page above, which binds this one
// too, or a grant there, which reaches it.
type InheritedRestriction struct {
	Page      Ref            `json:"page"`
	View      []perm.Subject `json:"view"`
	Edit      []perm.Subject `json:"edit"`
	EditGrant []perm.Subject `json:"editGrant"`
}

// RestrictionsInput replaces a page's own lists; an empty list lifts one.
// EditGrant left out keeps who else may edit as it is.
type RestrictionsInput struct {
	View      []perm.SubjectRef  `json:"view"`
	Edit      []perm.SubjectRef  `json:"edit"`
	EditGrant *[]perm.SubjectRef `json:"editGrant,omitempty"`
}

// ownLists are one page's own lists by kind.
type ownLists struct {
	view, edit, editGrant []perm.Subject
}

// lists reads one page's own lists.
func lists(ctx context.Context, tx db.DBTX, id uuid.UUID) (ownLists, error) {
	out := ownLists{view: []perm.Subject{}, edit: []perm.Subject{}, editGrant: []perm.Subject{}}
	rows, err := tx.Query(ctx, `SELECT `+perm.SubjectColumns+`, g.kind FROM page_restriction g`+perm.SubjectJoins+`
		WHERE g.page_id = $1 ORDER BY `+perm.SubjectOrder, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			s    perm.Subject
			kind string
		)
		if err := rows.Scan(&s.Type, &s.ID, &s.Name, &kind); err != nil {
			return out, err
		}
		switch perm.ListKind(kind) {
		case perm.ListView:
			out.view = append(out.view, s)
		case perm.ListEdit:
			out.edit = append(out.edit, s)
		case perm.ListEditGrant:
			out.editGrant = append(out.editGrant, s)
		}
	}
	return out, rows.Err()
}

func restrictionsOf(ctx context.Context, tx db.DBTX, p *Page) (*Restrictions, error) {
	own, err := lists(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	out := &Restrictions{View: own.view, Edit: own.edit, EditGrant: own.editGrant, Inherited: []InheritedRestriction{}}
	for _, above := range p.Ancestors {
		l, err := lists(ctx, tx, above.ID)
		if err != nil {
			return nil, err
		}
		if len(l.view) > 0 || len(l.edit) > 0 || len(l.editGrant) > 0 {
			out.Inherited = append(out.Inherited, InheritedRestriction{Page: above, View: l.view, Edit: l.edit, EditGrant: l.editGrant})
		}
	}
	return out, nil
}

// sameSubjects says two lists name the same people and groups, in any order.
func sameSubjects(a, b []perm.Subject) bool {
	if len(a) != len(b) {
		return false
	}
	keys := map[string]bool{}
	for _, s := range a {
		keys[subjectKey(s)] = true
	}
	for _, s := range b {
		if !keys[subjectKey(s)] {
			return false
		}
	}
	return true
}

func subjectKey(s perm.Subject) string {
	if s.ID == nil {
		return string(s.Type)
	}
	return string(s.Type) + ":" + s.ID.String()
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

// SetRestrictions replaces a page's own lists, which reach the pages below.
// Only an administrator of the space changes who else may edit.
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
		before, err := lists(ctx, tx, id)
		if err != nil {
			return err
		}
		grant := before.editGrant
		if in.EditGrant != nil {
			if grant, err = perm.ResolveSubjects(ctx, tx, "editGrant", *in.EditGrant, false); err != nil {
				return err
			}
			if !sameSubjects(grant, before.editGrant) {
				if err := p.must(perm.GrantEdit); err != nil {
					return err
				}
			}
		}
		var kinds, types, users, groups []string
		for kind, subjects := range map[perm.ListKind][]perm.Subject{perm.ListView: view, perm.ListEdit: edit, perm.ListEditGrant: grant} {
			for _, sub := range subjects {
				kinds = append(kinds, string(kind))
				types = append(types, string(sub.Type))
				users = append(users, idText(sub.UserID()))
				groups = append(groups, idText(sub.GroupID()))
			}
		}
		// One statement, so the policies judge every row by the page as it
		// was, and the rows it keeps are neither deleted nor written again:
		// a policy checks a row proposed even when ON CONFLICT drops it.
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
			SELECT current_org_id(), $1, w.kind, w.subject_type, w.user_id, w.group_id FROM wanted w
			WHERE NOT EXISTS (
				SELECT 1 FROM page_restriction r WHERE r.page_id = $1 AND r.kind = w.kind AND r.subject_type = w.subject_type
				  AND r.user_id IS NOT DISTINCT FROM w.user_id AND r.group_id IS NOT DISTINCT FROM w.group_id)
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
		// Whether a page is restricted decides how it is titled on an issue.
		if err := syncLinksBelow(ctx, tx, []uuid.UUID{id}, true); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionPageRestrictionsSet, id, map[string]any{
			"space": sp.Key, "title": p.Title, "view": perm.SubjectLog(view), "edit": perm.SubjectLog(edit),
			"editGrant": perm.SubjectLog(grant)}); err != nil {
			return err
		}
		out, err = restrictionsOf(ctx, tx, p)
		return err
	})
	if msg, ok := perm.GuestRefusal(err); ok {
		return nil, lsn, &FieldError{Field: "editGrant", Message: msg}
	}
	return out, lsn, err
}

// CannotEdit is somebody a page's edit list would name who could not edit it,
// since the space does not let them add pages and no grant names them.
type CannotEdit struct {
	Subject perm.Subject `json:"subject"`
	// Members is, for a group, how many of its members could not edit.
	Members int `json:"members"`
}

// RestrictionsCheck is what lists not yet saved would mean.
type RestrictionsCheck struct {
	// CannotEdit are the edit list's people and groups who could not edit
	// the page, in the order the list gave them.
	CannotEdit []CannotEdit `json:"cannotEdit"`
}

// CheckRestrictions says who of the edit list given could not edit the page
// for want of add pages, with the grant list given, for whoever may edit it.
func (s *Service) CheckRestrictions(ctx context.Context, actor perm.Actor, id uuid.UUID, in RestrictionsInput) (*RestrictionsCheck, error) {
	out := &RestrictionsCheck{CannotEdit: []CannotEdit{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		edit, err := perm.ResolveSubjects(ctx, tx, "edit", in.Edit, false)
		if err != nil {
			return err
		}
		grant, err := lists(ctx, tx, id)
		if err != nil {
			return err
		}
		granted := grant.editGrant
		if in.EditGrant != nil {
			if granted, err = perm.ResolveSubjects(ctx, tx, "editGrant", *in.EditGrant, false); err != nil {
				return err
			}
		}
		editUsers, editGroups := subjectIDs(edit)
		grantUsers, grantGroups := subjectIDs(granted)
		rows, err := tx.Query(ctx, `SELECT user_id, group_id FROM page_edit_blocked($1, $2, $3, $4, $5)`,
			id, editUsers, editGroups, grantUsers, grantGroups)
		if err != nil {
			return fmt.Errorf("check the edit list: %w", err)
		}
		blockedUsers, blockedMembers := map[uuid.UUID]bool{}, map[uuid.UUID]int{}
		for rows.Next() {
			var (
				user  uuid.UUID
				group *uuid.UUID
			)
			if err := rows.Scan(&user, &group); err != nil {
				rows.Close()
				return err
			}
			if group == nil {
				blockedUsers[user] = true
			} else {
				blockedMembers[*group]++
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, sub := range edit {
			switch {
			case sub.Type == perm.SubjectUser && blockedUsers[*sub.ID]:
				out.CannotEdit = append(out.CannotEdit, CannotEdit{Subject: sub})
			case sub.Type == perm.SubjectGroup && blockedMembers[*sub.ID] > 0:
				out.CannotEdit = append(out.CannotEdit, CannotEdit{Subject: sub, Members: blockedMembers[*sub.ID]})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// subjectIDs splits subjects into the ids of people and of groups.
func subjectIDs(subjects []perm.Subject) (users, groups []uuid.UUID) {
	users, groups = []uuid.UUID{}, []uuid.UUID{}
	for _, s := range subjects {
		switch s.Type {
		case perm.SubjectUser:
			users = append(users, *s.ID)
		case perm.SubjectGroup:
			groups = append(groups, *s.ID)
		}
	}
	return users, groups
}

// InspectAccess explains what a person may do to a page and why. Only an
// administrator of its space may ask, and only about a page they can view.
func (s *Service) InspectAccess(ctx context.Context, actor perm.Actor, id, person uuid.UUID) (*perm.AccessReport, error) {
	var out *perm.AccessReport
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var space uuid.UUID
		err := tx.QueryRow(ctx, `SELECT p.space_id FROM page p WHERE p.id = $1 AND`+live+` AND `+perm.ViewablePage("p", 2),
			id, actor.UserID).Scan(&space)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.InspectAccess, space); err != nil {
			return err
		}
		out, err = perm.Inspect(ctx, tx, person, id)
		return err
	})
	return out, err
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
