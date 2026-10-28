//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/diagram"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// stubKroki answers as Kroki does, by what the diagram's text asks for, and
// counts what it was asked.
type stubKroki struct {
	srv   *httptest.Server
	mu    sync.Mutex
	asked map[string]int
}

func newStubKroki(t *testing.T) *stubKroki {
	t.Helper()
	k := &stubKroki{asked: map[string]int{}}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		source := string(raw)
		k.mu.Lock()
		k.asked[r.URL.Path+" "+source]++
		k.mu.Unlock()
		switch {
		case strings.Contains(source, "unreadable"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, "Error 400: Syntax Error? (Assumed diagram type: sequence) (line: 2)\n\nmore that nobody reads")
		case strings.Contains(source, "endless"):
			// Kroki cuts a drawing off after about nine seconds with a bare 500.
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg"><text>%s</text></svg>`, strings.ReplaceAll(source, "<", "&lt;"))
		}
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *stubKroki) times(language, source string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.asked["/"+language+"/svg "+source]
}

func (k *stubKroki) total() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, c := range k.asked {
		n += c
	}
	return n
}

// drawing fails unless the answer is an SVG served as an image that runs
// nothing, and returns it.
func drawing(t *testing.T, resp *http.Response, data []byte, what string) string {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %s", what, resp.StatusCode, data)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("%s: Content-Type %q", what, got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "default-src 'none'; style-src 'unsafe-inline'; img-src data:" {
		t.Errorf("%s: Content-Security-Policy %q", what, got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options %q", what, got)
	}
	return string(data)
}

// drawnAt reads a drawing at a path, failing unless it is one.
func drawnAt(t *testing.T, c *client, path string) (string, *http.Response) {
	t.Helper()
	resp, data := c.download(t, path)
	return drawing(t, resp, data, path), resp
}

func diagramIn(language, source string) map[string]any {
	return map[string]any{"type": "diagram", "attrs": map[string]any{"language": language, "source": source}}
}

