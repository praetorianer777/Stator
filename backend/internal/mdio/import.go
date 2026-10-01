package mdio

import (
	"bytes"
	"context"
	"errors"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service moves pages in and out as Markdown through the page and file
// services, so every read and write is checked as the API checks it.
type Service struct {
	pages *page.Service
	// files is nil on a server without storage, which then moves no files.
	files *attachment.Service
	// MaxBytes is what one import may weigh.
	MaxBytes int64
}

func NewService(pages *page.Service, files *attachment.Service) *Service {
	return &Service{pages: pages, files: files, MaxBytes: DefaultMaxImportBytes}
}

// Imported is one page an import made, in reading order.
type Imported struct {
	ID       uuid.UUID `json:"id"`
	ParentID uuid.UUID `json:"parentId"`
	Title    string    `json:"title"`
	// Depth is 1 for a page made right under the one imported into.
	Depth int `json:"depth"`
}

// ImportResult is what an import made, and what did not come across as written.
type ImportResult struct {
	Pages    []Imported `json:"pages"`
	Warnings []string   `json:"warnings"`
}

// entry is one page to make: from a Markdown file, a folder, or both.
type entry struct {
	md       string
	dir      string
	name     string
	children []*entry
	title    string
	files    []string
	id       uuid.UUID
	uploaded map[string]*attachment.Attachment
}

// upload is an import's files by path, and its pages by the paths that name them.
type upload struct {
	files  map[string][]byte
	order  []string
	byPath map[string]*entry
}

// tree arranges the pages: each Markdown file is a page, and each folder
// holding Markdown one too, its index.md or README.md the folder's page.
func (u *upload) tree(dir, skip string) []*entry {
	type slot struct {
		e     *entry
		first int
	}
	var slots []slot
	dirs := map[string]*entry{}
	mds := map[string]*entry{}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	for i, p := range u.order {
		if !strings.HasPrefix(p, prefix) || p == skip {
			continue
		}
		rest := p[len(prefix):]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			sub := rest[:slash]
			if dirs[sub] == nil && u.holdsMarkdown(prefix+sub) {
				e := &entry{dir: prefix + sub, name: sub}
				dirs[sub] = e
				slots = append(slots, slot{e, i})
			}
			continue
		}
		if isMarkdown(rest) {
			e := &entry{md: p, name: rest}
			mds[strings.TrimSuffix(rest, path.Ext(rest))] = e
			slots = append(slots, slot{e, i})
		}
	}
	var out []*entry
	for _, s := range slots {
		e := s.e
		if e.md == "" {
			if base, ok := strings.CutSuffix(e.name, filesSuffix); ok && mds[base] != nil {
				continue
			}
			if paired := mds[e.name]; paired != nil {
				continue
			}
			e.md = u.indexOf(e.dir)
			e.children = u.tree(e.dir, e.md)
		} else if d := dirs[strings.TrimSuffix(e.name, path.Ext(e.name))]; d != nil {
			e.dir = d.dir
			e.children = u.tree(e.dir, "")
		}
		out = append(out, e)
	}
	return out
}

func (u *upload) holdsMarkdown(dir string) bool {
	for _, p := range u.order {
		if strings.HasPrefix(p, dir+"/") && isMarkdown(p) {
			return true
		}
	}
	return false
}

// indexOf finds a folder's own page among its files.
func (u *upload) indexOf(dir string) string {
	for _, name := range []string{"index", "readme"} {
		for _, p := range u.order {
			if path.Dir(p) == dir && isMarkdown(p) && strings.EqualFold(strings.TrimSuffix(path.Base(p), path.Ext(p)), name) {
				return p
			}
		}
	}
	return ""
}

func walk(entries []*entry, depth int, visit func(e *entry, parent *entry, depth int)) {
	var rec func(list []*entry, parent *entry, depth int)
	rec = func(list []*entry, parent *entry, depth int) {
		for _, e := range list {
			visit(e, parent, depth)
			rec(e.children, e, depth+1)
		}
	}
	rec(entries, nil, depth)
}

