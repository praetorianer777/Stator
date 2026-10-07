package collab

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// The same bytes are in web/src/features/collab/protocol.test.ts, which
// holds the browser to them.
func TestFramesAreEncodedAsTheBrowserReadsThem(t *testing.T) {
	room := uuid.MustParse("0195f000-0000-7000-8000-0000000000a1")
	for name, c := range map[string]struct {
		frame []byte
		want  string
	}{
		"an update":       {UpdateFrame([]byte{1, 2, 3}), "000203010203"},
		"a finished load": {LoadedFrame(), "0001020000"},
		"a room":          {RoomFrame(room, 300, true), "6424" + hex.EncodeToString([]byte(room.String())) + "ac0201"},
		"a base":          {BaseFrame(7), "6b07"},
		"a compaction":    {CompactFrame(1, 200, 200), "6801c801c801"},
		"awareness":       {AwarenessFrame(EncodeAwareness([]Peer{{Client: 5, Clock: 2, State: "{}"}})), "0106010502027b7d"},
	} {
		if got := hex.EncodeToString(c.frame); got != c.want {
			t.Errorf("%s = %s, want %s", name, got, c.want)
		}
	}
}

func TestMessagesFromTheBrowserAreDecoded(t *testing.T) {
	for name, c := range map[string]struct {
		hex  string
		want Message
	}{
		"an update":  {"000203010203", Message{Type: MsgSync, SubType: SyncUpdate, Update: []byte{1, 2, 3}}},
		"a seed":     {"66040201ff", Message{Type: MsgSeed, Version: 4, Update: []byte{1, 0xff}}},
		"published":  {"6505", Message{Type: MsgPublished, Version: 5}},
		"discard":    {"67", Message{Type: MsgDiscard}},
		"compacted":  {"6901c80102020909", Message{Type: MsgCompacted, From: 1, To: 200, Count: 2, Update: []byte{9, 9}}},
		"awareness":  {"0103000000", Message{Type: MsgAwareness, Awareness: []byte{0, 0, 0}}},
		"a question": {"03", Message{Type: MsgQueryAwareness}},
	} {
		frame, _ := hex.DecodeString(c.hex)
		got, err := Decode(frame)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Type != c.want.Type || got.SubType != c.want.SubType || !bytes.Equal(got.Update, c.want.Update) ||
			!bytes.Equal(got.Awareness, c.want.Awareness) || got.Version != c.want.Version || got.From != c.want.From ||
			got.To != c.want.To || got.Count != c.want.Count {
			t.Errorf("%s = %+v, want %+v", name, got, c.want)
		}
	}
	for _, bad := range []string{"", "00", "0002", "000205", "63", "6601"} {
		frame, _ := hex.DecodeString(bad)
		if _, err := Decode(frame); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
}

func TestAwarenessRoundTrips(t *testing.T) {
	peers := []Peer{{Client: 1 << 31, Clock: 9, State: `{"user":{"name":"Ann"}}`}, {Client: 3, Clock: 1, State: gone}}
	got, err := ParseAwareness(EncodeAwareness(peers))
	if err != nil || len(got) != 2 || got[0] != peers[0] || got[1] != peers[1] {
		t.Fatalf("%+v %v", got, err)
	}
	many := make([]Peer, MaxPeersPerUpdate+1)
	if _, err := ParseAwareness(EncodeAwareness(many)); err == nil {
		t.Error("an update speaking for too many browsers was read")
	}
}

// The database refuses what the hub would not send on, at the same size.
func TestTheMessageLimitIsTheDatabases(t *testing.T) {
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "00450_page_collab.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sql), "BETWEEN 1 AND "+strconv.Itoa(MaxMessageBytes)+")") {
		t.Errorf("00450 does not limit an update to %d bytes", MaxMessageBytes)
	}
}

// memStore is a room in memory, as one person.
type memStore struct {
	mu       sync.Mutex
	updates  []Update
	next     int64
	base     int
	seeded   bool
	gone     bool
	refuse   error
	compacts int
}

