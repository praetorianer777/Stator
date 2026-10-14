//go:build integration

package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
)

const (
	// claimTries is how often the test offers an event before giving up: a
	// worker of the test's other than the one watched may take one first.
	claimTries = 5
	// claimWait is how long one offer waits for the suite's worker to take it.
	claimWait = 5 * time.Second
	// leaseTopic is a topic no handler of the product knows.
	leaseTopic = "test.lease"
)

// deleteBound is how long deleting the handled event may take while the
// handler runs; it waits on nothing, so this is generous.
var deleteBound = bound{deadline: 10 * time.Second, watchdog: 2 * time.Second}

// errNotThisTest leaves an event the test did not offer to another worker.
var errNotThisTest = errors.New("not this test's event")

// While a handler runs, the worker holds no lock on its event: a handler's
// write may wait on a delete that reaches the event, and a delete that waited
// on the event in turn hung the suite until go test gave up (#217).
func TestOutboxWorkerHoldsNoLockWhileItHandles(t *testing.T) {
	h := newHarness(t)
	h.makeMember(t, "lease")

	var (
		mu   sync.Mutex
		ours = map[uuid.UUID]bool{}
	)
	started := make(chan uuid.UUID, 1)
	release := make(chan struct{})
	handler := events.HandlerFunc(func(ctx context.Context, e events.Event) error {
		mu.Lock()
		mine := ours[e.ID]
		mu.Unlock()
		if !mine {
			return errNotThisTest
		}
		started <- e.ID
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		events.NewWorker(h.cluster, handler, discard()).WithBatch(1).WithIdle(workerIdle).Run(ctx)
	}()
	var releaseOnce sync.Once
	stop := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		stop()
		cancel()
		<-done
	})

	var handling uuid.UUID
	for try := 0; try < claimTries && handling == uuid.Nil; try++ {
		var id uuid.UUID
		if err := h.super.QueryRow(context.Background(),
			`INSERT INTO outbox_event (topic, payload) VALUES ($1, '{}') RETURNING id`, leaseTopic).Scan(&id); err != nil {
			t.Fatalf("add an event: %v", err)
		}
		mu.Lock()
		ours[id] = true
		mu.Unlock()
		select {
		case handling = <-started:
		case <-time.After(claimWait):
			if h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1 AND processed_at IS NULL`, id) > 0 {
				t.Fatalf("the worker did not take event %s within %s", id, claimWait)
			}
		}
	}
	if handling == uuid.Nil {
		t.Fatalf("another worker took every one of %d events first", claimTries)
	}

	if report, err := h.execBounded(t, h.super, deleteBound, `DELETE FROM outbox_event WHERE id = $1`, handling); err != nil {
		t.Fatalf("deleting the event while it is handled: %v\n%s", err, report)
	}
	stop()
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE id = $1`, handling); n != 0 {
		t.Errorf("the deleted event is left %d times", n)
	}
}
