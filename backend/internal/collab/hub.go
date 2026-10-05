package collab

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Limits and timings. MaxMessageBytes is also the database's limit on one
// stored update, which a test holds to the migration.
const (
	MaxMessageBytes = 4 << 20
	// MaxConnsPerPage bounds the connections one api process holds to one page.
	MaxConnsPerPage = 50
	// PingInterval keeps a quiet connection alive through proxies that close
	// one idle for a minute, as nginx does by default.
	PingInterval = 25 * time.Second
	// RecheckInterval is how often a connection asks again whether its
	// person is still signed in and may still edit the page.
	RecheckInterval = 30 * time.Second
	WriteTimeout    = 10 * time.Second
	// CompactThreshold is how many stored updates make the next load ask the
	// browser for their merge; CompactCheckEvery is how many appends a
	// connection makes between counts of them.
	CompactThreshold  = 200
	CompactCheckEvery = 50
	// SendQueue is how many frames may wait for a slow browser before it is
	// let go to load afresh.
	SendQueue = 512
	// PeerTimeout is how long an awareness state lasts without being renewed,
	// as y-protocols times one out.
	PeerTimeout = 30 * time.Second
	// BusTimeout bounds one publish to the other api processes.
	BusTimeout = 2 * time.Second
)

// Refusals a Store answers with, which decide how a connection is closed.
var (
	// ErrGone: the room was thrown away or started afresh; load again.
	ErrGone = errors.New("the shared draft was started afresh")
	// ErrRefused: the person may no longer edit the page.
	ErrRefused = errors.New("you may no longer edit this page")
	// ErrSignedOut: the person's session ended.
	ErrSignedOut = errors.New("your session ended")
	// ErrIgnored: nothing to tell anybody, such as a compaction overtaken by another.
	ErrIgnored = errors.New("ignored")
	// ErrTooMany refuses a connection past MaxConnsPerPage.
	ErrTooMany = errors.New("too many people are editing this page")
	// ErrClosed refuses a connection to a hub shutting down.
	ErrClosed = errors.New("the server is shutting down")
)

// Update is one stored update of a room, by its number.
type Update struct {
	Seq  int64
	Body []byte
}

// Room is what a connection opens with.
type Room struct {
	ID      uuid.UUID
	Base    int
	Seed    bool
	Updates []Update
}

// Store is the database as the person a connection is for.
type Store interface {
	Append(ctx context.Context, body []byte) (int64, error)
	Seed(ctx context.Context, base int, body []byte) error
	Published(ctx context.Context, version int) (int, error)
	Discard(ctx context.Context) error
	Compact(ctx context.Context, from, to int64, count int, merged []byte) error
	// Reload is the room's base and every update it holds now.
	Reload(ctx context.Context) (int, []Update, error)
	Count(ctx context.Context) (int, error)
	// Check says whether the person is still signed in and may still edit.
	Check(ctx context.Context) error
}

// Bus carries frames to the other api processes; nil keeps them in this one.
type Bus interface {
	Publish(ctx context.Context, page uuid.UUID, envelope []byte) error
}

// Options are the hub's timings and limits; tests shorten them.
type Options struct {
	MaxConnsPerPage   int
	PingInterval      time.Duration
	RecheckInterval   time.Duration
	WriteTimeout      time.Duration
	CompactThreshold  int
	CompactCheckEvery int
	SendQueue         int
}

// DefaultOptions are the constants above.
func DefaultOptions() Options {
	return Options{
		MaxConnsPerPage: MaxConnsPerPage, PingInterval: PingInterval, RecheckInterval: RecheckInterval,
		WriteTimeout: WriteTimeout, CompactThreshold: CompactThreshold, CompactCheckEvery: CompactCheckEvery, SendQueue: SendQueue,
	}
}

// Hub holds this process's connections, by page, and passes frames between
// them and, through the bus, to and from the other processes.
type Hub struct {
	pod  uuid.UUID
	bus  Bus
	log  *slog.Logger
	opts Options

	mu     sync.Mutex
	pages  map[uuid.UUID]*pageConns
	closed bool
}

type pageConns struct {
	conns map[*Conn]struct{}
	// peers is the awareness of everybody in the page this process has
	// heard from, here or through the bus, so a newcomer sees them at once.
	peers map[uint64]peer
}

type peer struct {
	Peer
	conn *Conn
	seen time.Time
}

// NewHub makes a hub; bus may be nil for a single api process.
func NewHub(bus Bus, log *slog.Logger, opts Options) *Hub {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hub{pod: uuid.New(), bus: bus, log: log, opts: opts, pages: map[uuid.UUID]*pageConns{}}
}

// The kinds of frame the bus carries.
const (
	kindFrame = 0
	kindReset = 1
)

// envelope is what goes over the bus: the sending process, the room, the
// kind, and the frame.
func (h *Hub) envelope(room uuid.UUID, kind byte, frame []byte) []byte {
	out := make([]byte, 0, 33+len(frame))
	out = append(out, h.pod[:]...)
	out = append(out, room[:]...)
	out = append(out, kind)
	return append(out, frame...)
}

