package collab

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
	// ReadTimeout bounds one read of a room on behalf of the bus: an update
	// it named, or what a process missed while it could not hear the others.
	ReadTimeout = 5 * time.Second
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
	// ErrTooLarge is a bus refusing an envelope past what it carries.
	ErrTooLarge = errors.New("too large for the bus between api processes")
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

// Execer runs a statement in the transaction that stores an update.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Announce is called by a Store inside the transaction that stores an
// update, with its number; nil when the bus carries updates whole.
type Announce func(ctx context.Context, tx Execer, seq int64) error

// Store is the database as the person a connection is for.
type Store interface {
	Append(ctx context.Context, body []byte, announce Announce) (int64, error)
	Seed(ctx context.Context, base int, body []byte, announce Announce) (int64, error)
	Published(ctx context.Context, version int) (int, error)
	Discard(ctx context.Context) error
	Compact(ctx context.Context, from, to int64, count int, merged []byte) error
	// Reload is the room's base and every update it holds now.
	Reload(ctx context.Context) (int, []Update, error)
	Count(ctx context.Context) (int, error)
	// Range is the room's updates numbered from through to.
	Range(ctx context.Context, from, to int64) ([]Update, error)
	// Check says whether the person is still signed in and may still edit.
	Check(ctx context.Context) error
}

// Bus carries frames to the other api processes; nil keeps them in this one.
// Publish answers ErrTooLarge for an envelope it cannot carry.
type Bus interface {
	Publish(ctx context.Context, page uuid.UUID, envelope []byte) error
}

// Announcer is a bus that names a stored update from inside the transaction
// storing it, so no process hears of a row before it can read it.
type Announcer interface {
	Announce(ctx context.Context, tx Execer, page uuid.UUID, envelope []byte) error
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
	// last is the highest update number the bus brought, from which a
	// process that could not hear the others for a while catches up.
	last int64
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
	// kindUpdate is a stored update's number, then its frame.
	kindUpdate = 2
	// kindPointer is a stored update's number alone, for the receiver to read.
	kindPointer = 3
)

// An envelope's header is the sending process, the room and the kind; an
// update's number follows it in eight bytes.
const (
	headerBytes = 33
	seqBytes    = 8
)

// envelope is what goes over the bus: the sending process, the room, the
// kind, and the frame.
func (h *Hub) envelope(room uuid.UUID, kind byte, frame []byte) []byte {
	out := make([]byte, 0, headerBytes+len(frame))
	out = append(out, h.pod[:]...)
	out = append(out, room[:]...)
	out = append(out, kind)
	return append(out, frame...)
}

func numbered(seq int64, frame []byte) []byte {
	return append(binary.BigEndian.AppendUint64(make([]byte, 0, seqBytes+len(frame)), uint64(seq)), frame...)
}

// Receive takes an envelope the bus brought from any process. Its own it
// passes on to nobody, but counts the update it names as heard.
func (h *Hub) Receive(page uuid.UUID, envelope []byte) {
	if len(envelope) < headerBytes {
		return
	}
	pod, _ := uuid.FromBytes(envelope[:16])
	room, _ := uuid.FromBytes(envelope[16:32])
	kind, body := envelope[32], envelope[headerBytes:]
	var seq int64
	if kind == kindUpdate || kind == kindPointer {
		if len(body) < seqBytes {
			return
		}
		seq, body = int64(binary.BigEndian.Uint64(body)), body[seqBytes:]
		h.heard(page, seq)
	}
	if pod == h.pod {
		return
	}
	switch kind {
	case kindFrame:
		h.deliver(page, room, nil, body, false)
	case kindReset:
		h.reset(page, room, false)
	case kindUpdate:
		h.fanout(page, room, nil, body)
	case kindPointer:
		h.fetch(page, room, seq)
	}
}

// heard notes an update the bus brought, or catching up relayed.
func (h *Hub) heard(page uuid.UUID, seq int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pc := h.pages[page]; pc != nil && seq > pc.last {
		pc.last = seq
	}
}

// loaded starts the count of what the bus brought from a page's first
// connection's load, which nobody else here needs to catch up on.
func (h *Hub) loaded(c *Conn, updates []Update) {
	if len(updates) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if pc := h.pages[c.page]; pc != nil && len(pc.conns) == 1 && pc.last == 0 {
		pc.last = updates[len(updates)-1].Seq
	}
}

// viewer is a connection's store, with the context it reads in, to read
// the room as somebody in it, as row level security wants.
type viewer struct {
	ctx   context.Context
	store Store
}

func (h *Hub) viewers(page, room uuid.UUID) []viewer {
	h.mu.Lock()
	defer h.mu.Unlock()
	pc := h.pages[page]
	if pc == nil {
		return nil
	}
	var out []viewer
	for c := range pc.conns {
		if v, ok := c.viewer(room); ok {
			out = append(out, v)
		}
	}
	return out
}

