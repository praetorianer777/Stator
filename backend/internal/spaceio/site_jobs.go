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
)

// FailureNotEmpty is an organization import into an organization that has
// spaces already.
const FailureNotEmpty Failure = "not_empty"

// SiteExportFailures and SiteImportFailures are every failure, for the API's description.
var (
	SiteExportFailures = []Failure{FailureForbidden, FailureTooLarge, FailureFailed}
	SiteImportFailures = []Failure{FailureForbidden, FailureNotEmpty, FailureInvalid, FailureTooLarge, FailureFailed}
)

func siteExportMessage(f Failure, detail string) string {
	switch f {
	case FailureForbidden:
		return "Only administrators of the organization export it, and you no longer are one. Ask one of them to export it."
	case FailureTooLarge:
		if detail != "" {
			return detail
		}
		return fmt.Sprintf("A space of the organization holds more than one archive may (%d pages, %d files). Move some of its pages to another space, then export the organization again.", MaxPages, MaxFiles)
	}
	return "Exporting the organization failed. Export it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

func siteImportMessage(f Failure, detail string) string {
	switch f {
	case FailureForbidden:
		return "Only administrators of the organization import an archive into it, and you no longer are one. Ask one of them to import it."
	case FailureNotEmpty:
		return ErrSiteNotEmpty.Error()
	case FailureInvalid, FailureTooLarge:
		if detail != "" {
			return detail
		}
		return "The archive could not be read. Export the organization again and import the new file."
	}
	return "Importing the organization failed, and nothing of it was kept. Import it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

// SiteNotEmptyError refuses an organization import into an organization with
// spaces, in a sentence.
type SiteNotEmptyError struct{}

func (SiteNotEmptyError) Error() string {
	return "This organization has spaces already, and an organization's archive is imported only into one without any. Import it into a fresh Stator, or delete every space here first."
}

// ErrSiteNotEmpty is the one SiteNotEmptyError.
var ErrSiteNotEmpty error = SiteNotEmptyError{}

// SiteExportJob is one export of the organization, as its administrators follow it.
type SiteExportJob struct {
	ID          uuid.UUID  `json:"id"`
	State       State      `json:"state"`
	Progress    Progress   `json:"progress"`
	RequestedBy string     `json:"requestedBy"`
	Mine        bool       `json:"mine"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	FileName    *string    `json:"fileName"`
	Size        *int64     `json:"size"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	Failure     *Failure   `json:"failure"`
	Message     *string    `json:"message"`
	key         string
}

const selectSiteExport = `
SELECT e.id, e.state, e.done_steps, e.total_steps, COALESCE(u.name, ''), e.requested_by = current_actor_id(),
       e.requested_at, e.started_at, e.finished_at, e.file_name, e.size_bytes, e.expires_at, e.failure, COALESCE(e.detail, ''), e.object_key
FROM site_export e LEFT JOIN app_user u ON u.id = e.requested_by`

func scanSiteExport(row pgx.Row) (*SiteExportJob, error) {
	var (
		j      SiteExportJob
		detail string
	)
	if err := row.Scan(&j.ID, &j.State, &j.Progress.Done, &j.Progress.Total, &j.RequestedBy, &j.Mine,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &j.FileName, &j.Size, &j.ExpiresAt, &j.Failure, &detail, &j.key); err != nil {
		return nil, err
	}
	if j.Failure != nil {
		m := siteExportMessage(*j.Failure, detail)
		j.Message = &m
	}
	return &j, nil
}

// SiteImportJob is one import of an organization's archive.
type SiteImportJob struct {
	ID          uuid.UUID   `json:"id"`
	State       State       `json:"state"`
	Progress    Progress    `json:"progress"`
	RequestedBy string      `json:"requestedBy"`
	Report      *SiteReport `json:"report"`
	Failure     *Failure    `json:"failure"`
	Message     *string     `json:"message"`
	RequestedAt time.Time   `json:"requestedAt"`
	StartedAt   *time.Time  `json:"startedAt"`
	FinishedAt  *time.Time  `json:"finishedAt"`
	Written     db.LSN      `json:"-"`
}

const selectSiteImport = `
SELECT i.id, i.state, i.done_steps, i.total_steps, COALESCE(u.name, ''), i.report, i.failure, COALESCE(i.detail, ''),
       i.requested_at, i.started_at, i.finished_at, COALESCE(i.written_lsn::text, '')
FROM site_import i LEFT JOIN app_user u ON u.id = i.requested_by`

func scanSiteImport(row pgx.Row) (*SiteImportJob, error) {
	var (
		j       SiteImportJob
		report  []byte
		detail  string
		written string
	)
	if err := row.Scan(&j.ID, &j.State, &j.Progress.Done, &j.Progress.Total, &j.RequestedBy, &report, &j.Failure, &detail,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if report != nil {
		j.Report = &SiteReport{}
		if err := json.Unmarshal(report, j.Report); err != nil {
			return nil, err
		}
		j.Report.settle()
	}
	if j.Failure != nil {
		m := siteImportMessage(*j.Failure, detail)
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

// SiteReport is what an organization's import brought across, and what not.
type SiteReport struct {
	Spaces    int `json:"spaces"`
	Pages     int `json:"pages"`
	Versions  int `json:"versions"`
	Files     int `json:"files"`
	Comments  int `json:"comments"`
	Drafts    int `json:"drafts"`
	Templates int `json:"templates"`
	Themes    int `json:"themes"`
	Webhooks  int `json:"webhooks"`
	// People counts everybody let in; Matched those who had an account here
	// by their address already.
	People       int `json:"people"`
	Matched      int `json:"matched"`
	Groups       int `json:"groups"`
	AuditEntries int `json:"auditEntries"`
	// Kept names what this organization had of its own and kept, such as its
	// sign-in provider, in place of the archive's.
	Kept []string `json:"kept"`
	// Dropped are the permissions and restrictions whose subject is not here.
	Dropped []DroppedEntry `json:"dropped"`
	// Reattributed and DroppedReactions count the work and reactions of
	// people the archive names but not as members.
	Reattributed     int `json:"reattributed"`
	DroppedReactions int `json:"droppedReactions"`
	// NotCarried is what the archive says it left out.
	NotCarried []Omission `json:"notCarried"`
}

func (r *SiteReport) settle() {
	if r.Kept == nil {
		r.Kept = []string{}
	}
	if r.Dropped == nil {
		r.Dropped = []DroppedEntry{}
	}
	if r.NotCarried == nil {
		r.NotCarried = []Omission{}
	}
}

// siteDownloadName is the name an organization's archive downloads as.
func siteDownloadName(slug string, at time.Time) string {
	return fmt.Sprintf("%s-organization-%s.zip", slug, at.UTC().Format("2006-01-02"))
}

// SiteTooLargeError refuses an organization archive over the limit, naming it.
type SiteTooLargeError struct{ Limit int64 }

func (e *SiteTooLargeError) Error() string {
	return fmt.Sprintf("This archive is over %s, the most one import takes. Ask whoever runs Stator to raise STATOR_ORG_IMPORT_LIMIT.", attachment.Size(e.Limit))
}

// QueueSiteExport asks the worker to export the caller's organization.
func (j *Jobs) QueueSiteExport(ctx context.Context, actor perm.Actor) (*SiteExportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	var out *SiteExportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.MoveSite, uuid.Nil); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO site_export (id, requested_by) VALUES ($1, $2)`, id, actor.UserID); err != nil {
			return fmt.Errorf("queue the export: %w", err)
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgExported, "org", nil, map[string]any{"export": id.String()}); err != nil {
			return err
		}
		var err error
		out, err = scanSiteExport(tx.QueryRow(ctx, selectSiteExport+` WHERE e.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// SiteExports are the organization's latest exports, for its administrators.
func (j *Jobs) SiteExports(ctx context.Context, actor perm.Actor, limit int) ([]SiteExportJob, error) {
	out := []SiteExportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectSiteExport+` ORDER BY e.requested_at DESC, e.id DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanSiteExport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}

// DownloadSiteExport opens an organization's archive for its administrators.
func (j *Jobs) DownloadSiteExport(ctx context.Context, actor perm.Actor, id uuid.UUID) (*SiteExportJob, io.ReadCloser, error) {
	var found *SiteExportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanSiteExport(tx.QueryRow(ctx, selectSiteExport+` WHERE e.id = $1`, id))
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

// QueueSiteImport checks an uploaded archive is an organization's archive of
// ours, and that the organization has no space yet, stores it, and asks the
// worker to import it.
func (j *Jobs) QueueSiteImport(ctx context.Context, actor perm.Actor, archive *os.File, size int64) (*SiteImportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	if size > j.MaxSiteImportBytes {
		return nil, 0, &SiteTooLargeError{Limit: j.MaxSiteImportBytes}
	}
	err := j.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.MoveSite, uuid.Nil); err != nil {
			return err
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT site_has_spaces()`).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return ErrSiteNotEmpty
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	zr, err := zip.NewReader(archive, size)
	if err != nil {
		return nil, 0, invalid("This file is no zip. Export the organization from its settings in Stator, then import that file.")
	}
	if _, err := readSiteManifest(zr); err != nil {
		return nil, 0, err
	}
	id := uuid.Must(uuid.NewV7())
	objectKey := jobKey(siteImportFolder, id)
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	if err := j.store.Put(ctx, objectKey, archive, size, "application/zip"); err != nil {
		return nil, 0, err
	}
	var out *SiteImportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var stored string
		if err := tx.QueryRow(ctx, `
			INSERT INTO site_import (id, requested_by, size_bytes) VALUES ($1, $2, $3) RETURNING object_key`,
			id, actor.UserID, size).Scan(&stored); err != nil {
			return fmt.Errorf("queue the import: %w", err)
		}
		if err := sameKey(stored, objectKey); err != nil {
			return err
		}
		var err error
		out, err = scanSiteImport(tx.QueryRow(ctx, selectSiteImport+` WHERE i.id = $1`, id))
		return err
	})
	if err != nil {
		_ = j.store.Delete(context.WithoutCancel(ctx), objectKey)
	}
	return out, lsn, err
}

// SiteImport is one import, for the organization's administrators.
func (j *Jobs) SiteImport(ctx context.Context, actor perm.Actor, id uuid.UUID) (*SiteImportJob, error) {
	var out *SiteImportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanSiteImport(tx.QueryRow(ctx, selectSiteImport+` WHERE i.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	})
	return out, err
}

// SiteImports are the organization's latest imports.
func (j *Jobs) SiteImports(ctx context.Context, actor perm.Actor, limit int) ([]SiteImportJob, error) {
	out := []SiteImportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectSiteImport+` ORDER BY i.requested_at DESC, i.id DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanSiteImport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}
