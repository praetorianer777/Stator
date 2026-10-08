package spaceio

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
	"github.com/praetorianer777/stator/backend/internal/wikiread"
)

const (
	// DefaultWatchInterval is how often the worker looks for exports and
	// imports to run; somebody is waiting on the page for them.
	DefaultWatchInterval = 2 * time.Second
	// DefaultExportTTL is how long an export's file is kept for download.
	DefaultExportTTL = 24 * time.Hour
	// RunLimit bounds one export or import of the largest space taken.
	RunLimit = 30 * time.Minute
	// Lease is how long a worker holds a job before another takes it over,
	// past every run, so only a dead worker's lapses.
	Lease = RunLimit + 5*time.Minute
	// MaxAttempts is how many workers take a job before it fails.
	MaxAttempts = 3
	// progressEvery is how often a run writes how far it has come.
	progressEvery = time.Second
)

// errLeaseLost stops a run whose job another worker took over.
var errLeaseLost = errors.New("another worker took over the job")

// kind is which of the two tables a job is in.
type kind string

const (
	kindExport kind = "space_export"
	kindImport kind = "space_import"
)

// claim is a job a worker took, with whom it acts for.
type claim struct {
	kind     kind
	id, org  uuid.UUID
	slug     string
	user     uuid.UUID
	role     auth.OrgRole
	attempts int
	// Exports: which space, how. Imports: what was asked, and the space an
	// attempt before this one made and left.
	spaceID  *uuid.UUID
	format   ExportFormat
	key      *string
	name     *string
	size     int64
	source   ImportSource
	leftover *uuid.UUID
	object   string
}

func (c *claim) actor() perm.Actor { return perm.Actor{UserID: c.user, Role: c.role} }

// Watch is the worker's half of space export and import.
type Watch struct {
	db       *db.Cluster
	store    objectstore.Store
	log      *slog.Logger
	interval time.Duration
	ttl      time.Duration
	// TempDir is where files are written while they are made; empty is the
	// system's.
	TempDir string
}

func NewWatch(cluster *db.Cluster, store objectstore.Store, log *slog.Logger, interval, ttl time.Duration) *Watch {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	if ttl <= 0 {
		ttl = DefaultExportTTL
	}
	return &Watch{db: cluster, store: store, log: log, interval: interval, ttl: ttl}
}

// Run looks every interval until the context ends.
func (w *Watch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			took, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("a space export or import failed", "error", err)
				break
			}
			if !took || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once lets expired exports go, then takes one job waiting, or one a dead
// worker left, and runs it; it says whether there was one.
func (w *Watch) Once(ctx context.Context) (bool, error) {
	if err := w.expire(ctx); err != nil {
		return false, err
	}
	for _, k := range []kind{kindExport, kindImport} {
		c, err := w.claim(ctx, k)
		if err != nil {
			return false, err
		}
		if c == nil {
			continue
		}
		if k == kindExport {
			return true, w.runExport(ctx, c)
		}
		return true, w.runImport(ctx, c)
	}
	return false, nil
}

