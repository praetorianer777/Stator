//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// Settings for the clusters these tests open themselves: a lag bound small
// enough to cross in a test, and a health loop either quick or out of the way.
const (
	testMaxLag        = 300 * time.Millisecond
	quickHealth       = 100 * time.Millisecond
	neverHealth       = time.Hour
	lagBuildUp        = 3 * testMaxLag
	rotationWait      = 15 * time.Second
	writeReadRounds   = 50
	replicaCaughtWait = 15 * time.Second
)

func needReplica(t *testing.T, h *harness) {
	t.Helper()
	if h.replica == nil {
		t.Skipf("%s and STATOR_DB_REPLICA_URLS are not set; run the suite with make test-integration", envReplicaSuperuser)
	}
}

// openCluster opens a cluster of the test's own with adjusted settings.
func (h *harness) openCluster(t *testing.T, maxLag, health time.Duration) *db.Cluster {
	t.Helper()
	cfg := h.cfg.DB
	cfg.MaxReplicaLag = maxLag
	cfg.HealthInterval = health
	c, err := db.Open(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func addTheme(t *testing.T, c *db.Cluster, m member, name string) db.LSN {
	t.Helper()
	lsn, err := c.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO theme (org_id, owner_id, name) VALUES (current_org_id(), $1, $2)`, m.user, name)
		return err
	})
	if err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if lsn == 0 {
		t.Fatal("Write returned a zero position, so nothing can be pinned to it")
	}
	return lsn
}

func countThemes(t *testing.T, c *db.Cluster, ctx context.Context, name string) int {
	t.Helper()
	var n int
	if err := c.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM theme WHERE name = $1`, name).Scan(&n)
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	return n
}

// The baseline: without it the tests below would pass on a cluster that never
// used a replica at all.
func TestReadsReachTheReplica(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	m := h.makeMember(t, "routing")

	before := h.cluster.Stats()
	for range 5 {
		countThemes(t, h.cluster, m.ctx, "none")
	}
	after := h.cluster.Stats()
	if served := after.ReadsToReplica - before.ReadsToReplica; served != 5 {
		t.Errorf("%d of 5 reads went to the replica (primary: %d)", served, after.ReadsToPrimary-before.ReadsToPrimary)
	}
}

// A position read inside the transaction sits before the commit record, and
// a replica there has not replayed the change.
func TestWriteReportsThePositionPastItsCommit(t *testing.T) {
	h := newHarness(t)
	m := h.makeMember(t, "commitlsn")

	var inside db.LSN
	after, err := h.cluster.Write(m.ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO theme (org_id, owner_id, name) VALUES (current_org_id(), $1, 'commit')`, m.user); err != nil {
			return err
		}
		var text string
		if err := tx.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&text); err != nil {
			return err
		}
		var err error
		inside, err = db.ParseLSN(text)
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if after <= inside {
		t.Fatalf("reported position %v is not past the one inside the transaction %v", after, inside)
	}
}

// With replay paused the standby is behind by construction, rather than by a
// race the test hopes to win.
func TestReadYourWritesWhileReplayIsPaused(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	// The health loop is kept out of the way, so what is proven is the check
	// on the connection each read gets.
	c := h.openCluster(t, 0, neverHealth)
	m := h.makeMember(t, "ryw")

	h.pauseReplay(t)
	lsn := addTheme(t, c, m, "after-pause")

	t.Run("the writer sees their write, from the primary", func(t *testing.T) {
		before := c.Stats()
		if got := countThemes(t, c, db.PinLSN(m.ctx, lsn), "after-pause"); got != 1 {
			t.Fatalf("a reader pinned to their own write saw %d rows, want 1", got)
		}
		after := c.Stats()
		if after.StaleFallbacks-before.StaleFallbacks != 1 || after.ReadsToPrimary-before.ReadsToPrimary != 1 {
			t.Errorf("the pinned read was not a stale fallback to the primary: %+v then %+v", before, after)
		}
	})

	t.Run("a reader with no write of their own is served the older snapshot", func(t *testing.T) {
		before := c.Stats()
		if got := countThemes(t, c, m.ctx, "after-pause"); got != 0 {
			t.Fatalf("the paused replica answered with %d rows; it should not have the write yet", got)
		}
		if c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
			t.Error("the unpinned read did not go to the replica")
		}
	})

	h.resumeReplay(t)
	if !h.waitForReplica(t, lsn, replicaCaughtWait) {
		t.Fatal("the replica did not catch up after replay resumed")
	}

	t.Run("once replayed, the pinned read goes back to the replica", func(t *testing.T) {
		before := c.Stats()
		if got := countThemes(t, c, db.PinLSN(m.ctx, lsn), "after-pause"); got != 1 {
			t.Fatalf("after replay resumed the pinned read saw %d rows, want 1", got)
		}
		if c.Stats().ReadsToReplica-before.ReadsToReplica != 1 {
			t.Error("the caught up replica did not serve the pinned read")
		}
	})
}

// A connection further behind than the bound serves nobody, even between two
// passes of the health loop.
func TestALaggingConnectionFallsBackToThePrimary(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	c := h.openCluster(t, testMaxLag, neverHealth)
	m := h.makeMember(t, "lag")

	h.pauseReplay(t)
	// A write replayed before the pause stops the replay clock; the backlog
	// after it is what makes the lag measurable as time.
	for i := range 3 {
		addTheme(t, c, m, fmt.Sprintf("backlog-%d", i))
		time.Sleep(lagBuildUp / 3)
	}

	before := c.Stats()
	if got := countThemes(t, c, m.ctx, "backlog-2"); got != 1 {
		t.Fatalf("a read while the replica lags saw %d rows, want the primary's 1", got)
	}
	after := c.Stats()
	if after.LagFallbacks-before.LagFallbacks != 1 || after.ReadsToPrimary-before.ReadsToPrimary != 1 {
		t.Errorf("the read was not a lag fallback to the primary: %+v then %+v", before, after)
	}
}

// The health loop takes a lagging replica out of the rotation altogether and
// puts it back once it has caught up.
func TestALaggingReplicaLeavesTheRotationAndReturns(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	c := h.openCluster(t, testMaxLag, quickHealth)
	m := h.makeMember(t, "rotation")

	h.pauseReplay(t)
	var last db.LSN
	for i := range 3 {
		last = addTheme(t, c, m, fmt.Sprintf("rotation-%d", i))
		time.Sleep(lagBuildUp / 3)
	}
	waitFor(t, "the replica to leave the rotation", func() bool { return !c.Stats().Replicas[0].Healthy })

	before := c.Stats()
	if got := countThemes(t, c, m.ctx, "rotation-2"); got != 1 {
		t.Fatalf("with the replica out, a read saw %d rows, want the primary's 1", got)
	}
	if c.Stats().NoHealthyReplicaHit-before.NoHealthyReplicaHit != 1 {
		t.Error("the read was not sent to the primary for want of a healthy replica")
	}

	h.resumeReplay(t)
	if !h.waitForReplica(t, last, replicaCaughtWait) {
		t.Fatal("the replica did not catch up after replay resumed")
	}
	waitFor(t, "the replica to rejoin the rotation", func() bool { return c.Stats().Replicas[0].Healthy })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(rotationWait)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %s", rotationWait, what)
		}
		time.Sleep(quickHealth)
	}
}

// Through the API, as a browser: every write is seen by the read right after
// it, which with replay paused only a pinned read can manage.
func TestTheAPIReadsItsOwnWrites(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	api := newAPIServer(t, h)
	m := h.makeMember(t, "api-ryw")
	person := api.as(t, m.user, m.org, h.slugOf(t, m.org))

	roundTrip := func(t *testing.T, name string) {
		t.Helper()
		made := want(t, person.post(t, "/api/v1/themes", map[string]any{"name": name}), http.StatusCreated, "make "+name)
		id := obj(t, made, "theme")["id"].(string)
		want(t, person.get(t, "/api/v1/themes/"+id), http.StatusOK, "read "+name+" straight back")
		renamed := name + " renamed"
		want(t, person.patch(t, "/api/v1/themes/"+id, map[string]any{"name": renamed}), http.StatusOK, "rename "+name)
		got := want(t, person.get(t, "/api/v1/themes/"+id), http.StatusOK, "read the rename back")
		if obj(t, got, "theme")["name"] != renamed {
			t.Fatalf("the read after the rename saw %s", got.Raw)
		}
	}

	t.Run("with replication running", func(t *testing.T) {
		for i := range writeReadRounds {
			roundTrip(t, fmt.Sprintf("Round %d", i))
		}
	})

	t.Run("with replay paused", func(t *testing.T) {
		h.pauseReplay(t)
		before := h.cluster.Stats()
		roundTrip(t, "Paused "+uuid.NewString()[:8])
		if h.cluster.Stats().StaleFallbacks == before.StaleFallbacks {
			t.Error("no read fell back to the primary, so the replica cannot have been behind")
		}
		h.resumeReplay(t)
	})

	t.Run("somebody else is not held to another's write", func(t *testing.T) {
		h.pauseReplay(t)
		made := want(t, person.post(t, "/api/v1/themes", map[string]any{"name": "Unseen " + uuid.NewString()[:8]}), http.StatusCreated, "make a theme")
		id := obj(t, made, "theme")["id"].(string)
		stranger := api.as(t, m.user, m.org, h.slugOf(t, m.org))
		stranger.eager = true
		want(t, stranger.get(t, "/api/v1/themes/"+id), http.StatusNotFound, "a fresh browser reads the paused replica")
		h.resumeReplay(t)
	})
}

// The store is shared by every api process, so it, not a process, decides
// that an older position never replaces a newer one.
func TestValkeyKeepsTheNewestPosition(t *testing.T) {
	h := newHarness(t)
	tracker := h.freshness(t)
	ctx := t.Context()
	key := "c:" + uuid.NewString()

	tracker.Note(ctx, key, 0x1_0000_0000)
	tracker.Note(ctx, key, 0xFFFF)
	if got := tracker.Required(ctx, key); got != 0x1_0000_0000 {
		t.Fatalf("Required = %v after an older position arrived, want 1/0", got)
	}
	tracker.Note(ctx, key, 0x1_0000_0001)
	if got := tracker.Required(ctx, key); got != 0x1_0000_0001 {
		t.Fatalf("Required = %v, want the newer 1/1", got)
	}
	if got := tracker.Required(ctx, "c:"+uuid.NewString()); got != 0 {
		t.Fatalf("a key that never wrote requires %v", got)
	}
}
