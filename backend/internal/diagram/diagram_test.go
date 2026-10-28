package diagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func TestKrokisRefusalsAreReadAsTheReaderIsTold(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		said   string
		want   error
		reason string
	}{
		{"a diagram it cannot read gives its first line, without the status", 400, "Error 400: Syntax Error? (Assumed diagram type: sequence) (line: 2)\n\nat line 2", nil, "Syntax Error? (Assumed diagram type: sequence) (line: 2)"},
		{"a reason after blank lines is still found", 400, "\n\n  Error 400: no such shape\n", nil, "no such shape"},
		{"a bare 500 is a drawing Kroki cut off", 500, "", ErrTimeout, ""},
		{"a 500 with a reason is the diagram at fault", 500, "Error 500: d2 compile error: bad edge", nil, "d2 compile error: bad edge"},
		{"a gateway timeout is a drawing cut off", 504, "", ErrTimeout, ""},
		{"a 4xx without a reason is Kroki at fault", 404, "", ErrUnavailable, ""},
		{"a 503 is Kroki out of reach", 503, "Service Unavailable", ErrUnavailable, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := refusal(c.status, []byte(c.said))
			var unreadable *UnreadableError
			if c.want != nil {
				if !errors.Is(err, c.want) {
					t.Fatalf("got %v, want %v", err, c.want)
				}
				return
			}
			if !errors.As(err, &unreadable) || unreadable.Reason != c.reason {
				t.Fatalf("got %v, want the reason %q", err, c.reason)
			}
		})
	}
}

func TestALongReasonIsCut(t *testing.T) {
	got := firstLine([]byte("Error 400: " + strings.Repeat("x", 1000)))
	if len([]rune(got)) != MaxReasonLength || !strings.HasSuffix(got, "...") {
		t.Fatalf("got %d characters: %q", len([]rune(got)), got)
	}
}

func TestOnlyKrokisLanguagesWithSomeTextAreSent(t *testing.T) {
	on := New("http://kroki:8000", nil, nil)
	for _, c := range []struct {
		svc      *Service
		language string
		source   string
		want     error
	}{
		{on, document.DiagramMermaid, "graph TD", ErrLanguage},
		{on, "bpmn", "<xml/>", ErrLanguage},
		{on, "", "x", ErrLanguage},
		{New("", nil, nil), document.DiagramPlantUML, "a -> b", ErrOff},
		{nil, document.DiagramPlantUML, "a -> b", ErrOff},
		{on, document.DiagramD2, " \n\t", ErrEmpty},
		{on, document.DiagramGraphviz, strings.Repeat("ä", document.MaxDiagramLength+1), ErrTooLong},
		{on, document.DiagramGraphviz, strings.Repeat("ä", document.MaxDiagramLength), nil},
		{on, document.DiagramStructurizr, "workspace {}", nil},
	} {
		if got := c.svc.Check(c.language, c.source); !errors.Is(got, c.want) {
			t.Errorf("%q in %q: %v, want %v", c.source[:min(len(c.source), 20)], c.language, got, c.want)
		}
	}
}

func TestAServerWithoutKrokiOffersMermaidAlone(t *testing.T) {
	if got := New("", nil, nil).Languages(); len(got) != 1 || got[0] != document.DiagramMermaid {
		t.Errorf("without Kroki: %v", got)
	}
	if got := New("http://kroki:8000", nil, nil).Languages(); strings.Join(got, ",") != "mermaid,plantuml,structurizr,graphviz,d2" {
		t.Errorf("with Kroki: %v", got)
	}
}

func TestTheHashNamesTheLanguageAndTheSource(t *testing.T) {
	// printf 'plantuml\na -> b' | sha256sum, which a browser computes the same way.
	if got := Hash("plantuml", "a -> b"); got != "c0a8ecc53134aa4a7f7c3755bae5026c7659c435622e5d0f19b042c0f9802886" {
		t.Fatalf("hash %q", got)
	}
	if Hash("d2", "a -> b") == Hash("graphviz", "a -> b") {
		t.Error("the same text in two languages names the same diagram")
	}
}

func TestADrawingIsAskedOfKrokiByLanguage(t *testing.T) {
	var path, body, contentType atomic.Value
	kroki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		contentType.Store(r.Header.Get("Content-Type"))
		raw, _ := io.ReadAll(r.Body)
		body.Store(string(raw))
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>drawn</text></svg>`))
	}))
	t.Cleanup(kroki.Close)
	svg, err := New(kroki.URL+"/", nil, nil).Draw(context.Background(), document.DiagramGraphviz, "digraph { a -> b }")
	if err != nil || !strings.Contains(string(svg), "drawn") {
		t.Fatalf("drew %q, %v", svg, err)
	}
	if path.Load() != "/graphviz/svg" || body.Load() != "digraph { a -> b }" || !strings.HasPrefix(contentType.Load().(string), "text/plain") {
		t.Errorf("asked %v with %v as %v", path.Load(), body.Load(), contentType.Load())
	}
}

func TestWhatKrokiSaysIsCheckedAndBounded(t *testing.T) {
	var answer atomic.Value
	kroki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer.Load().(string)))
	}))
	t.Cleanup(kroki.Close)
	svc := New(kroki.URL, nil, nil)
	answer.Store("<svg>" + strings.Repeat(" ", MaxSVGBytes) + "</svg>")
	if _, err := svc.Draw(context.Background(), document.DiagramD2, "a -> b"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a drawing over 2 MB: %v", err)
	}
	answer.Store("not a drawing")
	if _, err := svc.Draw(context.Background(), document.DiagramD2, "a -> b"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("an answer without SVG: %v", err)
	}
}

func TestKrokiOutOfReachCannotDrawRightNow(t *testing.T) {
	kroki := httptest.NewServer(http.NotFoundHandler())
	kroki.Close()
	if _, err := New(kroki.URL, nil, nil).Draw(context.Background(), document.DiagramPlantUML, "a -> b"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a closed Kroki: %v", err)
	}
}

func TestEverySlotTakenIsBusy(t *testing.T) {
	svc := New("http://kroki:8000", nil, nil)
	svc.timeout = 50 * time.Millisecond
	for range Concurrency {
		svc.slots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	started := time.Now()
	_, err := svc.ask(ctx, document.DiagramPlantUML, "a -> b")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("with every slot taken: %v", err)
	}
	if waited := time.Since(started); waited < svc.timeout {
		t.Errorf("gave up after %s, before the call's %s", waited, svc.timeout)
	}
}