// prepare reads an upload into the pages it makes, and converts each once,
// so a file that cannot be a page refuses the import before anything is made.
func (s *Service) prepare(in []File) (*upload, []*entry, error) {
	files, err := collect(in, s.MaxBytes)
	if err != nil {
		return nil, nil, err
	}
	u := &upload{files: map[string][]byte{}, byPath: map[string]*entry{}}
	for _, f := range files {
		u.files[f.Path] = f.Data
		u.order = append(u.order, f.Path)
	}
	roots := u.tree("", "")
	count := 0
	walk(roots, 1, func(e *entry, _ *entry, _ int) {
		count++
		if e.md != "" {
			u.byPath[e.md] = e
		}
		if e.dir != "" {
			u.byPath[e.dir] = e
		}
	})
	if count == 0 {
		return nil, nil, ErrNoMarkdown
	}
	if count > MaxImportPages {
		return nil, nil, invalid("the upload would make %d pages, more than the %d one import makes; import its folders one at a time", count, MaxImportPages)
	}
	var failed error
	walk(roots, 1, func(e *entry, _ *entry, _ int) {
		if failed != nil {
			return
		}
		e.title = titleFrom(e.name)
		if e.md == "" {
			return
		}
		needed := map[string]bool{}
		result, err := markdown.Convert(u.files[e.md], u.resolver(e, func(p string) markdown.Target {
			if !needed[p] {
				needed[p] = true
				e.files = append(e.files, p)
			}
			return markdown.Target{Kind: markdown.TargetFile, AttachmentID: uuid.Nil.String(), FileName: objectstore.CleanName(path.Base(p))}
		}, func(*entry) string { return "/s/X/p/" + uuid.Nil.String() }))
		if err != nil {
			failed = fileError(e.md, err)
			return
		}
		if result.Title != "" {
			e.title = clip(result.Title)
		}
	})
	return u, roots, failed
}

// resolver points a page's relative references at the import: Markdown at
// the page it becomes, other files at the file they become.
func (u *upload) resolver(from *entry, file func(p string) markdown.Target, href func(*entry) string) markdown.Resolver {
	return func(dest string) markdown.Target {
		p, fragment, ok := resolvePath(from.md, dest)
		if !ok {
			return markdown.Target{}
		}
		if e := u.byPath[p]; e != nil {
			return markdown.Target{Kind: markdown.TargetPage, Href: href(e) + fragment}
		}
		if _, ok := u.files[p]; ok {
			return file(p)
		}
		return markdown.Target{}
	}
}

