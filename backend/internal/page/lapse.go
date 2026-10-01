package page

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// DefaultLapseInterval is how often the worker looks for verifications
	// that ran out.
	DefaultLapseInterval = 10 * time.Minute
	// lapseBatch is how many lapses one look takes before the next.
	lapseBatch = 500
)

// LapseWatch is the worker's half of verification: a term runs out while
// nobody makes a request, so it looks for lapses and has the owner told.
type LapseWatch struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
}

func NewLapseWatch(cluster *db.Cluster, log *slog.Logger, interval time.Duration) *LapseWatch {
	if interval <= 0 {
		interval = DefaultLapseInterval
	}
	return &LapseWatch{db: cluster, log: log, interval: interval}
}

// Run looks every interval until the context ends.
func (w *LapseWatch) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		for {
			n, err := w.Once(ctx)
			if err != nil {
				w.log.Warn("the verification watch failed", "error", err)
				break
			}
			if n > 0 {
				w.log.Info("verifications lapsed", "count", n)
			}
			if n < lapseBatch {
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

// Once notes up to one batch of lapsed verifications across every
// organization, each with an event for its owner, and says how many.
func (w *LapseWatch) Once(ctx context.Context) (int, error) {
	type due struct{ orgID, pageID uuid.UUID }
	var found []due
	// Finding them crosses tenants, which is what the admin role is for.
	err := w.db.ReadAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT org_id, page_id FROM page_verification
			WHERE lapse_noticed_at IS NULL AND expires_at <= now()
			ORDER BY expires_at LIMIT $1`, lapseBatch)
		if err != nil {
			return err
		}
		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
			var d due
			return d, row.Scan(&d.orgID, &d.pageID)
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	noted := 0
	for _, d := range found {
		org := tenant.WithOrg(db.PinPrimary(ctx), tenant.Org{ID: d.orgID})
		happened := false
		_, err := w.db.WriteAdmin(org, func(ctx context.Context, tx db.DBTX) error {
			var expires time.Time
			// A second worker, or a verification renewed meanwhile, finds no row.
			err := tx.QueryRow(ctx, `
				UPDATE page_verification SET lapse_noticed_at = now()
				WHERE org_id = current_org_id() AND page_id = $1 AND lapse_noticed_at IS NULL AND expires_at <= now()
				RETURNING expires_at`, d.pageID).Scan(&expires)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			happened = true
			return events.Emit(ctx, tx, events.TopicVerificationLapsed, events.VerificationLapsed{PageID: d.pageID, ExpiresAt: expires})
		})
		if err != nil {
			w.log.Warn("a lapsed verification could not be noted", "page", d.pageID, "error", err)
			continue
		}
		if happened {
			noted++
		}
	}
	return noted, nil
}
