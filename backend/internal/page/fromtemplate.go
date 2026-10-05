package page

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/template"
)

// A template button (#62) names its template by key, as GET /templates
// serves it, so whatever later answers to a key is found the same way.
const (
	msgNoTemplate = "That template is not there any more. Pick another one in the button's settings."
	msgNoTarget   = "Say where the page goes: a space, a page in it, or both."
)

// TemplateTarget is where a template button puts its page: under Parent, or
// at the top of the space when Parent is nil.
type TemplateTarget struct {
	Template string
	SpaceKey string
	Parent   *uuid.UUID
}

// TemplateRef names a template as a button shows it.
type TemplateRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Title is what a page made from it is called, with the date token, or empty.
	Title string `json:"title"`
}

// TemplateButton is what a template button shows its reader: the template,
// where the page would go, and whether they may put one there.
type TemplateButton struct {
	Template  TemplateRef `json:"template"`
	SpaceKey  string      `json:"spaceKey"`
	SpaceName string      `json:"spaceName"`
	// Parent is the page the new one goes under; the home page for the top of the space.
	Parent    Ref  `json:"parent"`
	CanCreate bool `json:"canCreate"`
}

// FromTemplateInput is a page made from a template: where it goes and,
// optionally, its title, in which the date token becomes today in UTC.
type FromTemplateInput struct {
	ParentID *uuid.UUID `json:"parentId,omitempty"`
	SpaceKey string     `json:"spaceKey,omitempty"`
	Title    string     `json:"title,omitempty"`
}

// templateByKey finds a template; a missing one is a field the caller fixes.
func templateByKey(key string) (template.Template, error) {
	tpl, err := template.ByKey(key)
	if errors.Is(err, template.ErrUnknown) {
		return tpl, &FieldError{Field: "template", Message: msgNoTemplate}
	}
	return tpl, err
}

// TitleFor is the title of a page a button makes: the button's own pattern,
// else the template's, else the template's name, the date token filled in.
func TitleFor(pattern string, tpl template.Template, now time.Time) string {
	title := strings.TrimSpace(pattern)
	if title == "" {
		title = strings.TrimSpace(tpl.Title)
	}
	if title == "" {
		title = tpl.Name
	}
	return strings.ReplaceAll(title, template.DateToken, now.UTC().Format(time.DateOnly))
}

// targetPage loads the page a new one goes under: the parent named, wherever
// it was moved, else the home page of the space named.
func targetPage(ctx context.Context, tx db.DBTX, actor perm.Actor, parent *uuid.UUID, spaceKey string) (*Page, *space.Space, error) {
	if parent != nil {
		return load(ctx, tx, actor, *parent, false)
	}
	if strings.TrimSpace(spaceKey) == "" {
		return nil, nil, &FieldError{Field: "spaceKey", Message: msgNoTarget}
	}
	sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
	if err != nil {
		return nil, nil, err
	}
	return load(ctx, tx, actor, sp.HomePageID, false)
}

// TemplateButton says what a button would make and where, as the actor sees
// it; a parent or space they may not view is not found.
func (s *Service) TemplateButton(ctx context.Context, actor perm.Actor, in TemplateTarget) (*TemplateButton, error) {
	tpl, err := templateByKey(in.Template)
	if err != nil {
		return nil, err
	}
	var out *TemplateButton
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := targetPage(ctx, tx, actor, in.Parent, in.SpaceKey)
		if err != nil {
			return err
		}
		out = &TemplateButton{
			Template: TemplateRef{Key: tpl.Key, Name: tpl.Name, Title: tpl.Title},
			SpaceKey: sp.Key, SpaceName: sp.Name,
			Parent:    Ref{ID: p.ID, Title: p.Title, Home: p.Home},
			CanCreate: p.must(perm.EditPages) == nil,
		}
		return nil
	})
	return out, err
}

// CreateFromTemplate makes an unpublished page of the actor's from a
// template, last under the target, as a new page from the tree is made.
func (s *Service) CreateFromTemplate(ctx context.Context, actor perm.Actor, key string, in FromTemplateInput, now time.Time) (*Page, db.LSN, error) {
	tpl, err := templateByKey(key)
	if err != nil {
		return nil, 0, err
	}
	parent := in.ParentID
	if parent == nil {
		var p *Page
		err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			var err error
			p, _, err = targetPage(ctx, tx, actor, nil, in.SpaceKey)
			return err
		})
		if err != nil {
			return nil, 0, err
		}
		parent = &p.ID
	}
	return s.Create(ctx, actor, CreateInput{
		Placement: Placement{ParentID: *parent},
		Title:     TitleFor(in.Title, tpl, now),
		Body:      tpl.Body,
	})
}