// fetch reads the update a pointer named, as the first person here in its
// room who still may, and passes it to them all.
func (h *Hub) fetch(page, room uuid.UUID, seq int64) {
	for _, v := range h.viewers(page, room) {
		ctx, cancel := context.WithTimeout(v.ctx, ReadTimeout)
		updates, err := v.store.Range(ctx, seq, seq)
		cancel()
		if err != nil {
			continue
		}
		// None means the update was merged away or its room thrown out;
		// either way the next load carries what it held.
		for _, u := range updates {
			h.fanout(page, room, nil, UpdateFrame(u.Body))
		}
		return
	}
}

// CatchUp relays, to every room this process holds connections to, the
// updates stored since the bus last brought one, and its base; a bus calls
// it once it hears the others again after losing them.
func (h *Hub) CatchUp() {
	type held struct {
		page, room uuid.UUID
		last       int64
	}
	var rooms []held
	h.mu.Lock()
	for page, pc := range h.pages {
		seen := map[uuid.UUID]bool{}
		for c := range pc.conns {
			if room := c.roomID(); room != uuid.Nil && !seen[room] {
				seen[room] = true
				rooms = append(rooms, held{page, room, pc.last})
			}
		}
	}
	h.mu.Unlock()
	for _, r := range rooms {
		h.catchUp(r.page, r.room, r.last)
	}
}

func (h *Hub) catchUp(page, room uuid.UUID, last int64) {
	for _, v := range h.viewers(page, room) {
		ctx, cancel := context.WithTimeout(v.ctx, ReadTimeout)
		base, updates, err := v.store.Reload(ctx)
		cancel()
		if errors.Is(err, ErrGone) {
			h.reset(page, room, false)
			return
		}
		if err != nil {
			continue
		}
		for _, u := range updates {
			if u.Seq > last {
				h.fanout(page, room, nil, UpdateFrame(u.Body))
				h.heard(page, u.Seq)
			}
		}
		h.fanout(page, room, nil, BaseFrame(base))
		return
	}
	h.log.Warn("could not catch a shared draft up with the other api processes", "page", page)
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
// sender, and through the bus when it began here.
func (h *Hub) deliver(page, room uuid.UUID, from *Conn, frame []byte, local bool) {
	h.fanout(page, room, from, frame)
	if local {
		h.publish(page, room, kindFrame, frame)
	}
}

// deliverUpdate passes on an update this process stored as seq. A bus that
// announced it in the storing transaction has nothing left to send.
func (h *Hub) deliverUpdate(page, room uuid.UUID, from *Conn, seq int64, update []byte) {
	frame := UpdateFrame(update)
	h.fanout(page, room, from, frame)
	if _, announced := h.bus.(Announcer); !announced {
		h.publish(page, room, kindUpdate, numbered(seq, frame))
	}
}

// announce is what a store calls to name an update to the other processes
// from inside the transaction storing it, or nil when the bus carries it.
func (h *Hub) announce(page, room uuid.UUID) Announce {
	a, ok := h.bus.(Announcer)
	if !ok {
		return nil
	}
	return func(ctx context.Context, tx Execer, seq int64) error {
		return a.Announce(ctx, tx, page, h.envelope(room, kindPointer, numbered(seq, nil)))
	}
}

// fanout passes a frame of a room to every connection of the page but its
// sender, which keep only their own room's. An awareness frame is
// remembered for newcomers.
func (h *Hub) fanout(page, room uuid.UUID, from *Conn, frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pc := h.pages[page]; pc != nil {
		h.remember(pc, from, frame)
		for c := range pc.conns {
			if c != from {
				c.enqueue(room, frame)
			}
		}
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
	err := h.bus.Publish(ctx, page, h.envelope(room, kind, frame))
	if errors.Is(err, ErrTooLarge) {
		// Browsers renew their awareness, so one too large is let go. Anything
		// else sends the room's browsers elsewhere to load it afresh instead.
		if kind == kindFrame && isAwareness(frame) {
			h.log.Debug("an awareness update was too large for the bus", "page", page, "bytes", len(frame))
			return
		}
		err = h.bus.Publish(ctx, page, h.envelope(room, kindReset, nil))
	}
	// A frame the other processes miss reaches their browsers when they
	// catch up or load again, so this is logged rather than failing the sender.
	if err != nil {
		h.log.Warn("could not pass a shared draft's change to the other api processes", "page", page, "error", err)
	}
}

func isAwareness(frame []byte) bool {
	r := &reader{buf: frame}
	return r.uint() == MsgAwareness && r.err == nil
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
