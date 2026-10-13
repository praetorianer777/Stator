package pageview

import (
	"context"
	"log/slog"
	"time"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// Retention moves views older than its window from page_view into each page's
// tally, as stator_admin through page_view_prune.
type Retention struct {
	db       *db.Cluster
	keep     time.Duration
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewRetention prunes views older than keep; zero keeps them forever.
func NewRetention(cluster *db.Cluster, keep time.Duration, log *slog.Logger) *Retention {
	return &Retention{db: cluster, keep: keep, log: log, interval: RetentionInterval, now: time.Now}
}

// Run prunes on start and then every interval until ctx ends.
func (r *Retention) Run(ctx context.Context) {
	if r.keep <= 0 {
		r.log.Info("page views are kept forever: STATOR_RETAIN_PAGE_VIEWS is 0")
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		n, err := r.Once(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Warn("old page views could not all be pruned", "error", err)
		}
		if n > 0 {
			r.log.Info("old page views pruned", "count", n, "keep", r.keep)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Cutoff is the first day a window of keep still holds on the day of now.
func Cutoff(now time.Time, keep time.Duration) time.Time {
	return now.UTC().Add(-keep).Truncate(24 * time.Hour)
}

// Once prunes and says how many views went.
func (r *Retention) Once(ctx context.Context) (int64, error) {
	if r.keep <= 0 {
		return 0, nil
	}
	cutoff := Cutoff(r.now(), r.keep)
	var n int64
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT page_view_prune($1::date)`, cutoff).Scan(&n)
	})
	return n, err
}