// fileError names the file a refusal is about.
func fileError(p string, err error) error {
	var bad *document.InvalidError
	switch {
	case errors.As(err, &bad):
		return invalid("the file %s cannot be a page: %s", p, lowerFirst(bad.Message))
	case errors.Is(err, markdown.ErrTooLarge), errors.Is(err, markdown.ErrTooDeep), errors.Is(err, markdown.ErrTooTangled):
		return invalid("the file %s cannot be a page: %s", p, err.Error())
	}
	return err
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// Import makes the pages of an upload under a page, publishing them once all
// are made and hold their files; a failure trashes what nobody else saw yet.
func (s *Service) Import(ctx context.Context, actor perm.Actor, parentID uuid.UUID, in []File) (*ImportResult, db.LSN, error) {
	u, roots, err := s.prepare(in)
	if err != nil {
		return nil, 0, err
	}
	parent, _, err := s.pages.Get(ctx, actor, parentID)
	if err != nil {
		return nil, 0, err
	}
	if !parent.Can.Edit {
		return nil, 0, parent.Refusal(perm.EditPages)
	}
	if err := s.checkFiles(u, roots); err != nil {
		return nil, 0, err
	}

	result := &ImportResult{Pages: []Imported{}, Warnings: []string{}}
	var lsn db.LSN
	var made []uuid.UUID
	fail := func(err error) (*ImportResult, db.LSN, error) {
		for _, id := range made {
			if l, terr := s.pages.Trash(context.WithoutCancel(ctx), actor, id); terr == nil {
				lsn = max(lsn, l)
			}
		}
		return nil, lsn, err
	}

	var failed error
	walk(roots, 1, func(e *entry, up *entry, depth int) {
		if failed != nil {
			return
		}
		under := parentID
		if up != nil {
			under = up.id
		}
		p, l, err := s.pages.Create(ctx, actor, page.CreateInput{Placement: page.Placement{ParentID: under}, Title: e.title})
		lsn = max(lsn, l)
		if err != nil {
			failed = err
			return
		}
		e.id = p.ID
		if up == nil {
			made = append(made, p.ID)
		}
		result.Pages = append(result.Pages, Imported{ID: p.ID, ParentID: under, Title: p.Title, Depth: depth})
		e.uploaded = map[string]*attachment.Attachment{}
		for _, f := range e.files {
			a, l, err := s.files.Upload(ctx, actor, p.ID, attachment.UploadInput{FileName: path.Base(f), Body: bytes.NewReader(u.files[f])})
			lsn = max(lsn, l)
			if err != nil {
				failed = err
				return
			}
			e.uploaded[f] = a
		}
	})
	if failed != nil {
		return fail(failed)
	}

	walk(roots, 1, func(e *entry, _ *entry, _ int) {
		if failed != nil {
			return
		}
		var body []byte
		if e.md != "" {
			converted, err := markdown.Convert(u.files[e.md], u.resolver(e, func(p string) markdown.Target {
				a := e.uploaded[p]
				return markdown.Target{Kind: markdown.TargetFile, AttachmentID: a.ID.String(), FileName: a.FileName}
			}, func(to *entry) string { return "/s/" + parent.SpaceKey + "/p/" + to.id.String() }))
			if err != nil {
				failed = fileError(e.md, err)
				return
			}
			body = converted.Body
			for _, w := range converted.Warnings {
				if len(result.Warnings) < maxWarnings {
					result.Warnings = append(result.Warnings, e.md+": "+w)
				}
			}
		}
		_, l, err := s.pages.Update(ctx, actor, e.id, page.UpdateInput{Body: body})
		lsn = max(lsn, l)
		if err != nil {
			failed = err
		}
	})
	if failed != nil {
		return fail(failed)
	}
	return result, lsn, nil
}

// checkFiles refuses an import whose files cannot be stored before any page
// is made: no storage, or a file over the upload limit.
func (s *Service) checkFiles(u *upload, roots []*entry) error {
	var err error
	walk(roots, 1, func(e *entry, _ *entry, _ int) {
		for _, f := range e.files {
			switch {
			case err != nil:
			case s.files == nil:
				err = objectstore.ErrUnavailable
			case int64(len(u.files[f])) > s.files.MaxSize:
				err = &attachment.TooLargeError{Limit: s.files.MaxSize}
			case len(u.files[f]) == 0:
				err = invalid("the file %s is empty; take it out of the upload, or put something in it", f)
			}
		}
	})
	return err
}

// ReplaceResult is a page after its content was replaced from Markdown.
type ReplaceResult struct {
	Page     *page.Page `json:"page"`
	Warnings []string   `json:"warnings"`
}

// Replace publishes one Markdown file as the next version of a page, over the
// version it was made from, with the files it shows put on the page first.
func (s *Service) Replace(ctx context.Context, actor perm.Actor, id uuid.UUID, version int, in []File) (*ReplaceResult, db.LSN, error) {
	files, err := collect(in, s.MaxBytes)
	if err != nil {
		return nil, 0, err
	}
	u := &upload{files: map[string][]byte{}, byPath: map[string]*entry{}}
	var source *entry
	for _, f := range files {
		u.files[f.Path] = f.Data
		u.order = append(u.order, f.Path)
		if isMarkdown(f.Path) {
			if source != nil {
				return nil, 0, invalid("the upload holds more than one Markdown file; send the one to replace the page with, and the files it shows")
			}
			source = &entry{md: f.Path}
		}
	}
	if source == nil {
		return nil, 0, ErrNoMarkdown
	}
	current, _, err := s.pages.Get(ctx, actor, id)
	if err != nil {
		return nil, 0, err
	}
	if !current.Can.Edit {
		return nil, 0, current.Refusal(perm.EditPages)
	}
	if current.Version != version {
		return nil, 0, page.ErrStale
	}
	noPages := func(*entry) string { return "" }
	needed := map[string]bool{}
	if _, err := markdown.Convert(u.files[source.md], u.resolver(source, func(p string) markdown.Target {
		if !needed[p] {
			needed[p] = true
			source.files = append(source.files, p)
		}
		return markdown.Target{Kind: markdown.TargetFile, AttachmentID: uuid.Nil.String(), FileName: objectstore.CleanName(path.Base(p))}
	}, noPages)); err != nil {
		return nil, 0, fileError(source.md, err)
	}
	if err := s.checkFiles(u, []*entry{source}); err != nil {
		return nil, 0, err
	}
	var lsn db.LSN
	uploaded := map[string]*attachment.Attachment{}
	for _, f := range source.files {
		a, l, err := s.files.Upload(ctx, actor, id, attachment.UploadInput{FileName: path.Base(f), Body: bytes.NewReader(u.files[f])})
		lsn = max(lsn, l)
		if err != nil {
			return nil, lsn, err
		}
		uploaded[f] = a
	}
	converted, err := markdown.Convert(u.files[source.md], u.resolver(source, func(p string) markdown.Target {
		a := uploaded[p]
		return markdown.Target{Kind: markdown.TargetFile, AttachmentID: a.ID.String(), FileName: a.FileName}
	}, noPages))
	if err != nil {
		return nil, lsn, fileError(source.md, err)
	}
	update := page.UpdateInput{Body: converted.Body, Version: version}
	if converted.Title != "" {
		title := clip(converted.Title)
		update.Title = &title
	}
	updated, l, err := s.pages.Update(ctx, actor, id, update)
	lsn = max(lsn, l)
	if err != nil {
		return nil, lsn, err
	}
	warnings := converted.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return &ReplaceResult{Page: updated, Warnings: warnings}, lsn, nil
}
