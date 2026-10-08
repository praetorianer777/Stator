package spaceio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// emptyDoc is the body of a folder, and of a page with nothing in it.
const emptyDoc = `{"type":"doc","content":[{"type":"paragraph"}]}`

// archive is an uploaded archive opened for reading, which counts what it
// unpacks against MaxUnpackedBytes whatever its directory claims.
type archive struct {
	zr       *zip.Reader
	entries  map[string]*zip.File
	unpacked int64
}

func openArchive(zr *zip.Reader) *archive {
	a := &archive{zr: zr, entries: map[string]*zip.File{}}
	for _, f := range zr.File {
		a.entries[f.Name] = f
	}
	return a
}

// open reads an entry, refusing more than limit bytes of it.
func (a *archive) read(name string, limit int64) ([]byte, error) {
	f := a.entries[name]
	if f == nil {
		return nil, invalid("The archive lacks %s, which its table of contents names. Export the space again and import the new file.", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, invalid("The archive's %s could not be read. Export the space again and import the new file.", name)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, invalid("The archive's %s could not be read. Export the space again and import the new file.", name)
	}
	if int64(len(data)) > limit {
		return nil, tooLarge("The archive's %s unpacks into more than %d MB, more than one import takes. Split the space before you export it.", name, limit>>20)
	}
	if a.unpacked += int64(len(data)); a.unpacked > MaxUnpackedBytes {
		return nil, tooLarge("The archive unpacks into more than %d GB, more than one import takes. Split the space before you export it.", MaxUnpackedBytes>>30)
	}
	return data, nil
}

// readManifest reads and checks an archive's table of contents.
func readManifest(zr *zip.Reader) (*Manifest, error) {
	a := openArchive(zr)
	if a.entries[manifestPath] == nil {
		return nil, invalid("This file is not a space archive of Stator: it holds no manifest.json. Export the space from Stator as an archive, then import that file.")
	}
	data, err := a.read(manifestPath, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, invalid("The archive's manifest.json is not one Stator reads. Export the space again and import the new file.")
	}
	if err := CheckManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// TooLargeArchiveError is an archive past what one import takes, in a sentence.
type TooLargeArchiveError struct{ Message string }

func (e *TooLargeArchiveError) Error() string { return e.Message }

func tooLarge(format string, args ...any) error {
	return &TooLargeArchiveError{Message: fmt.Sprintf(format, args...)}
}

// source is where an import reads pages and files: a Stator archive, or a
// space export of another wiki read into the same shapes beforehand.
type source interface {
	page(id uuid.UUID) (*ArchivePage, error)
	// fileSize is what a file's bytes weigh as the source holds them.
	fileSize(f File) (int64, bool)
	fileBytes(f File) ([]byte, error)
}

type archiveSource struct{ a *archive }

func (s archiveSource) page(id uuid.UUID) (*ArchivePage, error) {
	data, err := s.a.read(pagePath(id), MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	var p ArchivePage
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, invalid("The archive's entry for the page %s is not one Stator reads. Export the space again and import the new file.", id)
	}
	if p.ID != id {
		return nil, invalid("The archive's entry for the page %s describes another page. Export the space again and import the new file.", id)
	}
	return &p, nil
}

func (s archiveSource) fileSize(f File) (int64, bool) {
	entry := s.a.entries[filePath(f.ID)]
	if entry == nil {
		return 0, false
	}
	return int64(entry.UncompressedSize64), true
}

func (s archiveSource) fileBytes(f File) ([]byte, error) { return s.a.read(filePath(f.ID), f.Size) }

// importer makes one space of one archive, as the person who asked for it.
type importer struct {
	src      source
	m        *Manifest
	store    objectstore.Store
	org      uuid.UUID
	importer uuid.UUID
	key      string
	name     string
	spaceID  uuid.UUID
	r        renames
	// people and groups are the archive's refs found here; known, every
	// ref the archive names with what it says of them.
	people map[string]uuid.UUID
	known  map[string]Person
	groups map[string]uuid.UUID
	gnames map[string]string
	report Report
	// comments are the archive's comments by their ids here.
	comments map[uuid.UUID]uuid.UUID
	// uploaded are the objects stored so far, deleted again if the import fails.
	uploaded []string
	step     func()
	// from is what the space was made of; an export of another wiki brings
	// no permissions, so the space keeps those every new space starts with.
	from ImportSource
}

// scan reads every page once, before anything is written, and refuses an
// archive whose pages do not fit together, naming what is wrong.
func (im *importer) scan() error {
	im.r = renames{pages: map[uuid.UUID]uuid.UUID{}, files: map[uuid.UUID]uuid.UUID{}, threads: map[uuid.UUID]uuid.UUID{},
		calendars: map[uuid.UUID]uuid.UUID{}, people: map[uuid.UUID]uuid.UUID{}, oldKey: im.m.Space.Key, newKey: im.key}
	if len(im.m.Pages) == 0 || im.m.Pages[0] != im.m.Space.HomePage {
		return invalid("The archive's first page is not the space's home page. Export the space again and import the new file.")
	}
	seen := map[uuid.UUID]string{}
	versions, files := 0, 0
	for i, id := range im.m.Pages {
		if _, dup := seen[id]; dup {
			return invalid("The archive lists the page %s twice. Export the space again and import the new file.", id)
		}
		p, err := im.src.page(id)
		if err != nil {
			return err
		}
		if err := checkPage(p, i == 0, seen); err != nil {
			return err
		}
		seen[id] = p.Title
		im.r.pages[id] = uuid.Must(uuid.NewV7())
		for _, f := range p.Files {
			if _, dup := im.r.files[f.ID]; dup {
				return invalid("The archive names the file %s twice. Export the space again and import the new file.", f.ID)
			}
			if size, ok := im.src.fileSize(f); !ok || size != f.Size || f.Size <= 0 {
				return invalid("The file %q of the page %q is missing from the archive, or not as large as it says. Export the space again and import the new file.", f.Name, p.Title)
			}
			im.r.files[f.ID] = uuid.Must(uuid.NewV7())
		}
		for _, t := range p.Threads {
			if _, dup := im.r.threads[t.ID]; dup {
				return invalid("The archive names the discussion %s twice. Export the space again and import the new file.", t.ID)
			}
			im.r.threads[t.ID] = uuid.Must(uuid.NewV7())
		}
		versions += len(p.Versions)
		files += len(p.Files)
		if versions > MaxVersions || files > MaxFiles {
			return tooLarge("The archive holds more than %d versions or %d files, more than one import takes. Split the space before you export it.", MaxVersions, MaxFiles)
		}
	}
	for _, c := range im.m.Calendars {
		im.r.calendars[c.ID] = uuid.Must(uuid.NewV7())
	}
	return nil
}

var (
	pageKinds    = []string{"page", "folder", "post"}
	pageModes    = []string{"draft", "live"}
	pageWidths   = []string{"fixed", "full"}
	restrictions = []string{"view", "edit", "editGrant"}
	permissions  = []string{"view", "addPages", "addComments", "delete", "administer"}
)

// checkPage holds one page to what the space's tables take, so a refusal
// names the page rather than a constraint.
func checkPage(p *ArchivePage, home bool, before map[uuid.UUID]string) error {
	title := strings.TrimSpace(p.Title)
	name := p.Title
	if title == "" {
		name = p.ID.String()
	}
	bad := func(what string) error {
		return invalid("The archive's page %q %s. Export the space again and import the new file.", name, what)
	}
	switch {
	case title == "" || utf8.RuneCountInString(title) > page.MaxTitleLength:
		return bad(fmt.Sprintf("has no title, or one longer than %d characters", page.MaxTitleLength))
	case !slices.Contains(pageKinds, p.Kind):
		return bad("is of a kind Stator does not know")
	case home && (p.Parent != nil || p.Kind != "page"):
		return bad("is the home page but hangs from another page, or is no page")
	case !home && p.Kind == "post" && p.Parent != nil:
		return bad("is a blog post but hangs from a page")
	case !home && p.Kind != "post" && p.Parent == nil:
		return bad("hangs from no page")
	case p.Rank == "":
		return bad("has no place among its siblings")
	case !slices.Contains(pageModes, p.Mode) || !slices.Contains(pageWidths, p.Width):
		return bad("has a mode or width Stator does not know")
	case p.Kind == "folder" && (len(p.Versions) > 0 || len(p.Files) > 0 || len(p.Threads) > 0 || len(p.Labels) > 0 || len(p.Reactions) > 0):
		return bad("is a folder but holds content of its own")
	}
	if p.Parent != nil {
		if _, ok := before[*p.Parent]; !ok {
			return bad("hangs from a page the archive does not list before it")
		}
	}
	for i, v := range p.Versions {
		if v.Number != i+1 {
			return bad("has versions that are not numbered one after another")
		}
		if v.RestoredFrom != nil && (*v.RestoredFrom < 1 || *v.RestoredFrom >= v.Number) {
			return bad("has a version restored from one that does not come before it")
		}
		if strings.TrimSpace(v.Title) == "" {
			return bad(fmt.Sprintf("has a version %d without a title", v.Number))
		}
	}
	for _, r := range p.Restrictions {
		if !slices.Contains(restrictions, r.Kind) || (r.Subject.Type != "user" && r.Subject.Type != "group") {
			return bad("has a restriction Stator does not know")
		}
	}
	return nil
}

// mapPeople finds the archive's people by address and its groups by name
// among the organization's members and groups; a guest is not taken.
func (im *importer) mapPeople(ctx context.Context, tx db.DBTX) error {
	im.people, im.known, im.groups, im.gnames = map[string]uuid.UUID{}, map[string]Person{}, map[string]uuid.UUID{}, map[string]string{}
	emails := []string{}
	for _, p := range im.m.People {
		im.known[p.Ref] = p
		if p.Email != "" {
			emails = append(emails, strings.ToLower(p.Email))
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id, lower(u.email::text) FROM app_user u
		JOIN org_member m ON m.user_id = u.id AND m.org_id = $1 AND m.org_role <> 'guest'
		WHERE lower(u.email::text) = ANY($2)`, im.org, emails)
	if err != nil {
		return err
	}
	here := map[string]uuid.UUID{}
	for rows.Next() {
		var (
			id    uuid.UUID
			email string
		)
		if err := rows.Scan(&id, &email); err != nil {
			rows.Close()
			return err
		}
		here[email] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range im.m.People {
		if id, ok := here[strings.ToLower(p.Email)]; ok && p.Email != "" {
			im.people[p.Ref] = id
			if old, err := uuid.Parse(p.Ref); err == nil {
				im.r.people[old] = id
			}
			continue
		}
		if len(im.report.People) < maxReported {
			im.report.People = append(im.report.People, MissingPerson{Name: p.Name, Email: p.Email})
		}
	}
	names := []string{}
	for _, g := range im.m.Groups {
		im.gnames[g.Ref] = g.Name
		names = append(names, strings.ToLower(g.Name))
	}
	rows, err = tx.Query(ctx, `
		SELECT DISTINCT ON (lower(name)) id, lower(name) FROM groups WHERE org_id = $1 AND lower(name) = ANY($2)
		ORDER BY lower(name), id`, im.org, names)
	if err != nil {
		return err
	}
	groups := map[string]uuid.UUID{}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		groups[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, g := range im.m.Groups {
		if id, ok := groups[strings.ToLower(g.Name)]; ok {
			im.groups[g.Ref] = id
			continue
		}
		if len(im.report.Groups) < maxReported {
			im.report.Groups = append(im.report.Groups, g.Name)
		}
	}
	return nil
}

// by is whom a piece of work is attributed to here: its author when found,
// else the importer, with the author's name as the archive gave it.
func (im *importer) by(ref string) (uuid.UUID, *string) {
	if id, ok := im.people[ref]; ok {
		return id, nil
	}
	if p, ok := im.known[ref]; ok && p.Name != "" {
		im.report.Reattributed++
		name := p.Name
		return im.importer, &name
	}
	return im.importer, nil
}

// person is somebody found here, or nobody.
func (im *importer) person(ref string) *uuid.UUID {
	if id, ok := im.people[ref]; ok {
		return &id
	}
	return nil
}

// subject is a grant's or restriction's subject here, or false with the
// name it went by when nobody here answers to it.
func (im *importer) subject(s Subject) (typ string, user, group *uuid.UUID, name string, ok bool) {
	switch s.Type {
	case "user":
		if id, found := im.people[s.Person]; found {
			return s.Type, &id, nil, "", true
		}
		p := im.known[s.Person]
		name = p.Name
		if p.Email != "" {
			name = fmt.Sprintf("%s <%s>", p.Name, p.Email)
		}
		return s.Type, nil, nil, name, false
	case "group":
		if id, found := im.groups[s.Group]; found {
			return s.Type, nil, &id, "", true
		}
		return s.Type, nil, nil, im.gnames[s.Group], false
	case "everyone", "anonymous":
		return s.Type, nil, nil, "", true
	}
	return s.Type, nil, nil, s.Type, false
}

func (im *importer) dropped(pageTitle, permission, subject string) {
	if len(im.report.Dropped) < maxReported {
		im.report.Dropped = append(im.report.Dropped, DroppedEntry{Page: pageTitle, Permission: permission, Subject: subject})
	}
}

// pageError names the page a write failed on.
type pageError struct {
	title string
	err   error
}

func (e *pageError) Error() string { return fmt.Sprintf("page %q: %v", e.title, e.err) }
func (e *pageError) Unwrap() error { return e.err }

// write makes the space and everything in it, in the caller's transaction,
// and uploads the files' bytes as it goes.
func (im *importer) write(ctx context.Context, tx db.DBTX) error {
	description := strings.TrimSpace(im.m.Space.Description)
	if utf8.RuneCountInString(description) > space.MaxDescriptionLength {
		description = string([]rune(description)[:space.MaxDescriptionLength])
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO space (id, org_id, key, name, description, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, im.spaceID, im.org, im.key, im.name, description, im.importer); err != nil {
		return err
	}
	if err := im.writeGrants(ctx, tx); err != nil {
		return err
	}
	for _, c := range im.m.Calendars {
		if err := im.writeCalendar(ctx, tx, c); err != nil {
			return err
		}
	}
	for _, id := range im.m.Pages {
		p, err := im.src.page(id)
		if err != nil {
			return err
		}
		if err := im.writePage(ctx, tx, p); err != nil {
			var inv *InvalidError
			if errors.As(err, &inv) {
				return err
			}
			return &pageError{title: p.Title, err: err}
		}
		im.step()
	}
	for _, t := range im.m.Templates {
		if err := im.writeTemplate(ctx, tx, t); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE space SET home_page_id = $2 WHERE id = $1`, im.spaceID, im.r.pages[im.m.Space.HomePage]); err != nil {
		return err
	}
	im.report.Pages, im.report.Templates, im.report.Calendars = len(im.m.Pages), len(im.m.Templates), len(im.m.Calendars)
	im.report.MentionsAsText = im.r.asText
	data := map[string]any{
		"key": im.key, "name": im.name, "source": string(im.from), "pages": im.report.Pages, "versions": im.report.Versions,
		"files": im.report.Files, "peopleNotFound": len(im.report.People), "groupsNotFound": len(im.report.Groups),
	}
	if im.m.Space.Key != "" {
		data["from"] = im.m.Space.Key
	}
	if im.from != SourceArchive {
		data["lost"] = im.report.LostCount
	}
	return audit.Write(ctx, tx, im.org, audit.Entry{
		Action: audit.ActionSpaceImported, TargetType: "space", TargetID: &im.spaceID, Actor: im.importer, Data: data,
	})
}

// writeGrants replaces the permissions every new space starts with by the
// archive's, keeping the importer an administrator so the space is theirs.
func (im *importer) writeGrants(ctx context.Context, tx db.DBTX) error {
	if im.from != SourceArchive {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM space_grant WHERE space_id = $1
		AND NOT (permission = 'administer' AND subject_type = 'user' AND user_id = $2)`, im.spaceID, im.importer); err != nil {
		return err
	}
	for _, g := range im.m.Grants {
		if !slices.Contains(permissions, g.Permission) {
			return invalid("The archive grants %q, a permission Stator does not know. Export the space again and import the new file.", g.Permission)
		}
		typ, user, group, name, ok := im.subject(g.Subject)
		if !ok {
			im.dropped("", g.Permission, name)
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id, group_id)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, im.org, im.spaceID, g.Permission, typ, user, group); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) writeCalendar(ctx context.Context, tx db.DBTX, c Calendar) error {
	id := im.r.calendars[c.ID]
	if _, err := tx.Exec(ctx, `INSERT INTO calendar (org_id, id, space_id, name) VALUES ($1, $2, $3, $4)`, im.org, id, im.spaceID, c.Name); err != nil {
		return err
	}
	for _, e := range c.Events {
		if _, err := tx.Exec(ctx, `
			INSERT INTO calendar_event (org_id, space_id, calendar_id, title, kind, all_day, starts_at, ends_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, im.org, im.spaceID, id, e.Title, e.Kind, e.AllDay, e.StartsAt, e.EndsAt); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) writeTemplate(ctx context.Context, tx db.DBTX, t Template) error {
	body, err := im.r.rewrite(t.Body)
	if err != nil {
		return invalid("The archive's template %q is not one Stator reads. Export the space again and import the new file.", t.Name)
	}
	if _, err := document.ParseTemplate(body); err != nil {
		return invalid("The archive's template %q is not one Stator accepts: %s Export the space again and import the new file.", t.Name, sentenceOf(err))
	}
	variables := t.Variables
	if len(variables) == 0 {
		variables = json.RawMessage("[]")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO page_template (org_id, space_id, name, description, title, body, variables)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, im.org, im.spaceID, t.Name, t.Description, t.Title, body, variables)
	return err
}

// sentenceOf is an error's words ending in a full stop, to go inside another sentence.
func sentenceOf(err error) string {
	s := strings.TrimSpace(err.Error())
	if s != "" && !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// checked is a document of the page rewritten for here and held to the allowlist.
func (im *importer) checked(p *ArchivePage, body json.RawMessage, what string) (json.RawMessage, error) {
	out, err := im.r.rewrite(body)
	if err != nil {
		return nil, invalid("The archive's page %q holds %s that is not a document. Export the space again and import the new file.", p.Title, what)
	}
	if err := document.ValidatePage(out, im.r.pages[p.ID].String()); err != nil {
		return nil, invalid("The archive's page %q holds %s that Stator does not accept: %s Export the space again and import the new file.", p.Title, what, sentenceOf(err))
	}
	return out, nil
}

func (im *importer) writePage(ctx context.Context, tx db.DBTX, p *ArchivePage) error {
	id := im.r.pages[p.ID]
	var parent *uuid.UUID
	if p.Parent != nil {
		up := im.r.pages[*p.Parent]
		parent = &up
	}
	body := json.RawMessage(emptyDoc)
	if p.Kind != "folder" {
		var err error
		if body, err = im.checked(p, p.Body, "a body"); err != nil {
			return err
		}
	}
	version := 0
	if p.Kind == "folder" {
		version = 1
	}
	createdBy, _ := im.by(p.CreatedBy)
	var archivedAt *time.Time
	var archivedBy, archiveRoot *uuid.UUID
	if p.Archived != nil {
		root, ok := im.r.pages[p.Archived.Root]
		if !ok {
			root = id
		}
		by, _ := im.by(p.Archived.By)
		archivedAt, archivedBy, archiveRoot = &p.Archived.At, &by, &root
	}
	var postedAt *time.Time
	if p.Kind == "post" {
		postedAt = p.PostedAt
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO page (id, org_id, space_id, parent_id, rank, title, body, version, kind, mode, icon, width,
		                  created_by, created_at, updated_by, posted_at, archived_at, archived_by, archive_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $13, $15, $16, $17, $18)`,
		id, im.org, im.spaceID, parent, p.Rank, strings.TrimSpace(p.Title), body, version, p.Kind, p.Mode, p.Icon, p.Width,
		createdBy, p.CreatedAt, postedAt, archivedAt, archivedBy, archiveRoot); err != nil {
		return err
	}
	if p.Kind == "folder" {
		return nil
	}
	for _, f := range p.Files {
		if err := im.writeFile(ctx, tx, id, f); err != nil {
			return err
		}
	}
	var lastBy uuid.UUID = createdBy
	for _, v := range p.Versions {
		vbody, err := im.checked(p, v.Body, fmt.Sprintf("a version %d", v.Number))
		if err != nil {
			return err
		}
		author, original := im.by(v.CreatedBy)
		lastBy = author
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_version (org_id, page_id, number, title, body, comment, restored_from, live, created_by, created_at, updated_at, original_author)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			im.org, id, v.Number, strings.TrimSpace(v.Title), vbody, clip(v.Comment, page.MaxCommentLength), v.RestoredFrom, v.Live, author, v.CreatedAt, v.UpdatedAt, original); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET version = $2 WHERE id = $1`, id, v.Number); err != nil {
			return err
		}
		im.report.Versions++
	}
	var cover *uuid.UUID
	focusX, focusY := 50, 50
	if p.Cover != nil {
		if c, ok := im.r.files[p.Cover.File]; ok {
			cover, focusX, focusY = &c, p.Cover.FocusX, p.Cover.FocusY
		}
	}
	published := p.PublishedAt
	if len(p.Versions) == 0 {
		published = nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE page SET updated_at = $2, published_at = $3, updated_by = $4, cover_attachment_id = $5, cover_focus_x = $6, cover_focus_y = $7
		WHERE id = $1`, id, p.UpdatedAt, published, lastBy, cover, focusX, focusY); err != nil {
		return err
	}
	for _, l := range p.Labels {
		by, _ := im.by(l.CreatedBy)
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_label (org_id, page_id, name, created_by, created_at) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, im.org, id, l.Name, by, l.CreatedAt); err != nil {
			return err
		}
	}
	if err := im.writeRestrictions(ctx, tx, p, id); err != nil {
		return err
	}
	if err := im.writeThreads(ctx, tx, p, id); err != nil {
		return err
	}
	if err := im.writeReactions(ctx, tx, p, id); err != nil {
		return err
	}
	if len(p.Versions) > 0 {
		return page.SyncTasks(ctx, tx, id)
	}
	return nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// writeFile stores a file's bytes, then its row, whose trigger numbers it
// as the archive's order of versions does.
func (im *importer) writeFile(ctx context.Context, tx db.DBTX, pageID uuid.UUID, f File) error {
	id := im.r.files[f.ID]
	data, err := im.src.fileBytes(f)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		return invalid("The archive holds a file without a name. Export the space again and import the new file.")
	}
	contentType := f.ContentType
	if contentType == "" {
		contentType = attachment.ContentTypeFor("", data)
	}
	if f.Width == nil && f.Height == nil {
		f.Width, f.Height = attachment.Measure(contentType, data)
	}
	key := "org/" + im.org.String() + "/page/" + pageID.String() + "/" + id.String()
	if err := im.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return err
	}
	im.uploaded = append(im.uploaded, key)
	by, original := im.by(f.UploadedBy)
	if _, err := tx.Exec(ctx, `
		INSERT INTO attachment (id, org_id, page_id, uploaded_by, file_name, content_type, size_bytes, width, height, created_at,
		                        restored_from, edited_from, original_author)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, im.org, pageID, by, name, contentType, int64(len(data)), f.Width, f.Height, f.CreatedAt, f.RestoredFrom, f.EditedFrom, original); err != nil {
		return err
	}
	im.report.Files++
	im.step()
	return nil
}

func (im *importer) writeRestrictions(ctx context.Context, tx db.DBTX, p *ArchivePage, id uuid.UUID) error {
	for _, r := range p.Restrictions {
		typ, user, group, name, ok := im.subject(r.Subject)
		if !ok {
			im.dropped(p.Title, r.Kind, name)
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_restriction (org_id, page_id, kind, subject_type, user_id, group_id)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, im.org, id, r.Kind, typ, user, group); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) writeThreads(ctx context.Context, tx db.DBTX, p *ArchivePage, id uuid.UUID) error {
	for _, t := range p.Threads {
		if t.Kind != "page" && t.Kind != "inline" {
			return invalid("The archive's page %q holds a discussion of a kind Stator does not know. Export the space again and import the new file.", p.Title)
		}
		by, _ := im.by(t.CreatedBy)
		if _, err := tx.Exec(ctx, `
			INSERT INTO comment_thread (org_id, id, page_id, kind, created_by, created_at, quote, detached_at, resolved_at, resolved_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			im.org, im.r.threads[t.ID], id, t.Kind, by, t.CreatedAt, t.Quote, t.DetachedAt, t.ResolvedAt, im.resolver(t)); err != nil {
			return err
		}
		for _, c := range t.Comments {
			var body json.RawMessage
			if c.DeletedAt == nil {
				var err error
				if body, err = im.r.rewrite(c.Body); err != nil {
					return invalid("The archive's page %q holds a comment that is not a document. Export the space again and import the new file.", p.Title)
				}
				if _, err := document.ParseComment(body); err != nil {
					return invalid("The archive's page %q holds a comment that Stator does not accept: %s Export the space again and import the new file.", p.Title, sentenceOf(err))
				}
			}
			author, original := im.by(c.Author)
			if _, err := tx.Exec(ctx, `
				INSERT INTO comment (org_id, id, thread_id, page_id, author_id, body, created_at, edited_at, deleted_at, deleted_by, original_author)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				im.org, im.commentID(c.ID), im.r.threads[t.ID], id, author, body, c.CreatedAt, c.EditedAt, c.DeletedAt, im.deleter(c), original); err != nil {
				return err
			}
			im.report.Comments++
		}
	}
	return nil
}

func (im *importer) resolver(t Thread) *uuid.UUID {
	if t.ResolvedAt == nil {
		return nil
	}
	by, _ := im.by(t.ResolvedBy)
	return &by
}

func (im *importer) deleter(c Comment) *uuid.UUID {
	if c.DeletedAt == nil {
		return nil
	}
	return im.person(c.DeletedBy)
}

// commentID is a comment's id here, the same for every reaction to it.
func (im *importer) commentID(old uuid.UUID) uuid.UUID {
	if im.comments == nil {
		im.comments = map[uuid.UUID]uuid.UUID{}
	}
	if id, ok := im.comments[old]; ok {
		return id
	}
	id := uuid.Must(uuid.NewV7())
	im.comments[old] = id
	return id
}

func (im *importer) writeReactions(ctx context.Context, tx db.DBTX, p *ArchivePage, id uuid.UUID) error {
	gone := map[uuid.UUID]bool{}
	for _, t := range p.Threads {
		for _, c := range t.Comments {
			if c.DeletedAt != nil {
				gone[c.ID] = true
			}
		}
	}
	for _, r := range p.Reactions {
		who := im.person(r.Person)
		if who == nil {
			im.report.DroppedReactions++
			continue
		}
		var on *uuid.UUID
		if r.Comment != nil {
			if gone[*r.Comment] {
				continue
			}
			if _, ok := im.comments[*r.Comment]; !ok {
				continue
			}
			c := im.commentID(*r.Comment)
			on = &c
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reaction (org_id, page_id, comment_id, user_id, emoji, created_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`, im.org, id, on, *who, r.Emoji, r.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

// failureOf reads why an import failed, with the sentence its requester reads.
func failureOf(err error) (Failure, string) {
	var (
		inv   *InvalidError
		big   *TooLargeArchiveError
		pgErr *pgconn.PgError
		at    *pageError
	)
	switch {
	case errors.As(err, &inv):
		return FailureInvalid, inv.Message
	case errors.As(err, &big):
		return FailureTooLarge, big.Message
	case errors.As(err, &pgErr) && pgErr.ConstraintName == "space_key_unique":
		return FailureKeyTaken, ""
	case isDenied(err):
		return FailureForbidden, ""
	case errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "23") && errors.As(err, &at):
		return FailureInvalid, fmt.Sprintf("The archive's page %q could not be saved as it is (%s). Export the space again and import the new file.", at.title, pgErr.ConstraintName)
	}
	return FailureFailed, ""
}