// expire marks every export past its time expired and leaves a tombstone
// for its file, which the file reaper then deletes.
func (w *Watch) expire(ctx context.Context) error {
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			WITH gone AS (
				UPDATE space_export SET state = 'expired' WHERE state = 'done' AND expires_at < now()
				RETURNING org_id, object_key)
			INSERT INTO attachment_tombstone (object_key, org_id) SELECT object_key, org_id FROM gone
			ON CONFLICT (object_key) DO NOTHING`)
		return err
	})
	return err
}

func (w *Watch) claim(ctx context.Context, k kind) (*claim, error) {
	var c *claim
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		out := claim{kind: k}
		var role string
		extra := `j.space_id, j.format, NULL::text, NULL::text, 0::bigint, ''`
		if k == kindImport {
			extra = `j.space_id, '', j.key, j.name, j.size_bytes, j.source`
		}
		var format, source string
		// Materialized, so the pick runs once: joined as a subquery, it was
		// scanned again for the update and, skipping the row it had just
		// locked, handed this worker a second job it never ran.
		err := tx.QueryRow(ctx, fmt.Sprintf(`
			WITH next AS MATERIALIZED (
				SELECT id FROM %[1]s
				WHERE state = 'queued' OR (state = 'running' AND lease_until < now())
				ORDER BY requested_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			UPDATE %[1]s j
			SET state = 'running', attempts = j.attempts + 1, lease_until = now() + make_interval(secs => $1),
			    started_at = COALESCE(j.started_at, now())
			FROM next
			WHERE j.id = next.id
			RETURNING j.id, j.org_id, (SELECT slug FROM org WHERE id = j.org_id), j.requested_by,
			          COALESCE((SELECT org_role FROM org_member m WHERE m.org_id = j.org_id AND m.user_id = j.requested_by), ''),
			          j.attempts, j.object_key, %[2]s`, k, extra), Lease.Seconds()).
			Scan(&out.id, &out.org, &out.slug, &out.user, &role, &out.attempts, &out.object, &out.spaceID, &format, &out.key, &out.name, &out.size, &source)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.role, out.format, out.source = auth.OrgRole(role), ExportFormat(format), ImportSource(source)
		if k == kindImport {
			out.leftover, out.spaceID = out.spaceID, nil
		}
		c = &out
		return nil
	})
	return c, err
}

// progress writes how far a run has come, at most once a second.
type progress struct {
	w     *Watch
	c     *claim
	mu    sync.Mutex
	done  int
	total int
	last  time.Time
}

func (p *progress) setTotal(ctx context.Context, total int) {
	p.mu.Lock()
	p.total = total
	p.mu.Unlock()
	p.flush(ctx, true)
}

func (p *progress) step(ctx context.Context) func() {
	return func() {
		p.mu.Lock()
		p.done++
		due := time.Since(p.last) >= progressEvery
		p.mu.Unlock()
		if due {
			p.flush(ctx, false)
		}
	}
}

func (p *progress) flush(ctx context.Context, force bool) {
	p.mu.Lock()
	done, total := p.done, p.total
	p.last = time.Now()
	p.mu.Unlock()
	_, err := p.w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s SET done_steps = LEAST($3::int, $4::int), total_steps = $4::int
			WHERE id = $1 AND attempts = $2 AND state = 'running'`, p.c.kind), p.c.id, p.c.attempts, done, total)
		return err
	})
	if err != nil && force {
		p.w.log.Warn("the progress of a space export or import was not written", "job", p.c.id, "error", err)
	}
}

// acting is the context a run reads and writes in: the requester in their
// organization, on the primary, which has every write the run made.
func (c *claim) acting(ctx context.Context) context.Context {
	return tenant.WithOrg(db.WithUser(db.PinPrimary(ctx), c.user), tenant.Org{ID: c.org, Slug: c.slug})
}

func (w *Watch) tempFile() (*os.File, func(), error) {
	f, err := os.CreateTemp(w.TempDir, "stator-space-*.zip")
	if err != nil {
		return nil, nil, err
	}
	return f, func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}, nil
}

// isDenied says the requester may no longer do what they asked.
func isDenied(err error) bool {
	var (
		denied *perm.DeniedError
		pgErr  *pgconn.PgError
	)
	return errors.As(err, &denied) || errors.Is(err, perm.ErrDenied) || (errors.As(err, &pgErr) && pgErr.Code == "42501")
}

func (w *Watch) runExport(ctx context.Context, c *claim) error {
	switch {
	case c.role == "" || c.spaceID == nil:
		return w.finishExport(ctx, c, StateFailed, FailureForbidden, "", 0)
	case c.attempts > MaxAttempts:
		return w.finishExport(ctx, c, StateFailed, FailureFailed, "", 0)
	}
	began := time.Now()
	run, cancel := context.WithTimeout(c.acting(ctx), RunLimit)
	defer cancel()
	name, size, err := w.export(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("space exported", "org", c.org, "job", c.id, "format", c.format, "bytes", size, "took", time.Since(began).Round(time.Millisecond))
		return w.finishExport(settle, c, StateDone, "", name, size)
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		return w.finishExport(settle, c, StateQueued, "", "", 0)
	}
	failure := FailureFailed
	switch {
	case isDenied(err):
		failure = FailureForbidden
	case errors.Is(err, ErrTooLarge):
		failure = FailureTooLarge
	}
	w.log.Warn("the space was not exported", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.finishExport(settle, c, StateFailed, failure, "", 0)
}

// export writes the file and stores it, answering its name and size.
func (w *Watch) export(ctx context.Context, c *claim) (string, int64, error) {
	f, done, err := w.tempFile()
	if err != nil {
		return "", 0, err
	}
	defer done()
	zw := zip.NewWriter(f)
	p := &progress{w: w, c: c}
	step := p.step(ctx)
	var (
		snap   *snapshot
		counts Counts
	)
	err = w.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if snap, err = read(ctx, tx, c.actor(), *c.spaceID); err != nil {
			return err
		}
		if err := snap.tooMany(); err != nil {
			return err
		}
		if c.format == FormatHTML {
			p.setTotal(ctx, len(snap.pages)+len(layout(snap).files))
			return nil
		}
		p.setTotal(ctx, len(snap.pages)+snap.files)
		counts, err = snap.writeArchive(ctx, tx, zw, step)
		return err
	})
	if err != nil {
		return "", 0, err
	}
	if c.format == FormatHTML {
		err = layout(snap).write(ctx, w.store, zw, step)
	} else {
		err = snap.writeFiles(ctx, w.store, zw, archiveFilePath, everyFile, step)
		if err == nil {
			err = writeJSON(zw, manifestPath, snap.manifest(counts))
		}
	}
	if err != nil {
		return "", 0, err
	}
	if err := zw.Close(); err != nil {
		return "", 0, err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	if err := w.store.Put(ctx, c.object, f, size, "application/zip"); err != nil {
		return "", 0, err
	}
	p.flush(ctx, true)
	return downloadName(snap.space.Key, c.format), size, nil
}

