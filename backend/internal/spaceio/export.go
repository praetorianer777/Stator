package spaceio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// snapshot is a space as its exporter reads it, in one transaction, without
// its versions' bodies, which the archive reads page by page.
type snapshot struct {
	exportedAt time.Time
	exporter   uuid.UUID
	space      *space.Space
	pages      []*ArchivePage
	byID       map[uuid.UUID]*ArchivePage
	grants     []Grant
	templates  []Template
	calendars  []Calendar
	// people and groups are everybody named, by id here; names are what the
	// HTML pages say.
	people map[uuid.UUID]Person
	groups map[uuid.UUID]Group
	files  int
	// keys are the stored objects of the files, by file.
	keys map[uuid.UUID]string
	// brand is the organization's, read for the HTML export alone.
	brand *htmlBrand
}

// ref is how the archive names a person: their id, when they are one of
// People, else nobody.
func (s *snapshot) ref(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if _, ok := s.people[*id]; !ok {
		return ""
	}
	return id.String()
}

// name is a person's name for a reader of the HTML pages.
func (s *snapshot) name(ref string) string {
	id, err := uuid.Parse(ref)
	if err != nil {
		return ""
	}
	return s.people[id].Name
}

// ErrNotExportable refuses an export by somebody who does not administer the space.
var ErrNotExportable = &perm.DeniedError{Action: perm.AdministerSpace}