// Append calls announce after taking the update, as the database's commit
// delivers what was announced only once the row is there.
func (m *memStore) Append(ctx context.Context, body []byte, announce Announce) (int64, error) {
	m.mu.Lock()
	if m.refuse != nil {
		m.mu.Unlock()
		return 0, m.refuse
	}
	if m.gone {
		m.mu.Unlock()
		return 0, ErrGone
	}
	m.next++
	seq := m.next
	m.updates = append(m.updates, Update{Seq: seq, Body: body})
	m.mu.Unlock()
	if announce != nil {
		if err := announce(ctx, nil, seq); err != nil {
			return 0, err
		}
	}
	return seq, nil
}

func (m *memStore) Seed(ctx context.Context, base int, body []byte, announce Announce) (int64, error) {
	m.mu.Lock()
	if m.seeded {
		m.mu.Unlock()
		return 0, ErrGone
	}
	m.seeded, m.base = true, base
	m.mu.Unlock()
	return m.Append(ctx, body, announce)
}

func (m *memStore) Range(_ context.Context, from, to int64) ([]Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Update
	for _, u := range m.updates {
		if u.Seq >= from && u.Seq <= to {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *memStore) Published(_ context.Context, version int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if version <= m.base {
		return 0, ErrIgnored
	}
	m.base = version
	return version, nil
}

func (m *memStore) Discard(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gone = true
	return nil
}

func (m *memStore) Compact(_ context.Context, from, to int64, count int, merged []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []Update
	n := 0
	for _, u := range m.updates {
		if u.Seq >= from && u.Seq <= to {
			n++
			continue
		}
		kept = append(kept, u)
	}
	if n != count {
		return ErrIgnored
	}
	m.updates = append([]Update{{Seq: to, Body: merged}}, kept...)
	m.compacts++
	return nil
}

func (m *memStore) Reload(context.Context) (int, []Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gone {
		return 0, nil, ErrGone
	}
	return m.base, append([]Update(nil), m.updates...), nil
}

func (m *memStore) Count(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.updates), nil
}

func (m *memStore) Check(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refuse
}

func (m *memStore) room(id uuid.UUID, seed bool) Room {
	_, updates, _ := m.Reload(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	return Room{ID: id, Base: m.base, Seed: seed, Updates: updates}
}

// site serves one page's room from a hub, as handleCollab does.
type site struct {
	t     *testing.T
	hub   *Hub
	store *memStore
	page  uuid.UUID
	room  uuid.UUID
	srv   *httptest.Server
	mu    sync.Mutex
	opens int
}

func newSite(t *testing.T, hub *Hub, store *memStore, page, room uuid.UUID) *site {
	s := &site{t: t, hub: hub, store: store, page: page, room: room}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hub.Join(page)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		s.mu.Lock()
		s.opens++
		store.mu.Lock()
		seed := s.opens == 1 && !store.seeded
		store.mu.Unlock()
		s.mu.Unlock()
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			conn.Leave()
			return
		}
		ws.SetReadLimit(MaxMessageBytes)
		conn.Serve(context.Background(), ws, store.room(s.room, seed), store)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

func (s *site) dial() *client {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.srv.URL, "http"), nil)
	if err != nil {
		s.t.Fatal(err)
	}
	ws.SetReadLimit(MaxMessageBytes)
	c := &client{t: s.t, ws: ws}
	s.t.Cleanup(func() { _ = ws.CloseNow() })
	return c
}

func (c *client) send(frame []byte) {
	c.t.Helper()
	if err := c.ws.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		c.t.Fatal(err)
	}
}

// next is the next frame, or the close it ended with.
func (c *client) next() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, frame, err := c.ws.Read(ctx)
	return frame, err
}

// until reads frames until one passes match, and fails at a close first.
func (c *client) until(what string, match func([]byte) bool) []byte {
	c.t.Helper()
	for {
		frame, err := c.next()
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", what, err)
		}
		if match(frame) {
			return frame
		}
	}
}

// loaded reads one load, room frame to empty step 2, and answers its frames.
func (c *client) loaded() [][]byte {
	c.t.Helper()
	var frames [][]byte
	c.until("the room", func(f []byte) bool { return f[0] == MsgRoom })
	for {
		frame := c.until("the load", func([]byte) bool { return true })
		if bytes.Equal(frame, LoadedFrame()) {
			return frames
		}
		frames = append(frames, frame)
	}
}

