package httpapi

import (
	"net/http"
	"sync"
	"time"
)

// A brake per key per window, adapted from Armature's portal door. It is per
// process, which is what a brake needs to be; a quota would be a table.

// throttleMaxKeys bounds what a brake remembers at once.
const throttleMaxKeys = 10000

type throttle struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	seen   map[string][]time.Time
}

func newThrottle(limit int, window time.Duration) *throttle {
	return &throttle{limit: limit, window: window, seen: map[string][]time.Time{}}
}

// allow records one request from a key and says whether it was inside the limit.
// A key is forgotten as soon as its window has passed, so the brake holds
// nothing about anyone who is not knocking.
func (t *throttle) allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for other, times := range t.seen {
		if other != key && len(times) > 0 && now.Sub(times[len(times)-1]) >= t.window {
			delete(t.seen, other)
		}
	}
	// A caller who names a new key every time must not be able to grow this
	// map without bound; the oldest go first, and they were quiet anyway.
	if len(t.seen) > throttleMaxKeys {
		for other, times := range t.seen {
			if other != key && (len(times) == 0 || now.Sub(times[len(times)-1]) > t.window/2) {
				delete(t.seen, other)
			}
		}
	}
	kept := t.seen[key][:0]
	for _, at := range t.seen[key] {
		if now.Sub(at) < t.window {
			kept = append(kept, at)
		}
	}
	if len(kept) >= t.limit {
		t.seen[key] = kept
		return false
	}
	t.seen[key] = append(kept, now)
	return true
}

// gate answers a request whose key is over the limit and says whether it may go on.
func (t *throttle) gate(w http.ResponseWriter, r *http.Request, key, message string) bool {
	if t.allow(key, time.Now()) {
		return true
	}
	w.Header().Set("Retry-After", "60")
	respondError(w, r, &APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests", Message: message})
	return false
}
