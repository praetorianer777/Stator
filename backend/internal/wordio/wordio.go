// Package wordio makes pages of Word documents: one at once, in the request,
// or several and folders of them in the worker, through the page and file
// services as the person importing.
package wordio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/docx"
	"github.com/praetorianer777/stator/backend/internal/mdio"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// MaxFileBytes is what one Word document may weigh, as one export may.
	MaxFileBytes = docx.MaxBytes
	// MaxUploadBytes is what one import of several documents may weigh.
	MaxUploadBytes int64 = 200 << 20
	// MaxFiles bounds the documents of one import; the table holds it too.
	MaxFiles = 50
	// MaxPages bounds the pages one import makes, its folders counted.
	MaxPages = mdio.MaxImportPages
	// maxWarnings bounds what one document reports.
	maxWarnings = 30
	// maxArchiveEntries bounds what an uploaded archive may list, whatever
	// of it is Word documents.
	maxArchiveEntries = 5000
)

// InvalidError refuses an upload in a sentence that says what to change.
type InvalidError = mdio.InvalidError

// TooLargeError refuses an upload over its limit, naming it.
type TooLargeError = mdio.TooLargeError

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

var (
	// ErrNoWord refuses an upload without a Word document in it.
	ErrNoWord = errors.New("there is no Word document in the upload; choose .docx files, or a .zip of them")
	// ErrOneOnly refuses several files where one document is imported.
	ErrOneOnly = errors.New("send one Word document here; import several at once from the page's menu, which runs them in the background")
)

// File is one file of an upload: its path in the folder or archive it came
// from, slash separated, and its bytes.
type File = mdio.File

// Service imports Word documents through the page and file services, so
// every write is checked as the API checks it.
type Service struct {
	pages *page.Service
	// files is nil on a server without storage, which then takes no pictures.
	files *attachment.Service
	// store keeps an upload of several documents until the worker reads it.
	store objectstore.Store
	db    *db.Cluster
}

func NewService(cluster *db.Cluster, pages *page.Service, files *attachment.Service, store objectstore.Store) *Service {
	return &Service{db: cluster, pages: pages, files: files, store: store}
}

// checkParent refuses a parent the actor may not add pages under, as a
// Markdown import does.
func (s *Service) checkParent(ctx context.Context, actor perm.Actor, parentID uuid.UUID) (*page.Page, error) {
	parent, _, err := s.pages.Get(ctx, actor, parentID)
	if err != nil {
		return nil, err
	}
	if !parent.Can.Edit {
		return nil, parent.Refusal(perm.EditPages)
	}
	if !parent.Can.Add {
		return nil, parent.Refusal(perm.ArrangePages)
	}
	if parent.Kind == page.KindPost {
		return nil, page.ErrPostPlace
	}
	return parent, nil
}

// prepared is a document read and held to the allowlist, ready to be made a page.
type prepared struct {
	name     string
	title    string
	doc      *docx.Converted
	warnings []string
}

// prepare reads a document, refusing it in a sentence naming it before any page is made.
func (s *Service) prepare(name string, data []byte) (*prepared, error) {
	if int64(len(data)) > MaxFileBytes {
		return nil, invalid("the file %s is over %s; make its pictures smaller in Word, or split it, and import it again", name, attachment.Size(MaxFileBytes))
	}
	opts := docx.ReadOptions{}
	if s.files != nil {
		opts.FileLimit = s.files.MaxSize
	}
	doc, err := docx.Read(data, opts)
	if err != nil {
		return nil, fileError(name, err)
	}
	if len(doc.Pictures) > 0 && s.files == nil {
		return nil, objectstore.ErrUnavailable
	}
	raw, err := json.Marshal(doc.Body)
	if err != nil {
		return nil, err
	}
	if err := document.Validate(raw); err != nil {
		return nil, fileError(name, err)
	}
	title := clip(doc.Title)
	if title == "" {
		title = titleFrom(path.Base(name))
	}
	p := &prepared{name: name, title: title, doc: doc}
	for _, w := range doc.Warnings {
		if len(p.warnings) < maxWarnings {
			p.warnings = append(p.warnings, w)
		}
	}
	return p, nil
}