func (c *client) closedWith(want websocket.StatusCode) {
	c.t.Helper()
	for {
		_, err := c.next()
		if err == nil {
			continue
		}
		if got := websocket.CloseStatus(err); got != want {
			c.t.Fatalf("closed with %d (%v), want %d", got, err, want)
		}
		return
	}
}

func is(want []byte) func([]byte) bool { return func(f []byte) bool { return bytes.Equal(f, want) } }

func testOptions() Options {
	o := DefaultOptions()
	o.RecheckInterval = 50 * time.Millisecond
	o.CompactThreshold = 4
	o.CompactCheckEvery = 2
	return o
}

func TestUpdatesAreStoredAndPassedToTheOthersInOrder(t *testing.T) {
	store := &memStore{seeded: true, next: 1, updates: []Update{{Seq: 1, Body: []byte{7}}}}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann, bob := s.dial(), s.dial()
	for _, c := range []*client{ann, bob} {
		if load := c.loaded(); len(load) != 1 || !bytes.Equal(load[0], UpdateFrame([]byte{7})) {
			t.Fatalf("the load = %x", load)
		}
	}
	ann.send(UpdateFrame([]byte{1}))
	ann.send(UpdateFrame([]byte{2}))
	bob.until("ann's first", is(UpdateFrame([]byte{1})))
	bob.until("ann's second", is(UpdateFrame([]byte{2})))
	if _, updates, _ := store.Reload(context.Background()); len(updates) != 3 || updates[1].Body[0] != 1 || updates[2].Body[0] != 2 {
		t.Errorf("stored %+v", updates)
	}
	// An empty update and a state vector change nothing and are not kept.
	ann.send(UpdateFrame(emptyUpdate))
	ann.send((&writer{}).uint(MsgSync).uint(SyncStep1).bytes([]byte{0}).buf)
	ann.send(UpdateFrame([]byte{3}))
	bob.until("ann's third", is(UpdateFrame([]byte{3})))
	if n, _ := store.Count(context.Background()); n != 4 {
		t.Errorf("%d updates kept", n)
	}
}

func TestTheFirstToOpenIsAskedToSeedAndTheOthersGetIt(t *testing.T) {
	store := &memStore{}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann := s.dial()
	room := ann.until("the room", func(f []byte) bool { return f[0] == MsgRoom })
	if room[len(room)-1] != 1 {
		t.Fatalf("the first to open was not asked to seed: %x", room)
	}
	bob := s.dial()
	room = bob.until("the room", func(f []byte) bool { return f[0] == MsgRoom })
	if room[len(room)-1] != 0 {
		t.Fatalf("the second to open was asked to seed too: %x", room)
	}
	ann.send((&writer{}).uint(MsgSeed).uint(3).bytes([]byte{4, 2}).buf)
	bob.until("the seed", is(UpdateFrame([]byte{4, 2})))
	bob.until("the base", is(BaseFrame(3)))
	ann.until("the base", is(BaseFrame(3)))
	// A second seed is refused, and its sender loads again.
	bob.send((&writer{}).uint(MsgSeed).uint(3).bytes([]byte{5}).buf)
	bob.closedWith(CloseGone)
}

func TestAPublishMovesTheBaseForEverybody(t *testing.T) {
	store := &memStore{seeded: true, base: 1}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann, bob := s.dial(), s.dial()
	ann.loaded()
	bob.loaded()
	ann.send((&writer{}).uint(MsgPublished).uint(2).buf)
	ann.until("the base", is(BaseFrame(2)))
	bob.until("the base", is(BaseFrame(2)))
	// One that would move it back is ignored, and the connection stays.
	ann.send((&writer{}).uint(MsgPublished).uint(1).buf)
	ann.send(UpdateFrame([]byte{8}))
	bob.until("the next update", is(UpdateFrame([]byte{8})))
}

func TestADiscardClosesTheRoomForEverybody(t *testing.T) {
	store := &memStore{seeded: true}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann, bob := s.dial(), s.dial()
	ann.loaded()
	bob.loaded()
	ann.send([]byte{MsgDiscard})
	ann.closedWith(CloseGone)
	bob.closedWith(CloseGone)
}

