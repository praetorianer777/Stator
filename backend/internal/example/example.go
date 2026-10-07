package example

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/calendar"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Key is the key the example space asks for; KeyTries is how many keys,
// Key then Key2 and on, it tries before it gives up on a taken one.
const (
	Key      = "STATOR"
	KeyTries = 9
)

// Everyone is what every member may do in the example: read it and try
// comments and reactions, while its pages stay as they explain things.
var Everyone = []perm.SpacePermission{perm.SpaceView, perm.SpaceAddComments}

// Reaction is the emoji the example's comment page starts with.
const Reaction = "👋"

// The days the showcase's dates and the calendar's events fall on, from today.
const (
	soonDays     = 3
	nextWeekDays = 7
	absenceDays  = 2
	meetingHour  = 10
	meetingHours = 1
)

// Maker makes the example space through the services every space and page
// goes through; nil Attachments or Armature leave out what needs them.
type Maker struct {
	Spaces      *space.Service
	Pages       *page.Service
	Labels      *label.Service
	Calendars   *calendar.Service
	Comments    *comment.Service
	Reactions   *reaction.Service
	Attachments *attachment.Service
	Armature    *armature.Service
	Now         func() time.Time
	// Started hears of the space as soon as it exists, before its pages; an
	// error from it fails the making, which deletes the space again.
	Started func(context.Context, *space.Space) error
}

// ErrNotDiscarded says a failed making left its half made space behind.
var ErrNotDiscarded = errors.New("the half made example space is still there")

// CleanupLimit bounds deleting a half made example, which goes on after the
// context of the making ended: a making cut short is what leaves one.
const CleanupLimit = 2 * time.Minute

// ErrNoLanguage refuses a language the content is not written in.
var ErrNoLanguage = errors.New("the example space is written in English (en) and German (de); choose one of them")

// Language is the language the example is made in: the one asked for, else
// the person's own, else English.
func Language(asked, own string) (string, error) {
	switch {
	case asked != "":
		for _, l := range Languages {
			if asked == l {
				return l, nil
			}
		}
		return "", ErrNoLanguage
	case own == German:
		return German, nil
	}
	return English, nil
}

// Make finds the organization's example space or makes it, answering
// whether it made it. What fails part way is deleted again, with its files.
func (m *Maker) Make(ctx context.Context, actor perm.Actor, me Person, lang string) (*space.Space, bool, db.LSN, error) {
	existing, err := m.Spaces.Example(ctx, actor)
	if err != nil || existing != nil {
		return existing, false, 0, err
	}
	now := m.now()
	f := Facts{
		Lang: lang, Me: me,
		Today: day(now, 0), Soon: day(now, soonDays), NextWeek: day(now, nextWeekDays),
		Pages: map[string]uuid.UUID{}, Excerpts: map[string]uuid.UUID{}, Files: map[string]uuid.UUID{},
		Armature: m.armature(ctx),
	}
	sp, lsn, err := m.makeSpace(ctx, actor, &f)
	if errors.Is(err, space.ErrExampleExists) {
		existing, err := m.Spaces.Example(ctx, actor)
		return existing, false, 0, err
	}
	if err != nil {
		return nil, false, lsn, err
	}
	w := &writes{lsn: lsn}
	err = m.started(ctx, sp)
	if err == nil {
		err = m.fill(ctx, actor, sp, &f, w)
	}
	if err == nil {
		lsn, err = m.Spaces.ExampleMade(ctx, actor, sp.ID, lang)
		w.note(lsn)
	}
	if err != nil {
		if cleanup := m.Discard(ctx, actor, sp.ID); cleanup != nil {
			err = errors.Join(err, cleanup)
		}
		return nil, false, w.lsn, err
	}
	made, err := m.Spaces.Get(ctx, actor, sp.Key)
	return made, true, w.lsn, err
}

func (m *Maker) started(ctx context.Context, sp *space.Space) error {
	if m.Started == nil {
		return nil
	}
	return m.Started(ctx, sp)
}

// Discard deletes a half made example space with its files, also when ctx
// has ended, within CleanupLimit.
func (m *Maker) Discard(ctx context.Context, actor perm.Actor, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), CleanupLimit)
	defer cancel()
	if _, err := m.Spaces.DiscardExample(ctx, actor, id); err != nil {
		return fmt.Errorf("%w: %w", ErrNotDiscarded, err)
	}
	if m.Attachments != nil {
		_, _ = m.Attachments.Sweep(ctx)
	}
	return nil
}

