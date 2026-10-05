package attachment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/convert"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// PreviewKind says how a file is shown in place: a PDF as it is, an office
// document converted to PDF, anything else not at all.
type PreviewKind string

const (
	PreviewNone   PreviewKind = "none"
	PreviewPDF    PreviewKind = "pdf"
	PreviewOffice PreviewKind = "office"
)

// PreviewKinds is every kind, for the API's description.
var PreviewKinds = []PreviewKind{PreviewNone, PreviewPDF, PreviewOffice}

const (
	// MaxConvertSize is the largest office document converted for a preview;
	// a larger one takes the converter longer than a reader waits.
	MaxConvertSize int64 = 20 << 20
	// MaxPreviewSize is the largest PDF a conversion may make and be kept.
	MaxPreviewSize int64 = 50 << 20
)

const pdfType = "application/pdf"

// foreignKeyViolation is the SQLSTATE of a row naming one that is gone.
const foreignKeyViolation = "23503"

// officeTypes are the documents the office suite converts, by extension,
// with the media type a browser sends for each.
var officeTypes = map[string]string{
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"odt":  "application/vnd.oasis.opendocument.text",
	"ods":  "application/vnd.oasis.opendocument.spreadsheet",
	"odp":  "application/vnd.oasis.opendocument.presentation",
	"doc":  "application/msword",
	"xls":  "application/vnd.ms-excel",
	"ppt":  "application/vnd.ms-powerpoint",
}

var (
	// ErrNoPreview is a file of a kind that is not shown in place.
	ErrNoPreview = errors.New("this kind of file has no preview")
	// ErrPreviewOff is an office document on a site with no converter.
	ErrPreviewOff = errors.New("previews of office documents are turned off")
	// ErrPreviewTooLarge is an office document over MaxConvertSize.
	ErrPreviewTooLarge = fmt.Errorf("this file is larger than the %s a preview converts", Size(MaxConvertSize))
	// ErrPreviewFailed is a document the converter could not turn into a PDF,
	// which is remembered, so it is not tried again.
	ErrPreviewFailed = errors.New("this file could not be converted for a preview")
	// ErrConverterUnavailable is a converter that did not answer; it is not
	// remembered, so the next reader tries again.
	ErrConverterUnavailable = errors.New("the conversion service is not available")
)

// officeExtension is the extension the converter is told a document has:
// its name's, else the one its media type stands for; empty for anything else.
func officeExtension(fileName, contentType string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(fileName), "."))
	if _, ok := officeTypes[ext]; ok {
		return ext
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	for ext, known := range officeTypes {
		if known == mediaType {
			return ext
		}
	}
	return ""
}

// previewKind is how a file would be shown, whatever the site can convert.
func previewKind(fileName, contentType string) PreviewKind {
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil && mediaType == pdfType {
		return PreviewPDF
	}
	if officeExtension(fileName, contentType) != "" {
		return PreviewOffice
	}
	return PreviewNone
}

// pdfName is what a document's preview is called: its name, ending in .pdf.
func pdfName(fileName string) string {
	return strings.TrimSuffix(fileName, path.Ext(fileName)) + ".pdf"
}

// WithConverter has office documents converted for previews; without one
// they have none.
func (s *Service) WithConverter(c convert.Converter) *Service {
	s.converter = c
	return s
}

// describe says how this site shows the file: an office document only when
// there is a converter and the document is small enough for it.
func (s *Service) describe(a *Attachment) {
	a.Preview = previewKind(a.FileName, a.ContentType)
	if a.Preview == PreviewOffice && (s.converter == nil || a.Size > MaxConvertSize) {
		a.Preview = PreviewNone
	}
}

// PreviewFile is a file's preview as a PDF. The caller closes Body.
type PreviewFile struct {
	Name string
	Size int64
	Body io.ReadCloser
}

