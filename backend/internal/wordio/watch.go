package wordio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// DefaultWatchInterval is how often the worker looks for imports to run;
	// somebody is waiting on the page for them.
	DefaultWatchInterval = 2 * time.Second
	// RunLimit bounds one import; fifty documents take a minute or two.
	RunLimit = 15 * time.Minute
	// CleanupLimit bounds trashing what a failed import made.
	CleanupLimit = 2 * time.Minute
	// Lease is how long a worker holds an import before another takes it
	// over, past every run and its cleanup, so only a dead worker's lapses.
	Lease = RunLimit + CleanupLimit + time.Minute
	// MaxAttempts is how many workers take an import before it fails.
	MaxAttempts = 3
)

// errLeaseLost stops an import another worker took over.
var errLeaseLost = errors.New("another worker took over the Word import")

// claim is an import a worker took, with whom it acts for.
type claim struct {
	id, org, parent uuid.UUID
	slug            string
	requester       uuid.UUID
	role            auth.OrgRole
	attempts        int
	// made is what an attempt before this one made and left.
	made []uuid.UUID
}

func (c *claim) actor() perm.Actor { return perm.Actor{UserID: c.requester, Role: c.role} }

// Watch is the worker's half of importing several documents: it makes each
// import's pages as the person who queued it, through the services.
type Watch struct {
	svc      *Service
	log      *slog.Logger
	interval time.Duration
}

func NewWatch(svc *Service, log *slog.Logger, interval time.Duration) *Watch {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	return &Watch{svc: svc, log: log, interval: interval}
}

