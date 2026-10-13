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
	// OrgRunLimit bounds one export or import of a whole organization.
	OrgRunLimit = 4 * time.Hour
	// OrgLease is how long a worker holds an organization's job, past every run.
	OrgLease = OrgRunLimit + 5*time.Minute
)

// orgClaim is an organization's export or import a worker took.
type orgClaim struct {
	kind     kind
	id, org  uuid.UUID
	slug     string
	user     uuid.UUID
	role     auth.OrgRole
	attempts int
	size     int64
	object   string
}

func (c *orgClaim) actor() perm.Actor { return perm.Actor{UserID: c.user, Role: c.role} }

func (c *orgClaim) acting(ctx context.Context) context.Context {
	return db.WithUser(db.PinPrimary(ctx), c.user)
}

func (w *Watch) claimOrg(ctx context.Context, k kind) (*orgClaim, error) {
	var c *orgClaim
	size := `0::bigint`
	if k == kindOrgImport {
		size = `j.size_bytes`
	}
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		out := orgClaim{kind: k}
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
			RETURNING j.id, j.org_id, (SELECT slug FROM org WHERE id = j.org_id), j.requested_by,
			          COALESCE((SELECT org_role FROM org_member m WHERE m.org_id = j.org_id AND m.user_id = j.requested_by), ''),
			          j.attempts, j.object_key, %[2]s`, k, size), OrgLease.Seconds()).
			Scan(&out.id, &out.org, &out.slug, &out.user, &role, &out.attempts, &out.object, &out.size)
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

// mayMove asks, as the requester, whether they still administer the organization.
func (w *Watch) mayMove(ctx context.Context, c *orgClaim) (bool, error) {
	if c.role == "" {
		return false, nil
	}
	allowed := false
	err := w.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		allowed = perm.Allowed(ctx, tx, c.actor(), perm.MoveOrg, uuid.Nil)
		return nil
	})
	return allowed, err
}

func (w *Watch) runOrgExport(ctx context.Context, c *orgClaim) error {
	if c.attempts > MaxAttempts {
		return w.finishOrgExport(ctx, c, StateFailed, FailureFailed, "", "", 0)
	}
	run, cancel := context.WithTimeout(c.acting(ctx), OrgRunLimit)
	defer cancel()
	allowed, err := w.mayMove(run, c)
	if err != nil {
		return err
	}
	if !allowed {
		return w.finishOrgExport(ctx, c, StateFailed, FailureForbidden, "", "", 0)
	}
	began := time.Now()
	name, size, err := w.exportOrg(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("organization exported", "org", c.org, "job", c.id, "bytes", size, "took", time.Since(began).Round(time.Millisecond))
		return w.finishOrgExport(settle, c, StateDone, "", "", name, size)
	case ctx.Err() != nil:
		return w.finishOrgExport(settle, c, StateQueued, "", "", "", 0)
	}
	failure, detail := FailureFailed, ""
	var big *orgSpaceTooLarge
	switch {
	case isDenied(err):
		failure = FailureForbidden
	case errors.As(err, &big):
		failure, detail = FailureTooLarge, big.Error()
	}
	w.log.Warn("the organization was not exported", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.finishOrgExport(settle, c, StateFailed, failure, detail, "", 0)
}

func (w *Watch) finishOrgExport(ctx context.Context, c *orgClaim, state State, failure Failure, detail, name string, size int64) error {
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
			UPDATE org_export
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

func (w *Watch) runOrgImport(ctx context.Context, c *orgClaim) error {
	if c.attempts > MaxAttempts {
		return w.finishOrgImport(ctx, c, StateFailed, FailureFailed, "", 0)
	}
	run, cancel := context.WithTimeout(c.acting(ctx), OrgRunLimit)
	defer cancel()
	allowed, err := w.mayMove(run, c)
	if err != nil {
		return err
	}
	if !allowed {
		return w.finishOrgImport(ctx, c, StateFailed, FailureForbidden, "", 0)
	}
	began := time.Now()
	written, err := w.importOrg(run, c)
	settle := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		w.log.Info("organization imported", "org", c.org, "job", c.id, "took", time.Since(began).Round(time.Millisecond))
		return w.finishOrgImport(settle, c, StateDone, "", "", written)
	case errors.Is(err, errLeaseLost):
		return err
	case ctx.Err() != nil:
		return w.finishOrgImport(settle, c, StateQueued, "", "", 0)
	}
	failure, detail := orgFailureOf(err)
	w.log.Warn("the organization was not imported", "org", c.org, "job", c.id, "failure", failure, "error", err)
	return w.finishOrgImport(settle, c, StateFailed, failure, detail, 0)
}

// finishOrgImport ends an import that failed or was put back; one that
// succeeded is marked done in its own transaction and here only records
// where its writes ended. Either way the uploaded archive is let go.
func (w *Watch) finishOrgImport(ctx context.Context, c *orgClaim, state State, failure Failure, detail string, written db.LSN) error {
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if state == StateDone {
			if written != 0 {
				if _, err := tx.Exec(ctx, `UPDATE org_import SET written_lsn = $2::pg_lsn WHERE id = $1`, c.id, written.String()); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, $2) ON CONFLICT (object_key) DO NOTHING`, c.object, c.org)
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
			UPDATE org_import
			SET state = $3, failure = $4, detail = $5, lease_until = NULL,
			    finished_at = CASE WHEN $3 = 'failed' THEN now() END
			WHERE id = $1 AND state = 'running' AND attempts = $2`,
			c.id, c.attempts, string(state), f, d)
		if err != nil {
			return fmt.Errorf("record how the import ended: %w", err)
		}
		if tag.RowsAffected() > 0 && state == StateFailed {
			_, err = tx.Exec(ctx, `
				INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, $2) ON CONFLICT (object_key) DO NOTHING`, c.object, c.org)
		}
		return err
	})
	return err
}