// read takes the snapshot: every page the exporter may read that is meant
// for readers, everything on it, and the space's own settings.
func read(ctx context.Context, tx db.DBTX, actor perm.Actor, spaceID uuid.UUID) (*snapshot, error) {
	sp, err := space.Load(ctx, tx, actor, space.ByID, spaceID)
	if errors.Is(err, space.ErrNotFound) {
		return nil, ErrNotExportable
	}
	if err != nil {
		return nil, err
	}
	if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
		return nil, err
	}
	s := &snapshot{exporter: actor.UserID, space: sp, byID: map[uuid.UUID]*ArchivePage{}, people: map[uuid.UUID]Person{}, groups: map[uuid.UUID]Group{}, keys: map[uuid.UUID]string{}}
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&s.exportedAt); err != nil {
		return nil, err
	}
	users := map[uuid.UUID]bool{actor.UserID: true}
	note := func(id *uuid.UUID) {
		if id != nil {
			users[*id] = true
		}
	}
	if err := s.readPages(ctx, tx, note); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(s.pages))
	for i, p := range s.pages {
		ids[i] = p.ID
	}
	groupIDs := map[uuid.UUID]bool{}
	steps := []func() error{
		func() error { return s.readGrants(ctx, tx, note, groupIDs) },
		func() error { return s.readRestrictions(ctx, tx, ids, note, groupIDs) },
		func() error { return s.readLabels(ctx, tx, ids, note) },
		func() error { return s.readFiles(ctx, tx, ids, note) },
		func() error { return s.readThreads(ctx, tx, ids, note) },
		func() error { return s.readReactions(ctx, tx, ids, note) },
		func() error { return s.readTemplates(ctx, tx) },
		func() error { return s.readCalendars(ctx, tx) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	for _, p := range s.pages {
		for _, id := range mentioned(p.Body) {
			users[id] = true
		}
	}
	// Every version's author is named in the archive, so they are read now,
	// ahead of the versions themselves.
	authors, err := tx.Query(ctx, `SELECT DISTINCT created_by FROM page_version WHERE page_id = ANY($1) AND created_by IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	byAuthor, err := pgx.CollectRows(authors, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range byAuthor {
		users[id] = true
	}
	if err := s.readPeople(ctx, tx, users); err != nil {
		return nil, err
	}
	if err := s.readGroups(ctx, tx, groupIDs); err != nil {
		return nil, err
	}
	s.settleRefs()
	return s, nil
}

// pageRow is a page as read, before it is placed.
type pageRow struct {
	ArchivePage
	parent      *uuid.UUID
	version     int
	createdBy   *uuid.UUID
	cover       *uuid.UUID
	focusX      int
	focusY      int
	archivedBy  *uuid.UUID
	archivedAt  *time.Time
	archiveRoot *uuid.UUID
}

// readPages reads the pages readers read: the home page, folders, and every
// page or post published at least once, out of the trash. A page whose
// parent is not among them, a draft somebody published a page below, hangs
// from the nearest page above it that is.
func (s *snapshot) readPages(ctx context.Context, tx db.DBTX, note func(*uuid.UUID)) error {
	rows, err := tx.Query(ctx, `
		SELECT id, parent_id, kind, rank, title, mode, icon, width, cover_attachment_id, cover_focus_x, cover_focus_y,
		       created_by, created_at, updated_at, published_at, posted_at, archived_at, archived_by, archive_id, body, version
		FROM page WHERE space_id = $1 AND trashed_at IS NULL`, s.space.ID)
	if err != nil {
		return err
	}
	var all []*pageRow
	for rows.Next() {
		var r pageRow
		if err := rows.Scan(&r.ID, &r.parent, &r.Kind, &r.Rank, &r.Title, &r.Mode, &r.Icon, &r.Width, &r.cover, &r.focusX, &r.focusY,
			&r.createdBy, &r.CreatedAt, &r.UpdatedAt, &r.PublishedAt, &r.PostedAt, &r.archivedAt, &r.archivedBy, &r.archiveRoot, &r.Body, &r.version); err != nil {
			rows.Close()
			return err
		}
		all = append(all, &r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	parentOf := map[uuid.UUID]*uuid.UUID{}
	kept := map[uuid.UUID]*pageRow{}
	for _, r := range all {
		parentOf[r.ID] = r.parent
		home := r.parent == nil && r.Kind != "post"
		if home || r.Kind == "folder" || r.version > 0 {
			kept[r.ID] = r
		}
	}
	home := s.space.HomePageID
	if kept[home] == nil {
		return errors.New("the space's home page could not be read")
	}
	children := map[uuid.UUID][]*pageRow{}
	var posts []*pageRow
	for _, r := range kept {
		if r.ID == home {
			continue
		}
		if r.Kind == "post" {
			posts = append(posts, r)
			continue
		}
		up := r.parent
		for up != nil && kept[*up] == nil {
			up = parentOf[*up]
		}
		at := home
		if up != nil {
			at = *up
		}
		r.Parent = &at
		children[at] = append(children[at], r)
	}
	var walk func(r *pageRow)
	walk = func(r *pageRow) {
		s.add(r, note)
		below := children[r.ID]
		slices.SortFunc(below, func(a, b *pageRow) int {
			return strings.Compare(a.Rank+"\x00"+a.ID.String(), b.Rank+"\x00"+b.ID.String())
		})
		for _, c := range below {
			walk(c)
		}
	}
	walk(kept[home])
	slices.SortFunc(posts, func(a, b *pageRow) int {
		return compareTimes(a.PostedAt, b.PostedAt, a.ID, b.ID)
	})
	for _, p := range posts {
		s.add(p, note)
	}
	for _, p := range s.pages {
		if p.Archived != nil && s.byID[p.Archived.Root] == nil {
			p.Archived.Root = p.ID
		}
	}
	return nil
}

func compareTimes(a, b *time.Time, ida, idb uuid.UUID) int {
	switch {
	case a != nil && b != nil && !a.Equal(*b):
		return a.Compare(*b)
	case a == nil && b != nil:
		return 1
	case a != nil && b == nil:
		return -1
	}
	return strings.Compare(ida.String(), idb.String())
}

func (s *snapshot) add(r *pageRow, note func(*uuid.UUID)) {
	p := r.ArchivePage
	p.CreatedBy = idText(r.createdBy)
	note(r.createdBy)
	if r.cover != nil {
		p.Cover = &Cover{File: *r.cover, FocusX: r.focusX, FocusY: r.focusY}
	}
	if r.archivedAt != nil && r.archiveRoot != nil {
		p.Archived = &Archived{At: *r.archivedAt, By: idText(r.archivedBy), Root: *r.archiveRoot}
		note(r.archivedBy)
	}
	p.Versions, p.Labels, p.Restrictions, p.Files, p.Threads, p.Reactions = []PageVersion{}, []Label{}, []Restriction{}, []File{}, []Thread{}, []Reaction{}
	s.pages = append(s.pages, &p)
	s.byID[p.ID] = &p
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (s *snapshot) readGrants(ctx context.Context, tx db.DBTX, note func(*uuid.UUID), groups map[uuid.UUID]bool) error {
	rows, err := tx.Query(ctx, `
		SELECT permission, subject_type, user_id, group_id FROM space_grant WHERE space_id = $1
		ORDER BY permission, subject_type, user_id, group_id`, s.space.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			g             Grant
			user, group   *uuid.UUID
			permission, t string
		)
		if err := rows.Scan(&permission, &t, &user, &group); err != nil {
			return err
		}
		note(user)
		if group != nil {
			groups[*group] = true
		}
		g.Permission, g.Subject = permission, Subject{Type: t, Person: idText(user), Group: idText(group)}
		s.grants = append(s.grants, g)
	}
	return rows.Err()
}

func (s *snapshot) readRestrictions(ctx context.Context, tx db.DBTX, ids []uuid.UUID, note func(*uuid.UUID), groups map[uuid.UUID]bool) error {
	rows, err := tx.Query(ctx, `
		SELECT page_id, kind, subject_type, user_id, group_id FROM page_restriction WHERE page_id = ANY($1)
		ORDER BY page_id, kind, subject_type, user_id, group_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pageID      uuid.UUID
			kind, t     string
			user, group *uuid.UUID
		)
		if err := rows.Scan(&pageID, &kind, &t, &user, &group); err != nil {
			return err
		}
		note(user)
		if group != nil {
			groups[*group] = true
		}
		p := s.byID[pageID]
		p.Restrictions = append(p.Restrictions, Restriction{Kind: kind, Subject: Subject{Type: t, Person: idText(user), Group: idText(group)}})
	}
	return rows.Err()
}

