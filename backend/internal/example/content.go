// Package example makes the example space: a space whose pages explain
// Stator, in English or German, made through the services every person's
// pages go through, so every rule and guard holds for it as for them.
//
// The pages are Markdown files per language under content/, read as the
// Markdown import reads a page. What Markdown cannot say is a marker
// paragraph, %%name%%, which this package turns into the block it names,
// with the space's own ids; text/template fills in the rest, such as whom
// a task mentions or whether Armature is there to show.
package example

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// The languages the example is written in; the interface speaks the same two.
const (
	English = "en"
	German  = "de"
)

// Languages is every language the content comes in.
var Languages = []string{English, German}

//go:embed content
var files embed.FS

// Entry is one page of the example in its place in the tree, by the name of
// its file; a folder's file holds only its title.
type Entry struct {
	Name     string
	Kind     page.Kind
	Children []Entry
}

// Home, Showcase and Excerpted name the files the code refers to: the
// space's home page, the page of every block, and the page whose excerpt
// the showcase includes.
const (
	Home      = "home"
	Showcase  = "showcase"
	Excerpted = "spaces-and-pages"
	// MeetingNotes is the folder the showcase's template button fills.
	MeetingNotes = "meeting-notes"
	// Comments is the page that starts with a comment of its creator's.
	Comments = "comments-and-reactions"
)

// Tree is the example's pages below its home page, in order.
var Tree = []Entry{
	{Name: Excerpted},
	{Name: Showcase},
	{Name: "page-tree", Children: []Entry{{Name: MeetingNotes, Kind: page.KindFolder}}},
	{Name: "writing", Kind: page.KindFolder, Children: []Entry{
		{Name: "drafts-and-history"},
		{Name: "live-pages"},
		{Name: "scheduled-publishing"},
		{Name: "blog-posts"},
		{Name: "editing-together"},
		{Name: "templates"},
		{Name: "attachments"},
	}},
	{Name: "working-together", Kind: page.KindFolder, Children: []Entry{
		{Name: Comments},
		{Name: "labels"},
		{Name: "watching-and-notifications"},
		{Name: "search"},
	}},
	{Name: "access", Kind: page.KindFolder, Children: []Entry{
		{Name: "permissions"},
		{Name: "guests"},
		{Name: "public-access"},
	}},
	{Name: "connections", Kind: page.KindFolder, Children: []Entry{
		{Name: "armature"},
		{Name: "tokens-and-mcp"},
	}},
}

// Posts are the blog posts the example starts with, oldest first.
var Posts = []string{"post-welcome", "post-try-it"}

// Walk visits every entry of the tree, parents before their children.
func Walk(visit func(e Entry)) {
	var walk func(entries []Entry)
	walk = func(entries []Entry) {
		for _, e := range entries {
			visit(e)
			walk(e.Children)
		}
	}
	walk(Tree)
}

// Facts is what the content is filled in with: the space, its pages, files
// and calendar once made, and what the site can show.
type Facts struct {
	Lang string
	Key  string
	// Me is whoever makes the space, whom the showcase's tasks mention.
	Me Person
	// Today and the days after it, as a date node holds a day.
	Today, Soon, NextWeek string
	// Pages holds each made page's id by its file's name.
	Pages map[string]uuid.UUID
	// Excerpts holds the id of each page's excerpt by its file's name.
	Excerpts map[string]uuid.UUID
	// Files are the showcase's uploads by their names, none when the site
	// keeps no files.
	Files map[string]uuid.UUID
	// Calendar is the space's calendar, uuid.Nil before it is made.
	Calendar uuid.UUID
	// Armature is what the maker may see there, nil when nothing.
	Armature *ArmatureFacts
}

// Person is somebody the content names.
type Person struct {
	ID   uuid.UUID
	Name string
}

// ArmatureFacts is the project and issue the Armature blocks show.
type ArmatureFacts struct {
	Project string
	// Issue is an issue of Project, empty when it has none the maker sees.
	Issue string
}

// The files the showcase uploads, and the one uploaded twice to show versions.
const (
	ImageFile = "stator-example.png"
	DataFile  = "team-numbers.csv"
)

// Doc is one file of the content made into a page.
type Doc struct {
	Title  string
	Body   json.RawMessage
	Labels []string
	// Comment is the words of a comment the page starts with, if any.
	Comment string
}

// Content is the files the pages are written in, for the tests to read whole.
func Content() fs.FS { return files }

// Source is a file of the content as written, before anything is filled in.
func Source(lang, name string) ([]byte, error) {
	return files.ReadFile("content/" + lang + "/" + name + ".md")
}

// Title is the title a file gives its page, read without filling it in, so
// pages can be made before their bodies are known.
func Title(lang, name string) (string, error) {
	src, err := Source(lang, name)
	if err != nil {
		return "", err
	}
	_, rest := frontMatter(src)
	line, _, _ := bytes.Cut(rest, []byte("\n"))
	title, ok := strings.CutPrefix(strings.TrimSpace(string(line)), "# ")
	if !ok || strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("content/%s/%s.md does not open with its title", lang, name)
	}
	return strings.TrimSpace(title), nil
}

// frontMatter splits the lines between two --- at the top of a file from
// the Markdown after them.
func frontMatter(src []byte) (map[string]string, []byte) {
	const fence = "---\n"
	if !bytes.HasPrefix(src, []byte(fence)) {
		return nil, src
	}
	head, rest, ok := bytes.Cut(src[len(fence):], []byte("\n"+fence))
	if !ok {
		return nil, src
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(head), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out, bytes.TrimLeft(rest, "\n")
}

// Render makes one file of the content into a page, with facts filled in
// and every marker made the block it names. Warnings are what the Markdown
// import would have told a person; the content has none.
func Render(name string, f Facts) (*Doc, []string, error) {
	src, err := Source(f.Lang, name)
	if err != nil {
		return nil, nil, err
	}
	meta, rest := frontMatter(src)
	tpl, err := template.New(name).Option("missingkey=error").Parse(string(rest))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	var filled bytes.Buffer
	if err := tpl.Execute(&filled, f); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	result, err := markdown.Convert(filled.Bytes(), f.resolve)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	root, err := document.Parse(result.Body)
	if err != nil {
		return nil, nil, err
	}
	b := builder{facts: f, page: name}
	if root.Content, err = b.blocks(root.Content); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	body, err := json.Marshal(document.Normalize(root))
	if err != nil {
		return nil, nil, err
	}
	if err := document.Validate(body); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	doc := &Doc{Title: result.Title, Body: body, Comment: meta["comment"]}
	if labels := meta["labels"]; labels != "" {
		for l := range strings.SplitSeq(labels, ",") {
			doc.Labels = append(doc.Labels, strings.TrimSpace(l))
		}
	}
	return doc, result.Warnings, nil
}

var pageLink = regexp.MustCompile(`^([a-z0-9-]+)\.md(#.*)?$`)

// resolve leads a link to another file of the content to the page it made,
// and a picture or link under showcase.files to the upload of that name.
func (f Facts) resolve(dest string) markdown.Target {
	if m := pageLink.FindStringSubmatch(dest); m != nil {
		if id, ok := f.Pages[m[1]]; ok {
			return markdown.Target{Kind: markdown.TargetPage, Href: "/s/" + f.Key + "/p/" + id.String() + m[2]}
		}
		return markdown.Target{}
	}
	if file, ok := strings.CutPrefix(dest, Showcase+".files/"); ok {
		if id, ok := f.Files[file]; ok {
			return markdown.Target{Kind: markdown.TargetFile, AttachmentID: id.String(), FileName: file}
		}
	}
	return markdown.Target{}
}