func TestAwarenessIsPassedOnAndTakenBackWhenItsBrowserGoes(t *testing.T) {
	store := &memStore{seeded: true}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann, bob := s.dial(), s.dial()
	ann.loaded()
	bob.loaded()
	hello := AwarenessFrame(EncodeAwareness([]Peer{{Client: 11, Clock: 1, State: `{"user":{"name":"Ann"}}`}}))
	ann.send(hello)
	bob.until("ann's awareness", is(hello))
	// A newcomer sees ann at once.
	carl := s.dial()
	carl.until("ann's awareness", is(AwarenessFrame(EncodeAwareness([]Peer{{Client: 11, Clock: 1, State: `{"user":{"name":"Ann"}}`}}))))
	_ = ann.ws.Close(websocket.StatusNormalClosure, "")
	bob.until("ann leaving", is(AwarenessFrame(EncodeAwareness([]Peer{{Client: 11, Clock: 2, State: gone}}))))
}

func TestSomebodyWhoMayNoLongerEditIsLetGo(t *testing.T) {
	store := &memStore{seeded: true}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann := s.dial()
	ann.loaded()
	store.mu.Lock()
	store.refuse = ErrRefused
	store.mu.Unlock()
	ann.closedWith(CloseRefused)

	store.mu.Lock()
	store.refuse = ErrSignedOut
	store.mu.Unlock()
	bob := s.dial()
	bob.closedWith(CloseSignedOut)
}

func TestALongRoomIsMergedByABrowser(t *testing.T) {
	store := &memStore{seeded: true}
	s := newSite(t, NewHub(nil, nil, testOptions()), store, uuid.New(), uuid.New())
	ann := s.dial()
	ann.loaded()
	for i := byte(1); i <= 4; i++ {
		ann.send(UpdateFrame([]byte{i}))
	}
	// The fourth append crosses the threshold: the room is loaded again and
	// its merge asked for.
	ann.loaded()
	ask := ann.until("the request", func(f []byte) bool { return f[0] == MsgCompact })
	if !bytes.Equal(ask, CompactFrame(1, 4, 4)) {
		t.Fatalf("asked %x", ask)
	}
	ann.send((&writer{}).uint(MsgCompacted).uint(1).uint(4).uint(4).bytes([]byte{9}).buf)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, updates, _ := store.Reload(context.Background()); len(updates) == 1 && updates[0].Seq == 4 && updates[0].Body[0] == 9 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the merge did not replace the updates")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTooManyConnectionsToOnePageAreRefused(t *testing.T) {
	opts := testOptions()
	opts.MaxConnsPerPage = 1
	hub := NewHub(nil, nil, opts)
	page := uuid.New()
	if _, err := hub.Join(page); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Join(page); !errors.Is(err, ErrTooMany) {
		t.Errorf("a second connection: %v", err)
	}
	if _, err := hub.Join(uuid.New()); err != nil {
		t.Errorf("another page's connection: %v", err)
	}
}

func TestAShutdownAsksEveryBrowserBack(t *testing.T) {
	store := &memStore{seeded: true}
	hub := NewHub(nil, nil, testOptions())
	s := newSite(t, hub, store, uuid.New(), uuid.New())
	ann := s.dial()
	ann.loaded()
	hub.Shutdown()
	ann.closedWith(CloseRestart)
	if _, err := hub.Join(uuid.New()); !errors.Is(err, ErrClosed) {
		t.Errorf("a connection after shutdown: %v", err)
	}
}

// pipe is a bus between hubs in one process, which can be cut, and which
// refuses envelopes past limit when it has one.
type pipe struct {
	hubs  []*Hub
	limit int

	mu   sync.Mutex
	cut  bool
	sent []byte
}

func (p *pipe) Publish(_ context.Context, page uuid.UUID, envelope []byte) error {
	p.mu.Lock()
	if p.limit > 0 && len(envelope) > p.limit {
		p.mu.Unlock()
		return ErrTooLarge
	}
	cut := p.cut
	p.sent = append(p.sent, envelope[32])
	p.mu.Unlock()
	if cut {
		return nil
	}
	for _, h := range p.hubs {
		h.Receive(page, envelope)
	}
	return nil
}

