package collab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresChannel is the channel api processes speak on without Valkey.
const PostgresChannel = "stator_collab"

const (
	// postgresNotifyLimit is the most a notification's payload may hold.
	postgresNotifyLimit = 8000
	// MaxNotifyBytes is the largest payload sent, with room to spare below
	// postgresNotifyLimit, so that limit is never met at run time.
	MaxNotifyBytes = 7000
	// ListenPing is how long a quiet listener waits before asking whether
	// its connection still stands, which a lost one would never say.
	ListenPing = 30 * time.Second
	// ListenBackoff is the first wait before connecting the listener again,
	// doubled after each failure up to ListenBackoffMax.
	ListenBackoff    = 500 * time.Millisecond
	ListenBackoffMax = 30 * time.Second
)

// notifySQL sends a notification; pg_notify takes the channel as a value,
// where NOTIFY would want it spliced in.
const notifySQL = `SELECT pg_notify($1, $2)`

// PostgresBus passes frames between api processes over Postgres's LISTEN
// and NOTIFY. An update goes as a pointer to its row, sent in the
// transaction that stores it; anything else goes whole when it fits.
type PostgresBus struct {
	pool    Execer
	dsn     string
	channel string
	log     *slog.Logger
	// Backoff is the first wait before connecting the listener again.
	Backoff time.Duration
}

// NewPostgresBus publishes through pool and listens on a connection of its
// own to dsn, which must reach the primary every process writes to.
func NewPostgresBus(pool Execer, dsn, channel string, log *slog.Logger) *PostgresBus {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &PostgresBus{pool: pool, dsn: dsn, channel: channel, log: log, Backoff: ListenBackoff}
}

// payload is a page and an envelope as the text a notification carries.
func (b *PostgresBus) payload(page uuid.UUID, envelope []byte) (string, error) {
	raw := append(append(make([]byte, 0, len(page)+len(envelope)), page[:]...), envelope...)
	out := base64.StdEncoding.EncodeToString(raw)
	if len(out) > MaxNotifyBytes {
		return "", ErrTooLarge
	}
	return out, nil
}

func parsePayload(payload string) (uuid.UUID, []byte, bool) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) < len(uuid.UUID{}) {
		return uuid.Nil, nil, false
	}
	page, _ := uuid.FromBytes(raw[:len(uuid.UUID{})])
	return page, raw[len(uuid.UUID{}):], true
}

func (b *PostgresBus) Publish(ctx context.Context, page uuid.UUID, envelope []byte) error {
	payload, err := b.payload(page, envelope)
	if err != nil {
		return err
	}
	_, err = b.pool.Exec(ctx, notifySQL, b.channel, payload)
	return err
}

// Announce sends the envelope in the transaction tx, whose commit delivers
// it, so a receiver never reads for a row that is not yet there.
func (b *PostgresBus) Announce(ctx context.Context, tx Execer, page uuid.UUID, envelope []byte) error {
	payload, err := b.payload(page, envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, notifySQL, b.channel, payload)
	return err
}

// Listen hands every notification on the channel to the hub until ctx ends,
// connecting again after a loss and then catching the hub up. It returns
// once the first LISTEN stands, so nothing is served before it.
func (b *PostgresBus) Listen(ctx context.Context, hub *Hub) error {
	conn, err := b.listen(ctx)
	if err != nil {
		return err
	}
	go b.run(ctx, conn, hub)
	return nil
}

func (b *PostgresBus) listen(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, b.dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to listen for the shared drafts' changes: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{b.channel}.Sanitize()); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("listen for the shared drafts' changes: %w", err)
	}
	return conn, nil
}

func (b *PostgresBus) run(ctx context.Context, conn *pgx.Conn, hub *Hub) {
	for {
		err := b.receive(ctx, conn, hub)
		_ = conn.Close(context.WithoutCancel(ctx))
		if ctx.Err() != nil {
			return
		}
		b.log.Warn("lost the connection that hears the other api processes' shared draft changes; connecting again", "error", err)
		wait := b.Backoff
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if conn, err = b.listen(ctx); err == nil {
				break
			}
			b.log.Warn("could not listen for the other api processes' shared draft changes", "error", err, "retry_in", wait)
			wait = min(wait*2, ListenBackoffMax)
		}
		hub.CatchUp()
	}
}

func (b *PostgresBus) receive(ctx context.Context, conn *pgx.Conn, hub *Hub) error {
	for {
		wctx, cancel := context.WithTimeout(ctx, ListenPing)
		n, err := conn.WaitForNotification(wctx)
		cancel()
		switch {
		case err == nil:
			if page, envelope, ok := parsePayload(n.Payload); ok {
				hub.Receive(page, envelope)
			}
		case ctx.Err() != nil:
			return ctx.Err()
		case pgconn.Timeout(err) || errors.Is(err, context.DeadlineExceeded):
			pctx, cancel := context.WithTimeout(ctx, BusTimeout)
			err = conn.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
}
