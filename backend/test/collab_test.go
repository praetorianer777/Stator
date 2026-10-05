//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/collab"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// fastCollab rechecks often and compacts early, so a test sees both.
func fastCollab() collab.Options {
	o := collab.DefaultOptions()
	o.RecheckInterval = 200 * time.Millisecond
	o.CompactThreshold = 6
	o.CompactCheckEvery = 3
	return o
}

func withCollab(bus collab.Bus) func(*httpapi.Server) {
	return func(s *httpapi.Server) { s.Collab = collab.NewHub(bus, discard(), fastCollab()) }
}

// socket is a browser's connection to a page's shared draft.
type socket struct {
	t  *testing.T
	ws *websocket.Conn
}

func collabPath(page string) string { return pagePath(page, "/collab") }

// dial opens the shared draft as the client, or answers the status a
// refused handshake got.
func (c *client) dial(t *testing.T, page string) (*socket, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.api.handOver(t, c, http.MethodPost)
	ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.api.srv.URL, "http")+collabPath(page), &websocket.DialOptions{HTTPClient: c.http})
	if err != nil {
		if resp == nil {
			t.Fatalf("dial: %v", err)
		}
		return nil, resp.StatusCode
	}
	ws.SetReadLimit(collab.MaxMessageBytes)
	t.Cleanup(func() { _ = ws.CloseNow() })
	return &socket{t: t, ws: ws}, http.StatusSwitchingProtocols
}

func (c *client) open(t *testing.T, page string) *socket {
	t.Helper()
	s, status := c.dial(t, page)
	if s == nil {
		t.Fatalf("open the shared draft of %s: %d", page, status)
	}
	return s
}

func (s *socket) send(frame []byte) {
	s.t.Helper()
	if err := s.ws.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		s.t.Fatal(err)
	}
}

func (s *socket) next() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, frame, err := s.ws.Read(ctx)
	return frame, err
}

func (s *socket) until(what string, match func([]byte) bool) []byte {
	s.t.Helper()
	for {
		frame, err := s.next()
		if err != nil {
			s.t.Fatalf("waiting for %s: %v", what, err)
		}
		if match(frame) {
			return frame
		}
	}
}

// opened is a load as the server sends it: the room, then its updates up
// to the empty step 2.
type opened struct {
	room    string
	base    uint64
	seed    bool
	updates [][]byte
}

func (s *socket) load() opened {
	s.t.Helper()
	frame := s.until("the room", func(f []byte) bool { return f[0] == collab.MsgRoom })
	var o opened
	rest := frame[1:]
	n, k := binary.Uvarint(rest)
	o.room, rest = string(rest[k:k+int(n)]), rest[k+int(n):]
	o.base, k = binary.Uvarint(rest)
	o.seed = rest[k] == 1
	for {
		f := s.until("the load", func([]byte) bool { return true })
		if bytes.Equal(f, collab.LoadedFrame()) {
			return o
		}
		if f[0] == collab.MsgSync {
			o.updates = append(o.updates, f)
		}
	}
}

func (s *socket) closedWith(want websocket.StatusCode) {
	s.t.Helper()
	for {
		_, err := s.next()
		if err == nil {
			continue
		}
		if got := websocket.CloseStatus(err); got != want {
			s.t.Fatalf("closed with %d (%v), want %d", got, err, want)
		}
		return
	}
}

func is(want []byte) func([]byte) bool { return func(f []byte) bool { return bytes.Equal(f, want) } }

func frameOf(kind uint64, fields ...any) []byte {
	out := binary.AppendUvarint(nil, kind)
	for _, f := range fields {
		switch v := f.(type) {
		case int:
			out = binary.AppendUvarint(out, uint64(v))
		case []byte:
			out = binary.AppendUvarint(out, uint64(len(v)))
			out = append(out, v...)
		}
	}
	return out
}