func (p *pipe) setCut(cut bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cut = cut
}

func (p *pipe) setLimit(limit int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.limit = limit
}

func (p *pipe) kinds() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.sent...)
}

// announcing is a pipe that names updates in their transaction, as the
// Postgres bus does.
type announcing struct{ *pipe }

func (a announcing) Announce(ctx context.Context, _ Execer, page uuid.UUID, envelope []byte) error {
	return a.Publish(ctx, page, envelope)
}

func TestTwoProcessesShareARoomThroughTheBus(t *testing.T) {
	bus := &pipe{}
	one, two := NewHub(bus, nil, testOptions()), NewHub(bus, nil, testOptions())
	bus.hubs = []*Hub{one, two}
	store := &memStore{seeded: true}
	page, room := uuid.New(), uuid.New()
	a, b := newSite(t, one, store, page, room), newSite(t, two, store, page, room)
	ann, bob := a.dial(), b.dial()
	ann.loaded()
	bob.loaded()
	ann.send(UpdateFrame([]byte{1}))
	bob.until("ann's update", is(UpdateFrame([]byte{1})))
	bob.send(AwarenessFrame(EncodeAwareness([]Peer{{Client: 2, Clock: 1, State: "{}"}})))
	ann.until("bob's awareness", is(AwarenessFrame(EncodeAwareness([]Peer{{Client: 2, Clock: 1, State: "{}"}}))))
	// A frame of another room, as after a reset elsewhere, is not passed on.
	one.deliver(page, uuid.New(), nil, UpdateFrame([]byte{6}), true)
	ann.send(UpdateFrame([]byte{2}))
	if frame := bob.until("ann's next update", func(f []byte) bool { return f[0] == MsgSync }); !bytes.Equal(frame, UpdateFrame([]byte{2})) {
		t.Errorf("bob got %x", frame)
	}
	two.Reset(page, room)
	ann.closedWith(CloseGone)
	bob.closedWith(CloseGone)
}

func TestAnAnnouncingBusPassesUpdatesAsPointersReadAsSomebodyInTheRoom(t *testing.T) {
	p := &pipe{}
	bus := announcing{p}
	one, two := NewHub(bus, nil, testOptions()), NewHub(bus, nil, testOptions())
	p.hubs = []*Hub{one, two}
	store := &memStore{}
	page, room := uuid.New(), uuid.New()
	a, b := newSite(t, one, store, page, room), newSite(t, two, store, page, room)
	ann := a.dial()
	ann.loaded()
	bob := b.dial()
	bob.loaded()
	ann.send((&writer{}).uint(MsgSeed).uint(3).bytes([]byte{4, 2}).buf)
	bob.until("the seed", is(UpdateFrame([]byte{4, 2})))
	bob.until("the base", is(BaseFrame(3)))
	ann.send(UpdateFrame([]byte{1}))
	bob.until("ann's update", is(UpdateFrame([]byte{1})))
	for _, kind := range p.kinds() {
		if kind == kindUpdate {
			t.Fatal("an update went over the bus whole")
		}
	}
	if !bytes.Contains(p.kinds(), []byte{kindPointer}) {
		t.Error("no update went over the bus as a pointer")
	}
}