func (w *Watch) finishExport(ctx context.Context, c *claim, state State, failure Failure, name string, size int64) error {
	var (
		f       *Failure
		file    *string
		bytes   *int64
		expires *time.Time
	)
	if state == StateFailed {
		f = &failure
	}
	if state == StateDone {
		at := time.Now().Add(w.ttl)
		file, bytes, expires = &name, &size, &at
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE space_export
			SET state = $3, failure = $4, file_name = $5, size_bytes = $6, expires_at = $7, lease_until = NULL,
			    done_steps = CASE WHEN $3 = 'done' THEN total_steps ELSE done_steps END,
			    finished_at = CASE WHEN $3 IN ('done', 'failed') THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), f, file, bytes, expires)
		if err != nil {
			return fmt.Errorf("record how the export ended: %w", err)
		}
		return nil
	})
	return err
}

func (w *Watch) runImport(ctx context.Context, c *claim) error {
	if c.leftover != nil {
		// Made by a worker that died before it said so: gone before
		// anything else, with its files' bytes by their tombstones.
		if err := w.discard(ctx, *c.leftover); err != nil {
			return err
		}
	}
	switch {
	case c.role == "":
		return w.finishImport(ctx, c, StateFailed, FailureForbidden, "", nil, nil, 0)
	case c.attempts > MaxAttempts:
		return w.finishImport(ctx, c, StateFailed, FailureFailed, "", nil, nil, 0)
	}
	began := time.Now()
	run, cancel := context.WithTimeout(c.acting(ctx), RunLimit)
	defer cancel()
	im, written, err := w.importArchive(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("space imported", "org", c.org, "job", c.id, "space", im.key, "took", time.Since(began).Round(time.Millisecond))
		return w.finishImport(settle, c, StateDone, "", "", &im.spaceID, &im.report, written)
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		return w.finishImport(settle, c, StateQueued, "", "", nil, nil, 0)
	}
	failure, detail := failureOf(err)
	w.log.Warn("the space was not imported", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.finishImport(settle, c, StateFailed, failure, detail, nil, nil, 0)
}

// importArchive makes the space in one transaction, after reading the
// archive through once; the bytes stored for a failed one are let go.
func (w *Watch) importArchive(ctx context.Context, c *claim) (*importer, db.LSN, error) {
	allowed := false
	if err := w.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		allowed = perm.Allowed(ctx, tx, c.actor(), perm.CreateSpace, uuid.Nil)
		return nil
	}); err != nil {
		return nil, 0, err
	}
	if !allowed {
		return nil, 0, &perm.DeniedError{Action: perm.CreateSpace}
	}
	f, done, err := w.tempFile()
	if err != nil {
		return nil, 0, err
	}
	defer done()
	body, err := w.store.Get(ctx, c.object)
	if err != nil {
		return nil, 0, err
	}
	size, err := io.Copy(f, io.LimitReader(body, c.size+1))
	_ = body.Close()
	if err != nil {
		return nil, 0, err
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return nil, 0, invalid("This file is no zip. Export the space from Stator as an archive, or from the other wiki as HTML or XML, then import that zip.")
	}
	var (
		m        *Manifest
		src      source
		exported *exportSource
	)
	if c.source == SourceArchive {
		if m, err = readManifest(zr); err != nil {
			return nil, 0, err
		}
		src = archiveSource{openArchive(zr)}
	} else {
		var key, name string
		if c.key != nil {
			key = space.NormalizeKey(*c.key)
		}
		if c.name != nil {
			name = *c.name
		}
		if m, exported, err = readExport(zr, c.source, key, name); err != nil {
			return nil, 0, err
		}
		src = exported
	}
	im := &importer{src: src, m: m, store: w.store, org: c.org, importer: c.user, spaceID: uuid.Must(uuid.NewV7()), from: c.source,
		report: Report{People: []MissingPerson{}, Groups: []string{}, Dropped: []DroppedEntry{}, Lost: []wikiread.Loss{}}}
	if exported != nil {
		im.report.Lost, im.report.LostCount = exported.listed, exported.lostCount
		if im.report.Lost == nil {
			im.report.Lost = []wikiread.Loss{}
		}
	}
	im.key, im.name = m.Space.Key, m.Space.Name
	if c.key != nil {
		im.key = *c.key
	}
	if c.name != nil {
		im.name = *c.name
	}
	if err := checkSpace(im); err != nil {
		return nil, 0, err
	}
	if err := im.scan(); err != nil {
		return nil, 0, err
	}
	p := &progress{w: w, c: c}
	p.setTotal(ctx, len(m.Pages)+len(im.r.files))
	im.step = p.step(ctx)
	written, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := im.mapPeople(ctx, tx); err != nil {
			return err
		}
		if err := im.write(ctx, tx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE space_import SET space_id = $3 WHERE id = $1 AND attempts = $2 AND state = 'running'`, c.id, c.attempts, im.spaceID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errLeaseLost
		}
		return nil
	})
	if err != nil {
		w.letGo(context.WithoutCancel(ctx), c.org, im.uploaded)
		return nil, 0, err
	}
	return im, written, nil
}

// checkSpace holds the key and name an import makes the space with to
// what a space takes; the archive's own may be anything.
func checkSpace(im *importer) error {
	im.key = space.NormalizeKey(im.key)
	if err := space.CheckKey(im.key); err != nil {
		return invalid("The archive's key %q is not one a space may take here. Import it again with a key of your own.", im.key)
	}
	name, err := space.CleanName(im.name)
	if err != nil {
		return invalid("The archive's space name is empty or too long. Import it again with a name of your own.")
	}
	im.name = name
	return nil
}

// letGo leaves a tombstone for each object a failed import stored, which
// the file reaper deletes.
func (w *Watch) letGo(ctx context.Context, org uuid.UUID, keys []string) {
	if len(keys) == 0 {
		return
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO attachment_tombstone (object_key, org_id) SELECT k, $1 FROM unnest($2::text[]) AS k
			ON CONFLICT (object_key) DO NOTHING`, org, keys)
		return err
	})
	if err != nil {
		w.log.Warn("the files of a failed import were not let go; they stay in storage", "org", org, "error", err)
	}
}

