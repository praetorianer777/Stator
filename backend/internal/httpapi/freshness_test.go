package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// spyFreshness is a tracker that also says which keys were asked for.
type spyFreshness struct {
	mu    sync.Mutex
	lsns  map[string]db.LSN
	asked []string
}

func (f *spyFreshness) Note(_ context.Context, key string, lsn db.LSN) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lsns[key] = max(f.lsns[key], lsn)
}

func (f *spyFreshness) Required(_ context.Context, key string) db.LSN {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, key)
	return f.lsns[key]
}

func freshServer(t *testing.T) (*Server, *spyFreshness) {
	t.Helper()
	s := newServer(t)
	f := &spyFreshness{lsns: map[string]db.LSN{}}
	s.Fresh = f
	return s, f
}

// writing notes a position, the way a handler does after a write.
func writing(lsn db.LSN) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noteWrite(r.Context(), lsn)
		w.WriteHeader(http.StatusNoContent)
	})
}

func clientCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == ClientCookie {
			return c
		}
	}
	return nil
}

func TestAWriteWithoutASessionIsKeyedByAClientCookie(t *testing.T) {
	s, f := freshServer(t)
	h := s.readYourWrites(writing(42))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/themes", nil))
	cookie := clientCookie(rec.Result())
	if cookie == nil || !validClientID(cookie.Value) || !cookie.HttpOnly {
		t.Fatalf("a first write should be handed an http-only client cookie, got %+v", cookie)
	}
	if got := f.lsns["c:"+cookie.Value]; got != 42 {
		t.Fatalf("the write was noted as %v under the client cookie, want 42: %v", got, f.lsns)
	}

	read := httptest.NewRequest(http.MethodGet, "/api/v1/themes", nil)
	read.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.readYourWrites(http.NotFoundHandler()).ServeHTTP(rec, read)
	if len(f.asked) != 2 || f.asked[1] != "c:"+cookie.Value {
		t.Fatalf("the next read did not ask for the client's position: %v", f.asked)
	}
	if clientCookie(rec.Result()) != nil {
		t.Error("a client that has a cookie was handed another")
	}
}

func TestASessionWinsOverTheClientCookie(t *testing.T) {
	s, f := freshServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/themes/active", nil)
	req.AddCookie(&http.Cookie{Name: ClientCookie, Value: "00112233445566778899aabbccddeeff"})
	req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, &auth.Principal{SessionID: "sess-1"}))

	rec := httptest.NewRecorder()
	s.readYourWrites(writing(7)).ServeHTTP(rec, req)
	if f.lsns["s:sess-1"] != 7 || len(f.lsns) != 1 {
		t.Fatalf("the write should be the session's alone: %v", f.lsns)
	}
	if clientCookie(rec.Result()) != nil {
		t.Error("a caller with a session was handed a client cookie")
	}
}

func TestAReadWithNoIdentityIsLeftAlone(t *testing.T) {
	s, f := freshServer(t)
	rec := httptest.NewRecorder()
	s.readYourWrites(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/themes", nil))
	if len(f.asked) != 0 || clientCookie(rec.Result()) != nil {
		t.Fatalf("a read with nothing to key it by asked %v and set %v", f.asked, rec.Result().Cookies())
	}
}

func TestAForgedClientCookieIsReplaced(t *testing.T) {
	s, f := freshServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/themes", nil)
	req.AddCookie(&http.Cookie{Name: ClientCookie, Value: "stator:anything"})
	rec := httptest.NewRecorder()
	s.readYourWrites(writing(9)).ServeHTTP(rec, req)
	cookie := clientCookie(rec.Result())
	if cookie == nil || f.lsns["c:"+cookie.Value] != 9 {
		t.Fatalf("a malformed cookie should be replaced by a fresh one: %v, %v", cookie, f.lsns)
	}
	if _, ok := f.lsns["c:stator:anything"]; ok {
		t.Fatal("a malformed cookie became a key in the store")
	}
}

func TestWithoutATrackerNothingIsKept(t *testing.T) {
	s := newServer(t)
	rec := httptest.NewRecorder()
	s.readYourWrites(writing(3)).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if clientCookie(rec.Result()) != nil {
		t.Fatal("a server without a tracker handed out a client cookie")
	}
}
