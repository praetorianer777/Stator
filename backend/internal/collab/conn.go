package collab

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Close codes past the standard ones, which the browser reads to decide
// whether to load again or stop and say why. The web client uses the same.
const (
	CloseSignedOut websocket.StatusCode = 4401
	CloseRefused   websocket.StatusCode = 4403
	CloseSlow      websocket.StatusCode = 4408
	CloseGone      websocket.StatusCode = 4409
	CloseRestart                        = websocket.StatusServiceRestart
)

// A close frame's reason holds 123 bytes, so these stay short.
const (
	closeSignedOutReason = "Your session ended. Sign in again to keep editing."
	closeRefusedReason   = "You may no longer edit this page."
	closeSlowReason      = "The connection fell behind; it loads the draft again."
	closeGoneReason      = "The shared draft was started afresh; it loads again."
	closeRestartReason   = "The server is restarting; your changes are kept."
	closeInternalReason  = "Something went wrong on the server; it reconnects."
)

// pending is a frame that arrived before the connection knew its room.
type pending struct {
	room  uuid.UUID
	frame []byte
}

// Conn is one browser's connection to a page's shared draft.
type Conn struct {
	hub  *Hub
	page uuid.UUID
	send chan []byte

	mu      sync.Mutex
	room    uuid.UUID
	loading bool
	pending []pending
	ws      *websocket.Conn
	failed  bool
	code    websocket.StatusCode
	reason  string
	clients map[uint64]struct{}
}

func newConn(h *Hub, page uuid.UUID) *Conn {
	return &Conn{hub: h, page: page, send: make(chan []byte, h.opts.SendQueue),
		loading: true, clients: map[uint64]struct{}{}}
}

func (c *Conn) roomID() uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.room
}

// Leave counts out a connection that never served, as when its room could
// not be opened.
func (c *Conn) Leave() { c.hub.leave(c) }

// enqueue queues a frame of a room for the browser, holds it while the
// connection loads, and lets a browser go that cannot keep up.
func (c *Conn) enqueue(room uuid.UUID, frame []byte) {
	c.mu.Lock()
	if c.loading {
		c.pending = append(c.pending, pending{room, frame})
		c.mu.Unlock()
		return
	}
	mine := room == c.room
	c.mu.Unlock()
	if !mine {
		return
	}
	select {
	case c.send <- frame:
	default:
		c.fail(CloseSlow, closeSlowReason)
	}
}

// fail closes the connection with a code and a reason, once, without waiting.
func (c *Conn) fail(code websocket.StatusCode, reason string) {
	c.mu.Lock()
	if c.failed {
		c.mu.Unlock()
		return
	}
	c.failed, c.code, c.reason = true, code, reason
	ws := c.ws
	c.mu.Unlock()
	// The reader goes on until the browser answers the close, so the
	// browser gets the code before the connection ends.
	if ws != nil {
		go func() { _ = ws.Close(code, reason) }()
	}
}

// failFor closes the connection the way a store's refusal asks.
func (c *Conn) failFor(err error) {
	switch {
	case errors.Is(err, ErrSignedOut):
		c.fail(CloseSignedOut, closeSignedOutReason)
	case errors.Is(err, ErrRefused):
		c.fail(CloseRefused, closeRefusedReason)
	case errors.Is(err, ErrGone):
		c.fail(CloseGone, closeGoneReason)
	default:
		c.hub.log.Error("a shared draft's connection failed", "page", c.page, "error", err)
		c.fail(websocket.StatusInternalError, closeInternalReason)
	}
}

// Serve speaks to the browser until either side goes: it sends the room as
// opened, then relays what each side sends.
func (c *Conn) Serve(ctx context.Context, ws *websocket.Conn, room Room, store Store) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.mu.Lock()
	c.ws, c.room = ws, room.ID
	failed, code, reason := c.failed, c.code, c.reason
	c.mu.Unlock()
	defer c.hub.leave(c)
	defer func() { _ = ws.CloseNow() }()
	if failed {
		_ = ws.Close(code, reason)
		return
	}
	if err := c.load(ctx, ws, room, room.Updates, true); err != nil {
		return
	}
	c.mu.Lock()
	held := c.pending
	c.pending, c.loading = nil, false
	c.mu.Unlock()
	for _, p := range held {
		c.enqueue(p.room, p.frame)
	}

	go c.write(ctx, ws)
	go c.keepAlive(ctx, ws, store)
	c.read(ctx, ws, room, store)
}