func (s *snapshot) readLabels(ctx context.Context, tx db.DBTX, ids []uuid.UUID, note func(*uuid.UUID)) error {
	rows, err := tx.Query(ctx, `SELECT page_id, name, created_by, created_at FROM page_label WHERE page_id = ANY($1) ORDER BY page_id, name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pageID uuid.UUID
			l      Label
			by     *uuid.UUID
		)
		if err := rows.Scan(&pageID, &l.Name, &by, &l.CreatedAt); err != nil {
			return err
		}
		note(by)
		l.CreatedBy = idText(by)
		s.byID[pageID].Labels = append(s.byID[pageID].Labels, l)
	}
	return rows.Err()
}

// readFiles reads every version of every file, oldest first, which is the
// order an import uploads them in, so each takes its number again.
func (s *snapshot) readFiles(ctx context.Context, tx db.DBTX, ids []uuid.UUID, note func(*uuid.UUID)) error {
	rows, err := tx.Query(ctx, `
		SELECT page_id, id, file_name, version, content_type, size_bytes, width, height, restored_from, edited_from,
		       uploaded_by, created_at, object_key
		FROM attachment WHERE page_id = ANY($1) ORDER BY page_id, lower(file_name), version`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pageID uuid.UUID
			f      File
			by     *uuid.UUID
			key    string
		)
		if err := rows.Scan(&pageID, &f.ID, &f.Name, &f.Version, &f.ContentType, &f.Size, &f.Width, &f.Height, &f.RestoredFrom, &f.EditedFrom,
			&by, &f.CreatedAt, &key); err != nil {
			return err
		}
		note(by)
		f.UploadedBy = idText(by)
		s.byID[pageID].Files = append(s.byID[pageID].Files, f)
		s.keys[f.ID] = key
		s.files++
	}
	return rows.Err()
}

func (s *snapshot) readThreads(ctx context.Context, tx db.DBTX, ids []uuid.UUID, note func(*uuid.UUID)) error {
	rows, err := tx.Query(ctx, `
		SELECT t.page_id, t.id, t.kind, t.quote, t.created_by, t.created_at, t.resolved_at, t.resolved_by, t.detached_at
		FROM comment_thread t WHERE t.page_id = ANY($1) ORDER BY t.page_id, t.created_at, t.id`, ids)
	if err != nil {
		return err
	}
	threads := map[uuid.UUID]*Thread{}
	var order []uuid.UUID
	pageOf := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var (
			pageID   uuid.UUID
			t        Thread
			by, done *uuid.UUID
		)
		if err := rows.Scan(&pageID, &t.ID, &t.Kind, &t.Quote, &by, &t.CreatedAt, &t.ResolvedAt, &done, &t.DetachedAt); err != nil {
			rows.Close()
			return err
		}
		note(by)
		note(done)
		t.CreatedBy, t.ResolvedBy, t.Comments = idText(by), idText(done), []Comment{}
		threads[t.ID] = &t
		pageOf[t.ID] = pageID
		order = append(order, t.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `
		SELECT thread_id, id, author_id, body, created_at, edited_at, deleted_at, deleted_by
		FROM comment WHERE page_id = ANY($1) ORDER BY thread_id, created_at, id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			threadID     uuid.UUID
			c            Comment
			author, gone *uuid.UUID
			body         []byte
		)
		if err := rows.Scan(&threadID, &c.ID, &author, &body, &c.CreatedAt, &c.EditedAt, &c.DeletedAt, &gone); err != nil {
			rows.Close()
			return err
		}
		note(author)
		note(gone)
		c.Author, c.DeletedBy = idText(author), idText(gone)
		if body != nil {
			c.Body = body
		}
		if t := threads[threadID]; t != nil {
			t.Comments = append(t.Comments, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range order {
		p := s.byID[pageOf[id]]
		p.Threads = append(p.Threads, *threads[id])
	}
	return nil
}

func (s *snapshot) readReactions(ctx context.Context, tx db.DBTX, ids []uuid.UUID, note func(*uuid.UUID)) error {
	rows, err := tx.Query(ctx, `
		SELECT page_id, comment_id, user_id, emoji, created_at FROM reaction WHERE page_id = ANY($1)
		ORDER BY page_id, created_at, id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pageID, user uuid.UUID
			r            Reaction
		)
		if err := rows.Scan(&pageID, &r.Comment, &user, &r.Emoji, &r.CreatedAt); err != nil {
			return err
		}
		note(&user)
		r.Person = user.String()
		s.byID[pageID].Reactions = append(s.byID[pageID].Reactions, r)
	}
	return rows.Err()
}

func (s *snapshot) readTemplates(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT name, description, title, body, variables FROM page_template WHERE space_id = $1 ORDER BY lower(name)`, s.space.ID)
	if err != nil {
		return err
	}
	s.templates, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Template, error) {
		var t Template
		err := row.Scan(&t.Name, &t.Description, &t.Title, &t.Body, &t.Variables)
		return t, err
	})
	if s.templates == nil {
		s.templates = []Template{}
	}
	return err
}