func seedFrame(base int, update []byte) []byte { return frameOf(collab.MsgSeed, base, update) }
func publishedFrame(version int) []byte        { return frameOf(collab.MsgPublished, version) }

// roomOf is the page's shared draft as the database holds it.
func roomOf(t *testing.T, h *harness, page string) (id uuid.UUID, base, updates int, through int64) {
	t.Helper()
	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT c.id, c.base_version, (SELECT count(*) FROM page_collab_update u WHERE u.room_id = c.id), c.published_through
			FROM page_collab c WHERE c.page_id = $1`, page).Scan(&id, &base, &updates, &through)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, 0, 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return
}

// Two people edit one page: the first to open seeds it, every change is
// stored and passed on, a publish moves everybody's base, somebody who
// loses edit is let go, and a discard sends everybody to a fresh start.
func TestEditingAPageTogether(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h, withCollab(nil))
	home := h.makeMember(t, "collab")
	slug := h.slugOf(t, home.org)
	ann := api.as(t, home.user, home.org, slug)
	bobID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	bob, carl := api.as(t, bobID, home.org, slug), api.as(t, carlID, home.org, slug)
	docs := newTree(t, ann, "LIVE", "Live")
	notes := docs.add(docs.homeID, "Notes")
	want(t, restrict(t, ann, notes, nil, []any{user(home.user), user(bobID)}), http.StatusOK, "only ann and bob edit Notes")
	h.settle(t)

	t.Run("only somebody who may edit opens it, as a WebSocket", func(t *testing.T) {
		if _, status := api.anonymous().dial(t, notes); status != http.StatusUnauthorized {
			t.Errorf("nobody = %d", status)
		}
		if _, status := carl.dial(t, notes); status != http.StatusForbidden {
			t.Errorf("a reader = %d", status)
		}
		if _, status := ann.dial(t, uuid.NewString()); status != http.StatusNotFound {
			t.Errorf("a page that is not there = %d", status)
		}
		want(t, ann.get(t, collabPath(notes)), http.StatusUpgradeRequired, "a plain request")
	})

	annWS := ann.open(t, notes)
	first := annWS.load()
	if !first.seed || first.base != 1 || len(first.updates) != 0 {
		t.Fatalf("the first to open got %+v", first)
	}
	bobWS := bob.open(t, notes)
	second := bobWS.load()
	if second.seed || second.room != first.room {
		t.Fatalf("the second to open got %+v, the first %+v", second, first)
	}

	t.Run("the first content and every change reach the others and the database", func(t *testing.T) {
		annWS.send(seedFrame(1, []byte{1, 2, 3}))
		bobWS.until("the seed", is(collab.UpdateFrame([]byte{1, 2, 3})))
		bobWS.until("the base", is(collab.BaseFrame(1)))
		annWS.send(collab.UpdateFrame([]byte{4}))
		bobWS.until("ann's change", is(collab.UpdateFrame([]byte{4})))
		hello := collab.AwarenessFrame(collab.EncodeAwareness([]collab.Peer{{Client: 7, Clock: 1, State: `{"user":{"name":"Bob"}}`}}))
		bobWS.send(hello)
		annWS.until("bob's awareness", is(hello))
		if _, _, n, _ := roomOf(t, h, notes); n != 2 {
			t.Errorf("%d updates stored, want 2", n)
		}
		// Somebody opening later loads both.
		late := bob.open(t, notes)
		if got := late.load(); len(got.updates) != 2 || got.seed {
			t.Errorf("a later open loads %+v", got)
		}
		_ = late.ws.Close(websocket.StatusNormalClosure, "")
	})

	t.Run("a publish from it moves the base for everybody", func(t *testing.T) {
		want(t, ann.put(t, pagePath(notes, "/draft"), map[string]any{"title": "Notes", "body": textDoc("Together"), "baseVersion": 1}), http.StatusOK, "ann saves the draft")
		want(t, ann.post(t, pagePath(notes, "/publish"), map[string]any{}), http.StatusOK, "ann publishes")
		// Bob did not publish version 2, so he cannot say he did.
		bobWS.send(publishedFrame(2))
		annWS.send(publishedFrame(2))
		annWS.until("the base", is(collab.BaseFrame(2)))
		bobWS.until("the base", is(collab.BaseFrame(2)))
		_, base, n, through := roomOf(t, h, notes)
		if base != 2 || through == 0 || n != 2 {
			t.Errorf("the room is at base %d, %d updates, published through %d", base, n, through)
		}
	})

	t.Run("somebody who may no longer edit is let go", func(t *testing.T) {
		want(t, restrict(t, ann, notes, nil, []any{user(home.user)}), http.StatusOK, "only ann edits Notes")
		bobWS.closedWith(collab.CloseRefused)
		if _, status := bob.dial(t, notes); status != http.StatusForbidden {
			t.Errorf("bob opening again = %d", status)
		}
	})

	t.Run("a discard starts everybody afresh", func(t *testing.T) {
		other := ann.open(t, notes)
		other.load()
		annWS.send([]byte{collab.MsgDiscard})
		annWS.closedWith(collab.CloseGone)
		other.closedWith(collab.CloseGone)
		if id, _, _, _ := roomOf(t, h, notes); id != uuid.Nil {
			t.Errorf("the room %s is still there", id)
		}
		again := ann.open(t, notes).load()
		if !again.seed || again.room == first.room || again.base != 2 {
			t.Errorf("after a discard ann opens %+v", again)
		}
	})
}

// A room that holds nothing unpublished is started afresh once the page is
// published from elsewhere; one that holds changes is kept, so they are not
// lost, and its publish is refused as a conflict as any stale draft's is.
func TestASharedDraftFollowsPublishesFromElsewhere(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h, withCollab(nil))
	home := h.makeMember(t, "collab-reset")
	ann := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, ann, "RESET", "Reset")
	page := docs.add(docs.homeID, "Plans")

	ws := ann.open(t, page)
	room := ws.load()
	ws.send(seedFrame(1, []byte{1}))
	ws.until("the base", is(collab.BaseFrame(1)))
	want(t, ann.patch(t, pagePath(page), map[string]any{"title": "Plans", "body": textDoc("Elsewhere"), "version": 1}), http.StatusOK, "publish elsewhere")
	// The seed is unpublished content, so the room stays.
	kept := ann.open(t, page).load()
	if kept.room != room.room || kept.base != 1 || len(kept.updates) != 1 {
		t.Fatalf("a room with changes was not kept: %+v", kept)
	}

	want(t, ann.put(t, pagePath(page, "/draft"), map[string]any{"title": "Plans", "body": textDoc("Mine"), "baseVersion": 2}), http.StatusOK, "save over it")
	want(t, ann.post(t, pagePath(page, "/publish"), map[string]any{}), http.StatusOK, "publish version 3")
	ws.send(publishedFrame(3))
	ws.until("the base", is(collab.BaseFrame(3)))
	want(t, ann.patch(t, pagePath(page), map[string]any{"title": "Plans", "body": textDoc("Elsewhere again"), "version": 3}), http.StatusOK, "publish elsewhere again")
	fresh := ann.open(t, page).load()
	if fresh.room == room.room || !fresh.seed || fresh.base != 4 || len(fresh.updates) != 0 {
		t.Fatalf("a room with nothing unpublished was not started afresh: %+v", fresh)
	}
	// Whoever was still in the old one starts again.
	ws.closedWith(collab.CloseGone)
}

// Many updates are merged by a browser into one, in their place.
func TestASharedDraftIsCompacted(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h, withCollab(nil))
	home := h.makeMember(t, "collab-compact")
	ann := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, ann, "PACK", "Pack")
	page := docs.add(docs.homeID, "Long")

	ws := ann.open(t, page)
	ws.load()
	ws.send(seedFrame(1, []byte{1}))
	for i := byte(2); i <= 7; i++ {
		ws.send(collab.UpdateFrame([]byte{i}))
	}
	reload := ws.load()
	if len(reload.updates) != 7 {
		t.Fatalf("the reload carried %d updates", len(reload.updates))
	}
	ask := ws.until("the request", func(f []byte) bool { return f[0] == collab.MsgCompact })
	rest := ask[1:]
	from, k := binary.Uvarint(rest)
	to, k2 := binary.Uvarint(rest[k:])
	count, _ := binary.Uvarint(rest[k+k2:])
	if count != 7 {
		t.Fatalf("asked to merge %d updates", count)
	}
	ws.send(frameOf(collab.MsgCompacted, int(from), int(to), int(count), []byte{9, 9}))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, _, n, _ := roomOf(t, h, page); n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the updates were not merged")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := ann.open(t, page).load(); len(got.updates) != 1 || !bytes.Equal(got.updates[0], collab.UpdateFrame([]byte{9, 9})) {
		t.Errorf("after the merge a load carries %x", got.updates)
	}
}

// Two api processes, as two pods, pass each other's changes through Valkey.
func TestTwoAPIProcessesShareASharedDraftThroughValkey(t *testing.T) {
	h := newHarness(t)
	opts, err := redis.ParseURL(h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "stator-test:collab:" + uuid.NewString()[:8] + ":"
	var apis []*apiServer
	for range 2 {
		client := redis.NewClient(opts)
		t.Cleanup(func() { _ = client.Close() })
		bus := collab.NewValkeyBus(client, prefix)
		var hub *collab.Hub
		apis = append(apis, newAPIServer(t, h, func(s *httpapi.Server) {
			hub = collab.NewHub(bus, discard(), fastCollab())
			s.Collab = hub
		}))
		if err := bus.Listen(t.Context(), hub); err != nil {
			t.Fatal(err)
		}
	}
	home := h.makeMember(t, "collab-pods")
	slug := h.slugOf(t, home.org)
	bobID := h.addPerson(t, home.org, "member")
	ann, bob := apis[0].as(t, home.user, home.org, slug), apis[1].as(t, bobID, home.org, slug)
	docs := newTree(t, ann, "PODS", "Pods")
	page := docs.add(docs.homeID, "Shared")
	h.settle(t)

	annWS, bobWS := ann.open(t, page), bob.open(t, page)
	annWS.load()
	bobWS.load()
	annWS.send(seedFrame(1, []byte{1}))
	bobWS.until("ann's seed from the other process", is(collab.UpdateFrame([]byte{1})))
	bobWS.send(collab.UpdateFrame([]byte{2}))
	annWS.until("bob's change from the other process", is(collab.UpdateFrame([]byte{2})))
	annWS.send([]byte{collab.MsgDiscard})
	bobWS.closedWith(collab.CloseGone)
}

// postgresPods are two api processes, as two pods without Valkey, passing
// frames over a channel of the test's own; each listens as its own
// application, so a test can cut one of them off.
func postgresPods(t *testing.T, h *harness) (apis []*apiServer, names []string) {
	t.Helper()
	channel := "stator_collab_test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	for i := range 2 {
		name := channel + "_" + strconv.Itoa(i)
		dsn, err := url.Parse(h.cfg.DB.PrimaryURL)
		if err != nil {
			t.Fatal(err)
		}
		q := dsn.Query()
		q.Set("application_name", name)
		dsn.RawQuery = q.Encode()
		bus := collab.NewPostgresBus(h.cluster.Primary(), dsn.String(), channel, discard())
		bus.Backoff = listenerBackoff
		var hub *collab.Hub
		apis = append(apis, newAPIServer(t, h, func(s *httpapi.Server) {
			hub = collab.NewHub(bus, discard(), fastCollab())
			s.Collab = hub
		}))
		if err := bus.Listen(t.Context(), hub); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return apis, names
}

// listenerBackoff is long enough that a change sent after a listener was cut
// off is stored before it listens again, so only catching up can bring it.
const listenerBackoff = 2 * time.Second

// Two api processes without Valkey pass each other's changes, awareness and
// publishes through Postgres.
func TestTwoAPIProcessesShareASharedDraftThroughPostgres(t *testing.T) {
	h := newHarness(t)
	apis, _ := postgresPods(t, h)
	home := h.makeMember(t, "collab-pg-pods")
	slug := h.slugOf(t, home.org)
	bobID := h.addPerson(t, home.org, "member")
	ann, bob := apis[0].as(t, home.user, home.org, slug), apis[1].as(t, bobID, home.org, slug)
	docs := newTree(t, ann, "PGPODS", "Pods")
	page := docs.add(docs.homeID, "Shared")
	h.settle(t)

	annWS, bobWS := ann.open(t, page), bob.open(t, page)
	annWS.load()
	bobWS.load()
	annWS.send(seedFrame(1, []byte{1}))
	bobWS.until("ann's seed from the other process", is(collab.UpdateFrame([]byte{1})))
	bobWS.until("the seed's base from the other process", is(collab.BaseFrame(1)))
	bobWS.send(collab.UpdateFrame([]byte{2}))
	annWS.until("bob's change from the other process", is(collab.UpdateFrame([]byte{2})))
	// An update past what one notification holds goes as a pointer all the same.
	large := bytes.Repeat([]byte{3}, collab.MaxNotifyBytes*2)
	annWS.send(collab.UpdateFrame(large))
	bobWS.until("ann's large change from the other process", is(collab.UpdateFrame(large)))

	hello := collab.AwarenessFrame(collab.EncodeAwareness([]collab.Peer{{Client: 7, Clock: 1, State: `{"user":{"name":"Bob"}}`}}))
	bobWS.send(hello)
	annWS.until("bob's awareness from the other process", is(hello))

	want(t, ann.put(t, pagePath(page, "/draft"), map[string]any{"title": "Shared", "body": textDoc("Together"), "baseVersion": 1}), http.StatusOK, "ann saves the draft")
	want(t, ann.post(t, pagePath(page, "/publish"), map[string]any{}), http.StatusOK, "ann publishes")
	annWS.send(publishedFrame(2))
	bobWS.until("the base from the other process", is(collab.BaseFrame(2)))

	annWS.send([]byte{collab.MsgDiscard})
	bobWS.closedWith(collab.CloseGone)
}

// A process whose listening connection is cut off listens again and
// relays what was stored meanwhile.
func TestAProcessCutOffFromPostgresCatchesUp(t *testing.T) {
	h := newHarness(t)
	apis, names := postgresPods(t, h)
	home := h.makeMember(t, "collab-pg-catchup")
	slug := h.slugOf(t, home.org)
	bobID := h.addPerson(t, home.org, "member")
	ann, bob := apis[0].as(t, home.user, home.org, slug), apis[1].as(t, bobID, home.org, slug)
	docs := newTree(t, ann, "PGCATCH", "Catch")
	page := docs.add(docs.homeID, "Missed")
	h.settle(t)

	annWS, bobWS := ann.open(t, page), bob.open(t, page)
	annWS.load()
	bobWS.load()
	annWS.send(seedFrame(1, []byte{1}))
	bobWS.until("ann's seed", is(collab.UpdateFrame([]byte{1})))

	listeners := func() int {
		var n int
		if err := h.cluster.Primary().QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE application_name = $1`, names[1]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	until := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !done() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// The listener connects as the app role, which may end its own backends.
	if _, err := h.cluster.Primary().Exec(context.Background(),
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1`, names[1]); err != nil {
		t.Fatal(err)
	}
	until("bob's listener to go", func() bool { return listeners() == 0 })
	annWS.send(collab.UpdateFrame([]byte{2}))
	annWS.send(collab.UpdateFrame([]byte{3}))
	until("ann's changes to be stored", func() bool { _, _, n, _ := roomOf(t, h, page); return n == 3 })
	if listeners() != 0 {
		t.Fatal("bob's process listened again before the changes were stored, so this proves nothing")
	}
	bobWS.until("the first change missed", is(collab.UpdateFrame([]byte{2})))
	bobWS.until("the second change missed", is(collab.UpdateFrame([]byte{3})))
	until("bob's listener to be back", func() bool { return listeners() == 1 })
	annWS.send(collab.UpdateFrame([]byte{4}))
	bobWS.until("a change after catching up", is(collab.UpdateFrame([]byte{4})))
}

// The service refusing is not proof: straight through SQL, only somebody
// who may edit the page reads, adds to or throws away its shared draft, an
// update names its sender whatever it says, and none outgrows the limit.
func TestTheSharedDraftIsWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h, withCollab(nil))
	home := h.makeMember(t, "collab-rls")
	slug := h.slugOf(t, home.org)
	ann := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	other := h.makeMember(t, "collab-rls-other")
	docs := newTree(t, ann, "WALL", "Wall")
	page := docs.add(docs.homeID, "Walled")
	ws := ann.open(t, page)
	room := ws.load()
	ws.send(seedFrame(1, []byte{1}))
	ws.until("the base", is(collab.BaseFrame(1)))
	want(t, restrict(t, ann, page, nil, []any{user(home.user)}), http.StatusOK, "only ann edits")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	insert := `INSERT INTO page_collab_update (org_id, page_id, room_id, body, created_by) VALUES ($1, $2, $3, $4, $5)`

	t.Run("a reader neither reads nor writes it", func(t *testing.T) {
		actAs(t, conn, home.org, bobID)
		for _, table := range []string{"page_collab", "page_collab_update"} {
			if n := count(`SELECT count(*) FROM `+table+` WHERE page_id = $1`, page); n != 0 {
				t.Errorf("bob reads %d rows of %s", n, table)
			}
		}
		denied(t, conn, "an update", insert, home.org, page, room.room, []byte{2}, bobID)
		denied(t, conn, "a room of his own", `INSERT INTO page_collab (org_id, page_id, base_version) VALUES ($1, $2, 1)`, home.org, page)
		untouched(t, conn, "throwing it away", `DELETE FROM page_collab WHERE page_id = $1`, page)
		untouched(t, conn, "its updates", `DELETE FROM page_collab_update WHERE page_id = $1`, page)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		actAs(t, conn, other.org, other.user)
		if n := count(`SELECT count(*) FROM page_collab_update`); n != 0 {
			t.Errorf("another organization reads %d updates", n)
		}
		denied(t, conn, "an update into it", insert, home.org, page, room.room, []byte{2}, other.user)
	})

	t.Run("an editor's update names her, and stays within the limit", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		if _, err := conn.Exec(ctx, insert, home.org, page, room.room, []byte{3}, bobID); err != nil {
			t.Fatalf("ann adds an update: %v", err)
		}
		if n := count(`SELECT count(*) FROM page_collab_update WHERE page_id = $1 AND created_by = $2`, page, home.user); n != 2 {
			t.Errorf("%d of ann's updates name her", n)
		}
		denied(t, conn, "rewriting an update", `UPDATE page_collab_update SET body = '\x09' WHERE page_id = $1`, page)
		denied(t, conn, "an update past the limit", insert, home.org, page, room.room, make([]byte, collab.MaxMessageBytes+1), home.user)
		denied(t, conn, "an empty update", insert, home.org, page, room.room, []byte{}, home.user)
		refused(t, conn, "an update to a room that is not there", insert, home.org, page, uuid.New(), []byte{4}, home.user)
	})
}
