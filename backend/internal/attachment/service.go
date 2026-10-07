package attachment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/convert"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service keeps the rows and hands the bytes to the store. Whether the actor
// may see or change a file is decided on its page.
type Service struct {
	db    *db.Cluster
	store objectstore.Store
	log   *slog.Logger
	// MaxSize is the largest file an upload takes.
	MaxSize int64
	// converter makes the previews of office documents; nil turns them off.
	converter convert.Converter
}

// NewService makes the service and has it told of every page copy, so the
// copy's files come along.
func NewService(cluster *db.Cluster, store objectstore.Store, pages *page.Service) *Service {
	s := &Service{db: cluster, store: store, log: slog.Default(), MaxSize: DefaultMaxSize}
	if pages != nil {
		pages.ObserveCopies(s)
	}
	return s
}

// WithMaxSize sets what an upload may weigh.
func (s *Service) WithMaxSize(limit int64) *Service {
	if limit > 0 {
		s.MaxSize = limit
	}
	return s
}

// WithLogger sets where objects left for the reaper are noted.
func (s *Service) WithLogger(log *slog.Logger) *Service {
	if log != nil {
		s.log = log
	}
	return s
}

const selectAttachment = `
SELECT a.id, a.page_id, a.file_name, a.content_type, a.size_bytes, a.width, a.height,
       COALESCE(u.name, ''), a.created_at, a.version,
       (SELECT count(*) FROM attachment v WHERE v.org_id = a.org_id AND v.page_id = a.page_id AND lower(v.file_name) = lower(a.file_name)),
       a.restored_from, a.edited_from, a.object_key,
       (SELECT max(v.version) FROM attachment v WHERE v.org_id = a.org_id AND v.page_id = a.page_id AND lower(v.file_name) = lower(a.file_name))
FROM attachment a
LEFT JOIN app_user u ON u.id = a.uploaded_by`

// stored is a row with what a client is never shown: where its bytes are,
// and the latest version of its name.
type stored struct {
	Attachment
	objectKey string
	latest    int
}