// fileError names the document a refusal is about.
func fileError(name string, err error) error {
	var (
		bad *document.InvalidError
		big *docx.TooLargeError
	)
	switch {
	case errors.As(err, &bad):
		return invalid("the file %s cannot be a page: %s", name, lowerFirst(bad.Message))
	case errors.As(err, &big):
		return invalid("the file %s cannot be a page: %s", name, big.What)
	case errors.Is(err, docx.ErrNotWord):
		return invalid("the file %s cannot be a page: %s", name, docx.ErrNotWord.Error())
	case errors.Is(err, docx.ErrLegacyWord):
		return invalid("the file %s cannot be a page: %s", name, docx.ErrLegacyWord.Error())
	}
	return err
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// make makes one prepared document a published page under parent: the page
// first, unpublished, then its pictures as its files, then its body, which
// needs their ids. What fails part way is trashed.
func (s *Service) make(ctx context.Context, actor perm.Actor, parent uuid.UUID, p *prepared) (*page.Page, db.LSN, error) {
	made, lsn, err := s.pages.Create(ctx, actor, page.CreateInput{Placement: page.Placement{ParentID: parent}, Title: p.title})
	if err != nil {
		return nil, lsn, err
	}
	fail := func(err error) (*page.Page, db.LSN, error) {
		if l, terr := s.pages.Trash(context.WithoutCancel(ctx), actor, made.ID); terr == nil {
			lsn = max(lsn, l)
		}
		return nil, lsn, err
	}
	ids := make([]uuid.UUID, len(p.doc.Pictures))
	for i, pic := range p.doc.Pictures {
		a, l, err := s.files.Upload(ctx, actor, made.ID, attachment.UploadInput{FileName: pic.Name, Body: bytes.NewReader(pic.Data)})
		lsn = max(lsn, l)
		if err != nil {
			return fail(err)
		}
		ids[i] = a.ID
	}
	body, err := json.Marshal(p.doc.Bind(ids))
	if err != nil {
		return fail(err)
	}
	published, l, err := s.pages.Update(ctx, actor, made.ID, page.UpdateInput{Body: body})
	lsn = max(lsn, l)
	if err != nil {
		return fail(err)
	}
	return published, lsn, nil
}

// Import makes one Word document a published page under a page, as a
// Markdown import makes its pages, and answers it with its warnings.
func (s *Service) Import(ctx context.Context, actor perm.Actor, parentID uuid.UUID, in []File) (*mdio.ImportResult, db.LSN, error) {
	var docs []File
	for _, f := range in {
		if isWord(f.Path) {
			docs = append(docs, f)
		}
	}
	switch {
	case len(docs) == 0:
		return nil, 0, ErrNoWord
	case len(in) > 1:
		return nil, 0, ErrOneOnly
	}
	name := path.Base(strings.ReplaceAll(docs[0].Path, "\\", "/"))
	p, err := s.prepare(name, docs[0].Data)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.checkParent(ctx, actor, parentID); err != nil {
		return nil, 0, err
	}
	made, lsn, err := s.make(ctx, actor, parentID, p)
	if err != nil {
		return nil, lsn, err
	}
	warnings := make([]string, 0, len(p.warnings))
	for _, w := range p.warnings {
		warnings = append(warnings, name+": "+w)
	}
	return &mdio.ImportResult{
		Pages:    []mdio.Imported{{ID: made.ID, ParentID: parentID, Title: made.Title, Depth: 1}},
		Warnings: warnings,
	}, lsn, nil
}

// isWord says whether a path names a Word document, never Word's lock file.
func isWord(p string) bool {
	base := path.Base(strings.ReplaceAll(p, "\\", "/"))
	return strings.EqualFold(path.Ext(base), ".docx") && !strings.HasPrefix(base, "~$")
}

// titleFrom makes a title of a file or folder name, as a person would write it.
func titleFrom(name string) string {
	if isWord(name) {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	title := clip(strings.NewReplacer("-", " ", "_", " ").Replace(name))
	if title == "" {
		return untitled
	}
	return title
}

// untitled names a page whose file name has no words.
const untitled = "Untitled"

// clip trims a title to what a page takes.
func clip(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if runes := []rune(title); len(runes) > page.MaxTitleLength {
		title = strings.TrimSpace(string(runes[:page.MaxTitleLength]))
	}
	return title
}

// cleanPath makes an uploaded path relative and slash separated, and says
// whether to keep it: never a hidden file or a system's leftovers.
func cleanPath(p string) (string, bool, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", false, invalid("the upload names the path %q, which leaves its folder; upload the folder itself", p)
	}
	clean := path.Clean(strings.TrimLeft(p, "/"))
	if clean == "." {
		return "", false, nil
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") || part == "__MACOSX" {
			return "", false, nil
		}
	}
	return clean, true, nil
}

// batch is an upload of several documents: the documents by path in the
// order they came, and the names of the files that are none.
type batch struct {
	docs    []File
	skipped []string
	size    int64
}

