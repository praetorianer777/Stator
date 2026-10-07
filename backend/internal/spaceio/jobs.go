package spaceio

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// State is where an export or an import stands.
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
	// StateExpired is an export whose file was deleted after its time.
	StateExpired State = "expired"
)

// ExportStates and ImportStates are every state, for the API's description.
var (
	ExportStates = []State{StateQueued, StateRunning, StateDone, StateFailed, StateExpired}
	ImportStates = []State{StateQueued, StateRunning, StateDone, StateFailed}
)

// Open says the job is still to finish, so the page keeps asking.
func (s State) Open() bool { return s == StateQueued || s == StateRunning }

// ExportFormat is what an export writes: an archive to import again, or
// pages to read offline.
type ExportFormat string

const (
	FormatArchive ExportFormat = "archive"
	FormatHTML    ExportFormat = "html"
)

// Formats is every format, for the API's description.
var Formats = []ExportFormat{FormatArchive, FormatHTML}

// Failure is why an export or an import did not finish.
type Failure string

const (
	// FailureForbidden is a requester who may no longer do it.
	FailureForbidden Failure = "forbidden"
	// FailureKeyTaken is an import whose key another space took meanwhile.
	FailureKeyTaken Failure = "key_taken"
	// FailureInvalid is an archive Stator does not accept, with why.
	FailureInvalid Failure = "invalid"
	// FailureTooLarge is a space or an archive past what one may hold.
	FailureTooLarge Failure = "too_large"
	// FailureFailed is anything else: an error, or a run that took too long.
	FailureFailed Failure = "failed"
)

// ExportFailures and ImportFailures are every failure, for the API's description.
var (
	ExportFailures = []Failure{FailureForbidden, FailureTooLarge, FailureFailed}
	ImportFailures = []Failure{FailureForbidden, FailureKeyTaken, FailureInvalid, FailureTooLarge, FailureFailed}
)