func (m *Maker) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func day(now time.Time, after int) string {
	return now.UTC().AddDate(0, 0, after).Format(time.DateOnly)
}

// armature is the project and issue the maker sees first in Armature, nil
// when there is no Armature or nothing in it for them.
func (m *Maker) armature(ctx context.Context) *ArmatureFacts {
	if m.Armature == nil {
		return nil
	}
	status, projects, err := m.Armature.Projects(ctx)
	if err != nil || status != armature.StatusOK || len(projects) == 0 {
		return nil
	}
	out := &ArmatureFacts{Project: projects[0].Key}
	if status, found, err := m.Armature.Search(ctx, "project = "+out.Project, 1, 0); err == nil && status == armature.StatusOK && len(found.Issues) > 0 {
		out.Issue = found.Issues[0].Key
	}
	return out
}

// writes keeps the position past the latest write, for the caller's next read.
type writes struct{ lsn db.LSN }

func (w *writes) note(lsn db.LSN) { w.lsn = max(w.lsn, lsn) }

// makeSpace makes the space under the first of its keys nobody has taken.
func (m *Maker) makeSpace(ctx context.Context, actor perm.Actor, f *Facts) (*space.Space, db.LSN, error) {
	name, err := Title(f.Lang, Home)
	if err != nil {
		return nil, 0, err
	}
	for try := 1; try <= KeyTries; try++ {
		f.Key = Key
		if try > 1 {
			f.Key += strconv.Itoa(try)
		}
		home, _, err := Render(Home, *f)
		if err != nil {
			return nil, 0, err
		}
		sp, lsn, err := m.Spaces.CreateExample(ctx, actor, space.ExampleInput{
			Key: f.Key, Name: name, Description: description(f.Lang), Home: home.Body, Everyone: Everyone, Language: f.Lang,
		})
		var taken *space.FieldError
		if errors.As(err, &taken) && taken.Field == "key" {
			continue
		}
		return sp, lsn, err
	}
	return nil, 0, &space.FieldError{Field: "key", Message: fmt.Sprintf("The keys %s to %s%d are all taken by other spaces. Delete or rename one of them, then create the example space again.", Key, Key, KeyTries)}
}

func description(lang string) string {
	if lang == German {
		return "Seiten, die Stator erklären, und ein Beispiel für jeden Inhaltsblock."
	}
	return "Pages that explain Stator, and an example of every block a page can hold."
}

