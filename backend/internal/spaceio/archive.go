// Package spaceio moves whole spaces in and out, in the worker: an archive of a
// space, HTML pages to read offline, and a new space made from an archive.
package spaceio

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Format names an archive of a space, and Version its shape; an import reads
// every version up to its own and refuses a newer one.
const (
	Format  = "stator.space"
	Version = 1
)

// Paths inside an archive.
const (
	manifestPath = "manifest.json"
	pagesDir     = "pages/"
	filesDir     = "files/"
)

// Limits on what one archive may hold, past the bytes of the upload, so an
// archive that unpacks into far more than it weighs is refused before it is.
const (
	// MaxPages bounds the pages, posts and folders of one space.
	MaxPages = 5000
	// MaxVersions bounds the versions of all its pages together.
	MaxVersions = 100000
	// MaxFiles bounds the files, every version counted.
	MaxFiles = 20000
	// MaxJSONBytes bounds the manifest and each page's entry when unpacked.
	MaxJSONBytes int64 = 64 << 20
	// MaxUnpackedBytes bounds everything an archive unpacks into.
	MaxUnpackedBytes int64 = 8 << 30
	// DefaultMaxImportBytes is what an uploaded archive may weigh.
	DefaultMaxImportBytes int64 = 200 << 20
)

// Manifest is the archive's table of contents: the space, whom it names, who
// may do what in it, and its pages in the order an import makes them.
type Manifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	// ExportedBy is a person of People.
	ExportedBy string       `json:"exportedBy"`
	Space      ArchiveSpace `json:"space"`
	// People and Groups are everybody the archive names, by the identity an
	// import finds them by in another organization: an address, a name.
	People []Person `json:"people"`
	Groups []Group  `json:"groups"`
	Grants []Grant  `json:"grants"`
	// Pages lists every page's id, each after the page it hangs from.
	Pages     []uuid.UUID `json:"pages"`
	Templates []Template  `json:"templates"`
	Calendars []Calendar  `json:"calendars"`
	Counts    Counts      `json:"counts"`
}

// ArchiveSpace is the space itself.
type ArchiveSpace struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	HomePage    uuid.UUID `json:"homePage"`
}

// Person is somebody the archive names; Ref is how the rest of it names them.
type Person struct {
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Group is a group the archive names, found again by its name.
type Group struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// Subject is whom a grant or a restriction names: a person or group by
// ref, or everyone, or anybody without signing in.
type Subject struct {
	Type   string `json:"type"`
	Person string `json:"person,omitempty"`
	Group  string `json:"group,omitempty"`
}

// Grant is one permission of the space.
type Grant struct {
	Permission string  `json:"permission"`
	Subject    Subject `json:"subject"`
}

// Template is one of the space's own page templates.
type Template struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Title       string          `json:"title"`
	Body        json.RawMessage `json:"body"`
	Variables   json.RawMessage `json:"variables"`
}

// Calendar is one of the space's calendars with its events.
type Calendar struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Events []Event   `json:"events"`
}

// Event is one event or absence of a calendar.
type Event struct {
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`
	AllDay   bool      `json:"allDay"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
}

// Counts are what the archive holds, for the progress of its import.
type Counts struct {
	Pages    int `json:"pages"`
	Versions int `json:"versions"`
	Files    int `json:"files"`
	Comments int `json:"comments"`
}