// discard deletes a space an import made and never reported; its files'
// rows leave tombstones as they go.
func (w *Watch) discard(ctx context.Context, id uuid.UUID) error {
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM space WHERE id = $1`, id)
		return err
	})
	return err
}

// finishImport ends the attempt this worker holds, and lets the uploaded
// archive go once the job is done with it either way.
func (w *Watch) finishImport(ctx context.Context, c *claim, state State, failure Failure, detail string, spaceID *uuid.UUID, report *Report, written db.LSN) error {
	var (
		f        *Failure
		d        *string
		reported []byte
		lsn      *string
	)
	if state == StateFailed {
		f = &failure
		if detail != "" {
			d = &detail
		}
	}
	if report != nil {
		var err error
		if reported, err = json.Marshal(report); err != nil {
			return err
		}
	}
	if written != 0 {
		text := written.String()
		lsn = &text
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE space_import
			SET state = $3, failure = $4, detail = $5, space_id = COALESCE($6, space_id), report = $7, written_lsn = $8::pg_lsn,
			    lease_until = NULL, done_steps = CASE WHEN $3 = 'done' THEN total_steps ELSE done_steps END,
			    finished_at = CASE WHEN $3 IN ('done', 'failed') THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), f, d, spaceID, reported, lsn)
		if err != nil {
			return fmt.Errorf("record how the import ended: %w", err)
		}
		if tag.RowsAffected() > 0 && state != StateQueued {
			_, err = tx.Exec(ctx, `
				INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, $2) ON CONFLICT (object_key) DO NOTHING`, c.object, c.org)
		}
		return err
	})
	return err
}