func TestAProcessCatchesUpOnWhatItMissedWhileItCouldNotHearTheOthers(t *testing.T) {
	for name, bus := range map[string]func(*pipe) Bus{
		"whole updates": func(p *pipe) Bus { return p },
		"pointers":      func(p *pipe) Bus { return announcing{p} },
	} {
		t.Run(name, func(t *testing.T) {
			p := &pipe{}
			one, two := NewHub(bus(p), nil, testOptions()), NewHub(bus(p), nil, testOptions())
			p.hubs = []*Hub{one, two}
			store := &memStore{seeded: true, base: 1}
			page, room := uuid.New(), uuid.New()
			a, b := newSite(t, one, store, page, room), newSite(t, two, store, page, room)
			ann, bob := a.dial(), b.dial()
			ann.loaded()
			bob.loaded()
			ann.send(UpdateFrame([]byte{1}))
			bob.until("ann's first", is(UpdateFrame([]byte{1})))

			p.setCut(true)
			ann.send(UpdateFrame([]byte{2}))
			ann.send(UpdateFrame([]byte{3}))
			waitFor(t, "both stored", func() bool { n, _ := store.Count(context.Background()); return n == 3 })
			store.mu.Lock()
			store.base = 2
			store.mu.Unlock()
			p.setCut(false)
			two.CatchUp()
			// Only what bob missed, not the update the bus brought before.
			if f := bob.until("the first missed", func(f []byte) bool { return f[0] == MsgSync }); !bytes.Equal(f, UpdateFrame([]byte{2})) {
				t.Fatalf("bob got %x", f)
			}
			bob.until("the second missed", is(UpdateFrame([]byte{3})))
			bob.until("the base", is(BaseFrame(2)))

			// A room thrown away meanwhile sends its browsers to load afresh.
			p.setCut(true)
			store.mu.Lock()
			store.gone = true
			store.mu.Unlock()
			two.CatchUp()
			bob.closedWith(CloseGone)
		})
	}
}

func TestWhatTheBusCannotCarryIsDroppedOrSendsTheOthersToLoadAfresh(t *testing.T) {
	p := &pipe{}
	one, two := NewHub(p, nil, testOptions()), NewHub(p, nil, testOptions())
	p.hubs = []*Hub{one, two}
	store := &memStore{seeded: true, base: 1}
	page, room := uuid.New(), uuid.New()
	a, b := newSite(t, one, store, page, room), newSite(t, two, store, page, room)
	ann, bob := a.dial(), b.dial()
	ann.loaded()
	bob.loaded()
	small := AwarenessFrame(EncodeAwareness([]Peer{{Client: 1, Clock: 1, State: "{}"}}))
	p.setLimit(headerBytes + len(small))
	big := AwarenessFrame(EncodeAwareness([]Peer{{Client: 1, Clock: 2, State: `{"user":{"name":"` + strings.Repeat("a", 64) + `"}}`}}))
	ann.send(big)
	ann.send(small)
	// The large one is let go; the next that fits arrives, and nothing else.
	if f := bob.until("ann's awareness", func([]byte) bool { return true }); !bytes.Equal(f, small) {
		t.Fatalf("bob got %x", f)
	}
	p.setLimit(headerBytes + 1)
	ann.send((&writer{}).uint(MsgPublished).uint(2).buf)
	ann.until("the base", is(BaseFrame(2)))
	bob.closedWith(CloseGone)
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// execRecorder is a pool or transaction that keeps what it was asked.
type execRecorder struct{ args [][]any }

func (e *execRecorder) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	e.args = append(e.args, args)
	return pgconn.CommandTag{}, nil
}

func TestThePostgresBusSendsWhatFitsAndReadsItBack(t *testing.T) {
	pool, tx := &execRecorder{}, &execRecorder{}
	bus := NewPostgresBus(pool, "", "chan", nil)
	page := uuid.New()
	envelope := bytes.Repeat([]byte{7}, 100)
	if err := bus.Publish(context.Background(), page, envelope); err != nil {
		t.Fatal(err)
	}
	if err := bus.Announce(context.Background(), tx, page, envelope[:40]); err != nil {
		t.Fatal(err)
	}
	if len(pool.args) != 1 || len(tx.args) != 1 || pool.args[0][0] != "chan" {
		t.Fatalf("sent %v through the pool and %v in the transaction", pool.args, tx.args)
	}
	got, body, ok := parsePayload(pool.args[0][1].(string))
	if !ok || got != page || !bytes.Equal(body, envelope) {
		t.Errorf("read back %s %x", got, body)
	}
	// Base64 makes four characters of every three bytes.
	tooBig := make([]byte, MaxNotifyBytes/4*3)
	if err := bus.Publish(context.Background(), page, tooBig); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an envelope past the limit: %v", err)
	}
	if MaxNotifyBytes >= postgresNotifyLimit {
		t.Errorf("MaxNotifyBytes %d is not below Postgres's %d", MaxNotifyBytes, postgresNotifyLimit)
	}
	if _, _, ok := parsePayload("not base64!"); ok {
		t.Error("a payload that is not ours was read")
	}
}
