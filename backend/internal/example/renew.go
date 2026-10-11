package example

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// ErrGone says the example space was deleted before it could be renewed.
var ErrGone = errors.New("the example space is gone")

// postsListed bounds the blog an example made before #320 is read from; it
// started with two posts.
const postsListed = 50

// Renewal is what renewing the example starts from: its space, the version
// and language its pages are in, the language they are to be in, and the
// pages it knows, none for an example made before it kept them.
type Renewal struct {
	Space uuid.UUID
	From  int
	Was   string
	Lang  string
	Pages []Tracked
}

// Report is what a renewal added, and what it left to people.
type Report struct {
	// Added are the pages new in this version of the content.
	Added []ReportPage `json:"added"`
	// Remade are guide pages somebody deleted, made again.
	Remade []ReportPage `json:"remade"`
	// Kept are guide pages somebody changed, left as they are.
	Kept []ReportPage `json:"kept"`
	// Retired are pages the content no longer has, left as they are.
	Retired []ReportPage `json:"retired"`
}

// ReportPage names a page of the report.
type ReportPage struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

func reportOf(p *page.Page) ReportPage { return ReportPage{ID: p.ID, Title: p.Title} }

type standing int

const (
	// standingNew is a page this renewal made, to be published.
	standingNew standing = iota
	standingUnchanged
	standingChanged
	standingGone
)

type known struct {
	tracked  Tracked
	standing standing
	page     *page.Page
}

type renewing struct {
	m      *Maker
	actor  perm.Actor
	sp     *space.Space
	f      *Facts
	w      *writes
	pages  map[string]*known
	report *Report
}

// Renew brings the example's guides to the current content as the actor:
// pages nobody changed are published again, pages somebody changed are
// kept, pages new in the content are made, and what people added stays.
func (m *Maker) Renew(ctx context.Context, actor perm.Actor, me Person, in Renewal) (*Report, db.LSN, error) {
	sp, err := m.Spaces.Example(ctx, actor)
	if err != nil {
		return nil, 0, err
	}
	if sp == nil || sp.ID != in.Space {
		return nil, 0, ErrGone
	}
	now := m.now()
	r := &renewing{
		m: m, actor: actor, sp: sp, w: &writes{},
		f: &Facts{
			Lang: in.Lang, Key: sp.Key, Me: me,
			Today: day(now, 0), Soon: day(now, soonDays), NextWeek: day(now, nextWeekDays),
			Pages: map[string]uuid.UUID{}, Excerpts: map[string]uuid.UUID{}, Files: map[string]uuid.UUID{},
			Armature: m.armature(ctx),
		},
		pages:  map[string]*known{},
		report: &Report{Added: []ReportPage{}, Remade: []ReportPage{}, Kept: []ReportPage{}, Retired: []ReportPage{}},
	}
	tracked := in.Pages
	if len(tracked) == 0 {
		if tracked, err = m.adopt(ctx, actor, sp, in.Was); err != nil {
			return nil, 0, err
		}
	}
	steps := []func(context.Context) error{
		func(ctx context.Context) error { return r.resolve(ctx, tracked) },
		r.place, r.files, r.calendar, r.posts, r.publish,
		func(ctx context.Context) error { return r.rename(ctx, in.Was) },
	}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return nil, r.w.lsn, err
		}
	}
	lsn, err := m.Spaces.ExampleRenewed(ctx, actor, sp.ID, in.Lang, in.From, Current())
	r.w.note(lsn)
	return r.report, r.w.lsn, err
}