// Run looks every interval until the context ends.
func (w *Watch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			took, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("a Word import failed", "error", err)
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

// Once takes one import waiting, or one a dead worker left, and runs it;
// it says whether there was one. An error leaves the import to lapse.
func (w *Watch) Once(ctx context.Context) (bool, error) {
	c, err := w.claim(ctx)
	if err != nil || c == nil {
		return false, err
	}
	return true, w.run(ctx, c)
}

// progress is an import's report as it grows.
type progress struct {
	files []FileReport
	made  []uuid.UUID
	done  int
	total int
	lsn   db.LSN
}

func (w *Watch) run(ctx context.Context, c *claim) error {
	org := tenant.WithOrg(db.WithUser(db.PinPrimary(ctx), c.requester), tenant.Org{ID: c.org, Slug: c.slug})
	settle := context.WithoutCancel(ctx)
	if len(c.made) > 0 {
		// Left by a worker that died: gone before anything else is made.
		w.trash(context.WithoutCancel(org), c, c.made)
		if err := w.record(settle, c, &progress{}); err != nil {
			return err
		}
	}
	switch {
	case c.role == "":
		return w.finish(settle, c, &progress{}, StateFailed, FailureForbidden)
	case c.attempts > MaxAttempts:
		return w.finish(settle, c, &progress{}, StateFailed, FailureFailed)
	}
	began := time.Now()
	run, cancel := context.WithTimeout(org, RunLimit)
	defer cancel()
	p := &progress{}
	err := w.importAll(run, c, p)
	switch {
	case err == nil:
		w.log.Info("Word documents imported", "org", c.org, "import", c.id, "pages", p.done, "took", time.Since(began).Round(time.Millisecond))
		w.discardUpload(settle, c)
		return w.finish(settle, c, p, StateDone, "")
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		// The worker is stopping, which is not the import's fault; the next
		// one trashes what this one made and begins again.
		return w.finish(settle, c, p, StateQueued, "")
	}
	failure := failureOf(err)
	w.log.Warn("a Word import stopped", "org", c.org, "import", c.id, "failure", failure, "error", err)
	cleanup, stop := context.WithTimeout(context.WithoutCancel(org), CleanupLimit)
	defer stop()
	w.trash(cleanup, c, p.made)
	p.made = nil
	w.discardUpload(settle, c)
	return w.finish(settle, c, p, StateFailed, failure)
}

// failureOf reads why an import stopped from its error.
func failureOf(err error) Failure {
	var (
		denied *perm.DeniedError
		pgErr  *pgconn.PgError
	)
	switch {
	case errors.Is(err, page.ErrNotFound):
		return FailureParentGone
	case errors.As(err, &denied), errors.Is(err, perm.ErrDenied), errors.Is(err, page.ErrPostPlace):
		return FailureForbidden
	case errors.As(err, &pgErr) && pgErr.Code == "42501":
		return FailureForbidden
	}
	return FailureFailed
}

// importAll makes a page of each document and folder in order, recording
// each as it is made; a document that cannot be read is reported and passed.
func (w *Watch) importAll(ctx context.Context, c *claim, p *progress) error {
	rc, err := w.svc.store.Get(ctx, ObjectKey(c.org, c.id))
	if err != nil {
		return fmt.Errorf("read the uploaded documents: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxUploadBytes+MaxFileBytes))
	_ = rc.Close()
	if err != nil {
		return fmt.Errorf("read the uploaded documents: %w", err)
	}
	docs, err := readPacked(data)
	if err != nil {
		return fmt.Errorf("open the uploaded documents: %w", err)
	}
	byPath := map[string][]byte{}
	for _, f := range docs {
		byPath[f.Path] = f.Data
	}
	roots := tree(docs, "")
	p.total = min(count(roots), MaxPages)
	if _, err := w.svc.checkParent(ctx, c.actor(), c.parent); err != nil {
		return err
	}
	ids := map[*entry]uuid.UUID{}
	var failed error
	walk(roots, 1, func(e, up *entry, depth int) {
		if failed != nil || p.done >= p.total {
			return
		}
		under := c.parent
		if up != nil {
			id, ok := ids[up]
			if !ok {
				return
			}
			under = id
		}
		report := FileReport{Path: e.doc, Warnings: []string{}}
		if e.doc == "" {
			report.Path = e.dir + "/"
		}
		var made *page.Page
		var lsn db.LSN
		var err error
		if e.doc != "" {
			prepared, perr := w.svc.prepare(e.doc, byPath[e.doc])
			if perr == nil {
				made, lsn, err = w.svc.make(ctx, c.actor(), under, prepared)
				report.Warnings = append(report.Warnings, prepared.warnings...)
			} else if refusal := sentenceOf(perr); refusal != "" {
				report.Error = &refusal
			} else {
				err = perr
			}
		}
		// A folder, or a document that holds a folder's pages and could not
		// be read, is a page of its name, so what is below has a place.
		if made == nil && err == nil && (e.doc == "" || len(e.children) > 0) {
			made, lsn, err = w.svc.folder(ctx, c.actor(), under, titleFrom(e.name))
		}
		p.lsn = max(p.lsn, lsn)
		if err != nil {
			failed = err
			return
		}
		if made != nil {
			ids[e] = made.ID
			report.Page = &Made{ID: made.ID, ParentID: under, Title: made.Title, Depth: depth}
			if up == nil {
				p.made = append(p.made, made.ID)
			}
		}
		p.files = append(p.files, report)
		p.done++
		failed = w.record(context.WithoutCancel(ctx), c, p)
	})
	return failed
}

// sentenceOf is a refusal of one document said to its importer, or empty
// for an error that stops the whole import.
func sentenceOf(err error) string {
	var bad *InvalidError
	if errors.As(err, &bad) {
		return sentence(bad.Message)
	}
	return ""
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	out := []rune(s)
	if out[0] >= 'a' && out[0] <= 'z' {
		out[0] -= 'a' - 'A'
	}
	if last := out[len(out)-1]; last != '.' && last != '?' && last != '!' {
		out = append(out, '.')
	}
	return string(out)
}

// folder makes an empty published page, for a folder of the upload.
func (s *Service) folder(ctx context.Context, actor perm.Actor, parent uuid.UUID, title string) (*page.Page, db.LSN, error) {
	made, lsn, err := s.pages.Create(ctx, actor, page.CreateInput{Placement: page.Placement{ParentID: parent}, Title: title})
	if err != nil {
		return nil, lsn, err
	}
	published, l, err := s.pages.Update(ctx, actor, made.ID, page.UpdateInput{})
	lsn = max(lsn, l)
	if err != nil {
		if l, terr := s.pages.Trash(context.WithoutCancel(ctx), actor, made.ID); terr == nil {
			lsn = max(lsn, l)
		}
		return nil, lsn, err
	}
	return published, lsn, nil
}

// trash moves what an import made to the trash as its requester; what they
// may no longer trash stays, and is logged.
func (w *Watch) trash(ctx context.Context, c *claim, ids []uuid.UUID) {
	for _, id := range ids {
		if _, err := w.svc.pages.Trash(ctx, c.actor(), id); err != nil && !errors.Is(err, page.ErrNotFound) {
			w.log.Warn("a page a failed Word import made could not be trashed", "org", c.org, "import", c.id, "page", id, "error", err)
		}
	}
}

// discardUpload deletes the upload once nothing will read it again.
func (w *Watch) discardUpload(ctx context.Context, c *claim) {
	if err := w.svc.store.Delete(ctx, ObjectKey(c.org, c.id)); err != nil {
		w.log.Warn("a Word import's upload could not be deleted", "org", c.org, "import", c.id, "error", err)
	}
}

// claim takes the next import waiting, or one whose worker died. Claiming
// crosses organizations and recording is the worker's alone, both of which
// the admin role is for.
func (w *Watch) claim(ctx context.Context) (*claim, error) {
	var c *claim
	_, err := w.svc.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			out  claim
			role string
		)
		// Materialized, so the pick runs once, as the example space's claim
		// learned: a joined subquery is scanned again and hands out a second job.
		err := tx.QueryRow(ctx, `
			WITH next AS MATERIALIZED (
				SELECT id FROM word_import
				WHERE state = 'queued' OR (state = 'running' AND lease_until < now())
				ORDER BY requested_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			UPDATE word_import j
			SET state = 'running', attempts = j.attempts + 1, lease_until = now() + make_interval(secs => $1),
			    started_at = COALESCE(j.started_at, now())
			FROM next
			WHERE j.id = next.id
			RETURNING j.id, j.org_id, (SELECT slug FROM org WHERE id = j.org_id), j.requested_by,
			          COALESCE((SELECT org_role FROM org_member m WHERE m.org_id = j.org_id AND m.user_id = j.requested_by), ''),
			          j.parent_id, j.attempts, j.made`, Lease.Seconds()).
			Scan(&out.id, &out.org, &out.slug, &out.requester, &role, &out.parent, &out.attempts, &out.made)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.role = auth.OrgRole(role)
		c = &out
		return nil
	})
	return c, err
}

