package mdio

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// exported is one page of an export and where its parts go: its Markdown at
// dir/slug.md, its files in dir/slug.files and the pages below in dir/slug.
type exported struct {
	page  *page.Page
	dir   string
	slug  string
	files []exportedFile
}

type exportedFile struct {
	attachment attachment.Attachment
	name       string
}

func (e *exported) mdPath() string { return join(e.dir, e.slug+".md") }

func (e *exported) childDir() string { return join(e.dir, e.slug) }

func (e *exported) filePath(name string) string { return join(e.dir, e.slug+filesSuffix, name) }

func join(parts ...string) string {
	return strings.TrimPrefix(path.Join(parts...), "/")
}

// Export is a page, and with its subtree the pages below it the caller may
// view, read and laid out, ready to write as an archive.
type Export struct {
	s     *Service
	actor perm.Actor
	pages []*exported
	byID  map[uuid.UUID]*exported
}

// Name is what the archive is called: the page's slug.
func (e *Export) Name() string { return e.pages[0].slug }

// Scopes of an export as its audit entry names them: one Markdown file, a
// page with its files, or a page with the pages below it.
const (
	ScopeMarkdown = "markdown"
	ScopePage     = "page"
	ScopeSubtree  = "subtree"
)

// Audit is the export's entry in the audit log.
func (e *Export) Audit(scope string) audit.Entry {
	root := e.pages[0].page
	return audit.Entry{
		Action: audit.ActionPageExported, TargetType: "page", TargetID: &root.ID, Actor: e.actor.UserID,
		Data: map[string]any{"title": root.Title, "space": root.SpaceKey, "scope": scope, "pages": len(e.pages)},
	}
}

// Export reads a page, and the pages below it when subtree is set, as the
// caller may view them, with the files on each.
func (s *Service) Export(ctx context.Context, actor perm.Actor, id uuid.UUID, subtree bool) (*Export, error) {
	root, _, err := s.pages.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	out := &Export{s: s, actor: actor, byID: map[uuid.UUID]*exported{}}
	taken := map[string]map[string]bool{}
	place := func(p *page.Page, dir string) *exported {
		if taken[dir] == nil {
			taken[dir] = map[string]bool{}
		}
		slug := document.Dedupe(document.Slug(p.Title), taken[dir])
		taken[dir][slug] = true
		e := &exported{page: p, dir: dir, slug: slug}
		out.pages = append(out.pages, e)
		out.byID[p.ID] = e
		return e
	}
	place(root, "")
	if subtree {
		below, cut, err := s.pages.Below(ctx, actor, id, page.BelowQuery{Scope: "subtree", Sort: "tree"})
		if err != nil {
			return nil, err
		}
		if cut {
			return nil, ErrTooManyBelow
		}
		for _, b := range below {
			up := out.byID[b.ParentID]
			if up == nil {
				continue
			}
			p, _, err := s.pages.Get(ctx, actor, b.ID)
			if err != nil {
				return nil, err
			}
			place(p, up.childDir())
		}
	}
	if s.files != nil {
		for _, e := range out.pages {
			list, err := s.files.List(ctx, actor, e.page.ID, false)
			if err != nil {
				return nil, err
			}
			names := map[string]bool{}
			for i := len(list) - 1; i >= 0; i-- {
				name := uniqueName(list[i].FileName, names)
				e.files = append(e.files, exportedFile{attachment: list[i], name: name})
			}
		}
	}
	return out, nil
}

// uniqueName keeps two files of one name apart, numbering the later ones.
func uniqueName(name string, taken map[string]bool) string {
	out := name
	ext := path.Ext(name)
	for i := 2; taken[strings.ToLower(out)]; i++ {
		out = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), i, ext)
	}
	taken[strings.ToLower(out)] = true
	return out
}

// Markdown renders one exported page, its files and the other exported pages
// linked by paths relative to it.
func (e *Export) Markdown(p *exported) (string, error) {
	root, err := document.Parse(p.page.Body)
	if err != nil {
		return "", err
	}
	links := markdown.Links{
		File: func(id string) (string, bool) {
			for _, f := range p.files {
				if f.attachment.ID.String() == id {
					return relPath(p.dir, p.filePath(f.name)), true
				}
			}
			return "", false
		},
		Page: func(id string) (string, bool) {
			parsed, err := uuid.Parse(id)
			if err != nil {
				return "", false
			}
			to := e.byID[parsed]
			if to == nil {
				return "", false
			}
			return relPath(p.dir, to.mdPath()), true
		},
	}
	return markdown.Render(p.page.Title, root, links), nil
}

// Single is the first page alone as Markdown, its files named where an
// archive of it would hold them.
func (e *Export) Single() (string, error) {
	return e.Markdown(e.pages[0])
}

// Write writes the archive: each page's Markdown, then its files' bytes,
// read from storage one at a time.
func (e *Export) Write(ctx context.Context, w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, p := range e.pages {
		md, err := e.Markdown(p)
		if err != nil {
			return err
		}
		out, err := zw.CreateHeader(&zip.FileHeader{Name: p.mdPath(), Method: zip.Deflate, Modified: p.page.UpdatedAt})
		if err != nil {
			return err
		}
		if _, err := io.WriteString(out, md); err != nil {
			return err
		}
		for _, f := range p.files {
			if err := e.writeFile(ctx, zw, p, f); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

func (e *Export) writeFile(ctx context.Context, zw *zip.Writer, p *exported, f exportedFile) error {
	_, body, err := e.s.files.Open(ctx, e.actor, f.attachment.ID)
	if err != nil {
		return err
	}
	defer body.Close()
	out, err := zw.CreateHeader(&zip.FileHeader{Name: p.filePath(f.name), Method: zip.Deflate, Modified: f.attachment.CreatedAt})
	if err != nil {
		return err
	}
	_, err = io.Copy(out, body)
	return err
}
