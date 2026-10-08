// Package wikiread reads the space exports other wikis write, an HTML export
// and an XML export, into pages held to the allowlist; docs/wiki-import.md.
package wikiread

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Format is which kind of export a zip holds.
type Format string

const (
	// FormatHTML is a folder of one HTML file per page, an index page with
	// the page tree, and an attachments folder per page.
	FormatHTML Format = "html"
	// FormatXML is one XML document of objects, with the files beside it.
	FormatXML Format = "xml"
)

// Paths the formats are told apart by.
const (
	indexPath    = "index.html"
	entitiesPath = "entities.xml"
	attachDir    = "attachments/"
)

// Limits are what one export may hold; the caller's archive limits, so a
// space from elsewhere is held to what a space from Stator is.
type Limits struct {
	Pages    int
	Versions int
	Files    int
	// Unpacked bounds everything read from the zip together.
	Unpacked int64
}

// Per entry, past Limits: an HTML page and the XML document when unpacked.
const (
	MaxPageBytes     int64 = 8 << 20
	MaxEntitiesBytes int64 = 1 << 30
)

// MaxLosses bounds the losses a space lists; LostCount stays whole.
const MaxLosses = 200

// Space is an export read: its pages in the order they are made, home first,
// each after the page it hangs from, posts last.
type Space struct {
	Format Format
	// Name and Key are what the export calls the space; either may be empty.
	Name        string
	Key         string
	Description string
	Pages       []*Page
	// People are everybody a page, version, comment or file names, by Ref.
	People    []Person
	Losses    []Loss
	LostCount int
}

// Page is one page or post with everything that hangs off it.
type Page struct {
	ID     uuid.UUID
	Parent *uuid.UUID
	Post   bool
	Title  string
	// CreatedBy is a Person's Ref, empty when the export names nobody.
	CreatedBy string
	CreatedAt time.Time
	// Versions are oldest first; the last is the page as it reads now.
	Versions []Version
	Labels   []string
	Threads  []Thread
	// Files are every version of every file, each name's oldest first.
	Files []File
}

// Version is one version of a page, its document held to the allowlist.
type Version struct {
	Title   string
	Body    json.RawMessage
	Comment string
	By      string
	At      time.Time
}

// Thread is a comment below the page with its replies.
type Thread struct{ Comments []Comment }

// Comment is one comment, its document held to the comment allowlist.
type Comment struct {
	ID   uuid.UUID
	By   string
	At   time.Time
	Body json.RawMessage
}

// File is one version of a file on a page; Read gives its bytes.
type File struct {
	ID          uuid.UUID
	Name        string
	ContentType string
	Size        int64
	By          string
	At          time.Time
	entry       *zip.File
}

// Person is somebody the export names, found again by Email when it has one.
type Person struct {
	Ref   string
	Name  string
	Email string
}

// LossKind is what did not come across, which the importer reads as a sentence.
type LossKind string

const (
	// LossEmbed is a frame, video, script or form, left out.
	LossEmbed LossKind = "embed"
	// LossMacro is a block of the other wiki with nothing like it here; Detail names it.
	LossMacro LossKind = "macro"
	// LossExternalImage is a picture from another site, now a link to it.
	LossExternalImage LossKind = "externalImage"
	// LossMissingFile is a picture or file the export names and lacks.
	LossMissingFile LossKind = "missingFile"
	// LossOutsideLink is a link to a page the export lacks, now its words.
	LossOutsideLink LossKind = "outsideLink"
	// LossPlainText is a page or comment no document could hold, kept as its words.
	LossPlainText LossKind = "plainText"
	// LossLabel is a label Stator does not take: not a word, or past a page's number.
	LossLabel LossKind = "label"
	// LossUnreadPage is a page file that could not be read, left out.
	LossUnreadPage LossKind = "unreadPage"
	// LossFormat is content in a format older than the export's pages, kept as its words.
	LossFormat LossKind = "format"
)

// LossKinds is every kind, for the API's description.
var LossKinds = []LossKind{LossEmbed, LossMacro, LossExternalImage, LossMissingFile, LossOutsideLink, LossPlainText, LossLabel, LossUnreadPage, LossFormat}

// Loss is one thing that did not come across as it was, on the page named.
type Loss struct {
	Page   string   `json:"page"`
	Kind   LossKind `json:"kind"`
	Detail string   `json:"detail"`
}

// InvalidError refuses an export in a sentence that says what is wrong.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// TooLargeError refuses an export past Limits in a sentence.
type TooLargeError struct{ Message string }