// record writes how far the attempt this worker holds has come; one taken
// over since is no longer its to write.
func (w *Watch) record(ctx context.Context, c *claim, p *progress) error {
	report, err := json.Marshal(nonNilReports(p.files))
	if err != nil {
		return err
	}
	_, err = w.svc.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE word_import SET done_steps = $3, total_steps = $4, report = $5, made = $6
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, p.done, max(p.total, p.done), report, nonNilIDs(p.made))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errLeaseLost
		}
		return nil
	})
	return err
}

// finish ends the attempt this worker holds.
func (w *Watch) finish(ctx context.Context, c *claim, p *progress, state State, failure Failure) error {
	report, err := json.Marshal(nonNilReports(p.files))
	if err != nil {
		return err
	}
	var (
		why     *Failure
		written *string
	)
	if state == StateFailed {
		why = &failure
	}
	if p.lsn != 0 {
		text := p.lsn.String()
		written = &text
	}
	// A failed import trashed what it made; one handed back keeps it named,
	// for the next attempt to trash before it begins again.
	made := p.made
	if state == StateFailed {
		made = nil
	}
	_, err = w.svc.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE word_import
			SET state = $3, failure = $4, written_lsn = $5::pg_lsn, lease_until = NULL, report = $6, made = $7,
			    done_steps = $8, total_steps = $9,
			    finished_at = CASE WHEN $3 IN ('done', 'failed') THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), why, written, report, nonNilIDs(made), p.done, max(p.total, p.done))
		if err != nil {
			return fmt.Errorf("record how a Word import ended: %w", err)
		}
		return nil
	})
	return err
}

func nonNilReports(r []FileReport) []FileReport {
	if r == nil {
		return []FileReport{}
	}
	return r
}

func nonNilIDs(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}
