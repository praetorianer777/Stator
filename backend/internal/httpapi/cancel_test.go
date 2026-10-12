package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/praetorianer777/stator/backend/internal/observability"
)

// serverDeadline is the request timeout the deadline case runs into at once.
const serverDeadline = time.Millisecond

// cancelChain is the front of Routes' chain around one handler, with its log
// lines kept and its metrics scrapable; done closes once a request is through.
type cancelChain struct {
	h    http.Handler
	tel  *observability.Telemetry
	logs *bytes.Buffer
	done chan struct{}
}

func newCancelChain(t *testing.T, timeout time.Duration, handler http.HandlerFunc) *cancelChain {
	t.Helper()
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	c := &cancelChain{tel: tel, logs: &bytes.Buffer{}, done: make(chan struct{}, 1)}
	log := slog.New(slog.NewJSONHandler(c.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { c.done <- struct{}{} }()
			next.ServeHTTP(w, r)
		})
	})
	r.Use(requestID, observe(tel.Metrics), logging(log), middleware.Timeout(timeout))
	r.Get("/work", handler)
	c.h = r
	return c
}

func (c *cancelChain) scrape() string {
	rec := httptest.NewRecorder()
	c.tel.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

// lines are the log records written, as level and message.
func (c *cancelChain) lines(t *testing.T) map[string]string {
	t.Helper()
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(c.logs.Bytes()))
	for sc.Scan() {
		var rec struct{ Level, Msg string }
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("a log line is not JSON: %q", sc.Text())
		}
		got[rec.Msg] = rec.Level
	}
	return got
}

// A client hanging up over a real connection is nginx's 499 and an info line,
// not a 500 and an error.
func TestAClientHangingUpIsNotAServerError(t *testing.T) {
	started := make(chan struct{})
	c := newCancelChain(t, time.Minute, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		respondError(w, r, asValidationError(r.Context().Err()))
	})
	srv := httptest.NewServer(c.h)
	defer srv.Close()

	ctx, hangUp := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/work", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		<-started
		hangUp()
	}()
	if _, err := srv.Client().Do(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("the client's request ended with %v, want it cancelled", err)
	}
	<-c.done

	scrape := c.scrape()
	if want := `stator_http_requests_total{method="GET",route="/work",status="499"} 1`; !strings.Contains(scrape, want) {
		t.Errorf("the scrape lacks %s", want)
	}
	if strings.Contains(scrape, `status="500"`) {
		t.Error("the hang-up was counted as a 500")
	}
	lines := c.lines(t)
	if lines["request cancelled by the client"] != slog.LevelInfo.String() {
		t.Errorf("no info line names the hang-up: %v", lines)
	}
	for msg, level := range lines {
		if level == slog.LevelError.String() {
			t.Errorf("the hang-up was logged as an error: %s", msg)
		}
	}
}

// A deadline the server hit, or a cancellation of its own while the client
// still waits, stays an internal error.
func TestTheServersOwnCancellationStaysAnError(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout time.Duration
		handler http.HandlerFunc
	}{
		"deadline": {serverDeadline, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			respondError(w, r, asValidationError(r.Context().Err()))
		}},
		"own cancel": {time.Minute, func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			respondError(w, r, asValidationError(ctx.Err()))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			c := newCancelChain(t, tc.timeout, tc.handler)
			resp, body := serve(t, c.h, httptest.NewRequest(http.MethodGet, "/work", nil))
			<-c.done
			if resp.StatusCode != http.StatusInternalServerError || errorOf(t, body)["code"] != "internal_error" {
				t.Errorf("answered %d %v, want a 500", resp.StatusCode, body)
			}
			// The timeout middleware stamps a 504 over the 500 once the deadline passed.
			if want := `stator_http_requests_total{method="GET",route="/work",status="5`; !strings.Contains(c.scrape(), want) {
				t.Errorf("the scrape counts no server error: %s", c.scrape())
			}
			if level := c.lines(t)["request failed"]; level != slog.LevelError.String() {
				t.Errorf("the failure was logged at %q, want ERROR", level)
			}
		})
	}
}