// collect reads an upload of several documents, opening its archives and
// holding it to the limits before anything is stored.
func collect(in []File) (*batch, error) {
	b := &batch{}
	seen := map[string]bool{}
	add := func(f File) error {
		p, keep, err := cleanPath(f.Path)
		if err != nil || !keep || seen[p] {
			return err
		}
		seen[p] = true
		if !isWord(p) {
			b.skipped = append(b.skipped, p)
			return nil
		}
		if int64(len(f.Data)) > MaxFileBytes {
			return invalid("the file %s is over %s; make its pictures smaller in Word, or split it, and import it again", p, attachment.Size(MaxFileBytes))
		}
		if len(b.docs) >= MaxFiles {
			return invalid("the upload holds more than %d Word documents; import them in several parts", MaxFiles)
		}
		b.size += int64(len(f.Data))
		if b.size > MaxUploadBytes {
			return &TooLargeError{Limit: MaxUploadBytes}
		}
		b.docs = append(b.docs, File{Path: p, Data: f.Data})
		return nil
	}
	for _, f := range in {
		if strings.EqualFold(path.Ext(f.Path), ".zip") {
			if err := unzip(f, add); err != nil {
				return nil, err
			}
			continue
		}
		if err := add(f); err != nil {
			return nil, err
		}
	}
	if len(b.docs) == 0 {
		return nil, ErrNoWord
	}
	return b, nil
}

// unzip reads an archive's files, each held to what a document may weigh
// whatever the archive's directory claims about its size.
func unzip(f File, add func(File) error) error {
	r, err := zip.NewReader(bytes.NewReader(f.Data), int64(len(f.Data)))
	if err != nil {
		return invalid("the archive %s could not be read; make the archive again and upload it", path.Base(f.Path))
	}
	if len(r.File) > maxArchiveEntries {
		return invalid("the archive %s holds more than %d files; archive the Word documents alone", path.Base(f.Path), maxArchiveEntries)
	}
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if !isWord(entry.Name) {
			if err := add(File{Path: entry.Name}); err != nil {
				return err
			}
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return invalid("the archive %s holds %s, which could not be read; make the archive again and upload it", path.Base(f.Path), entry.Name)
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
		_ = rc.Close()
		if err != nil {
			return invalid("the archive %s holds %s, which could not be read; make the archive again and upload it", path.Base(f.Path), entry.Name)
		}
		if err := add(File{Path: entry.Name, Data: data}); err != nil {
			return err
		}
	}
	return nil
}

// pack writes the documents as one archive, the form the worker reads them in.
func (b *batch) pack() ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range b.docs {
		// Stored, not deflated: a document is a zip already.
		w, err := z.CreateHeader(&zip.FileHeader{Name: f.Path, Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readPacked reads an archive pack wrote.
func readPacked(data []byte) ([]File, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out []File
	for _, entry := range r.File {
		if len(out) >= MaxFiles {
			break
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: entry.Name, Data: data})
	}
	return out, nil
}

// entry is one page a batch makes: from a document, a folder, or both when
// a document and a folder beside it share a name.
type entry struct {
	doc      string
	dir      string
	name     string
	children []*entry
}

// tree arranges a batch's documents: each a page, and each folder holding
// some a page too, unless a document beside it has its name, which then
// holds the folder's pages.
func tree(docs []File, dir string) []*entry {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var out []*entry
	dirs := map[string]*entry{}
	files := map[string]*entry{}
	for _, f := range docs {
		rest, ok := strings.CutPrefix(f.Path, prefix)
		if !ok {
			continue
		}
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			sub := rest[:slash]
			if dirs[sub] == nil {
				e := &entry{dir: prefix + sub, name: sub}
				dirs[sub] = e
				out = append(out, e)
			}
			continue
		}
		e := &entry{doc: f.Path, name: rest}
		files[strings.TrimSuffix(rest, path.Ext(rest))] = e
		out = append(out, e)
	}
	kept := out[:0]
	for _, e := range out {
		if e.doc == "" {
			if paired := files[e.name]; paired != nil {
				paired.dir = e.dir
				continue
			}
		}
		kept = append(kept, e)
	}
	for _, e := range kept {
		if e.dir != "" {
			e.children = tree(docs, e.dir)
		}
	}
	return kept
}

func walk(entries []*entry, depth int, visit func(e, parent *entry, depth int)) {
	var rec func(list []*entry, parent *entry, depth int)
	rec = func(list []*entry, parent *entry, depth int) {
		for _, e := range list {
			visit(e, parent, depth)
			rec(e.children, e, depth+1)
		}
	}
	rec(entries, nil, depth)
}

func count(entries []*entry) int {
	n := 0
	walk(entries, 1, func(*entry, *entry, int) { n++ })
	return n
}