// Kroki's diagrams (#546): an author's preview is drawn by its text and a
// reader's by the page that holds it, through a stub Kroki, kept in Valkey,
// and every refusal and failure is a sentence.
func TestDiagramsAreDrawnThroughKrokiAndKept(t *testing.T) {
	h := newHarness(t)
	kroki := newStubKroki(t)
	cache := h.valkey(t)
	api := newAPIServer(t, h, func(s *httpapi.Server) { s.Diagrams = diagram.New(kroki.srv.URL, cache, discard()) })
	off := newAPIServer(t, h)
	home := h.makeMember(t, "kroki")
	owner := api.as(t, home.user)
	// Every run draws texts of its own, so nothing kept by an earlier one answers.
	run := uuid.NewString()[:8]
	text := func(what string) string { return "@startuml\nAlice -> Bob: " + what + " " + run + "\n@enduml" }

	preview := func(c *client, language, source string) (*http.Response, []byte) {
		t.Helper()
		return c.postRaw(t, "/api/v1/diagrams", map[string]any{"language": language, "source": source})
	}
	drawn := func(c *client, language, source string) string {
		t.Helper()
		resp, data := preview(c, language, source)
		return drawing(t, resp, data, "a preview in "+language)
	}

	t.Run("an author's preview is drawn by Kroki and served as an image", func(t *testing.T) {
		source := text("hello")
		resp, data := preview(owner, document.DiagramPlantUML, source)
		if svg := drawing(t, resp, data, "the preview"); !strings.Contains(svg, "hello "+run) {
			t.Errorf("the preview reads %s", svg)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("a preview is kept as %q", got)
		}
		if n := kroki.times(document.DiagramPlantUML, source); n != 1 {
			t.Errorf("Kroki was asked %d times", n)
		}
	})

	t.Run("a drawing is kept thirty days, and asked of Kroki once", func(t *testing.T) {
		source := text("kept")
		for range 3 {
			drawn(owner, document.DiagramGraphviz, source)
		}
		// Another api process reads what the first kept.
		other := newAPIServer(t, h, func(s *httpapi.Server) { s.Diagrams = diagram.New(kroki.srv.URL, cache, discard()) })
		drawn(other.as(t, home.user), document.DiagramGraphviz, source)
		if n := kroki.times(document.DiagramGraphviz, source); n != 1 {
			t.Errorf("Kroki was asked %d times for one text", n)
		}
		ttl := cache.TTL(context.Background(), diagram.CachePrefix+diagram.Hash(document.DiagramGraphviz, source)).Val()
		if ttl < diagram.CacheTTL-time.Hour || ttl > diagram.CacheTTL {
			t.Errorf("the drawing is kept %s", ttl)
		}
		if n := kroki.times(document.DiagramD2, source); n != 0 {
			t.Errorf("the same text in another language was kept as one: %d", n)
		}
	})

	t.Run("a Valkey that fails is a miss, and the diagram is drawn afresh", func(t *testing.T) {
		broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
		t.Cleanup(func() { _ = broken.Close() })
		lost := newAPIServer(t, h, func(s *httpapi.Server) { s.Diagrams = diagram.New(kroki.srv.URL, broken, discard()) })
		c := lost.as(t, home.user)
		source := text("no valkey")
		for range 2 {
			drawn(c, document.DiagramPlantUML, source)
		}
		if n := kroki.times(document.DiagramPlantUML, source); n != 2 {
			t.Errorf("Kroki was asked %d times without a Valkey", n)
		}
	})

	t.Run("a diagram Kroki cannot read is refused with its reason, kept a minute", func(t *testing.T) {
		source := text("unreadable")
		for range 2 {
			got := asResponse(preview(owner, document.DiagramPlantUML, source))
			want(t, got, http.StatusUnprocessableEntity, "an unreadable diagram")
			if code := errorCode(t, got); code != "diagram_unreadable" {
				t.Errorf("code %q", code)
			}
			if msg := got.Body["error"].(map[string]any)["message"]; msg != "Syntax Error? (Assumed diagram type: sequence) (line: 2)." {
				t.Errorf("the reason reads %q", msg)
			}
		}
		if n := kroki.times(document.DiagramPlantUML, source); n != 1 {
			t.Errorf("Kroki was asked %d times for a failure it gave", n)
		}
		ttl := cache.TTL(context.Background(), diagram.CachePrefix+diagram.Hash(document.DiagramPlantUML, source)).Val()
		if ttl <= 0 || ttl > diagram.FailureTTL {
			t.Errorf("the failure is kept %s", ttl)
		}
	})

	t.Run("a drawing Kroki cut off took too long to draw", func(t *testing.T) {
		got := asResponse(preview(owner, document.DiagramStructurizr, text("endless")))
		want(t, got, http.StatusUnprocessableEntity, "a drawing cut off")
		if code := errorCode(t, got); code != "diagram_timeout" || !strings.Contains(string(got.Raw), "took too long to draw") {
			t.Errorf("%s", got.Raw)
		}
	})

	t.Run("a Kroki that is down cannot draw right now", func(t *testing.T) {
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()
		down := newAPIServer(t, h, func(s *httpapi.Server) { s.Diagrams = diagram.New(gone.URL, cache, discard()) })
		got := asResponse(preview(down.as(t, home.user), document.DiagramD2, text("down")))
		want(t, got, http.StatusServiceUnavailable, "Kroki down")
		if code := errorCode(t, got); code != "diagram_unavailable" || !strings.Contains(string(got.Raw), "cannot be drawn right now") {
			t.Errorf("%s", got.Raw)
		}
	})

	t.Run("a text too long, blank or in another language is refused before Kroki sees it", func(t *testing.T) {
		before := kroki.total()
		for what, body := range map[string]map[string]any{
			"too long": {"language": document.DiagramPlantUML, "source": strings.Repeat("ä", document.MaxDiagramLength+1)},
			"blank":    {"language": document.DiagramPlantUML, "source": " \n "},
			"mermaid":  {"language": document.DiagramMermaid, "source": "graph TD"},
			"bpmn":     {"language": "bpmn", "source": "<definitions/>"},
		} {
			got := owner.post(t, "/api/v1/diagrams", body)
			if got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		got := owner.post(t, "/api/v1/diagrams", map[string]any{"language": document.DiagramPlantUML, "source": strings.Repeat("ä", document.MaxDiagramLength+1)})
		if fields := got.Body["error"].(map[string]any)["fields"].(map[string]any); !strings.Contains(fields["source"].(string), "20000") {
			t.Errorf("too long reads %s", got.Raw)
		}
		if n := kroki.total() - before; n != 0 {
			t.Errorf("Kroki was asked %d times for texts it never sees", n)
		}
	})

	t.Run("a server without Kroki draws Mermaid alone, and says so", func(t *testing.T) {
		got := off.as(t, home.user).post(t, "/api/v1/diagrams", map[string]any{"language": document.DiagramPlantUML, "source": text("off")})
		want(t, got, http.StatusUnprocessableEntity, "without Kroki")
		if code := errorCode(t, got); code != "diagram_language_off" {
			t.Errorf("code %q", code)
		}
		langs := want(t, off.anonymous().get(t, "/api/v1/meta"), http.StatusOK, "meta").Body["diagramLanguages"]
		if fmt.Sprint(langs) != "[mermaid]" {
			t.Errorf("meta without Kroki names %v", langs)
		}
		langs = want(t, api.anonymous().get(t, "/api/v1/meta"), http.StatusOK, "meta").Body["diagramLanguages"]
		if fmt.Sprint(langs) != "[mermaid plantuml structurizr graphviz d2]" {
			t.Errorf("meta with Kroki names %v", langs)
		}
	})

	t.Run("only people who may edit somewhere preview, and nobody signed out", func(t *testing.T) {
		newTree(t, owner, "LOOK", "Look only")
		invite := func(role string) *client {
			email := fmt.Sprintf("%s-%s@example.test", role, uuid.NewString()[:8])
			t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
			made := obj(t, want(t, owner.post(t, "/api/v1/spaces/LOOK/guests", map[string]any{"email": email, "role": role}), http.StatusCreated, "invite a "+role), "guest")
			return api.as(t, uuid.MustParse(made["userId"].(string)))
		}
		got := asResponse(preview(invite("viewer"), document.DiagramPlantUML, text("viewer")))
		want(t, got, http.StatusForbidden, "a guest who only reads")
		if code := errorCode(t, got); code != "forbidden" {
			t.Errorf("code %q", code)
		}
		drawn(invite("editor"), document.DiagramPlantUML, text("editor"))
		// A member edits in a space of their own, whatever they hold elsewhere.
		drawn(api.as(t, h.addPerson(t, "member")), document.DiagramPlantUML, text("member"))
		if got := asResponse(preview(api.anonymous(), document.DiagramPlantUML, text("anybody"))); got.Status != http.StatusUnauthorized {
			t.Errorf("signed out: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("one person's previews are braked", func(t *testing.T) {
		brisk := api.as(t, h.addPerson(t, "member"))
		source := text("brisk")
		for i := range httpapi.DiagramPreviewsPerMinute {
			if resp, data := preview(brisk, document.DiagramPlantUML, source); resp.StatusCode != http.StatusOK {
				t.Fatalf("preview %d: %d %s", i+1, resp.StatusCode, data)
			}
		}
		resp, data := preview(brisk, document.DiagramPlantUML, source)
		got := asResponse(resp, data)
		want(t, got, http.StatusTooManyRequests, "one preview too many")
		if code := errorCode(t, got); code != "too_many_requests" || resp.Header.Get("Retry-After") == "" {
			t.Errorf("code %q, Retry-After %q", code, resp.Header.Get("Retry-After"))
		}
		drawn(owner, document.DiagramPlantUML, source)
	})

	docs := newTree(t, owner, "DRAW", "Drawn")
	plant := text("on the page")
	mermaid := "graph TD\n  a --> b " + run
	page := docs.add(docs.homeID, "Drawn page", map[string]any{"body": docOf(
		diagramNode(mermaid),
		map[string]any{"type": "expand", "attrs": map[string]any{"title": "More"}, "content": []any{diagramIn(document.DiagramPlantUML, plant)}},
		diagramIn(document.DiagramD2, text("unreadable on the page")),
	)})
	hash := diagram.Hash(document.DiagramPlantUML, plant)

	t.Run("a reader's diagram is drawn by the page and its hash", func(t *testing.T) {
		svg, resp := drawnAt(t, owner, pagePath(page, "/diagrams/"+hash))
		if !strings.Contains(svg, "on the page "+run) {
			t.Errorf("the drawing reads %s", svg)
		}
		if got := resp.Header.Get("Cache-Control"); !strings.HasPrefix(got, "private") {
			t.Errorf("a reader's drawing is kept as %q", got)
		}
		got := asResponse(owner.download(t, pagePath(page, "/diagrams/"+diagram.Hash(document.DiagramD2, text("unreadable on the page")))))
		want(t, got, http.StatusUnprocessableEntity, "the page's unreadable diagram")
		if code := errorCode(t, got); code != "diagram_unreadable" {
			t.Errorf("code %q", code)
		}
	})

	t.Run("a hash the page does not hold, or a Mermaid one, is not found, and asks Kroki nothing", func(t *testing.T) {
		before := kroki.total()
		for what, other := range map[string]string{
			"another text":     diagram.Hash(document.DiagramPlantUML, text("not on the page")),
			"another language": diagram.Hash(document.DiagramGraphviz, plant),
			"a Mermaid one":    diagram.Hash(document.DiagramMermaid, mermaid),
			"no hash":          "nothing",
		} {
			got := asResponse(owner.download(t, pagePath(page, "/diagrams/"+other)))
			want(t, got, http.StatusNotFound, what)
			if code := errorCode(t, got); code != "not_found" {
				t.Errorf("%s: code %q", what, code)
			}
		}
		if n := kroki.total() - before; n != 0 {
			t.Errorf("Kroki was asked %d times", n)
		}
	})

	t.Run("somebody who may not read the page is not told of its diagram", func(t *testing.T) {
		want(t, restrict(t, owner, page, []any{user(home.user)}, nil), http.StatusOK, "keep the page to its owner")
		t.Cleanup(func() { want(t, restrict(t, owner, page, nil, nil), http.StatusOK, "open the page again") })
		got := asResponse(api.as(t, h.addPerson(t, "member")).download(t, pagePath(page, "/diagrams/"+hash)))
		want(t, got, http.StatusNotFound, "an outsider")
		if got := asResponse(api.anonymous().download(t, pagePath(page, "/diagrams/"+hash))); got.Status != http.StatusUnauthorized {
			t.Errorf("signed out: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("anybody reads the diagrams of a public page", func(t *testing.T) {
		anon := api.anonymous()
		path := publicPath("/pages/", page, "/diagrams/", hash)
		if got := asResponse(anon.download(t, path)); got.Status != http.StatusNotFound || errorCode(t, got) != "not_public" {
			t.Fatalf("before the space is open: %d %s", got.Status, got.Raw)
		}
		want(t, owner.put(t, "/api/v1/site/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
		want(t, owner.put(t, "/api/v1/spaces/DRAW/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open DRAW")
		if svg, _ := drawnAt(t, anon, path); !strings.Contains(svg, "on the page "+run) {
			t.Errorf("the public drawing reads %s", svg)
		}
		got := asResponse(anon.download(t, publicPath("/pages/", page, "/diagrams/", diagram.Hash(document.DiagramPlantUML, text("elsewhere")))))
		want(t, got, http.StatusNotFound, "a hash the public page does not hold")
	})

	t.Run("whoever holds a link reads its page's diagrams, and nobody once it ends", func(t *testing.T) {
		made := want(t, owner.post(t, pagePath(page, "/public-links"), map[string]any{"label": "Diagrams"}), http.StatusCreated, "make a link")
		token := made.Body["token"].(string)
		_, resp := drawnAt(t, api.anonymous(), linkPath(token, "/diagrams/"+hash))
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("a linked drawing is kept as %q", got)
		}
		want(t, owner.delete(t, pagePath(page, "/public-links/"+obj(t, made, "link")["id"].(string))), http.StatusNoContent, "revoke the link")
		got := asResponse(api.anonymous().download(t, linkPath(token, "/diagrams/"+hash)))
		want(t, got, http.StatusNotFound, "a revoked link")
		if code := errorCode(t, got); code != "link_gone" {
			t.Errorf("code %q", code)
		}
	})
}

// asResponse reads a raw answer as the client's other calls answer.
func asResponse(resp *http.Response, data []byte) response {
	out := response{Status: resp.StatusCode, Raw: data}
	if len(data) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(data, &out.Body)
	}
	return out
}

// postRaw posts JSON and keeps the headers, for an answer that is a file.
func (c *client) postRaw(t *testing.T, path string, body any) (*http.Response, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	c.api.handOver(t, c, req.Method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}
