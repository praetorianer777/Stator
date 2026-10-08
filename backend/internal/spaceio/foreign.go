package spaceio

import (
	"archive/zip"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/wikiread"
)

// ImportSource is what an import makes its space of: a Stator archive, or a
// space export of another wiki, HTML or XML (docs/wiki-import.md).
type ImportSource string

const (
	SourceArchive ImportSource = "archive"
	SourceHTML    ImportSource = "html"
	SourceXML     ImportSource = "xml"
)

// ImportSources is every source, for the API's description.
var ImportSources = []ImportSource{SourceArchive, SourceHTML, SourceXML}

// detect says what an uploaded zip holds, refusing one that is neither an
// archive of Stator nor an export Stator reads.
func detect(zr *zip.Reader) (ImportSource, error) {
	for _, f := range zr.File {
		if f.Name == manifestPath {
			return SourceArchive, nil
		}
	}
	switch wikiread.Detect(zr) {
	case wikiread.FormatHTML:
		return SourceHTML, nil
	case wikiread.FormatXML:
		return SourceXML, nil
	}
	return "", invalid("This zip is neither a space archive of Stator nor a space export Stator reads: it holds no manifest.json, entities.xml or index.html. Export the space as an archive from Stator, or as HTML or XML from the other wiki, then import that zip.")
}

// limits are an archive's, so a space from elsewhere is held to what one
// from Stator is.
var limits = wikiread.Limits{Pages: MaxPages, Versions: MaxVersions, Files: MaxFiles, Unpacked: MaxUnpackedBytes}

// readExport reads an export of another wiki as a manifest and its pages.
func readExport(zr *zip.Reader, from ImportSource, key, name string) (*Manifest, *exportSource, error) {
	sp, err := wikiread.Read(zr, wikiread.Format(from), wikiread.Options{Key: key, Limits: limits})
	var (
		inv *wikiread.InvalidError
		big *wikiread.TooLargeError
	)
	switch {
	case errors.As(err, &inv):
		return nil, nil, invalid("%s", inv.Message)
	case errors.As(err, &big):
		return nil, nil, tooLarge("%s", big.Message)
	case err != nil:
		return nil, nil, err
	}
	return fromExport(sp, key, name)
}

// exportSource is an export read whole beforehand, its files' bytes still in the zip.
type exportSource struct {
	pages map[uuid.UUID]*ArchivePage
	files map[uuid.UUID]wikiread.File
	// listed are the losses the report lists, the reader's and those of the
	// archive's shapes; lostCount counts every one.
	listed    []wikiread.Loss
	lostCount int
}

func (s *exportSource) page(id uuid.UUID) (*ArchivePage, error) {
	if p := s.pages[id]; p != nil {
		return p, nil
	}
	return nil, invalid("The export lost its page %s while it was read. Import it again.", id)
}

func (s *exportSource) fileSize(f File) (int64, bool) {
	wf, ok := s.files[f.ID]
	return wf.Size, ok
}

func (s *exportSource) fileBytes(f File) ([]byte, error) {
	wf, ok := s.files[f.ID]
	if !ok {
		return nil, invalid("The export lost its file %q while it was read. Import it again.", f.Name)
	}
	data, err := wf.Read()
	var inv *wikiread.InvalidError
	if errors.As(err, &inv) {
		return nil, invalid("%s", inv.Message)
	}
	return data, err
}

func (s *exportSource) lose(pageTitle string, kind wikiread.LossKind, detail string) {
	s.lostCount++
	if len(s.listed) < wikiread.MaxLosses {
		s.listed = append(s.listed, wikiread.Loss{Page: pageTitle, Kind: kind, Detail: detail})
	}
}

// fromExport gives an export the shapes of an archive: a manifest without
// grants, groups, templates or calendars, and pages with one or more
// versions, their files, labels and comments. Every page gets a rank among
// its siblings in the order the export gave.
func fromExport(sp *wikiread.Space, key, name string) (*Manifest, *exportSource, error) {
	if len(sp.Pages) == 0 {
		return nil, nil, invalid("The export holds no pages. Export the space again and import the new zip.")
	}
	src := &exportSource{pages: map[uuid.UUID]*ArchivePage{}, files: map[uuid.UUID]wikiread.File{}, listed: sp.Losses, lostCount: sp.LostCount}
	if name == "" {
		name = strings.Join(strings.Fields(sp.Name), " ")
		if utf8.RuneCountInString(name) > space.MaxNameLength {
			name = string([]rune(name)[:space.MaxNameLength])
		}
	}
	if strings.TrimSpace(name) == "" {
		name = key
	}
	now := time.Now().UTC()
	m := &Manifest{
		Format: Format, Version: Version, ExportedAt: now,
		Space:  ArchiveSpace{Key: sp.Key, Name: name, Description: sp.Description, CreatedAt: now, HomePage: sp.Pages[0].ID},
		People: []Person{}, Groups: []Group{}, Grants: []Grant{}, Templates: []Template{}, Calendars: []Calendar{},
	}
	for _, p := range sp.People {
		m.People = append(m.People, Person{Ref: p.Ref, Name: p.Name, Email: p.Email})
	}
	siblings := map[string][]*wikiread.Page{}
	for _, p := range sp.Pages {
		siblings[parentKey(p)] = append(siblings[parentKey(p)], p)
	}
	ranks := map[uuid.UUID]string{}
	for _, list := range siblings {
		seq, err := rank.Sequence(len(list))
		if err != nil {
			return nil, nil, err
		}
		for i, p := range list {
			ranks[p.ID] = seq[i]
		}
	}
	var unpacked int64
	for i, p := range sp.Pages {
		ap := src.archivePage(p, ranks[p.ID], i == 0, now)
		for _, f := range p.Files {
			unpacked += f.Size
			src.files[f.ID] = f
		}
		if unpacked > MaxUnpackedBytes {
			return nil, nil, tooLarge("The export unpacks into more than %d GB, more than one import takes. Split the space before you export it.", MaxUnpackedBytes>>30)
		}
		src.pages[p.ID] = ap
		m.Pages = append(m.Pages, p.ID)
		m.Counts.Pages++
		m.Counts.Versions += len(ap.Versions)
		m.Counts.Files += len(ap.Files)
	}
	return m, src, CheckManifest(m)
}