// resolve finds each page the example knows, and whether somebody changed it
// since the example last wrote it.
func (r *renewing) resolve(ctx context.Context, tracked []Tracked) error {
	content := map[string]bool{Home: true}
	Walk(func(e Entry) { content[e.Name] = true })
	for _, name := range Posts {
		content[name] = true
	}
	for _, t := range tracked {
		k := &known{tracked: t, standing: standingGone}
		p, _, err := r.m.Pages.Get(ctx, r.actor, t.PageID)
		if err != nil && !errors.Is(err, page.ErrNotFound) {
			return err
		}
		r.pages[t.Name] = k
		if err != nil {
			continue
		}
		k.page = p
		if !content[t.Name] {
			r.report.Retired = append(r.report.Retired, reportOf(p))
			continue
		}
		k.standing = standingUnchanged
		if changed := p.Version != t.Version || (p.Kind == page.KindFolder && p.Title != t.Title); changed {
			k.standing = standingChanged
			// An example made before #320 learns its pages here, kept ones too.
			if err := r.m.wrote(ctx, t); err != nil {
				return err
			}
		}
		r.f.Pages[t.Name] = p.ID
		if r.f.Excerpts[t.Name], err = r.excerptOf(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// excerptOf keeps a page's excerpt its id, so what includes it, the showcase
// or a page of somebody's, still finds it.
func (r *renewing) excerptOf(ctx context.Context, p *page.Page) (uuid.UUID, error) {
	if p.Kind == page.KindFolder {
		return uuid.Must(uuid.NewV7()), nil
	}
	excerpts, err := r.m.Pages.Excerpts(ctx, r.actor, p.ID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, e := range excerpts {
		if id, err := uuid.Parse(e.ID); err == nil {
			return id, nil
		}
	}
	return uuid.Must(uuid.NewV7()), nil
}

// place makes, unpublished, the pages new in the content and those somebody
// deleted, below the page they belong under, else the home page.
func (r *renewing) place(ctx context.Context) error {
	var place func(parent uuid.UUID, entries []Entry) error
	place = func(parent uuid.UUID, entries []Entry) error {
		for _, e := range entries {
			k := r.pages[e.Name]
			if k == nil || k.standing == standingGone {
				title, err := Title(r.f.Lang, e.Name)
				if err != nil {
					return err
				}
				made, lsn, err := r.m.Pages.Create(ctx, r.actor, page.CreateInput{Placement: page.Placement{ParentID: parent}, Title: title, Kind: e.Kind})
				r.w.note(lsn)
				if err != nil {
					return fmt.Errorf("make %s: %w", e.Name, err)
				}
				r.noteMade(e.Name, k, made)
				if e.Kind == page.KindFolder {
					if err := r.m.wrote(ctx, Tracked{Name: e.Name, PageID: made.ID, Version: made.Version, Title: made.Title}); err != nil {
						return err
					}
				}
				k = r.pages[e.Name]
			}
			if err := place(k.page.ID, e.Children); err != nil {
				return err
			}
		}
		return nil
	}
	return place(r.sp.HomePageID, Tree)
}

func (r *renewing) noteMade(name string, was *known, made *page.Page) {
	if was == nil {
		r.report.Added = append(r.report.Added, reportOf(made))
	} else {
		r.report.Remade = append(r.report.Remade, reportOf(made))
	}
	r.pages[name] = &known{standing: standingNew, page: made}
	r.f.Pages[name] = made.ID
	r.f.Excerpts[name] = uuid.Must(uuid.NewV7())
}

// files finds the showcase's uploads by name, and uploads what it lacks to a
// showcase the renewal writes; one somebody changed keeps its files.
func (r *renewing) files(ctx context.Context) error {
	showcase := r.pages[Showcase]
	if r.m.Attachments == nil || showcase.standing == standingChanged {
		return nil
	}
	if showcase.standing == standingNew {
		return r.m.upload(ctx, r.actor, r.f, r.w)
	}
	have, err := r.m.Attachments.List(ctx, r.actor, showcase.page.ID, true)
	if err != nil {
		return err
	}
	for _, a := range have {
		r.f.Files[a.FileName] = a.ID
	}
	files, err := showcaseFiles(r.f.Lang)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, ok := r.f.Files[file.name]; ok {
			continue
		}
		if err := r.m.uploadOne(ctx, r.actor, r.f, file, r.w); err != nil {
			return err
		}
	}
	return nil
}

// calendar finds the space's calendar, or makes it again for a showcase the
// renewal writes.
func (r *renewing) calendar(ctx context.Context) error {
	calendars, err := r.m.Calendars.List(ctx, r.actor, r.sp.Key)
	if err != nil {
		return err
	}
	if len(calendars) > 0 {
		r.f.Calendar = calendars[0].ID
		return nil
	}
	if r.pages[Showcase].standing == standingChanged {
		return nil
	}
	return r.m.calendar(ctx, r.actor, r.sp.Key, r.f, r.w)
}

func (r *renewing) posts(ctx context.Context) error {
	for _, name := range Posts {
		k := r.pages[name]
		switch {
		case k == nil || k.standing == standingGone:
			doc, _, err := Render(name, *r.f)
			if err != nil {
				return err
			}
			post, lsn, err := r.m.Pages.CreatePost(ctx, r.actor, r.sp.Key, page.PostInput{Title: doc.Title, Body: doc.Body, Publish: true})
			r.w.note(lsn)
			if err != nil {
				return fmt.Errorf("post %s: %w", name, err)
			}
			r.noteMade(name, k, post)
			if err := r.m.wrote(ctx, Tracked{Name: name, PageID: post.ID, Version: post.Version, Title: post.Title}); err != nil {
				return err
			}
			if err := r.m.label(ctx, r.actor, post.ID, doc.Labels, r.w); err != nil {
				return err
			}
		case k.standing == standingUnchanged:
			if err := r.rewrite(ctx, name, k); err != nil {
				return err
			}
		case k.standing == standingChanged:
			r.report.Kept = append(r.report.Kept, reportOf(k.page))
		}
	}
	return nil
}

// publish publishes the pages made, and again those nobody changed, in the
// order of the tree, once every page's id is known.
func (r *renewing) publish(ctx context.Context) error {
	names := []string{Home}
	Walk(func(e Entry) { names = append(names, e.Name) })
	for _, name := range names {
		k := r.pages[name]
		if k == nil {
			continue
		}
		switch k.standing {
		case standingNew:
			if k.page.Kind == page.KindFolder {
				continue
			}
			if err := r.m.publish(ctx, r.actor, name, *r.f, r.w); err != nil {
				return err
			}
		case standingUnchanged:
			if err := r.rewrite(ctx, name, k); err != nil {
				return err
			}
		case standingChanged:
			r.report.Kept = append(r.report.Kept, reportOf(k.page))
		}
	}
	return nil
}

// rewrite publishes a page nobody changed as the content has it now; one
// changed under the renewal is kept as it is.
func (r *renewing) rewrite(ctx context.Context, name string, k *known) error {
	var in page.UpdateInput
	var labels []string
	if k.page.Kind == page.KindFolder {
		title, err := Title(r.f.Lang, name)
		if err != nil {
			return err
		}
		if title == k.page.Title {
			return r.m.wrote(ctx, Tracked{Name: name, PageID: k.page.ID, Version: k.page.Version, Title: title})
		}
		in = page.UpdateInput{Title: &title, Version: k.page.Version}
	} else {
		doc, _, err := Render(name, *r.f)
		if err != nil {
			return err
		}
		in = page.UpdateInput{Title: &doc.Title, Body: doc.Body, Version: k.page.Version}
		labels = doc.Labels
	}
	updated, lsn, err := r.m.Pages.Update(ctx, r.actor, k.page.ID, in)
	r.w.note(lsn)
	if errors.Is(err, page.ErrStale) {
		r.report.Kept = append(r.report.Kept, reportOf(k.page))
		return nil
	}
	if err != nil {
		return fmt.Errorf("renew %s: %w", name, err)
	}
	if err := r.m.wrote(ctx, Tracked{Name: name, PageID: updated.ID, Version: updated.Version, Title: updated.Title}); err != nil {
		return err
	}
	return r.m.label(ctx, r.actor, updated.ID, labels, r.w)
}

// rename gives the space the name and description of the language its pages
// are now in, unless somebody gave it others.
func (r *renewing) rename(ctx context.Context, was string) error {
	if was == r.f.Lang {
		return nil
	}
	old, err := Title(was, Home)
	if err != nil {
		return err
	}
	name, err := Title(r.f.Lang, Home)
	if err != nil {
		return err
	}
	var in space.UpdateInput
	if r.sp.Name == old {
		in.Name = &name
	}
	if r.sp.Description == description(was) {
		words := description(r.f.Lang)
		in.Description = &words
	}
	if in.Name == nil && in.Description == nil {
		return nil
	}
	_, lsn, err := r.m.Spaces.Update(ctx, r.actor, r.sp.Key, in)
	r.w.note(lsn)
	return err
}

// adopt finds the pages of an example made before it kept them, by the titles
// it gave them in their first versions, which renaming leaves; it published
// each once, so version 1 is a page nobody changed. A folder has no versions
// and is found by its title.
func (m *Maker) adopt(ctx context.Context, actor perm.Actor, sp *space.Space, lang string) ([]Tracked, error) {
	out := []Tracked{{Name: Home, PageID: sp.HomePageID, Version: 1, Title: sp.Name}}
	var walk func(parent uuid.UUID, entries []Entry) error
	walk = func(parent uuid.UUID, entries []Entry) error {
		children, err := m.Pages.Children(ctx, actor, sp.Key, &parent)
		if err != nil {
			return err
		}
		for _, e := range entries {
			title, err := Title(lang, e.Name)
			if err != nil {
				return err
			}
			for _, c := range children {
				if (c.Kind == page.KindFolder) != (e.Kind == page.KindFolder) {
					continue
				}
				first := c.Title
				if c.Kind != page.KindFolder {
					if first, err = m.firstTitle(ctx, actor, c.ID, c.Title); err != nil {
						return err
					}
				}
				if first != title {
					continue
				}
				out = append(out, Tracked{Name: e.Name, PageID: c.ID, Version: 1, Title: c.Title})
				if err := walk(c.ID, e.Children); err != nil {
					return err
				}
				break
			}
		}
		return nil
	}
	if err := walk(sp.HomePageID, Tree); err != nil {
		return nil, err
	}
	posts, _, err := m.Pages.Posts(ctx, actor, page.PostsInput{SpaceKey: sp.Key, Limit: postsListed})
	if err != nil {
		return nil, err
	}
	for _, name := range Posts {
		title, err := Title(lang, name)
		if err != nil {
			return nil, err
		}
		for _, p := range posts {
			first, err := m.firstTitle(ctx, actor, p.ID, p.Title)
			if err != nil {
				return nil, err
			}
			if first == title {
				out = append(out, Tracked{Name: name, PageID: p.ID, Version: 1, Title: title})
				break
			}
		}
	}
	return out, nil
}

// firstTitle is the title a page was first published with, else its own.
func (m *Maker) firstTitle(ctx context.Context, actor perm.Actor, id uuid.UUID, title string) (string, error) {
	v, err := m.Pages.GetVersion(ctx, actor, id, 1)
	switch {
	case errors.Is(err, page.ErrVersionNotFound):
		return title, nil
	case err != nil:
		return "", err
	}
	return v.Title, nil
}