// Preview is a file as a PDF to show in place: a PDF itself, or an office
// document converted by the first reader who asks and kept for the rest.
func (s *Service) Preview(ctx context.Context, actor perm.Actor, id uuid.UUID) (*PreviewFile, db.LSN, error) {
	var (
		found *stored
		kept  keptPreview
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if found, _, err = find(ctx, tx, actor, id); err != nil {
			return err
		}
		kept, err = readKept(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, 0, err
	}

	switch previewKind(found.FileName, found.ContentType) {
	case PreviewPDF:
		body, err := s.store.Get(ctx, found.objectKey)
		if err != nil {
			return nil, 0, err
		}
		return &PreviewFile{Name: found.FileName, Size: found.Size, Body: body}, 0, nil
	case PreviewOffice:
	default:
		return nil, 0, ErrNoPreview
	}

	if kept.state == "" {
		// A replica may not have the conversion another reader just kept;
		// asking the primary is far cheaper than converting again.
		err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
			var err error
			kept, err = readKept(ctx, tx, id)
			return err
		})
		if err != nil {
			return nil, 0, err
		}
	}
	name := pdfName(found.FileName)
	switch kept.state {
	case "ready":
		body, err := s.store.Get(ctx, kept.key)
		if err != nil {
			return nil, 0, err
		}
		return &PreviewFile{Name: name, Size: kept.size, Body: body}, 0, nil
	case "failed":
		return nil, 0, ErrPreviewFailed
	}
	if s.converter == nil {
		return nil, 0, ErrPreviewOff
	}
	if found.Size > MaxConvertSize {
		return nil, 0, ErrPreviewTooLarge
	}

	source, err := s.store.Get(ctx, found.objectKey)
	if err != nil {
		return nil, 0, err
	}
	data, err := io.ReadAll(io.LimitReader(source, MaxConvertSize+1))
	_ = source.Close()
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", found.objectKey, err)
	}
	// The converter learns the kind of document from its name alone, and
	// needs nothing else of it.
	pdf, err := s.converter.ToPDF(ctx, "document."+officeExtension(found.FileName, found.ContentType), data)
	switch {
	case errors.Is(err, convert.ErrFailed), errors.Is(err, convert.ErrTooLarge):
		s.log.Info("attachment could not be converted for a preview", "attachment", id, "error", err)
		lsn, err := s.keepPreview(ctx, id, nil)
		if err != nil {
			return nil, lsn, err
		}
		return nil, lsn, ErrPreviewFailed
	case err != nil:
		s.log.Warn("conversion service failed", "attachment", id, "error", err)
		return nil, 0, fmt.Errorf("%w: %v", ErrConverterUnavailable, err)
	}
	lsn, err := s.keepPreview(ctx, id, pdf)
	if err != nil {
		return nil, lsn, err
	}
	return &PreviewFile{Name: name, Size: int64(len(pdf)), Body: io.NopCloser(bytes.NewReader(pdf))}, lsn, nil
}

// keptPreview is what is recorded of a file's conversion; no state is none yet.
type keptPreview struct {
	state, key string
	size       int64
}

func readKept(ctx context.Context, tx db.DBTX, id uuid.UUID) (keptPreview, error) {
	var k keptPreview
	err := tx.QueryRow(ctx, `SELECT state, COALESCE(size_bytes, 0), object_key FROM attachment_preview WHERE attachment_id = $1`, id).
		Scan(&k.state, &k.size, &k.key)
	if errors.Is(err, pgx.ErrNoRows) {
		return keptPreview{}, nil
	}
	return k, err
}

// keepPreview notes a conversion, its PDF going to the bucket in the same
// transaction; a nil PDF notes a failure. Of two readers converting at once,
// the first to commit keeps theirs and the other's is let go.
func (s *Service) keepPreview(ctx context.Context, id uuid.UUID, pdf []byte) (db.LSN, error) {
	state, size := "failed", (*int64)(nil)
	if pdf != nil {
		n := int64(len(pdf))
		state, size = "ready", &n
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var key string
		err := tx.QueryRow(ctx, `
			INSERT INTO attachment_preview (attachment_id, org_id, state, size_bytes)
			VALUES ($1, current_org_id(), $2, $3)
			ON CONFLICT (attachment_id) DO NOTHING
			RETURNING object_key`, id, state, size).Scan(&key)
		var pgErr *pgconn.PgError
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation:
			// The file was deleted while it was being converted.
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("record the preview: %w", err)
		case pdf == nil:
			return nil
		}
		return s.store.Put(ctx, key, bytes.NewReader(pdf), int64(len(pdf)), pdfType)
	})
}