func parentKey(p *wikiread.Page) string {
	switch {
	case p.Post:
		return "post"
	case p.Parent == nil:
		return "root"
	}
	return p.Parent.String()
}

// untitled names a page the export gives no title.
const untitled = "Untitled"

func title(t string) string {
	t = strings.TrimSpace(strings.Join(strings.Fields(t), " "))
	if t == "" {
		return untitled
	}
	if utf8.RuneCountInString(t) > page.MaxTitleLength {
		t = string([]rune(t)[:page.MaxTitleLength])
	}
	return t
}

func (s *exportSource) archivePage(p *wikiread.Page, rnk string, home bool, now time.Time) *ArchivePage {
	created := p.CreatedAt
	if created.IsZero() && len(p.Versions) > 0 {
		created = p.Versions[0].At
	}
	if created.IsZero() {
		created = now
	}
	ap := &ArchivePage{
		ID: p.ID, Parent: p.Parent, Kind: "page", Rank: rnk, Title: title(p.Title), Mode: "draft", Width: "fixed",
		CreatedBy: p.CreatedBy, CreatedAt: created,
		Labels: []Label{}, Restrictions: []Restriction{}, Files: []File{}, Threads: []Thread{}, Reactions: []Reaction{},
	}
	if p.Post && !home {
		ap.Kind, ap.Parent = "post", nil
		ap.PostedAt = &ap.CreatedAt
	}
	last := created
	for i, v := range p.Versions {
		at := v.At
		if at.IsZero() {
			at = last
		}
		last = at
		ap.Versions = append(ap.Versions, PageVersion{
			Number: i + 1, Title: title(v.Title), Body: v.Body, Comment: v.Comment, CreatedBy: v.By, CreatedAt: at, UpdatedAt: at,
		})
	}
	if len(ap.Versions) > 0 {
		ap.Body = ap.Versions[len(ap.Versions)-1].Body
		ap.Title = ap.Versions[len(ap.Versions)-1].Title
	}
	ap.UpdatedAt = last
	ap.PublishedAt = &last

	for _, raw := range p.Labels {
		name, err := label.Normalize(raw)
		if err != nil || len(ap.Labels) >= label.MaxPerPage {
			s.lose(ap.Title, wikiread.LossLabel, raw)
			continue
		}
		dup := false
		for _, l := range ap.Labels {
			dup = dup || l.Name == name
		}
		if !dup {
			ap.Labels = append(ap.Labels, Label{Name: name, CreatedBy: p.CreatedBy, CreatedAt: created})
		}
	}

	versions := map[string]int{}
	for _, f := range p.Files {
		at := f.At
		if at.IsZero() {
			at = created
		}
		versions[f.Name]++
		by := f.By
		if by == "" {
			by = p.CreatedBy
		}
		ap.Files = append(ap.Files, File{
			ID: f.ID, Name: fileName(f.Name), Version: versions[f.Name], ContentType: f.ContentType, Size: f.Size, UploadedBy: by, CreatedAt: at,
		})
	}

	for _, t := range p.Threads {
		if len(t.Comments) == 0 {
			continue
		}
		thread := Thread{ID: uuid.Must(uuid.NewV7()), Kind: "page", CreatedBy: t.Comments[0].By, CreatedAt: t.Comments[0].At}
		for _, c := range t.Comments {
			at := c.At
			if at.IsZero() {
				at = last
			}
			thread.Comments = append(thread.Comments, Comment{ID: c.ID, Author: c.By, Body: c.Body, CreatedAt: at})
		}
		if thread.CreatedAt.IsZero() {
			thread.CreatedAt = thread.Comments[0].CreatedAt
		}
		ap.Threads = append(ap.Threads, thread)
	}
	return ap
}

// maxFileName is the longest name a file keeps, as an upload keeps it.
const maxFileName = 200

func fileName(n string) string {
	n = strings.TrimSpace(n)
	if n == "" {
		return "file"
	}
	if utf8.RuneCountInString(n) > maxFileName {
		n = string([]rune(n)[:maxFileName])
	}
	return n
}
