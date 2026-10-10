// Package docx writes a page as a Word document, Office Open XML made from
// the page's document tree with its pictures inside; docs/word.md lists how each block comes out.
package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// ContentType is what a Word document is served as.
const ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// MaxBytes bounds the pictures one document carries, as a PDF is bounded:
// a file this large is no longer one to mail or open offline.
const MaxBytes int64 = 50 << 20

// ErrTooLarge refuses a page whose pictures would make the file larger than MaxBytes.
var ErrTooLarge = errors.New("the page's pictures make the Word file larger than this server hands out")

// Page is the published page and the facts its properties and first lines
// carry; Author and Editor stay empty for a reader told nothing of who wrote it.
type Page struct {
	ID       uuid.UUID
	Title    string
	Space    string
	Author   string
	Editor   string
	Created  time.Time
	Modified time.Time
	Version  int
	// Language is "en" or "de": the words the document adds, its dates and
	// the language Word checks its spelling in.
	Language string
	// URL is where the page is read, or empty where the reader has no address to keep.
	URL  string
	Body document.Node
	// Brand is the organization's name, logo, footer line and colour; nil adds nothing.
	Brand *Brand
}

// Included is what an include shows its reader: a page's published body or one excerpt.
type Included struct {
	Title string
	URL   string
	Body  document.Node
}

// Reader reads what the page's blocks point at, as the person exporting it
// may. A nil function leaves what it would read out, said in a sentence.
type Reader struct {
	// Picture opens a file's bytes; nil and no error is a file the reader may not open.
	Picture func(ctx context.Context, id uuid.UUID) (io.ReadCloser, error)
	// Include reads what an include shows; via is the chain of pages it sits
	// in, outermost first. Nil and no error is a page the reader may not read.
	Include func(ctx context.Context, pageID uuid.UUID, excerptID string, via []uuid.UUID) (*Included, error)
	// IncludeURL is where an include's page is read when Include is nil.
	IncludeURL func(pageID uuid.UUID) string
	// FileURL is where a file named in the text is downloaded.
	FileURL func(id uuid.UUID) string
	// BaseURL is the web client's address, which links within Stator are read against.
	BaseURL string
}

// Write writes the page as a .docx.
func Write(ctx context.Context, out io.Writer, p Page, r Reader) error {
	w := newWriter(ctx, p, r)
	body, err := w.document(p)
	branded := w.branded
	if err != nil {
		return err
	}
	if branded != nil && branded.logo != nil {
		w.media = append(w.media, *branded.logo)
	}
	zw := zip.NewWriter(out)
	modified := p.Modified
	if modified.IsZero() {
		modified = time.Now()
	}
	part := func(name string, data []byte) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	parts := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", contentTypes(w.media, branded != nil)},
		{"_rels/.rels", []byte(packageRels)},
		{"docProps/core.xml", coreProperties(p)},
		{"docProps/app.xml", []byte(appProperties)},
		{"word/document.xml", body},
		{"word/styles.xml", styles(p.Language, w.accent, p.Brand != nil)},
		{"word/numbering.xml", w.numbering()},
		{"word/settings.xml", []byte(settings)},
		{"word/_rels/document.xml.rels", w.relationships()},
	}
	if branded != nil {
		parts = append(parts,
			struct {
				name string
				data []byte
			}{"word/" + headerPart, branded.header},
			struct {
				name string
				data []byte
			}{"word/" + footerPart, branded.footer})
		if branded.logo != nil {
			parts = append(parts, struct {
				name string
				data []byte
			}{"word/_rels/" + headerPart + ".rels", branded.headerRels})
		}
	}
	for _, m := range w.media {
		parts = append(parts, struct {
			name string
			data []byte
		}{"word/" + m.target, m.data})
	}
	for _, f := range parts {
		if err := part(f.name, f.data); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return zw.Close()
}

// Bytes is Write into memory, so a failure is known before anything is sent.
func Bytes(ctx context.Context, p Page, r Reader) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(ctx, &buf, p, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileName names a document after its space, its page and the day, as a
// PDF of it is named.
func FileName(spaceKey, title string, day time.Time) string {
	name := document.Slug(title) + "-" + day.Format("2006-01-02") + ".docx"
	if spaceKey == "" {
		return name
	}
	return spaceKey + "-" + name
}

// language is the document's language as Word names it.
func language(lang string) string {
	if strings.EqualFold(lang, "de") {
		return "de-DE"
	}
	return "en-US"
}