// Receive takes an envelope the bus brought from any process, this one's
// own included, which it ignores.
func (h *Hub) Receive(page uuid.UUID, envelope []byte) {
	if len(envelope) < 33 {
		return
	}
	pod, _ := uuid.FromBytes(envelope[:16])
	if pod == h.pod {
		return
	}
	room, _ := uuid.FromBytes(envelope[16:32])
	switch envelope[32] {
	case kindFrame:
		h.deliver(page, room, nil, envelope[33:], false)
	case kindReset:
		h.reset(page, room, false)
	}
}

// Join counts a new connection in before its room is opened, so nothing
// sent to the page meanwhile passes it by.
func (h *Hub) Join(page uuid.UUID) (*Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	pc := h.pages[page]
	if pc == nil {
		pc = &pageConns{conns: map[*Conn]struct{}{}, peers: map[uint64]peer{}}
		h.pages[page] = pc
	}
	if len(pc.conns) >= h.opts.MaxConnsPerPage {
		return nil, ErrTooMany
	}
	c := newConn(h, page)
	pc.conns[c] = struct{}{}
	return c, nil
}

// leave counts a connection out and tells everybody its browsers have gone.
func (h *Hub) leave(c *Conn) {
	h.mu.Lock()
	pc := h.pages[c.page]
	var left []Peer
	if pc != nil {
		delete(pc.conns, c)
		for id, p := range pc.peers {
			if p.conn == c {
				delete(pc.peers, id)
				if p.State != gone {
					left = append(left, Peer{Client: id, Clock: p.Clock + 1, State: gone})
				}
			}
		}
		if len(pc.conns) == 0 {
			delete(h.pages, c.page)
		}
	}
	h.mu.Unlock()
	if len(left) > 0 {
		h.deliver(c.page, c.roomID(), c, AwarenessFrame(EncodeAwareness(left)), true)
	}
}

// deliver passes a frame of a room to every connection of the page but its
// sender, which keep only their own room's, and through the bus when it
// began here. An awareness frame is remembered for newcomers.
func (h *Hub) deliver(page, room uuid.UUID, from *Conn, frame []byte, local bool) {
	h.mu.Lock()
	if pc := h.pages[page]; pc != nil {
		h.remember(pc, from, frame)
		for c := range pc.conns {
			if c != from {
				c.enqueue(room, frame)
			}
		}
	}
	h.mu.Unlock()
	if local {
		h.publish(page, room, kindFrame, frame)
	}
}

// remember keeps the awareness a frame carries; the caller holds h.mu.
func (h *Hub) remember(pc *pageConns, from *Conn, frame []byte) {
	r := &reader{buf: frame}
	if r.uint() != MsgAwareness || r.err != nil {
		return
	}
	peers, err := ParseAwareness(r.bytes())
	if err != nil {
		return
	}
	now := time.Now()
	for _, p := range peers {
		if known, ok := pc.peers[p.Client]; ok && known.Clock >= p.Clock && p.State != gone {
			continue
		}
		if p.State == gone {
			delete(pc.peers, p.Client)
			continue
		}
		pc.peers[p.Client] = peer{Peer: p, conn: from, seen: now}
	}
}

// peersOf is the awareness of a page's browsers still renewing theirs.
func (h *Hub) peersOf(page uuid.UUID) []Peer {
	h.mu.Lock()
	defer h.mu.Unlock()
	pc := h.pages[page]
	if pc == nil {
		return nil
	}
	var out []Peer
	for _, p := range pc.peers {
		if time.Since(p.seen) < PeerTimeout {
			out = append(out, p.Peer)
		}
	}
	return out
}

// Reset closes every connection to a room, here and elsewhere, so each
// loads the room that took its place.
func (h *Hub) Reset(page, room uuid.UUID) { h.reset(page, room, true) }

func (h *Hub) reset(page, room uuid.UUID, local bool) {
	h.mu.Lock()
	if pc := h.pages[page]; pc != nil {
		for c := range pc.conns {
			if c.roomID() == room {
				c.fail(CloseGone, closeGoneReason)
			}
		}
	}
	h.mu.Unlock()
	if local {
		h.publish(page, room, kindReset, nil)
	}
}

func (h *Hub) publish(page, room uuid.UUID, kind byte, frame []byte) {
	if h.bus == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), BusTimeout)
	defer cancel()
	// A frame the other processes miss reaches their browsers at their next
	// load, so this is logged rather than failing the sender.
	if err := h.bus.Publish(ctx, page, h.envelope(room, kind, frame)); err != nil {
		h.log.Warn("could not pass a shared draft's change to the other api processes", "page", page, "error", err)
	}
}

// Shutdown closes every connection, asking each browser to come back, and
// refuses new ones.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	h.closed = true
	var all []*Conn
	for _, pc := range h.pages {
		for c := range pc.conns {
			all = append(all, c)
		}
	}
	h.mu.Unlock()
	for _, c := range all {
		c.fail(CloseRestart, closeRestartReason)
	}
}