func (e *TooLargeError) Error() string { return e.Message }

func tooLarge(format string, args ...any) error {
	return &TooLargeError{Message: fmt.Sprintf(format, args...)}
}

// Options are what reading an export needs from the import.
type Options struct {
	// Key is the new space's key, which links between its pages carry.
	Key    string
	Limits Limits
}

// Detect says which export a zip holds, or nothing when it is neither.
func Detect(zr *zip.Reader) Format {
	b := openBundle(zr, Limits{})
	switch {
	case b.files[entitiesPath] != nil:
		return FormatXML
	case b.files[indexPath] != nil:
		return FormatHTML
	}
	return ""
}

// Read reads an export of the format Detect found.
func Read(zr *zip.Reader, format Format, opts Options) (*Space, error) {
	b := openBundle(zr, opts.Limits)
	var (
		sp  *Space
		err error
	)
	switch format {
	case FormatHTML:
		sp, err = readHTML(b, opts)
	case FormatXML:
		sp, err = readXML(b, opts)
	default:
		return nil, invalid("This file is no space export Stator reads. Export the space as HTML or XML, then import the zip.")
	}
	if err != nil {
		return nil, err
	}
	sp.Format = format
	return sp, nil
}

// bundle is a zip's files by path, the one folder they may all sit in left
// out, read against Limits.Unpacked whatever the zip's directory claims.
type bundle struct {
	files    map[string]*zip.File
	names    []string
	limits   Limits
	unpacked int64
}

func openBundle(zr *zip.Reader, limits Limits) *bundle {
	b := &bundle{files: map[string]*zip.File{}, limits: limits}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasSuffix(name, "/") || name == "" {
			continue
		}
		names = append(names, name)
	}
	prefix := commonFolder(names)
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasSuffix(name, "/") || name == "" {
			continue
		}
		name = path.Clean(strings.TrimPrefix(name, prefix))
		if strings.HasPrefix(name, "../") || name == "." || name == ".." {
			continue
		}
		if _, dup := b.files[name]; dup {
			continue
		}
		b.files[name] = f
		b.names = append(b.names, name)
	}
	return b
}

// commonFolder is the one folder every file sits in, as a zip of a folder
// holds it, or nothing.
func commonFolder(names []string) string {
	if len(names) == 0 {
		return ""
	}
	first, _, found := strings.Cut(names[0], "/")
	if !found {
		return ""
	}
	prefix := first + "/"
	for _, n := range names[1:] {
		if !strings.HasPrefix(n, prefix) {
			return ""
		}
	}
	return prefix
}

// read unpacks one file, refusing more than limit bytes of it.
func (b *bundle) read(name string, limit int64) ([]byte, error) {
	f := b.files[name]
	if f == nil {
		return nil, invalid("The export lacks %s. Export the space again and import the new zip.", name)
	}
	return b.readFile(f, name, limit)
}

func (b *bundle) readFile(f *zip.File, name string, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, invalid("The export's %s could not be read. Export the space again and import the new zip.", name)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, invalid("The export's %s could not be read. Export the space again and import the new zip.", name)
	}
	if int64(len(data)) > limit {
		return nil, tooLarge("The export's %s unpacks into more than %d MB, more than one import takes. Split the space before you export it.", name, limit>>20)
	}
	if err := b.count(int64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

func (b *bundle) count(n int64) error {
	b.unpacked += n
	if b.limits.Unpacked > 0 && b.unpacked > b.limits.Unpacked {
		return tooLarge("The export unpacks into more than %d GB, more than one import takes. Split the space before you export it.", b.limits.Unpacked>>30)
	}
	return nil
}

// Read gives a file's bytes, held to the size the export said it has.
func (f File) Read() ([]byte, error) {
	rc, err := f.entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, f.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != f.Size {
		return nil, invalid("The file %q is not as large as the export says. Export the space again and import the new zip.", f.Name)
	}
	return data, nil
}

// losses collects what did not come across, the first MaxLosses of it.
type losses struct {
	list  []Loss
	count int
}

func (l *losses) add(page string, kind LossKind, detail string) {
	l.count++
	if len(l.list) < MaxLosses {
		if len([]rune(detail)) > maxDetail {
			detail = string([]rune(detail)[:maxDetail]) + "..."
		}
		l.list = append(l.list, Loss{Page: page, Kind: kind, Detail: detail})
	}
}

// maxDetail bounds what a loss quotes of the export.
const maxDetail = 200
