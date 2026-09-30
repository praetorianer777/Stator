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
       COALESCE(u.name, ''), a.created_at, a.object_key
FROM attachment a
LEFT JOIN app_user u ON u.id = a.uploaded_by`

// stored is a row with the one column a client is never shown.
type stored struct {
	Attachment
	objectKey string
}

func scan(row pgx.Row) (*stored, error) {
	var a stored
	err := row.Scan(&a.ID, &a.PageID, &a.FileName, &a.ContentType, &a.Size, &a.Width, &a.Height,
		&a.UploadedByName, &a.CreatedAt, &a.objectKey)
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

// List is the files on a page, the latest first.
func (s *Service) List(ctx context.Context, actor perm.Actor, pageID uuid.UUID) ([]Attachment, error) {
	out := []Attachment{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := page.Load(ctx, tx, actor, pageID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAttachment+` WHERE a.page_id = $1 ORDER BY a.created_at DESC, a.id DESC`, pageID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
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

	// Read whole, as Armature does, so the limit holds before anything is
	// written and the store is handed a body it can hash and rewind.
	data, err := io.ReadAll(io.LimitReader(in.Body, s.MaxSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > s.MaxSize {
		return nil, 0, &TooLargeError{Limit: s.MaxSize}
	}
	if len(data) == 0 {
		return nil, 0, ErrEmpty
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
			return &perm.DeniedError{Action: perm.EditPages}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		var key string
		if err := tx.QueryRow(ctx, `
			INSERT INTO attachment (id, org_id, page_id, uploaded_by, file_name, content_type, size_bytes, width, height)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $7, $8)
			RETURNING object_key`,
			id, pageID, actor.UserID, name, contentType, len(data), width, height).Scan(&key); err != nil {
			return fmt.Errorf("record the file: %w", err)
		}
		if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
			return err
		}
		found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
		if err != nil {
			return err
		}
		created = &found.Attachment
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// Open reads a file and its bytes. The caller closes the reader.
func (s *Service) Open(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Attachment, io.ReadCloser, error) {
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, _, err = find(ctx, tx, actor, id)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	body, err := s.store.Get(ctx, found.objectKey)
	if err != nil {
		return nil, nil, err
	}
	return &found.Attachment, body, nil
}

// Delete removes a file for good. The row goes in the transaction, which
// leaves a tombstone; the object goes after the commit, else the reaper takes it.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	var found *stored
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			p   *page.Page
			err error
		)
		found, p, err = find(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return &perm.DeniedError{Action: perm.EditPages}
		}
		// No row lock first: that takes UPDATE, which the app role does not
		// have. Of two deletes racing, the one that removes nothing lost.
		tag, err := tx.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	if err := s.reap(ctx, found.objectKey, s.db.Write); err != nil {
		s.log.Warn("attachment object left for the reaper", "key", found.objectKey, "error", err)
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