// load sends a room and everything it holds, then, when there is much of it,
// asks for its merge.
func (c *Conn) load(ctx context.Context, ws *websocket.Conn, room Room, updates []Update, first bool) error {
	frames := [][]byte{RoomFrame(room.ID, room.Base, room.Seed && first)}
	for _, u := range updates {
		frames = append(frames, UpdateFrame(u.Body))
	}
	frames = append(frames, LoadedFrame())
	if first {
		for _, p := range c.hub.peersOf(c.page) {
			frames = append(frames, AwarenessFrame(EncodeAwareness([]Peer{p})))
		}
	}
	if len(updates) >= c.hub.opts.CompactThreshold {
		frames = append(frames, CompactFrame(updates[0].Seq, updates[len(updates)-1].Seq, len(updates)))
	}
	for _, f := range frames {
		wctx, cancel := context.WithTimeout(ctx, c.hub.opts.WriteTimeout)
		err := ws.Write(wctx, websocket.MessageBinary, f)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) write(ctx context.Context, ws *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, c.hub.opts.WriteTimeout)
			err := ws.Write(wctx, websocket.MessageBinary, f)
			cancel()
			if err != nil {
				c.fail(websocket.StatusGoingAway, closeSlowReason)
				return
			}
		}
	}
}

// keepAlive pings the browser and asks again, now and then, whether its
// person may still be here.
func (c *Conn) keepAlive(ctx context.Context, ws *websocket.Conn, store Store) {
	ping := time.NewTicker(c.hub.opts.PingInterval)
	defer ping.Stop()
	recheck := time.NewTicker(c.hub.opts.RecheckInterval)
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, c.hub.opts.WriteTimeout)
			err := ws.Ping(pctx)
			cancel()
			if err != nil {
				c.fail(websocket.StatusGoingAway, closeSlowReason)
				return
			}
		case <-recheck.C:
			if err := store.Check(ctx); err != nil && ctx.Err() == nil {
				c.failFor(err)
				return
			}
		}
	}
}

// read handles what the browser sends, one message at a time, so its
// updates are stored in the order it sent them.
func (c *Conn) read(ctx context.Context, ws *websocket.Conn, room Room, store Store) {
	appends := 0
	for {
		kind, frame, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageBinary {
			continue
		}
		if c.isFailed() {
			continue
		}
		m, err := Decode(frame)
		if err != nil {
			c.fail(websocket.StatusUnsupportedData, "That message could not be read.")
			continue
		}
		if err := c.handle(ctx, ws, room, store, m, frame, &appends); err != nil && !errors.Is(err, ErrIgnored) && ctx.Err() == nil {
			// Reading goes on, ignoring what comes, until the browser answers the close.
			c.failFor(err)
		}
	}
}

func (c *Conn) isFailed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failed
}

func (c *Conn) handle(ctx context.Context, ws *websocket.Conn, room Room, store Store, m Message, frame []byte, appends *int) error {
	switch m.Type {
	case MsgSync:
		if m.SubType == SyncStep1 || len(m.Update) == 0 || isEmptyUpdate(m.Update) {
			return nil
		}
		if _, err := store.Append(ctx, m.Update); err != nil {
			return err
		}
		c.hub.deliver(c.page, room.ID, c, UpdateFrame(m.Update), true)
		*appends++
		if *appends%c.hub.opts.CompactCheckEvery == 0 {
			return c.compactIfLong(ctx, ws, room, store)
		}
	case MsgAwareness:
		peers, err := ParseAwareness(m.Awareness)
		if err != nil {
			return ErrIgnored
		}
		c.mu.Lock()
		for _, p := range peers {
			c.clients[p.Client] = struct{}{}
		}
		c.mu.Unlock()
		c.hub.deliver(c.page, room.ID, c, frame, true)
	case MsgQueryAwareness:
		for _, p := range c.hub.peersOf(c.page) {
			c.enqueue(room.ID, AwarenessFrame(EncodeAwareness([]Peer{p})))
		}
	case MsgSeed:
		if len(m.Update) == 0 {
			return ErrIgnored
		}
		if err := store.Seed(ctx, int(m.Version), m.Update); err != nil {
			return err
		}
		c.hub.deliver(c.page, room.ID, c, UpdateFrame(m.Update), true)
		c.hub.deliver(c.page, room.ID, nil, BaseFrame(int(m.Version)), true)
	case MsgPublished:
		base, err := store.Published(ctx, int(m.Version))
		if err != nil {
			return err
		}
		c.hub.deliver(c.page, room.ID, nil, BaseFrame(base), true)
	case MsgDiscard:
		if err := store.Discard(ctx); err != nil {
			return err
		}
		c.hub.Reset(c.page, room.ID)
	case MsgCompacted:
		if len(m.Update) == 0 {
			return ErrIgnored
		}
		if err := store.Compact(ctx, int64(m.From), int64(m.To), int(m.Count), m.Update); err != nil {
			return err
		}
	}
	return nil
}

// compactIfLong loads the room again on this connection when it holds
// enough updates to be worth merging, which the load then asks for.
func (c *Conn) compactIfLong(ctx context.Context, ws *websocket.Conn, room Room, store Store) error {
	n, err := store.Count(ctx)
	if err != nil {
		return err
	}
	if n < c.hub.opts.CompactThreshold {
		return nil
	}
	base, updates, err := store.Reload(ctx)
	if err != nil {
		return err
	}
	room.Base = base
	return c.load(ctx, ws, room, updates, false)
}

// isEmptyUpdate is an update that changes nothing, which is not worth a row.
func isEmptyUpdate(u []byte) bool {
	return len(u) == 2 && u[0] == 0 && u[1] == 0
}
