package spaceio

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// SiteRunLimit bounds one export or import of a whole organization.
	SiteRunLimit = 4 * time.Hour
	// SiteLease is how long a worker holds an organization's job, past every run.
	SiteLease = SiteRunLimit + 5*time.Minute
)

// siteClaim is an organization's export or import a worker took.
type siteClaim struct {
	kind     kind
	id       uuid.UUID
	user     uuid.UUID
	role     auth.Role
	attempts int
	size     int64
	object   string
}

func (c *siteClaim) actor() perm.Actor { return perm.Actor{UserID: c.user, Role: c.role} }

func (c *siteClaim) acting(ctx context.Context) context.Context {
	return db.WithUser(db.PinPrimary(ctx), c.user)
}

func (w *Watch) claimSite(ctx context.Context, k kind) (*siteClaim, error) {
	var c *siteClaim
	size := `0::bigint`
	if k == kindSiteImport {
		size = `j.size_bytes`
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		out := siteClaim{kind: k}
		var role string
		// Materialized for the reason claim gives.
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
			RETURNING j.id, j.requested_by,
			          COALESCE((SELECT m.role FROM member m WHERE m.user_id = j.requested_by), ''),
			          j.attempts, j.object_key, %[2]s`, k, size), SiteLease.Seconds()).
			Scan(&out.id, &out.user, &role, &out.attempts, &out.object, &out.size)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.role = auth.Role(role)
		c = &out
		return nil
	})
	return c, err
}

// mayMove asks, as the requester, whether they still administer the organization.
func (w *Watch) mayMove(ctx context.Context, c *siteClaim) (bool, error) {
	if c.role == "" {
		return false, nil
	}
	allowed := false
	err := w.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		allowed = perm.Allowed(ctx, tx, c.actor(), perm.MoveSite, uuid.Nil)
		return nil
	})
	return allowed, err
}

func (w *Watch) runSiteExport(ctx context.Context, c *siteClaim) error {
	if c.attempts > MaxAttempts {
		return w.finishSiteExport(ctx, c, StateFailed, FailureFailed, "", "", 0)
	}
	run, cancel := context.WithTimeout(c.acting(ctx), SiteRunLimit)
	defer cancel()
	allowed, err := w.mayMove(run, c)
	if err != nil {
		return err
	}
	if !allowed {
		return w.finishSiteExport(ctx, c, StateFailed, FailureForbidden, "", "", 0)
	}
	began := time.Now()
	name, size, err := w.exportSite(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("organization exported", "job", c.id, "bytes", size, "took", time.Since(began).Round(time.Millisecond))
		return w.finishSiteExport(settle, c, StateDone, "", "", name, size)
	case ctx.Err() != nil:
		return w.finishSiteExport(settle, c, StateQueued, "", "", "", 0)
	}
	failure, detail := FailureFailed, ""
	var big *siteSpaceTooLarge
	switch {
	case isDenied(err):
		failure = FailureForbidden
	case errors.As(err, &big):
		failure, detail = FailureTooLarge, big.Error()
	}
	w.log.Warn("the organization was not exported", "job", c.id, "failure", failure, "error", err)
	return w.finishSiteExport(settle, c, StateFailed, failure, detail, "", 0)
}

func (w *Watch) finishSiteExport(ctx context.Context, c *siteClaim, state State, failure Failure, detail, name string, size int64) error {
	var (
		f       *Failure
		d       *string
		file    *string
		bytes   *int64
		expires *time.Time
	)
	if state == StateFailed {
		f = &failure
		if detail != "" {
			d = &detail
		}
	}
	if state == StateDone {
		at := time.Now().Add(w.ttl)
		file, bytes, expires = &name, &size, &at
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE site_export
			SET state = $3, failure = $4, detail = $5, file_name = $6, size_bytes = $7, expires_at = $8, lease_until = NULL,
			    done_steps = CASE WHEN $3 = 'done' THEN total_steps ELSE done_steps END,
			    finished_at = CASE WHEN $3 IN ('done', 'failed') THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), f, d, file, bytes, expires)
		if err != nil {
			return fmt.Errorf("record how the export ended: %w", err)
		}
		return nil
	})
	return err
}

func (w *Watch) runSiteImport(ctx context.Context, c *siteClaim) error {
	if c.attempts > MaxAttempts {
		return w.finishSiteImport(ctx, c, StateFailed, FailureFailed, "", 0)
	}
	run, cancel := context.WithTimeout(c.acting(ctx), SiteRunLimit)
	defer cancel()
	allowed, err := w.mayMove(run, c)
	if err != nil {
		return err
	}
	if !allowed {
		return w.finishSiteImport(ctx, c, StateFailed, FailureForbidden, "", 0)
	}
	began := time.Now()
	written, err := w.importSite(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("organization imported", "job", c.id, "took", time.Since(began).Round(time.Millisecond))
		return w.finishSiteImport(settle, c, StateDone, "", "", written)
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		return w.finishSiteImport(settle, c, StateQueued, "", "", 0)
	}
	failure, detail := siteFailureOf(err)
	w.log.Warn("the organization was not imported", "job", c.id, "failure", failure, "error", err)
	return w.finishSiteImport(settle, c, StateFailed, failure, detail, 0)
}

// finishSiteImport ends an import that failed or was put back; one that
// succeeded is marked done in its own transaction and here only records
// where its writes ended. Either way the uploaded archive is let go.
func (w *Watch) finishSiteImport(ctx context.Context, c *siteClaim, state State, failure Failure, detail string, written db.LSN) error {
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if state == StateDone {
			if written != 0 {
				if _, err := tx.Exec(ctx, `UPDATE site_import SET written_lsn = $2::pg_lsn WHERE id = $1`, c.id, written.String()); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO attachment_tombstone (object_key) VALUES ($1) ON CONFLICT (object_key) DO NOTHING`, c.object)
			return err
		}
		var (
			f *Failure
			d *string
		)
		if state == StateFailed {
			f = &failure
			if detail != "" {
				d = &detail
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE site_import
			SET state = $3, failure = $4, detail = $5, lease_until = NULL,
			    finished_at = CASE WHEN $3 = 'failed' THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), f, d)
		if err != nil {
			return fmt.Errorf("record how the import ended: %w", err)
		}
		if tag.RowsAffected() > 0 && state == StateFailed {
			_, err = tx.Exec(ctx, `
				INSERT INTO attachment_tombstone (object_key) VALUES ($1) ON CONFLICT (object_key) DO NOTHING`, c.object)
		}
		return err
	})
	return err
}