func scan(row pgx.Row) (*stored, error) {
	var a stored
	err := row.Scan(&a.ID, &a.PageID, &a.FileName, &a.ContentType, &a.Size, &a.Width, &a.Height,
		&a.UploadedByName, &a.CreatedAt, &a.Version, &a.Versions, &a.RestoredFrom, &a.EditedFrom, &a.objectKey, &a.latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// find reads a file and its page, checking the actor may see the page; one on
// a page they may not see, or one in the trash, is not found.
func find(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*stored, *page.Page, error) {
	found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
	if err != nil {
		return nil, nil, err
	}
	p, _, err := page.Load(ctx, tx, actor, found.PageID)
	if errors.Is(err, page.ErrNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return found, p, nil
}

// List is the files on a page, the latest first; current leaves out every
// version but the latest of each name.
func (s *Service) List(ctx context.Context, actor perm.Actor, pageID uuid.UUID, current bool) ([]Attachment, error) {
	out := []Attachment{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := page.Load(ctx, tx, actor, pageID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAttachment+` WHERE a.page_id = $1
			AND (NOT $2 OR NOT EXISTS (SELECT 1 FROM attachment n WHERE n.org_id = a.org_id AND n.page_id = a.page_id
			                           AND lower(n.file_name) = lower(a.file_name) AND n.version > a.version))
			ORDER BY a.created_at DESC, a.id DESC`, pageID, current)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
			s.describe(&a.Attachment)
			out = append(out, a.Attachment)
		}
		return rows.Err()
	})
	return out, err
}

// UploadInput is one file as it arrived.
type UploadInput struct {
	FileName    string
	ContentType string
	Body        io.Reader
}

// Upload stores a file on a page. The bytes go to the store inside the
// transaction that writes the row, so a store that refuses leaves no row.
func (s *Service) Upload(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in UploadInput) (*Attachment, db.LSN, error) {
	name := objectstore.CleanName(in.FileName)
	data, err := s.readWhole(in.Body)
	if err != nil {
		return nil, 0, err
	}
	contentType := contentTypeFor(in.ContentType, data)
	width, height := dimensions(contentType, data)

	var created *Attachment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := page.Load(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return p.Refusal(perm.EditPages)
		}
		created, err = s.add(ctx, tx, actor, pageID, version{name: name, contentType: contentType, data: data, width: width, height: height})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// readWhole reads an upload whole, as Armature does, so the limit holds
// before anything is written and the store is handed a body it can rewind.
func (s *Service) readWhole(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, s.MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > s.MaxSize {
		return nil, &TooLargeError{Limit: s.MaxSize}
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	return data, nil
}

// version is a file to put on a page: an upload, a restored version, or an
// edited picture, which under a name the page has is that name's next version.
type version struct {
	name, contentType string
	data              []byte
	width, height     *int
	// restoredFrom is the earlier version of the name whose bytes these are.
	restoredFrom *int
	// editedFrom is the version of the name these bytes were drawn on.
	editedFrom *int
}

// add writes the row and then the bytes, inside the caller's transaction,
// which has checked the actor may edit the page.
func (s *Service) add(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID, v version) (*Attachment, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var key string
	if err := tx.QueryRow(ctx, `
		INSERT INTO attachment (id, org_id, page_id, uploaded_by, file_name, content_type, size_bytes, width, height, restored_from, edited_from)
		VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING object_key`,
		id, pageID, actor.UserID, v.name, v.contentType, len(v.data), v.width, v.height, v.restoredFrom, v.editedFrom).Scan(&key); err != nil {
		return nil, fmt.Errorf("record the file: %w", err)
	}
	if err := s.store.Put(ctx, key, bytes.NewReader(v.data), int64(len(v.data)), v.contentType); err != nil {
		return nil, err
	}
	found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
	if err != nil {
		return nil, err
	}
	s.describe(&found.Attachment)
	return &found.Attachment, nil
}

// AlreadyLatestError refuses to restore the version that is already the latest.
type AlreadyLatestError struct {
	Name    string
	Version int
}

func (e *AlreadyLatestError) Error() string {
	return fmt.Sprintf("version %d of %s is already the latest; restore an earlier version instead", e.Version, e.Name)
}

// Restore brings an earlier version of a file back as its name's next
// version, with that version's bytes; the history keeps every version.
func (s *Service) Restore(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Attachment, db.LSN, error) {
	var restored *Attachment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, p, err := find(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return p.Refusal(perm.EditPages)
		}
		if found.Version == found.latest {
			return &AlreadyLatestError{Name: found.FileName, Version: found.Version}
		}
		body, err := s.store.Get(ctx, found.objectKey)
		if err != nil {
			return err
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("read version %d of %s: %w", found.Version, found.FileName, err)
		}
		from := found.Version
		restored, err = s.add(ctx, tx, actor, found.PageID, version{
			name: found.FileName, contentType: found.ContentType, data: data,
			width: found.Width, height: found.Height, restoredFrom: &from,
		})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return restored, lsn, nil
}

// Located is a file its reader may read, found but not fetched yet, so the
// caller can fetch all of it or the stretch a player asked for.
type Located struct {
	Attachment
	key   string
	store objectstore.Store
}

// Bytes fetches the whole file. The caller closes the reader.
func (l *Located) Bytes(ctx context.Context) (io.ReadCloser, error) {
	return l.store.Get(ctx, l.key)
}

// Range fetches length bytes from offset on, which must lie inside the file.
func (l *Located) Range(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length <= 0 || offset+length > l.Size {
		return nil, fmt.Errorf("bytes %d to %d lie outside the file's %d", offset, offset+length, l.Size)
	}
	return l.store.GetRange(ctx, l.key, offset, length)
}

func (s *Service) located(found *stored) *Located {
	s.describe(&found.Attachment)
	return &Located{Attachment: found.Attachment, key: found.objectKey, store: s.store}
}

// Open reads a file and its bytes. The caller closes the reader.
func (s *Service) Open(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Attachment, io.ReadCloser, error) {
	found, err := s.Locate(ctx, actor, id)
	if err != nil {
		return nil, nil, err
	}
	body, err := found.Bytes(ctx)
	if err != nil {
		return nil, nil, err
	}
	return &found.Attachment, body, nil
}

// Locate finds a file the actor may read, on a page they may view.
func (s *Service) Locate(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Located, error) {
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, _, err = find(ctx, tx, actor, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.located(found), nil
}

// LocateAnonymous finds a file for somebody who is not signed in: one on a
// published page anybody may read, else none.
func (s *Service) LocateAnonymous(ctx context.Context, id uuid.UUID) (*Located, error) {
	if !db.AnonymousFrom(ctx) {
		return nil, errors.New("a public file is read as an anonymous reader")
	}
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+`
			JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
			WHERE a.id = $1 AND p.trashed_at IS NULL AND p.version > 0 AND perm_page_viewable(p.id, NULL::uuid)`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.located(found), nil
}

// LocateLinked finds a file for a reader holding a public link: one of the
// page the link opens, else none, whatever else is open.
func (s *Service) LocateLinked(ctx context.Context, id uuid.UUID) (*Located, error) {
	if !db.LinkFrom(ctx) {
		return nil, errors.New("a file is read through a link by a reader holding one")
	}
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+`
			JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
			WHERE a.id = $1 AND p.id = perm_link_page() AND p.trashed_at IS NULL AND p.version > 0 AND perm_page_viewable(p.id, NULL::uuid)`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.located(found), nil
}

// Delete removes a version of a file for good, or with every set all the
// versions of its name. The rows go in the transaction, which leaves
// tombstones; the objects go after the commit, else the reaper takes them.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, id uuid.UUID, every bool) (db.LSN, error) {
	var keys []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, p, err := find(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return p.Refusal(perm.EditPages)
		}
		// The previews go with their files, by the cascade; their bytes go
		// with the files' below.
		rows, err := tx.Query(ctx, `
			SELECT a.object_key, p.object_key FROM attachment a
			LEFT JOIN attachment_preview p ON p.attachment_id = a.id AND p.state = 'ready'
			WHERE (a.id = $1 OR ($4 AND a.page_id = $2 AND lower(a.file_name) = lower($3)))`, id, found.PageID, found.FileName, every)
		if err != nil {
			return err
		}
		for rows.Next() {
			var file string
			var preview *string
			if err := rows.Scan(&file, &preview); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, file)
			if preview != nil {
				keys = append(keys, *preview)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// No row lock first: that takes UPDATE, which the app role does not
		// have. Of two deletes racing, the one that removes nothing lost.
		tag, err := tx.Exec(ctx, `DELETE FROM attachment a WHERE (a.id = $1 OR ($4 AND a.page_id = $2 AND lower(a.file_name) = lower($3)))`, id, found.PageID, found.FileName, every)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, key := range keys {
		if err := s.reap(ctx, key, s.db.Write); err != nil {
			s.log.Warn("attachment object left for the reaper", "key", key, "error", err)
		}
	}
	return lsn, nil
}

// contentTypeFor trusts a specific type the client sent and sniffs the bytes
// otherwise, so a browser that says nothing still gets a sensible download.
func contentTypeFor(declared string, data []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" && !strings.ContainsAny(declared, "\r\n") {
		if _, _, err := mime.ParseMediaType(declared); err == nil {
			return declared
		}
	}
	return http.DetectContentType(data)
}