// fill makes the space's pages unpublished, then its files, calendar and
// posts, and publishes every page once all their ids are known.
func (m *Maker) fill(ctx context.Context, actor perm.Actor, sp *space.Space, f *Facts, w *writes) error {
	var order []Entry
	var place func(parent uuid.UUID, entries []Entry) error
	place = func(parent uuid.UUID, entries []Entry) error {
		for _, e := range entries {
			title, err := Title(f.Lang, e.Name)
			if err != nil {
				return err
			}
			made, lsn, err := m.Pages.Create(ctx, actor, page.CreateInput{Placement: page.Placement{ParentID: parent}, Title: title, Kind: e.Kind})
			w.note(lsn)
			if err != nil {
				return fmt.Errorf("make %s: %w", e.Name, err)
			}
			f.Pages[e.Name] = made.ID
			f.Excerpts[e.Name] = uuid.Must(uuid.NewV7())
			order = append(order, e)
			if err := place(made.ID, e.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := place(sp.HomePageID, Tree); err != nil {
		return err
	}
	if err := m.upload(ctx, actor, f, w); err != nil {
		return err
	}
	if err := m.calendar(ctx, actor, sp.Key, f, w); err != nil {
		return err
	}
	for _, name := range Posts {
		doc, _, err := Render(name, *f)
		if err != nil {
			return err
		}
		post, lsn, err := m.Pages.CreatePost(ctx, actor, sp.Key, page.PostInput{Title: doc.Title, Body: doc.Body, Publish: true})
		w.note(lsn)
		if err != nil {
			return fmt.Errorf("post %s: %w", name, err)
		}
		if err := m.label(ctx, actor, post.ID, doc.Labels, w); err != nil {
			return err
		}
	}
	for _, e := range order {
		if e.Kind == page.KindFolder {
			continue
		}
		if err := m.publish(ctx, actor, e.Name, *f, w); err != nil {
			return err
		}
	}
	return nil
}

func (m *Maker) publish(ctx context.Context, actor perm.Actor, name string, f Facts, w *writes) error {
	doc, _, err := Render(name, f)
	if err != nil {
		return err
	}
	id := f.Pages[name]
	_, lsn, err := m.Pages.Update(ctx, actor, id, page.UpdateInput{Title: &doc.Title, Body: doc.Body})
	w.note(lsn)
	if err != nil {
		return fmt.Errorf("publish %s: %w", name, err)
	}
	if err := m.label(ctx, actor, id, doc.Labels, w); err != nil {
		return err
	}
	if doc.Comment == "" {
		return nil
	}
	said, err := markdown.Convert([]byte(doc.Comment), nil)
	if err != nil {
		return err
	}
	thread, lsn, err := m.Comments.Start(ctx, actor, id, comment.ThreadInput{Body: said.Body})
	w.note(lsn)
	if err != nil {
		return fmt.Errorf("comment on %s: %w", name, err)
	}
	_, lsn, err = m.Reactions.ReactToComment(ctx, actor, thread.ID, Reaction)
	w.note(lsn)
	if err != nil {
		return fmt.Errorf("react on %s: %w", name, err)
	}
	_, lsn, err = m.Reactions.ReactToPage(ctx, actor, id, Reaction)
	w.note(lsn)
	return err
}

func (m *Maker) label(ctx context.Context, actor perm.Actor, id uuid.UUID, labels []string, w *writes) error {
	for _, l := range labels {
		_, lsn, err := m.Labels.Add(ctx, actor, id, l)
		w.note(lsn)
		if err != nil {
			return fmt.Errorf("label %s: %w", l, err)
		}
	}
	return nil
}

// upload puts the showcase's two pictures and its table of numbers, twice so
// it has versions, on the showcase; without file storage there are none.
func (m *Maker) upload(ctx context.Context, actor perm.Actor, f *Facts, w *writes) error {
	if m.Attachments == nil {
		return nil
	}
	picture, err := Picture()
	if err != nil {
		return err
	}
	board, err := Board()
	if err != nil {
		return err
	}
	for _, file := range []struct {
		name, contentType string
		data              []byte
	}{
		{ImageFile, "image/png", picture},
		{BoardFile, "image/png", board},
		{DataFile, "text/csv", numbers(f.Lang, false)},
		{DataFile, "text/csv", numbers(f.Lang, true)},
	} {
		made, lsn, err := m.Attachments.Upload(ctx, actor, f.Pages[Showcase], attachment.UploadInput{FileName: file.name, ContentType: file.contentType, Body: bytes.NewReader(file.data)})
		w.note(lsn)
		if err != nil {
			return fmt.Errorf("upload %s: %w", file.name, err)
		}
		f.Files[file.name] = made.ID
	}
	return nil
}

// calendar makes the space's calendar with a meeting, a day off and a
// release, around today, so the month it opens on has something in it.
func (m *Maker) calendar(ctx context.Context, actor perm.Actor, key string, f *Facts, w *writes) error {
	de := f.Lang == German
	pick := func(en, german string) string {
		if de {
			return german
		}
		return en
	}
	cal, lsn, err := m.Calendars.Create(ctx, actor, key, calendar.CalendarInput{Name: pick("Team calendar", "Teamkalender")})
	w.note(lsn)
	if err != nil {
		return fmt.Errorf("make the calendar: %w", err)
	}
	f.Calendar = cal.ID
	today := m.now().UTC().Truncate(24 * time.Hour)
	meeting := today.AddDate(0, 0, soonDays).Add(meetingHour * time.Hour)
	for _, e := range []calendar.CalendarEventInput{
		{Title: pick("Team meeting", "Teambesprechung"), Start: meeting, End: meeting.Add(meetingHours * time.Hour)},
		{Title: pick("Day off", "Freier Tag"), Kind: calendar.KindAbsence, AllDay: true, Start: today.AddDate(0, 0, nextWeekDays), End: today.AddDate(0, 0, nextWeekDays+absenceDays-1)},
		{Title: pick("Release day", "Veröffentlichungstag"), AllDay: true, Start: today, End: today},
	} {
		_, lsn, err := m.Calendars.CreateEvent(ctx, actor, cal.ID, e)
		w.note(lsn)
		if err != nil {
			return fmt.Errorf("add %q to the calendar: %w", e.Title, err)
		}
	}
	return nil
}

// numbers is the table the showcase's files block lists, the second version
// with one more month.
func numbers(lang string, later bool) []byte {
	head := "month,pages,comments\n"
	if lang == German {
		head = "Monat,Seiten,Kommentare\n"
	}
	rows := "2026-07,12,30\n2026-08,18,41\n"
	if later {
		rows += "2026-09,25,57\n"
	}
	return []byte(head + rows)
}