func (s *snapshot) readCalendars(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `SELECT id, name FROM calendar WHERE space_id = $1 ORDER BY lower(name)`, s.space.ID)
	if err != nil {
		return err
	}
	s.calendars, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Calendar, error) {
		c := Calendar{Events: []Event{}}
		err := row.Scan(&c.ID, &c.Name)
		return c, err
	})
	if err != nil {
		return err
	}
	if s.calendars == nil {
		s.calendars = []Calendar{}
	}
	for i := range s.calendars {
		rows, err := tx.Query(ctx, `
			SELECT title, kind, all_day, starts_at, ends_at FROM calendar_event WHERE calendar_id = $1 ORDER BY starts_at, id`, s.calendars[i].ID)
		if err != nil {
			return err
		}
		events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
			var e Event
			err := row.Scan(&e.Title, &e.Kind, &e.AllDay, &e.StartsAt, &e.EndsAt)
			return e, err
		})
		if err != nil {
			return err
		}
		if events != nil {
			s.calendars[i].Events = events
		}
	}
	return nil
}

// readPeople reads the address of everybody named who is still a member
// here; anybody else is left out, and their work reads as nobody's.
func (s *snapshot) readPeople(ctx context.Context, tx db.DBTX, users map[uuid.UUID]bool) error {
	ids := make([]uuid.UUID, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT id, name, email::text FROM app_user WHERE id = ANY($1) ORDER BY lower(name), id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			p  Person
		)
		if err := rows.Scan(&id, &p.Name, &p.Email); err != nil {
			return err
		}
		p.Ref = id.String()
		s.people[id] = p
	}
	return rows.Err()
}

func (s *snapshot) readGroups(ctx context.Context, tx db.DBTX, groups map[uuid.UUID]bool) error {
	ids := make([]uuid.UUID, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT id, name FROM groups WHERE id = ANY($1) ORDER BY lower(name), id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			g  Group
		)
		if err := rows.Scan(&id, &g.Name); err != nil {
			return err
		}
		g.Ref = id.String()
		s.groups[id] = g
	}
	return rows.Err()
}

