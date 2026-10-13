package example

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

const (
	// DefaultWatchInterval is how often the worker looks for an example
	// space to make; somebody is waiting on the page for it.
	DefaultWatchInterval = 2 * time.Second
	// RunLimit bounds one making; on a quiet machine it takes seconds.
	RunLimit = 5 * time.Minute
	// Lease is how long a worker holds a job before another takes it over,
	// past every making and its cleanup, so only a dead worker's lapses.
	Lease = RunLimit + CleanupLimit + time.Minute
	// MaxAttempts is how many workers take a job before it fails.
	MaxAttempts = 3
)

// errLeaseLost stops a making whose job another worker took over.
var errLeaseLost = errors.New("another worker took over making the example space")

// claim is a job a worker took, with whom it acts for.
type claim struct {
	id, org   uuid.UUID
	slug      string
	kind      Kind
	requester Person
	role      auth.OrgRole
	language  string
	attempts  int
	// leftover is the space an attempt before this one made and left.
	leftover *uuid.UUID
	// target is the space a renewal renews, nil once it was deleted.
	target *uuid.UUID
}

func (c *claim) actor() perm.Actor { return perm.Actor{UserID: c.requester.ID, Role: c.role} }

// outcome is how a job ends, or, queued, that it goes back to wait.
type outcome struct {
	state   State
	spaceID *uuid.UUID
	failure Failure
	written db.LSN
	// report is a renewal's, which then records the version it renewed to.
	report *Report
}

// jobStore is where jobs are claimed and finished, apart for the unit tests.
type jobStore interface {
	claim(ctx context.Context) (*claim, error)
	started(ctx context.Context, c *claim, spaceID uuid.UUID) error
	finish(ctx context.Context, c *claim, o outcome) error
	// discard deletes a half made example as the worker.
	discard(ctx context.Context, spaceID uuid.UUID) error
	// track keeps a page the example wrote, and edition reads what a
	// renewal starts from.
	track(ctx context.Context, c *claim, spaceID uuid.UUID, t Tracked) error
	edition(ctx context.Context, spaceID uuid.UUID) (*Edition, []Tracked, error)
}

// making is the Maker as the watch drives it, apart for the unit tests.
type making interface {
	make(ctx context.Context, c *claim, started func(context.Context, *space.Space) error, wrote func(context.Context, Tracked) error) (*space.Space, db.LSN, error)
	renew(ctx context.Context, c *claim, in Renewal, wrote func(context.Context, Tracked) error) (*Report, db.LSN, error)
	discard(ctx context.Context, actor perm.Actor, id uuid.UUID) error
}

// Watch is the worker's half of the example space: it makes each one asked
// for as the administrator who asked, through the services.
type Watch struct {
	store    jobStore
	maker    making
	log      *slog.Logger
	interval time.Duration
}

func NewWatch(cluster *db.Cluster, maker *Maker, log *slog.Logger, interval time.Duration) *Watch {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	return &Watch{store: &dbStore{db: cluster}, maker: makerOf{maker}, log: log, interval: interval}
}

