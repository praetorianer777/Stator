// Package db owns the Postgres connection topology: one writable primary and a
// rotation of read replicas, routed so a user's own writes stay visible to them.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/stator/backend/internal/config"
)

// Timeouts for the cluster's own round trips, which must not hang a boot or a
// health pass on a database that has gone quiet.
const (
	pingTimeout        = 5 * time.Second
	healthCheckTimeout = 3 * time.Second
)

// DBTX is the query surface shared by pgxpool.Pool and pgx.Tx, so the same
// repository code runs on the primary or a replica, in a transaction or not.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// replicaState tracks one read replica's health and replay position. Request
// goroutines read it while the health loop writes it, so every field is atomic.
type replicaState struct {
	name string
	pool *pgxpool.Pool

	healthy   atomic.Bool
	replayLSN atomic.Uint64
	lagMillis atomic.Int64
	lastErr   atomic.Pointer[string]
}

func (r *replicaState) snapshot() ReplicaStatus {
	var errStr string
	if p := r.lastErr.Load(); p != nil {
		errStr = *p
	}
	return ReplicaStatus{
		Name:      r.name,
		Healthy:   r.healthy.Load(),
		ReplayLSN: LSN(r.replayLSN.Load()),
		Lag:       time.Duration(r.lagMillis.Load()) * time.Millisecond,
		LastError: errStr,
	}
}

// ReplicaStatus is a point in time view of one replica, for health endpoints.
type ReplicaStatus struct {
	Name      string        `json:"name"`
	Healthy   bool          `json:"healthy"`
	ReplayLSN LSN           `json:"-"`
	Lag       time.Duration `json:"-"`
	LastError string        `json:"lastError,omitempty"`
}

// Cluster is the primary plus its replica rotation.
type Cluster struct {
	primary  *pgxpool.Pool
	admin    *pgxpool.Pool
	replicas []*replicaState

	maxLag  time.Duration
	samples int
	rr      atomic.Uint64
	log     *slog.Logger
	stop    context.CancelFunc
	stopped chan struct{}

	readsToPrimary   atomic.Uint64
	readsToReplica   atomic.Uint64
	staleFallbacks   atomic.Uint64
	lagFallbacks     atomic.Uint64
	noHealthyReplica atomic.Uint64
}

// Option adjusts how the cluster is opened.
type Option func(*options)

type options struct {
	tracer func(pool string) pgx.QueryTracer
}

// WithQueryTracer gives every pool a tracer, made per pool so a span can say
// which one it ran on. The pools are named primary, admin and replica-<n>.
func WithQueryTracer(make func(pool string) pgx.QueryTracer) Option {
	return func(o *options) { o.tracer = make }
}

// Open connects the primary and every configured replica, then starts the
// health loop. The caller must Close the returned cluster.
func Open(ctx context.Context, cfg config.DB, log *slog.Logger, opts ...Option) (*Cluster, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	tracerFor := func(pool string) pgx.QueryTracer {
		if o.tracer == nil {
			return nil
		}
		return o.tracer(pool)
	}

	primary, err := openPool(ctx, cfg, cfg.PrimaryURL, tracerFor("primary"))
	if err != nil {
		return nil, fmt.Errorf("connect to the primary database: %w", err)
	}

	admin := primary
	if cfg.AdminURL != "" && cfg.AdminURL != cfg.PrimaryURL {
		admin, err = openPool(ctx, cfg, cfg.AdminURL, tracerFor("admin"))
		if err != nil {
			primary.Close()
			return nil, fmt.Errorf("connect to the database as the admin role: %w", err)
		}
	}

	c := &Cluster{
		primary: primary,
		admin:   admin,
		maxLag:  cfg.MaxReplicaLag,
		samples: max(cfg.ReplicaLagSamples, 1),
		log:     log,
		stopped: make(chan struct{}),
	}
	// Start the rotation at a random offset so that many api processes booting
	// at once do not all send their first read to the same replica.
	c.rr.Store(rand.Uint64())

	for i, url := range cfg.ReplicaURLs {
		name := fmt.Sprintf("replica-%d", i)
		pool, err := openPool(ctx, cfg, url, tracerFor(name))
		if err != nil {
			// A replica that is down at boot must not stop the process: the
			// health loop will pick it up when it returns.
			log.Warn("replica unavailable at startup", "replica", i, "error", err)
			continue
		}
		c.replicas = append(c.replicas, &replicaState{name: name, pool: pool})
	}

	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.stop = cancel
	c.checkAll(loopCtx) // seed health before serving the first request
	go c.healthLoop(loopCtx, cfg.HealthInterval)

	log.Info("database cluster ready", "replicas", len(c.replicas), "max_lag", cfg.MaxReplicaLag)
	return c, nil
}

