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
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// FailureNotEmpty is an organization import into an organization that has
// spaces already.
const FailureNotEmpty Failure = "not_empty"

// OrgExportFailures and OrgImportFailures are every failure, for the API's description.
var (
	OrgExportFailures = []Failure{FailureForbidden, FailureTooLarge, FailureFailed}
	OrgImportFailures = []Failure{FailureForbidden, FailureNotEmpty, FailureInvalid, FailureTooLarge, FailureFailed}
)

func orgExportMessage(f Failure, detail string) string {
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

func orgImportMessage(f Failure, detail string) string {
	switch f {
	case FailureForbidden:
		return "Only administrators of the organization import an archive into it, and you no longer are one. Ask one of them to import it."
	case FailureNotEmpty:
		return ErrOrgNotEmpty.Error()
	case FailureInvalid, FailureTooLarge:
		if detail != "" {
			return detail
		}
		return "The archive could not be read. Export the organization again and import the new file."
	}
	return "Importing the organization failed, and nothing of it was kept. Import it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

// OrgNotEmptyError refuses an organization import into an organization with
// spaces, in a sentence.
type OrgNotEmptyError struct{}

func (OrgNotEmptyError) Error() string {
	return "This organization has spaces already, and an organization's archive is imported only into one without any. Import it into a fresh Stator, or delete every space here first."
}

// ErrOrgNotEmpty is the one OrgNotEmptyError.
var ErrOrgNotEmpty error = OrgNotEmptyError{}

// OrgExportJob is one export of the organization, as its administrators follow it.
type OrgExportJob struct {
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

const selectOrgExport = `
SELECT e.id, e.state, e.done_steps, e.total_steps, COALESCE(u.name, ''), e.requested_by = current_actor_id(),
       e.requested_at, e.started_at, e.finished_at, e.file_name, e.size_bytes, e.expires_at, e.failure, COALESCE(e.detail, ''), e.object_key
FROM org_export e LEFT JOIN app_user u ON u.id = e.requested_by`

func scanOrgExport(row pgx.Row) (*OrgExportJob, error) {
	var (
		j      OrgExportJob
		detail string
	)
	if err := row.Scan(&j.ID, &j.State, &j.Progress.Done, &j.Progress.Total, &j.RequestedBy, &j.Mine,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &j.FileName, &j.Size, &j.ExpiresAt, &j.Failure, &detail, &j.key); err != nil {
		return nil, err
	}
	if j.Failure != nil {
		m := orgExportMessage(*j.Failure, detail)
		j.Message = &m
	}
	return &j, nil
}

// OrgImportJob is one import of an organization's archive.
type OrgImportJob struct {
	ID          uuid.UUID  `json:"id"`
	State       State      `json:"state"`
	Progress    Progress   `json:"progress"`
	RequestedBy string     `json:"requestedBy"`
	Report      *OrgReport `json:"report"`
	Failure     *Failure   `json:"failure"`
	Message     *string    `json:"message"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	Written     db.LSN     `json:"-"`
}

const selectOrgImport = `
SELECT i.id, i.state, i.done_steps, i.total_steps, COALESCE(u.name, ''), i.report, i.failure, COALESCE(i.detail, ''),
       i.requested_at, i.started_at, i.finished_at, COALESCE(i.written_lsn::text, '')
FROM org_import i LEFT JOIN app_user u ON u.id = i.requested_by`

func scanOrgImport(row pgx.Row) (*OrgImportJob, error) {
	var (
		j       OrgImportJob
		report  []byte
		detail  string
		written string
	)
	if err := row.Scan(&j.ID, &j.State, &j.Progress.Done, &j.Progress.Total, &j.RequestedBy, &report, &j.Failure, &detail,
		&j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if report != nil {
		j.Report = &OrgReport{}
		if err := json.Unmarshal(report, j.Report); err != nil {
			return nil, err
		}
		j.Report.settle()
	}
	if j.Failure != nil {
		m := orgImportMessage(*j.Failure, detail)
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

// OrgReport is what an organization's import brought across, and what not.
type OrgReport struct {
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

func (r *OrgReport) settle() {
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

// orgDownloadName is the name an organization's archive downloads as.
func orgDownloadName(slug string, at time.Time) string {
	return fmt.Sprintf("%s-organization-%s.zip", slug, at.UTC().Format("2006-01-02"))
}

// OrgTooLargeError refuses an organization archive over the limit, naming it.
type OrgTooLargeError struct{ Limit int64 }

func (e *OrgTooLargeError) Error() string {
	return fmt.Sprintf("This archive is over %s, the most one import takes. Ask whoever runs Stator to raise STATOR_ORG_IMPORT_LIMIT.", attachment.Size(e.Limit))
}

// QueueOrgExport asks the worker to export the caller's organization.
func (j *Jobs) QueueOrgExport(ctx context.Context, actor perm.Actor) (*OrgExportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	var out *OrgExportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.MoveOrg, uuid.Nil); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO org_export (id, org_id, requested_by) VALUES ($1, current_org_id(), $2)`, id, actor.UserID); err != nil {
			return fmt.Errorf("queue the export: %w", err)
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgExported, "org", nil, map[string]any{"export": id.String()}); err != nil {
			return err
		}
		var err error
		out, err = scanOrgExport(tx.QueryRow(ctx, selectOrgExport+` WHERE e.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// OrgExports are the organization's latest exports, for its administrators.
func (j *Jobs) OrgExports(ctx context.Context, actor perm.Actor, limit int) ([]OrgExportJob, error) {
	out := []OrgExportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectOrgExport+` ORDER BY e.requested_at DESC, e.id DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanOrgExport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}

// DownloadOrgExport opens an organization's archive for its administrators.
func (j *Jobs) DownloadOrgExport(ctx context.Context, actor perm.Actor, id uuid.UUID) (*OrgExportJob, io.ReadCloser, error) {
	var found *OrgExportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanOrgExport(tx.QueryRow(ctx, selectOrgExport+` WHERE e.id = $1`, id))
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

// QueueOrgImport checks an uploaded archive is an organization's archive of
// ours, and that the organization has no space yet, stores it, and asks the
// worker to import it.
func (j *Jobs) QueueOrgImport(ctx context.Context, actor perm.Actor, archive *os.File, size int64) (*OrgImportJob, db.LSN, error) {
	if objectstore.IsUnavailable(j.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	if size > j.MaxOrgImportBytes {
		return nil, 0, &OrgTooLargeError{Limit: j.MaxOrgImportBytes}
	}
	err := j.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.MoveOrg, uuid.Nil); err != nil {
			return err
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT org_has_spaces()`).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return ErrOrgNotEmpty
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
	if _, err := readOrgManifest(zr); err != nil {
		return nil, 0, err
	}
	org, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, 0, tenant.ErrNoTenant
	}
	id := uuid.Must(uuid.NewV7())
	objectKey := orgImportObjectKey(org.ID, id)
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	if err := j.store.Put(ctx, objectKey, archive, size, "application/zip"); err != nil {
		return nil, 0, err
	}
	var out *OrgImportJob
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_import (id, org_id, requested_by, size_bytes) VALUES ($1, current_org_id(), $2, $3)`, id, actor.UserID, size); err != nil {
			return fmt.Errorf("queue the import: %w", err)
		}
		var err error
		out, err = scanOrgImport(tx.QueryRow(ctx, selectOrgImport+` WHERE i.id = $1`, id))
		return err
	})
	if err != nil {
		_ = j.store.Delete(context.WithoutCancel(ctx), objectKey)
	}
	return out, lsn, err
}

func orgImportObjectKey(org, id uuid.UUID) string {
	return "org/" + org.String() + "/org-import/" + id.String()
}

// OrgImport is one import, for the organization's administrators.
func (j *Jobs) OrgImport(ctx context.Context, actor perm.Actor, id uuid.UUID) (*OrgImportJob, error) {
	var out *OrgImportJob
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanOrgImport(tx.QueryRow(ctx, selectOrgImport+` WHERE i.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	})
	return out, err
}

// OrgImports are the organization's latest imports.
func (j *Jobs) OrgImports(ctx context.Context, actor perm.Actor, limit int) ([]OrgImportJob, error) {
	out := []OrgImportJob{}
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectOrgImport+` ORDER BY i.requested_at DESC, i.id DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanOrgImport(rows)
			if err != nil {
				return err
			}
			out = append(out, *one)
		}
		return rows.Err()
	})
	return out, err
}