// Run looks every interval until the context ends.
func (w *Watch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			took, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("making the example space failed", "error", err)
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

// Once takes one job waiting, or one a dead worker left, and makes its
// space; it says whether there was one. An error leaves the job to lapse.
func (w *Watch) Once(ctx context.Context) (bool, error) {
	c, err := w.store.claim(ctx)
	if err != nil || c == nil {
		return false, err
	}
	return true, w.run(ctx, c)
}

func (w *Watch) run(ctx context.Context, c *claim) error {
	pinned := db.WithUser(db.PinPrimary(ctx), c.requester.ID)
	if c.leftover != nil {
		// Left by a worker that died: gone before anything else, or the job
		// waits for the next worker to try again.
		if err := w.discard(pinned, c, *c.leftover); err != nil {
			return err
		}
	}
	switch {
	case c.role == "":
		return w.store.finish(ctx, c, outcome{state: StateFailed, failure: FailureForbidden})
	case c.attempts > MaxAttempts:
		return w.store.finish(ctx, c, outcome{state: StateFailed, failure: FailureFailed})
	}
	run, cancel := context.WithTimeout(pinned, RunLimit)
	defer cancel()
	if c.kind == KindRenew {
		return w.renew(ctx, run, c)
	}
	began := time.Now()
	var begun uuid.UUID
	made, written, err := w.maker.make(run, c, func(ctx context.Context, sp *space.Space) error {
		begun = sp.ID
		return w.store.started(ctx, c, sp.ID)
	}, func(ctx context.Context, t Tracked) error {
		return w.store.track(ctx, c, begun, t)
	})
	settle := context.WithoutCancel(ctx)
	if errors.Is(err, ErrNotDiscarded) {
		if again := w.discard(context.WithoutCancel(pinned), c, begun); again != nil {
			return errors.Join(err, again)
		}
	}
	switch {
	case err == nil:
		o := outcome{state: StateDone, written: written}
		if made != nil {
			o.spaceID = &made.ID
		}
		w.log.Info("example space made", "org", c.org, "job", c.id, "took", time.Since(began).Round(time.Millisecond))
		return w.store.finish(settle, c, o)
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		// The worker is stopping, which is not the job's fault.
		return w.store.finish(settle, c, outcome{state: StateQueued})
	}
	failure := FailureOf(err)
	w.log.Warn("the example space was not made", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.store.finish(settle, c, outcome{state: StateFailed, failure: failure})
}

// renew renews the example space the job names. What it renewed before it
// failed stays: each page is a person's publish, and the next renewal finds
// the pages it wrote unchanged.
func (w *Watch) renew(ctx, run context.Context, c *claim) error {
	settle := context.WithoutCancel(ctx)
	if c.target == nil {
		return w.store.finish(settle, c, outcome{state: StateFailed, failure: FailureGone})
	}
	began := time.Now()
	e, tracked, err := w.store.edition(run, *c.target)
	var (
		report  *Report
		written db.LSN
	)
	if err == nil {
		report, written, err = w.maker.renew(run, c, Renewal{Space: *c.target, From: e.Version, Was: e.Language, Lang: c.language, Pages: tracked},
			func(ctx context.Context, t Tracked) error { return w.store.track(ctx, c, *c.target, t) })
	}
	switch {
	case err == nil:
		w.log.Info("example space renewed", "org", c.org, "job", c.id, "from", e.Version, "to", Current(), "took", time.Since(began).Round(time.Millisecond))
		return w.store.finish(settle, c, outcome{state: StateDone, spaceID: c.target, written: written, report: report})
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		return w.store.finish(settle, c, outcome{state: StateQueued, spaceID: c.target})
	}
	failure := FailureOf(err)
	w.log.Warn("the example space was not renewed", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.store.finish(settle, c, outcome{state: StateFailed, spaceID: c.target, failure: failure})
}

// discard deletes a half made space as the requester, else, when they may
// no longer, as the worker, which made it on their behalf.
func (w *Watch) discard(ctx context.Context, c *claim, id uuid.UUID) error {
	err := w.maker.discard(ctx, c.actor(), id)
	if err == nil {
		return nil
	}
	w.log.Warn("the requester could not delete the half made example space, so the worker does", "org", c.org, "space", id, "error", err)
	if again := w.store.discard(ctx, id); again != nil {
		return errors.Join(err, again)
	}
	return nil
}

// makerOf drives a Maker for one claimed job.
type makerOf struct{ m *Maker }

func (m makerOf) make(ctx context.Context, c *claim, started func(context.Context, *space.Space) error, wrote func(context.Context, Tracked) error) (*space.Space, db.LSN, error) {
	one := *m.m
	one.Started, one.Wrote = started, wrote
	made, _, written, err := one.Make(ctx, c.actor(), c.requester, c.language)
	return made, written, err
}

func (m makerOf) renew(ctx context.Context, c *claim, in Renewal, wrote func(context.Context, Tracked) error) (*Report, db.LSN, error) {
	one := *m.m
	one.Wrote = wrote
	return one.Renew(ctx, c.actor(), c.requester, in)
}

func (m makerOf) discard(ctx context.Context, actor perm.Actor, id uuid.UUID) error {
	return m.m.Discard(ctx, actor, id)
}

// dbStore keeps jobs in example_job. Claiming crosses organizations and
// finishing is the worker's alone, both of which the admin role is for.
type dbStore struct{ db *db.Cluster }

func (s *dbStore) claim(ctx context.Context) (*claim, error) {
	var c *claim
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			out  claim
			role string
			kind string
			on   *uuid.UUID
		)
		// Materialized, so the pick runs once: joined as a subquery, it was
		// scanned again for the update and, skipping the row it had just
		// locked, handed this worker a second job it never ran.
		err := tx.QueryRow(ctx, `
			WITH next AS MATERIALIZED (
				SELECT id FROM example_job
				WHERE state = 'queued' OR (state = 'running' AND lease_until < now())
				ORDER BY requested_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			UPDATE example_job j
			SET state = 'running', attempts = j.attempts + 1, lease_until = now() + make_interval(secs => $1),
			    started_at = COALESCE(j.started_at, now())
			FROM next
			WHERE j.id = next.id
			RETURNING j.id, j.org_id, (SELECT slug FROM org WHERE id = j.org_id), j.requested_by,
			          (SELECT name FROM app_user WHERE id = j.requested_by),
			          COALESCE((SELECT org_role FROM org_member m WHERE m.org_id = j.org_id AND m.user_id = j.requested_by), ''),
			          j.language, j.attempts, j.space_id, j.kind`, Lease.Seconds()).
			Scan(&out.id, &out.org, &out.slug, &out.requester.ID, &out.requester.Name, &role, &out.language, &out.attempts, &on, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.role, out.kind = auth.OrgRole(role), Kind(kind)
		if out.kind == KindRenew {
			out.target = on
		} else {
			out.leftover = on
		}
		c = &out
		return nil
	})
	return c, err
}

func (s *dbStore) started(ctx context.Context, c *claim, spaceID uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE example_job SET space_id = $3
			WHERE id = $1 AND state = 'running' AND attempts = $2`, c.id, c.attempts, spaceID)
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

// finish ends the attempt this worker holds; one taken over since is no
// longer its to end.
func (s *dbStore) finish(ctx context.Context, c *claim, o outcome) error {
	var (
		failure *Failure
		written *string
	)
	if o.state == StateFailed {
		failure = &o.failure
	}
	if o.written != 0 {
		text := o.written.String()
		written = &text
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE example_job
			SET state = $3, space_id = $4, failure = $5, written_lsn = $6::pg_lsn, lease_until = NULL, report = $7,
			    finished_at = CASE WHEN $3 IN ('done', 'failed') THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(o.state), o.spaceID, failure, written, o.report)
		if err != nil {
			return fmt.Errorf("record how the example space job ended: %w", err)
		}
		if tag.RowsAffected() == 0 || o.report == nil || o.spaceID == nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE space SET example_version = $2, example_language = $3 WHERE id = $1 AND example`,
			*o.spaceID, Current(), c.language); err != nil {
			return fmt.Errorf("record the version the example space was renewed to: %w", err)
		}
		return nil
	})
	return err
}

