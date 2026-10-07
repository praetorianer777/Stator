package spaceio

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"html/template"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// The HTML export is the space as static pages: the home page as index.html,
// every other page beside it, its files under files/, and one style sheet.
// No page runs a script, and each says so to the browser.
const (
	indexPage = "index.html"
	styleFile = "style.css"
	htmlFiles = "files/"
)

//go:embed html/style.css
var styleSheet []byte

//go:embed html/page.html
var pageTemplate string

var pageHTML = template.Must(template.New("page").Parse(pageTemplate))

// converter turns the Markdown of a page into HTML. Raw HTML passes, since
// the Markdown is written by the server from a checked document, and what
// it writes as HTML is escaped there; the pages refuse every script anyway.
var converter = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
)

// drawnInStator are the blocks whose content is read for each reader when
// the page is opened, which a page read offline cannot do.
var drawnInStator = map[string]bool{
	document.NodeCalendar: true, document.NodeLabelledPages: true, document.NodeRecentlyUpdated: true,
	document.NodeBlogPosts: true, document.NodeTaskReport: true, document.NodePropertiesReport: true,
	document.NodeContributors: true, armature.NodeIssueList: true, armature.NodeChart: true, armature.NodeRoadmap: true,
}

// drawnNote is what stands in their place.
const drawnNote = "This block is drawn when the page is read in Stator."

// site is the snapshot laid out as files: each page's file name, each
// file's path, and the tree to walk.
type site struct {
	s        *snapshot
	names    map[uuid.UUID]string
	files    map[uuid.UUID]string
	children map[uuid.UUID][]*ArchivePage
	posts    []*ArchivePage
}

func layout(s *snapshot) *site {
	out := &site{s: s, names: map[uuid.UUID]string{}, files: map[uuid.UUID]string{}, children: map[uuid.UUID][]*ArchivePage{}}
	taken := map[string]bool{"index": true, "style": true, "files": true}
	for _, p := range s.pages {
		switch {
		case p.ID == s.space.HomePageID:
			out.names[p.ID] = indexPage
		default:
			slug := document.Dedupe(document.Slug(p.Title), taken)
			taken[slug] = true
			out.names[p.ID] = slug + ".html"
		}
		if p.Kind == "post" {
			out.posts = append(out.posts, p)
		} else if p.Parent != nil {
			out.children[*p.Parent] = append(out.children[*p.Parent], p)
		}
		named := fileIDs(p.Body)
		latest := map[string]uuid.UUID{}
		for _, f := range p.Files {
			latest[strings.ToLower(f.Name)] = f.ID
		}
		for _, f := range p.Files {
			if named[f.ID] || latest[strings.ToLower(f.Name)] == f.ID {
				out.files[f.ID] = htmlFiles + f.ID.String() + "/" + objectstore.CleanName(f.Name)
			}
		}
	}
	return out
}

func (w *site) included(_ *ArchivePage, f File) bool { return w.files[f.ID] != "" }

func (w *site) filePath(_ *ArchivePage, f File) string { return w.files[f.ID] }

// pageView is what the template draws of one page.
type pageView struct {
	Title, Space, Key  string
	Home               string
	IsHome             bool
	Trail              []link
	Content            template.HTML
	Below              []link
	Tree               template.HTML
	Posts              []link
	Labels             []string
	Files              []link
	Published          string
	Exported, Exporter string
}

type link struct{ Title, Href string }

// write writes the pages, their files and the style sheet.
func (w *site) write(ctx context.Context, store objectstore.Store, zw *zip.Writer, step func()) error {
	if err := writeBytes(zw, styleFile, styleSheet); err != nil {
		return err
	}
	for _, p := range w.s.pages {
		if err := w.writePage(zw, p); err != nil {
			return err
		}
		step()
	}
	return w.s.writeFiles(ctx, store, zw, w.filePath, w.included, step)
}