// settleRefs clears every reference to a person or group the snapshot could
// not name, and drops the grants and reactions that only meant them.
func (s *snapshot) settleRefs() {
	person := func(ref string) string {
		id, err := uuid.Parse(ref)
		if err != nil {
			return ""
		}
		return s.ref(&id)
	}
	known := func(sub Subject) (Subject, bool) {
		switch sub.Type {
		case "user":
			sub.Person = person(sub.Person)
			return sub, sub.Person != ""
		case "group":
			id, err := uuid.Parse(sub.Group)
			if _, ok := s.groups[id]; err != nil || !ok {
				return sub, false
			}
		}
		return sub, true
	}
	grants := s.grants[:0]
	for _, g := range s.grants {
		if sub, ok := known(g.Subject); ok {
			g.Subject = sub
			grants = append(grants, g)
		}
	}
	s.grants = grants
	for _, p := range s.pages {
		p.CreatedBy = person(p.CreatedBy)
		if p.Archived != nil {
			p.Archived.By = person(p.Archived.By)
		}
		restrictions := p.Restrictions[:0]
		for _, r := range p.Restrictions {
			if sub, ok := known(r.Subject); ok {
				r.Subject = sub
				restrictions = append(restrictions, r)
			}
		}
		p.Restrictions = restrictions
		for i := range p.Labels {
			p.Labels[i].CreatedBy = person(p.Labels[i].CreatedBy)
		}
		for i := range p.Files {
			p.Files[i].UploadedBy = person(p.Files[i].UploadedBy)
		}
		for i := range p.Threads {
			t := &p.Threads[i]
			t.CreatedBy, t.ResolvedBy = person(t.CreatedBy), person(t.ResolvedBy)
			for j := range t.Comments {
				t.Comments[j].Author, t.Comments[j].DeletedBy = person(t.Comments[j].Author), person(t.Comments[j].DeletedBy)
			}
		}
		reactions := p.Reactions[:0]
		for _, r := range p.Reactions {
			if r.Person = person(r.Person); r.Person != "" {
				reactions = append(reactions, r)
			}
		}
		p.Reactions = reactions
	}
}

// versions reads one page's published versions, oldest first.
func (s *snapshot) versions(ctx context.Context, tx db.DBTX, pageID uuid.UUID) ([]PageVersion, error) {
	rows, err := tx.Query(ctx, `
		SELECT number, title, body, comment, restored_from, live, created_by, created_at, updated_at
		FROM page_version WHERE page_id = $1 ORDER BY number`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PageVersion{}
	for rows.Next() {
		var (
			v  PageVersion
			by *uuid.UUID
		)
		if err := rows.Scan(&v.Number, &v.Title, &v.Body, &v.Comment, &v.RestoredFrom, &v.Live, &by, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.CreatedBy = s.ref(by)
		out = append(out, v)
	}
	return out, rows.Err()
}

// mentioned is everybody a document names: by a mention, or as a task
// report's assignee.
func mentioned(body json.RawMessage) []uuid.UUID {
	var root any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&root) != nil {
		return nil
	}
	var out []uuid.UUID
	var walk func(v any)
	walk = func(v any) {
		n, ok := v.(map[string]any)
		if !ok {
			return
		}
		if attrs, ok := n["attrs"].(map[string]any); ok {
			name := ""
			switch n["type"] {
			case "mention":
				name = "id"
			case "taskReport":
				name = "assignee"
			}
			if text, ok := attrs[name].(string); ok && name != "" {
				if id, err := uuid.Parse(text); err == nil {
					out = append(out, id)
				}
			}
		}
		if children, ok := n["content"].([]any); ok {
			for _, c := range children {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// manifest is the snapshot's table of contents, with counts once the pages
// are written.
func (s *snapshot) manifest(counts Counts) *Manifest {
	m := &Manifest{
		Format: Format, Version: Version, ExportedAt: s.exportedAt, ExportedBy: s.ref(&s.exporter),
		Space:  ArchiveSpace{Key: s.space.Key, Name: s.space.Name, Description: s.space.Description, CreatedAt: s.space.CreatedAt, HomePage: s.space.HomePageID},
		People: []Person{}, Groups: []Group{}, Grants: s.grants, Templates: s.templates, Calendars: s.calendars, Counts: counts,
	}
	for _, p := range s.people {
		m.People = append(m.People, p)
	}
	slices.SortFunc(m.People, func(a, b Person) int { return strings.Compare(a.Ref, b.Ref) })
	for _, g := range s.groups {
		m.Groups = append(m.Groups, g)
	}
	slices.SortFunc(m.Groups, func(a, b Group) int { return strings.Compare(a.Ref, b.Ref) })
	if m.Grants == nil {
		m.Grants = []Grant{}
	}
	for _, p := range s.pages {
		m.Pages = append(m.Pages, p.ID)
	}
	return m
}

// tooMany refuses an export of a space past what one import takes, which
// would make an archive nobody can bring back.
func (s *snapshot) tooMany() error {
	switch {
	case len(s.pages) > MaxPages:
		return fmt.Errorf("%w: %d pages", ErrTooLarge, len(s.pages))
	case s.files > MaxFiles:
		return fmt.Errorf("%w: %d files", ErrTooLarge, s.files)
	}
	return nil
}

// ErrTooLarge is a space past what one archive may hold.
var ErrTooLarge = errors.New("the space holds more than one archive may")