func (s *dbStore) track(ctx context.Context, c *claim, spaceID uuid.UUID, t Tracked) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO example_page (space_id, name, org_id, page_id, version, title)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (space_id, name) DO UPDATE
			SET page_id = EXCLUDED.page_id, version = EXCLUDED.version, title = EXCLUDED.title`,
			spaceID, t.Name, c.org, t.PageID, t.Version, t.Title)
		return err
	})
	return err
}

func (s *dbStore) edition(ctx context.Context, spaceID uuid.UUID) (*Edition, []Tracked, error) {
	var (
		e       = Edition{SpaceID: spaceID}
		tracked []Tracked
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT example_version, example_language FROM space WHERE id = $1 AND example`, spaceID).Scan(&e.Version, &e.Language)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGone
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT name, page_id, version, title FROM example_page WHERE space_id = $1 AND page_id IS NOT NULL`, spaceID)
		if err != nil {
			return err
		}
		tracked, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Tracked, error) {
			var t Tracked
			return t, row.Scan(&t.Name, &t.PageID, &t.Version, &t.Title)
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return &e, tracked, nil
}

func (s *dbStore) discard(ctx context.Context, spaceID uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, false, '') FROM space WHERE id = $1 AND example`, spaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM space WHERE id = $1 AND example`, spaceID)
		return err
	})
	return err
}
