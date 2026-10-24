package oauth

import (
	"context"
	"log/slog"
	"time"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// The sweep, adapted from Exciter's backend/internal/oauth/sweep.go. Every
// read checks expiry itself, so nothing waits on it.

// SweepInterval is how often the worker removes what has run out.
const SweepInterval = 10 * time.Minute

// Sweeper removes used and expired codes, apps nobody allowed in time and
// refresh tokens rotated longer ago than any could still be presented.
type Sweeper struct {
	db       *db.Cluster
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewSweeper returns the sweep the worker runs.
func NewSweeper(cluster *db.Cluster, log *slog.Logger) *Sweeper {
	return &Sweeper{db: cluster, log: log, interval: SweepInterval, now: time.Now}
}

// Run sweeps on start and then every interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if err := s.Once(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("what OAuth no longer needs could not all be removed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once sweeps once, as the admin role, since nobody is signed in to these rows.
func (s *Sweeper) Once(ctx context.Context) error {
	now := s.now()
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A used code is kept a while, so presenting it again still ends its grant.
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_code WHERE expires_at < $1`, now.Add(-CodeKeep)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_rotated_refresh WHERE rotated_at < $1`, now.Add(-RefreshTokenTTL)); err != nil {
			return err
		}
		return deletePending(ctx, tx, now)
	})
	return err
}
