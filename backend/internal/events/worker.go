package events

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/observability"
)

const (
	// DefaultBatch is how many events one pass claims.
	DefaultBatch = 20
	// DefaultIdle is how long the worker waits when nothing is waiting.
	DefaultIdle = time.Second
	// MaxAttempts is how often an event is tried before it is marked done
	// with its last error, so one bad event cannot block a tenant.
	MaxAttempts = 10
	// retryStep is how much longer each failed attempt waits than the last.
	retryStep = 5 * time.Second
	// Retention is how long a done event is kept, to look into what went out.
	Retention = 7 * 24 * time.Hour
	// pruneEvery is how often the done events past Retention are deleted.
	pruneEvery = time.Hour
)

// Handler acts on one event, at least once, so a second run with the same
// event must change nothing.
type Handler interface {
	Handle(ctx context.Context, e Event) error
}

// HandlerFunc is a function as a Handler.
type HandlerFunc func(ctx context.Context, e Event) error

func (f HandlerFunc) Handle(ctx context.Context, e Event) error { return f(ctx, e) }

// Worker drains the outbox. Any number may run and restart at any time: an
// event is claimed with SKIP LOCKED in the transaction that marks it done.
type Worker struct {
	db      *db.Cluster
	handler Handler
	log     *slog.Logger
	batch   int
	idle    time.Duration
	pruned  time.Time
}

func NewWorker(cluster *db.Cluster, handler Handler, log *slog.Logger) *Worker {
	return &Worker{db: cluster, handler: handler, log: log, batch: DefaultBatch, idle: DefaultIdle}
}

// WithBatch sets how many events one pass claims.
func (w *Worker) WithBatch(n int) *Worker {
	w.batch = max(n, 1)
	return w
}

// WithIdle sets how long the worker waits when nothing is waiting.
func (w *Worker) WithIdle(d time.Duration) *Worker {
	w.idle = d
	return w
}

// Run drains until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("outbox worker started", "batch", w.batch)
	for {
		n, err := w.Once(ctx)
		if ctx.Err() != nil {
			w.log.Info("outbox worker stopped")
			return
		}
		if err != nil {
			w.log.Error("outbox pass failed", "error", err)
		}
		if time.Since(w.pruned) > pruneEvery {
			w.prune(ctx)
		}
		// A full batch means more is probably waiting, so go straight round.
		if n < w.batch || err != nil {
			select {
			case <-ctx.Done():
				w.log.Info("outbox worker stopped")
				return
			case <-time.After(w.idle):
			}
		}
	}
}

// Once claims up to one batch, hands each event to the handler and marks it
// done, and returns how many it claimed.
func (w *Worker) Once(ctx context.Context) (int, error) {
	claimed := 0
	_, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, org_id, topic, payload, created_at, COALESCE(trace_parent, ''), attempts
			FROM outbox_event
			WHERE processed_at IS NULL AND available_at <= now()
			ORDER BY created_at, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED`, w.batch)
		if err != nil {
			return err
		}
		type claim struct {
			Event
			attempts int
		}
		var batch []claim
		for rows.Next() {
			var c claim
			if err := rows.Scan(&c.ID, &c.OrgID, &c.Topic, &c.Payload, &c.CreatedAt, &c.Trace, &c.attempts); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		claimed = len(batch)
		for _, c := range batch {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if herr := w.handle(ctx, c.Event); herr != nil {
				attempts := c.attempts + 1
				giveUp := attempts >= MaxAttempts
				w.log.Warn("an event could not be delivered yet", "event", c.ID, "topic", c.Topic, "attempt", attempts, "given_up", giveUp, "error", herr)
				if _, err := tx.Exec(ctx, `
					UPDATE outbox_event
					SET attempts = $2, last_error = $3,
					    available_at = now() + make_interval(secs => $4),
					    processed_at = CASE WHEN $5 THEN now() END
					WHERE id = $1`, c.ID, attempts, herr.Error(), float64(attempts)*retryStep.Seconds(), giveUp); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE outbox_event SET processed_at = now(), attempts = attempts + 1 WHERE id = $1`, c.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return claimed, err
}

func (w *Worker) handle(ctx context.Context, e Event) (err error) {
	ctx, span := observability.Tracer().Start(continueTrace(ctx, e), "events "+e.Topic,
		trace.WithAttributes(attribute.String("stator.event_id", e.ID.String()), attribute.String("stator.topic", e.Topic)))
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("the handler panicked")
			w.log.Error("an event's handler panicked", "event", e.ID, "panic", r)
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return w.handler.Handle(ctx, e)
}

// prune deletes done events past Retention.
func (w *Worker) prune(ctx context.Context) {
	w.pruned = time.Now()
	if _, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM outbox_event WHERE processed_at < now() - make_interval(secs => $1)`, Retention.Seconds())
		return err
	}); err != nil && ctx.Err() == nil {
		w.log.Warn("old outbox events could not be pruned", "error", err)
	}
}

// Mux hands each event to the handler of its topic and any other to the
// fallback, so a topic that fails is retried without running the others again.
type Mux struct {
	routes   map[string]Handler
	fallback Handler
}

// NewMux routes every topic not named to fallback, which may be nil.
func NewMux(fallback Handler) *Mux {
	return &Mux{routes: map[string]Handler{}, fallback: fallback}
}

// Route sends a topic's events to h.
func (m *Mux) Route(topic string, h Handler) *Mux {
	m.routes[topic] = h
	return m
}

func (m *Mux) Handle(ctx context.Context, e Event) error {
	if h, ok := m.routes[e.Topic]; ok {
		return h.Handle(ctx, e)
	}
	if m.fallback == nil {
		return nil
	}
	return m.fallback.Handle(ctx, e)
}