func exportMessage(f Failure) string {
	switch f {
	case FailureForbidden:
		return "Only administrators of the space export it, and you no longer administer it. Ask one of its administrators to export it."
	case FailureTooLarge:
		return fmt.Sprintf("The space holds more than one archive may (%d pages, %d files). Move some of its pages to another space, then export each.", MaxPages, MaxFiles)
	}
	return "Exporting the space failed. Export it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

func importMessage(f Failure, detail string) string {
	switch f {
	case FailureForbidden:
		return "You may no longer create spaces, so the archive was not imported. Ask an administrator of the organization to let you, or to import it for you."
	case FailureKeyTaken:
		return "Another space took the key meanwhile. Import the archive again with another key."
	case FailureInvalid, FailureTooLarge:
		if detail != "" {
			return detail
		}
		return "The archive could not be read. Export the space again and import the new file."
	}
	return "Importing the space failed, and nothing of it was kept. Import it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

// Progress is how far a job has come, in pages and files.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// ExportJob is one export of a space, as its requester follows it.
type ExportJob struct {
	ID       uuid.UUID    `json:"id"`
	SpaceKey string       `json:"spaceKey"`
	Format   ExportFormat `json:"format"`
	State    State        `json:"state"`
	Progress Progress     `json:"progress"`
	// RequestedBy is who asked for it.
	RequestedBy string     `json:"requestedBy"`
	Mine        bool       `json:"mine"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	// FileName and Size describe the file once it is made; ExpiresAt is when
	// it is deleted.
	FileName  *string    `json:"fileName"`
	Size      *int64     `json:"size"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Failure   *Failure   `json:"failure"`
	// Message is the failure as a sentence that says what to do.
	Message *string `json:"message"`
	key     string
}

const selectExport = `
SELECT e.id, e.space_key, e.format, e.state, e.done_steps, e.total_steps, COALESCE(u.name, ''), e.requested_by = current_actor_id(),
       e.requested_at, e.started_at, e.finished_at, e.file_name, e.size_bytes, e.expires_at, e.failure, e.object_key
FROM space_export e LEFT JOIN app_user u ON u.id = e.requested_by`

func scanExport(row pgx.Row) (*ExportJob, error) {
	var j ExportJob
	if err := row.Scan(&j.ID, &j.SpaceKey, &j.Format, &j.State, &j.Progress.Done, &j.Progress.Total, &j.RequestedBy, &j.Mine,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &j.FileName, &j.Size, &j.ExpiresAt, &j.Failure, &j.key); err != nil {
		return nil, err
	}
	if j.Failure != nil {
		m := exportMessage(*j.Failure)
		j.Message = &m
	}
	return &j, nil
}

// ImportJob is one import of an archive, as its requester follows it.
type ImportJob struct {
	ID       uuid.UUID `json:"id"`
	Key      *string   `json:"key"`
	Name     *string   `json:"name"`
	State    State     `json:"state"`
	Progress Progress  `json:"progress"`
	// SpaceKey is the new space's key once it is made.
	SpaceKey    *string    `json:"spaceKey"`
	Report      *Report    `json:"report"`
	Failure     *Failure   `json:"failure"`
	Message     *string    `json:"message"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	// Written is past the worker's last write, for the requester's next read.
	Written db.LSN `json:"-"`
}

const selectImport = `
SELECT i.id, i.key, i.name, i.state, i.done_steps, i.total_steps, s.key, i.report, i.failure, COALESCE(i.detail, ''),
       i.requested_at, i.started_at, i.finished_at, COALESCE(i.written_lsn::text, '')
FROM space_import i LEFT JOIN space s ON s.id = i.space_id AND i.state = 'done'`

func scanImport(row pgx.Row) (*ImportJob, error) {
	var (
		j       ImportJob
		report  []byte
		detail  string
		written string
	)
	if err := row.Scan(&j.ID, &j.Key, &j.Name, &j.State, &j.Progress.Done, &j.Progress.Total, &j.SpaceKey, &report, &j.Failure, &detail,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if report != nil {
		j.Report = &Report{}
		if err := json.Unmarshal(report, j.Report); err != nil {
			return nil, err
		}
	}
	if j.Failure != nil {
		m := importMessage(*j.Failure, detail)
		j.Message = &m
	}
	if written != "" {
		lsn, err := db.ParseLSN(written)
		if err != nil {
			return nil, err
		}
		j.Written = lsn
	}
	return &j, nil
}

// Report is what an import could not bring across as it was.
type Report struct {
	Pages     int `json:"pages"`
	Versions  int `json:"versions"`
	Files     int `json:"files"`
	Comments  int `json:"comments"`
	Templates int `json:"templates"`
	Calendars int `json:"calendars"`
	// People and Groups are those the archive names and this organization
	// does not have: nobody of that address, no group of that name.
	People []MissingPerson `json:"people"`
	Groups []string        `json:"groups"`
	// Dropped are the permissions and restrictions that named them.
	Dropped []DroppedEntry `json:"dropped"`
	// Reattributed counts the versions, comments, files and labels of
	// somebody not found, now in the importer's name.
	Reattributed int `json:"reattributed"`
	// DroppedReactions counts the reactions of somebody not found.
	DroppedReactions int `json:"droppedReactions"`
	// MentionsAsText counts the mentions of somebody not found, kept as words.
	MentionsAsText int `json:"mentionsAsText"`
}

// MissingPerson is somebody the archive names who is not found here.
type MissingPerson struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// DroppedEntry is a permission of the space, or an entry of a page's list,
// that named somebody not found.
type DroppedEntry struct {
	// Page is the page's title for a restriction, empty for the space.
	Page       string `json:"page"`
	Permission string `json:"permission"`
	Subject    string `json:"subject"`
}

// maxReported bounds each list of a report; the counts stay whole.
const maxReported = 200

var (
	// ErrJobNotFound is an export or import the caller may not read, or none.
	ErrJobNotFound = errors.New("no such export or import")
	// ErrNotReady is an export whose file is not made yet, or failed.
	ErrNotReady = errors.New("the export is not ready")
	// ErrExpired is an export whose file was deleted after its time.
	ErrExpired = errors.New("the export expired")
)

// TooLargeError refuses an upload over the limit, naming it.
type TooLargeError struct{ Limit int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("This archive is over %s, the most one import takes. Split the space before you export it, or ask whoever runs Stator to raise STATOR_SPACE_IMPORT_LIMIT.", attachment.Size(e.Limit))
}

// Jobs queues exports and imports for the worker, and reads how they go, as
// whoever asks; the database holds them to who may.
type Jobs struct {
	db    *db.Cluster
	store objectstore.Store
	// MaxImportBytes is what an uploaded archive may weigh.
	MaxImportBytes int64
}

func NewJobs(cluster *db.Cluster, store objectstore.Store) *Jobs {
	return &Jobs{db: cluster, store: store, MaxImportBytes: DefaultMaxImportBytes}
}

// QueueExport asks the worker to export a space the caller administers, and
// records that it was asked for.
func (j *Jobs) QueueExport(ctx context.Context, actor perm.Actor, key string, format ExportFormat) (*ExportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	var out *ExportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		if err := perm.Check(ctx, tx, actor, perm.AdministerSpace, sp.ID); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO space_export (id, org_id, space_id, space_key, requested_by, format)
			VALUES ($1, current_org_id(), $2, $3, $4, $5)`, id, sp.ID, sp.Key, actor.UserID, string(format)); err != nil {
			return fmt.Errorf("queue the export: %w", err)
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionSpaceExported, "space", &sp.ID,
			map[string]any{"key": sp.Key, "name": sp.Name, "format": string(format), "export": id.String()}); err != nil {
			return err
		}
		out, err = scanExport(tx.QueryRow(ctx, selectExport+` WHERE e.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Exports are the latest exports of a space the caller may read: their own,
// and every one while they administer the space.
func (j *Jobs) Exports(ctx context.Context, actor perm.Actor, key string, limit int) ([]ExportJob, error) {
	out := []ExportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectExport+` WHERE e.space_id = $1 ORDER BY e.requested_at DESC, e.id DESC LIMIT $2`, sp.ID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanExport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}

// Download opens an export's file for whoever may read the export.
func (j *Jobs) Download(ctx context.Context, actor perm.Actor, id uuid.UUID) (*ExportJob, io.ReadCloser, error) {
	var found *ExportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanExport(tx.QueryRow(ctx, selectExport+` WHERE e.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	switch {
	case found.State == StateExpired, found.State == StateDone && found.ExpiresAt != nil && found.ExpiresAt.Before(time.Now()):
		return nil, nil, ErrExpired
	case found.State != StateDone:
		return nil, nil, ErrNotReady
	}
	body, err := j.store.Get(ctx, found.key)
	if errors.Is(err, objectstore.ErrNoObject) {
		return nil, nil, ErrExpired
	}
	return found, body, err
}

// ImportInput is what an import asks for besides its archive.
type ImportInput struct {
	// Key is the new space's key; empty takes the archive's.
	Key string
	// Name is the new space's name; empty takes the archive's.
	Name string
}

// QueueImport checks an uploaded archive is one of ours under a free key,
// stores it, and asks the worker to make a space of it.
func (j *Jobs) QueueImport(ctx context.Context, actor perm.Actor, in ImportInput, archive *os.File, size int64) (*ImportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	if size > j.MaxImportBytes {
		return nil, 0, &TooLargeError{Limit: j.MaxImportBytes}
	}
	var key, name *string
	if in.Key != "" {
		k := space.NormalizeKey(in.Key)
		if err := space.CheckKey(k); err != nil {
			return nil, 0, err
		}
		key = &k
	}
	if in.Name != "" {
		n, err := space.CleanName(in.Name)
		if err != nil {
			return nil, 0, err
		}
		name = &n
	}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return perm.Check(ctx, tx, actor, perm.CreateSpace, uuid.Nil)
	})
	if err != nil {
		return nil, 0, err
	}
	m, err := peekManifest(archive, size)
	if err != nil {
		return nil, 0, err
	}
	wanted := m.Space.Key
	if key != nil {
		wanted = *key
	}
	if err := j.keyFree(ctx, wanted); err != nil {
		return nil, 0, err
	}
	id := uuid.Must(uuid.NewV7())
	org, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, 0, tenant.ErrNoTenant
	}
	objectKey := importObjectKey(org.ID, id)
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	if err := j.store.Put(ctx, objectKey, archive, size, "application/zip"); err != nil {
		return nil, 0, err
	}
	var out *ImportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO space_import (id, org_id, requested_by, key, name, size_bytes)
			VALUES ($1, current_org_id(), $2, $3, $4, $5)`, id, actor.UserID, key, name, size); err != nil {
			return fmt.Errorf("queue the import: %w", err)
		}
		var err error
		out, err = scanImport(tx.QueryRow(ctx, selectImport+` WHERE i.id = $1`, id))
		return err
	})
	if err != nil {
		_ = j.store.Delete(context.WithoutCancel(ctx), objectKey)
	}
	return out, lsn, err
}

func importObjectKey(org, id uuid.UUID) string {
	return "org/" + org.String() + "/space-import/" + id.String()
}

// keyFree refuses a key another space of the organization has, whether or
// not the caller may see that space.
func (j *Jobs) keyFree(ctx context.Context, key string) error {
	var taken bool
	err := j.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT space_key_taken($1)`, key).Scan(&taken)
	})
	if err != nil {
		return err
	}
	if taken {
		return &space.FieldError{Field: "key", Message: fmt.Sprintf("The key %s is taken by another space. Choose another key for the imported space.", key)}
	}
	return nil
}

// peekManifest reads the manifest of an uploaded archive, so a file that is
// no archive of ours is refused while its uploader waits.
func peekManifest(f *os.File, size int64) (*Manifest, error) {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return nil, invalid("This file is no zip archive. Export the space from Stator as an archive, then import that file.")
	}
	return readManifest(zr)
}

// Import is one import the caller may read.
func (j *Jobs) Import(ctx context.Context, actor perm.Actor, id uuid.UUID) (*ImportJob, error) {
	var out *ImportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanImport(tx.QueryRow(ctx, selectImport+` WHERE i.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	})
	return out, err
}

// Imports are the caller's own latest imports.
func (j *Jobs) Imports(ctx context.Context, actor perm.Actor, limit int) ([]ImportJob, error) {
	out := []ImportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectImport+` WHERE i.requested_by = $1 ORDER BY i.requested_at DESC, i.id DESC LIMIT $2`, actor.UserID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanImport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}