func writeBytes(zw *zip.Writer, name string, data []byte) error {
	out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func (w *site) writePage(zw *zip.Writer, p *ArchivePage) error {
	view := pageView{
		Title: p.Title, Space: w.s.space.Name, Key: w.s.space.Key, Home: indexPage, IsHome: p.ID == w.s.space.HomePageID,
		Labels: []string{}, Exported: w.s.exportedAt.UTC().Format(time.DateOnly), Exporter: w.s.people[w.s.exporter].Name,
	}
	content, err := w.content(p)
	if err != nil {
		return err
	}
	view.Content = content
	for up := p.Parent; up != nil; {
		parent := w.s.byID[*up]
		if parent == nil {
			break
		}
		view.Trail = append([]link{{Title: parent.Title, Href: w.names[parent.ID]}}, view.Trail...)
		up = parent.Parent
	}
	if !view.IsHome {
		for _, c := range w.children[p.ID] {
			view.Below = append(view.Below, link{Title: c.Title, Href: w.names[c.ID]})
		}
	} else {
		view.Tree = w.tree(p.ID)
		for _, post := range w.posts {
			view.Posts = append(view.Posts, link{Title: post.Title, Href: w.names[post.ID]})
		}
	}
	for _, l := range p.Labels {
		view.Labels = append(view.Labels, l.Name)
	}
	for _, f := range p.Files {
		if href := w.files[f.ID]; href != "" {
			view.Files = append(view.Files, link{Title: f.Name, Href: href})
		}
	}
	if p.PublishedAt != nil {
		view.Published = p.PublishedAt.UTC().Format(time.DateOnly)
	}
	var b bytes.Buffer
	if err := pageHTML.Execute(&b, view); err != nil {
		return err
	}
	return writeBytes(zw, w.names[p.ID], b.Bytes())
}

// tree is every page below the home page as nested lists, for the index.
func (w *site) tree(id uuid.UUID) template.HTML {
	var b strings.Builder
	var walk func(id uuid.UUID)
	walk = func(id uuid.UUID) {
		below := w.children[id]
		if len(below) == 0 {
			return
		}
		b.WriteString("<ul>")
		for _, c := range below {
			b.WriteString(`<li><a href="`)
			b.WriteString(template.HTMLEscapeString(w.names[c.ID]))
			b.WriteString(`">`)
			b.WriteString(template.HTMLEscapeString(c.Title))
			b.WriteString("</a>")
			walk(c.ID)
			b.WriteString("</li>")
		}
		b.WriteString("</ul>")
	}
	walk(id)
	return template.HTML(b.String())
}

// content is the page's document as HTML, its links pointing at the pages
// and files beside it.
func (w *site) content(p *ArchivePage) (template.HTML, error) {
	if p.Kind == "folder" {
		return "", nil
	}
	root, err := document.Parse(p.Body)
	if err != nil {
		return "", err
	}
	root.Content = w.offline(p, root.Content, 0)
	links := markdown.Links{
		File: func(id string) (string, bool) {
			parsed, err := uuid.Parse(id)
			if err != nil || w.files[parsed] == "" {
				return "", false
			}
			return w.files[parsed], true
		},
		Page: func(id string) (string, bool) {
			parsed, err := uuid.Parse(id)
			if err != nil || w.names[parsed] == "" {
				return "", false
			}
			return w.names[parsed], true
		},
	}
	md := markdown.Render(p.Title, root, links)
	var b bytes.Buffer
	if err := converter.Convert([]byte(md), &b); err != nil {
		return "", err
	}
	return template.HTML(b.String()), nil
}

// offline puts what a reader of the page in Stator would see in place of
// the blocks that are read when it is opened, where the export knows it.
func (w *site) offline(p *ArchivePage, nodes []document.Node, depth int) []document.Node {
	out := make([]document.Node, 0, len(nodes))
	for _, n := range nodes {
		switch {
		case n.Type == "childPages":
			if list, ok := w.pageList(w.children[p.ID]); ok {
				out = append(out, list)
			}
			continue
		case n.Type == document.NodeAttachmentList:
			if list, ok := w.fileList(p); ok {
				out = append(out, list)
			}
			continue
		case n.Type == document.NodeInclude:
			out = append(out, w.include(n, depth)...)
			continue
		case n.Type == document.NodeTemplateButton:
			continue
		case drawnInStator[n.Type]:
			out = append(out, paragraph(text(drawnNote)))
			continue
		}
		if len(n.Content) > 0 {
			n.Content = w.offline(p, n.Content, depth)
		}
		out = append(out, n)
	}
	return out
}

// include shows the included page, or its excerpt, as it is in the export;
// one included in it shows as a link, so no chain of includes runs on.
func (w *site) include(n document.Node, depth int) []document.Node {
	id, _ := n.Attrs["pageId"].(string)
	parsed, err := uuid.Parse(id)
	from := w.s.byID[parsed]
	if err != nil || from == nil {
		return []document.Node{paragraph(text("A page included here is not part of this export."))}
	}
	if depth > 0 {
		return []document.Node{paragraph(linkTo(from.Title, w.names[from.ID]))}
	}
	root, err := document.Parse(from.Body)
	if err != nil {
		return nil
	}
	content := root.Content
	if excerpt, _ := n.Attrs["excerptId"].(string); excerpt != "" {
		content = findExcerpt(root.Content, excerpt)
	}
	return w.offline(from, content, depth+1)
}

func findExcerpt(nodes []document.Node, id string) []document.Node {
	for _, n := range nodes {
		if n.Type == document.NodeExcerpt {
			if got, _ := n.Attrs["id"].(string); got == id {
				return n.Content
			}
		}
		if found := findExcerpt(n.Content, id); found != nil {
			return found
		}
	}
	return nil
}

func (w *site) pageList(pages []*ArchivePage) (document.Node, bool) {
	list := document.Node{Type: "bulletList"}
	for _, c := range pages {
		list.Content = append(list.Content, document.Node{Type: "listItem", Content: []document.Node{paragraph(linkTo(c.Title, w.names[c.ID]))}})
	}
	return list, len(list.Content) > 0
}

func (w *site) fileList(p *ArchivePage) (document.Node, bool) {
	list := document.Node{Type: "bulletList"}
	seen := map[string]bool{}
	for i := len(p.Files) - 1; i >= 0; i-- {
		f := p.Files[i]
		if seen[strings.ToLower(f.Name)] || w.files[f.ID] == "" {
			continue
		}
		seen[strings.ToLower(f.Name)] = true
		list.Content = append(list.Content, document.Node{Type: "listItem", Content: []document.Node{paragraph(linkTo(f.Name, w.files[f.ID]))}})
	}
	return list, len(list.Content) > 0
}

func paragraph(inline document.Node) document.Node {
	return document.Node{Type: "paragraph", Content: []document.Node{inline}}
}

func text(words string) document.Node { return document.Node{Type: "text", Text: words} }

// linkTo is a link to a file beside the page; the Markdown writes it as
// it is, being no address of Stator's.
func linkTo(title, href string) document.Node {
	return document.Node{Type: "text", Text: title, Marks: []document.Mark{{Type: "link", Attrs: map[string]any{"href": href}}}}
}

// downloadName is what an export's file is saved as.
func downloadName(key string, format ExportFormat) string {
	if format == FormatHTML {
		return strings.ToLower(key) + "-html.zip"
	}
	return strings.ToLower(key) + "-space.zip"
}