// ArchivePage is one page, post or folder with everything that hangs off it,
// written to pages/{id}.json.
type ArchivePage struct {
	ID uuid.UUID `json:"id"`
	// Parent is null for the home page and for posts.
	Parent    *uuid.UUID `json:"parent"`
	Kind      string     `json:"kind"`
	Rank      string     `json:"rank"`
	Title     string     `json:"title"`
	Mode      string     `json:"mode"`
	Icon      *string    `json:"icon"`
	Width     string     `json:"width"`
	Cover     *Cover     `json:"cover"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	// PublishedAt is when the page was last published; PostedAt a post's date.
	PublishedAt *time.Time `json:"publishedAt"`
	PostedAt    *time.Time `json:"postedAt"`
	Archived    *Archived  `json:"archived"`
	// Body is the page as readers read it now, with its inline threads marked.
	Body         json.RawMessage `json:"body"`
	Versions     []PageVersion   `json:"versions"`
	Labels       []Label         `json:"labels"`
	Restrictions []Restriction   `json:"restrictions"`
	Files        []File          `json:"files"`
	Threads      []Thread        `json:"threads"`
	Reactions    []Reaction      `json:"reactions"`
}

// Cover is the picture above a page, one of its files.
type Cover struct {
	File   uuid.UUID `json:"file"`
	FocusX int       `json:"focusX"`
	FocusY int       `json:"focusY"`
}

// Archived says the page was archived, with the page it was archived with.
type Archived struct {
	At   time.Time `json:"at"`
	By   string    `json:"by"`
	Root uuid.UUID `json:"root"`
}

// PageVersion is one published version, its document as it was stored.
type PageVersion struct {
	Number       int             `json:"number"`
	Title        string          `json:"title"`
	Body         json.RawMessage `json:"body"`
	Comment      string          `json:"comment"`
	RestoredFrom *int            `json:"restoredFrom"`
	Live         bool            `json:"live"`
	CreatedBy    string          `json:"createdBy"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// Label is a word on the page.
type Label struct {
	Name      string    `json:"name"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// Restriction is one entry of a page's view, edit or grant list.
type Restriction struct {
	Kind    string  `json:"kind"`
	Subject Subject `json:"subject"`
}

// File is one version of a file on the page; its bytes are at files/{id}.
type File struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Version      int       `json:"version"`
	ContentType  string    `json:"contentType"`
	Size         int64     `json:"size"`
	Width        *int      `json:"width"`
	Height       *int      `json:"height"`
	RestoredFrom *int      `json:"restoredFrom"`
	EditedFrom   *int      `json:"editedFrom"`
	UploadedBy   string    `json:"uploadedBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Thread is a discussion below the page or on a passage of it.
type Thread struct {
	ID         uuid.UUID  `json:"id"`
	Kind       string     `json:"kind"`
	Quote      *string    `json:"quote"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	ResolvedBy string     `json:"resolvedBy,omitempty"`
	DetachedAt *time.Time `json:"detachedAt"`
	Comments   []Comment  `json:"comments"`
}

// Comment is one comment of a thread; a deleted one keeps its place, not its words.
type Comment struct {
	ID        uuid.UUID       `json:"id"`
	Author    string          `json:"author"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"createdAt"`
	EditedAt  *time.Time      `json:"editedAt"`
	DeletedAt *time.Time      `json:"deletedAt"`
	DeletedBy string          `json:"deletedBy,omitempty"`
}

// Reaction is an emoji on the page, or on one of its comments.
type Reaction struct {
	Comment   *uuid.UUID `json:"comment"`
	Person    string     `json:"person"`
	Emoji     string     `json:"emoji"`
	CreatedAt time.Time  `json:"createdAt"`
}

func pagePath(id uuid.UUID) string { return pagesDir + id.String() + ".json" }

func filePath(id uuid.UUID) string { return filesDir + id.String() }

// InvalidError refuses an archive in a sentence that says what is wrong.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// CheckManifest refuses a manifest that is not one of ours, or newer than
// this Stator reads, before anything is made of it.
func CheckManifest(m *Manifest) error {
	if m.Format != Format {
		return invalid("This file is not a space archive of Stator. Export the space from Stator as an archive, then import that file.")
	}
	if m.Version < 1 || m.Version > Version {
		return invalid("This archive is of version %d, which this Stator does not read yet (it reads up to version %d). Update Stator, or export the space from a Stator of the same version.", m.Version, Version)
	}
	switch {
	case len(m.Pages) == 0:
		return invalid("The archive lists no pages, so it holds no space. Export the space again and import the new file.")
	case len(m.Pages) > MaxPages:
		return invalid("The archive holds %d pages, more than the %d one import takes. Split the space before you export it.", len(m.Pages), MaxPages)
	case m.Counts.Versions > MaxVersions:
		return invalid("The archive holds %d versions, more than the %d one import takes. Split the space before you export it.", m.Counts.Versions, MaxVersions)
	case m.Counts.Files > MaxFiles:
		return invalid("The archive holds %d files, more than the %d one import takes. Split the space before you export it.", m.Counts.Files, MaxFiles)
	}
	return nil
}