func openPool(ctx context.Context, cfg config.DB, url string, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime
	if tracer != nil {
		pcfg.ConnConfig.Tracer = tracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Close shuts down the health loop and every pool.
func (c *Cluster) Close() {
	if c.stop != nil {
		c.stop()
		<-c.stopped
	}
	for _, r := range c.replicas {
		r.pool.Close()
	}
	if c.admin != c.primary {
		c.admin.Close()
	}
	c.primary.Close()
}

// Primary returns the writable pool. Prefer Write for anything that mutates.
func (c *Cluster) Primary() *pgxpool.Pool { return c.primary }

// readConn is a replica connection fit to serve a read made with ctx, or nil
// for the primary.
func (c *Cluster) readConn(ctx context.Context) *pgxpool.Conn {
	// Fitness is asked of the connection itself: behind a load balanced read
	// service two connections of one pool can reach replicas at different
	// positions, so no figure about the pool holds for all of them.
	p := pinFrom(ctx)
	r, outcome := pickReplica(c.replicas, int(c.rr.Add(1)), p)
	if outcome != routeReplica {
		c.countFallback(outcome)
		return nil
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		c.countFallback(routeNoHealthy)
		return nil
	}
	s, err := sampleConn(ctx, conn)
	if err != nil {
		conn.Release()
		c.countFallback(routeNoHealthy)
		return nil
	}
	if outcome := admit(s, p, c.maxLag); outcome != routeReplica {
		conn.Release()
		c.countFallback(outcome)
		return nil
	}
	c.readsToReplica.Add(1)
	return conn
}

func (c *Cluster) countFallback(outcome routeOutcome) {
	switch outcome {
	case routeStale:
		c.staleFallbacks.Add(1)
	case routeLagging:
		c.lagFallbacks.Add(1)
	case routeNoHealthy:
		c.noHealthyReplica.Add(1)
	}
	c.readsToPrimary.Add(1)
}

// routeOutcome explains why a read was routed where it was.
type routeOutcome int

const (
	routeReplica   routeOutcome = iota // served by a replica
	routePinned                        // caller demanded the primary
	routeStale                         // the replica has not replayed the caller's own write
	routeLagging                       // the replica is further behind than allowed
	routeNoHealthy                     // no replica is currently usable
)

// pickReplica chooses the replica to try for a read, or says why none can
// serve it. Freshness is judged later, on the connection the read gets.
func pickReplica(replicas []*replicaState, start int, p pin) (*replicaState, routeOutcome) {
	if p.forcePrimary {
		return nil, routePinned
	}
	n := len(replicas)
	if start < 0 {
		start = -start
	}
	for i := 0; i < n; i++ {
		if r := replicas[(start+i)%n]; r.healthy.Load() {
			return r, routeReplica
		}
	}
	return nil, routeNoHealthy
}

// connSample is what one connection says about the node it reached.
type connSample struct {
	inRecovery bool
	replayLSN  LSN
	lag        time.Duration
	receiver   string
}

// admit decides whether a replica connection may serve a read. Falling back
// to the primary is always correct, only more expensive.
func admit(s connSample, p pin, maxLag time.Duration) routeOutcome {
	if !s.inRecovery {
		// Promoted since it joined the pool: it has everything it ever saw.
		return routeReplica
	}
	if p.requiredLSN != 0 && s.replayLSN < p.requiredLSN {
		return routeStale
	}
	// A standby whose receiver has dropped reports zero lag forever, because
	// it has replayed everything it received. An empty status means this role
	// may not read the view, so the lag figure is trusted instead.
	if s.receiver != "" && s.receiver != "streaming" {
		return routeLagging
	}
	if maxLag > 0 && s.lag > maxLag {
		return routeLagging
	}
	return routeReplica
}

func (s connSample) reason(maxLag time.Duration) string {
	if s.receiver != "" && s.receiver != "streaming" {
		return fmt.Sprintf("wal receiver is %q, not streaming", s.receiver)
	}
	return fmt.Sprintf("replication lag %s exceeds %s", s.lag, maxLag)
}

func (c *Cluster) healthLoop(ctx context.Context, interval time.Duration) {
	defer close(c.stopped)
	if interval <= 0 {
		interval = config.DefaultHealthInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.checkAll(ctx)
		}
	}
}

func (c *Cluster) checkAll(ctx context.Context) {
	for _, r := range c.replicas {
		c.check(ctx, r)
	}
}

// replicaHealthSQL's CASE arms avoid functions that fail on the other kind of node;
// lag is zero once all received WAL is replayed, since an idle standby's clock stops.
const replicaHealthSQL = `
SELECT pg_is_in_recovery(),
       CASE WHEN pg_is_in_recovery()
            THEN COALESCE(pg_last_wal_replay_lsn(), '0/0'::pg_lsn)
            ELSE pg_current_wal_lsn()
       END::text,
       CASE
            WHEN NOT pg_is_in_recovery() THEN 0
            WHEN pg_last_wal_receive_lsn() IS NULL THEN 0
            WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
            ELSE GREATEST(0, EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())))
       END,
       COALESCE((SELECT status FROM pg_stat_wal_receiver LIMIT 1), '')`

// sampleConn asks one connection where its node stands.
func sampleConn(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (connSample, error) {
	var (
		s          connSample
		lsnText    string
		lagSeconds float64
	)
	if err := q.QueryRow(ctx, replicaHealthSQL).Scan(&s.inRecovery, &lsnText, &lagSeconds, &s.receiver); err != nil {
		return s, err
	}
	lsn, err := ParseLSN(lsnText)
	if err != nil {
		return s, err
	}
	s.replayLSN = lsn
	// Clock skew between primary and standby can make the figure negative.
	s.lag = max(time.Duration(lagSeconds*float64(time.Second)), 0)
	return s, nil
}

// check samples several connections of a replica's pool at once, so behind a
// load balanced service it sees more than one replica.
func (c *Cluster) check(ctx context.Context, r *replicaState) {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// The pool leaves the rotation only when no sample is fit to serve, since
	// every read checks its own connection anyway; it reports the worst figures.

	n := min(c.samples, int(r.pool.Config().MaxConns))
	conns := make([]*pgxpool.Conn, 0, n)
	defer func() {
		for _, conn := range conns {
			conn.Release()
		}
	}()
	var lastErr error
	for range n {
		conn, err := r.pool.Acquire(ctx)
		if err != nil {
			lastErr = err
			break
		}
		conns = append(conns, conn)
	}

	var (
		fit, sampled bool
		worst        connSample
		reason       string
	)
	for _, conn := range conns {
		s, err := sampleConn(ctx, conn)
		if err != nil {
			lastErr = err
			continue
		}
		if !sampled || s.replayLSN < worst.replayLSN {
			worst.replayLSN = s.replayLSN
		}
		worst.lag = max(worst.lag, s.lag)
		sampled = true
		if admit(s, pin{}, c.maxLag) == routeReplica {
			fit = true
		} else {
			reason = s.reason(c.maxLag)
		}
	}
	if !sampled {
		msg := "no connection could be sampled"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		c.markUnhealthy(r, msg)
		return
	}
	r.replayLSN.Store(uint64(worst.replayLSN))
	r.lagMillis.Store(worst.lag.Milliseconds())
	if !fit {
		c.markUnhealthy(r, reason)
		return
	}
	if was := r.healthy.Swap(true); !was {
		r.lastErr.Store(nil)
		c.log.Info("replica healthy", "replica", r.name, "lag", worst.lag)
	}
}

func (c *Cluster) markUnhealthy(r *replicaState, reason string) {
	r.lastErr.Store(&reason)
	if was := r.healthy.Swap(false); was {
		c.log.Warn("replica unhealthy", "replica", r.name, "reason", reason)
	}
}

// Stats reports routing counters and replica health for the metrics endpoint.
type Stats struct {
	Replicas            []ReplicaStatus `json:"replicas"`
	ReadsToPrimary      uint64          `json:"readsToPrimary"`
	ReadsToReplica      uint64          `json:"readsToReplica"`
	StaleFallbacks      uint64          `json:"staleFallbacks"`
	LagFallbacks        uint64          `json:"lagFallbacks"`
	NoHealthyReplicaHit uint64          `json:"noHealthyReplicaHits"`
	// Pools is how full each connection pool is, which is what a saturated
	// api looks like from the outside.
	Pools []PoolStats `json:"-"`
}

// PoolStats is a point in time count of one pool's connections.
type PoolStats struct {
	Name         string
	Idle         int32
	Acquired     int32
	Constructing int32
	Max          int32
}

// Stats is a snapshot of the routing counters, replica health and pool sizes.
func (c *Cluster) Stats() Stats {
	s := Stats{
		ReadsToPrimary:      c.readsToPrimary.Load(),
		ReadsToReplica:      c.readsToReplica.Load(),
		StaleFallbacks:      c.staleFallbacks.Load(),
		LagFallbacks:        c.lagFallbacks.Load(),
		NoHealthyReplicaHit: c.noHealthyReplica.Load(),
	}
	type named struct {
		name string
		pool *pgxpool.Pool
	}
	pools := []named{{"primary", c.primary}}
	if c.admin != c.primary {
		pools = append(pools, named{"admin", c.admin})
	}
	for _, r := range c.replicas {
		s.Replicas = append(s.Replicas, r.snapshot())
		pools = append(pools, named{r.name, r.pool})
	}
	for _, p := range pools {
		stat := p.pool.Stat()
		s.Pools = append(s.Pools, PoolStats{Name: p.name, Idle: stat.IdleConns(), Acquired: stat.AcquiredConns(), Constructing: stat.ConstructingConns(), Max: stat.MaxConns()})
	}
	return s
}

// ErrPrivilegedRole is returned when the application connects as a role that
// row level security does not apply to.
var ErrPrivilegedRole = errors.New("the database connection uses a role that bypasses row level security; connect as stator_app instead")

// RefuseSuperuser fails when this connects as a role that ignores row level
// security, which would make every policy in the schema decoration.
func (c *Cluster) RefuseSuperuser(ctx context.Context) error {
	var privileged bool
	err := c.primary.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&privileged)
	if err != nil {
		return fmt.Errorf("ask which role this connects as: %w", err)
	}
	if privileged {
		return ErrPrivilegedRole
	}
	return nil
}

// Ping verifies the primary is reachable.
func (c *Cluster) Ping(ctx context.Context) error {
	if err := c.primary.Ping(ctx); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	return nil
}
