//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/render"
)

// printTap is the stack's render service with a look at each print as it is
// asked for: what was sent, and whatever the test wants to see meanwhile.
type printTap struct {
	inner  render.Renderer
	mu     sync.Mutex
	asked  []render.Request
	during func(render.Request)
}

func (p *printTap) PDF(ctx context.Context, req render.Request) ([]byte, error) {
	p.mu.Lock()
	p.asked = append(p.asked, req)
	during := p.during
	p.mu.Unlock()
	if during != nil {
		during(req)
	}
	return p.inner.PDF(ctx, req)
}

func (p *printTap) last(t *testing.T) render.Request {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.asked) == 0 {
		t.Fatal("nothing was printed")
	}
	return p.asked[len(p.asked)-1]
}

// printer is the stack's render service, tapped.
func (h *harness) printer(t *testing.T) *printTap {
	t.Helper()
	if h.cfg.Render.URL == "" {
		t.Fatal("STATOR_RENDER_URL is not set; run the suite with make test-integration against the running stack")
	}
	return &printTap{inner: render.New(h.cfg.Render.URL, render.Options{})}
}

var pdfTitle = regexp.MustCompile(`/Title\s*(\((?:\\.|[^\\)])*\)|<[0-9A-Fa-f\s]*>)`)

// titleOf reads the title a PDF's document information names, which Chromium
// takes from the printed page's document title, as a literal or a UTF-16 hex string.
func titleOf(t *testing.T, pdf []byte) string {
	t.Helper()
	m := pdfTitle.FindSubmatch(pdf)
	if m == nil {
		t.Fatalf("the PDF names no title: %q", pdf[:min(len(pdf), 200)])
	}
	raw := string(m[1])
	if strings.HasPrefix(raw, "(") {
		return strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`).Replace(raw[1 : len(raw)-1])
	}
	data, err := hex.DecodeString(strings.Join(strings.Fields(raw[1:len(raw)-1]), ""))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		units := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		}
		return string(utf16.Decode(units))
	}
	return string(data)
}

// printed fails unless the answer is a PDF titled title, and returns it.
func printed(t *testing.T, resp *http.Response, data []byte, title, what string) []byte {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %s", what, resp.StatusCode, data)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("%s: Content-Type %q", what, got)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("%s: the body starts %q", what, data[:min(len(data), 16)])
	}
	if got := titleOf(t, data); got != title {
		t.Errorf("%s: the PDF is titled %q, want %q", what, got, title)
	}
	return data
}

// renderTokens counts the print tokens of a person in the database.
func (h *harness) renderTokens(t *testing.T, user uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM api_token WHERE user_id = $1 AND for_render`, user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// PDF export (#86): the render service prints the page's print view in a
// browser, as the reader reads it, with a token made for that print alone;
// anybody prints what anybody may read; a page nobody may print is refused.
func TestAPageIsPrintedAsPDFAsItsReaderReadsIt(t *testing.T) {
	h := newHarness(t)
	tap := h.printer(t)
	api := newAPIServer(t, h, func(s *httpapi.Server) { s.Renderer = tap })
	home := h.makeMember(t, "pdf-export")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	inaID := h.addPerson(t, home.org, "member")
	ina := api.as(t, inaID, home.org, slug)
	ctx := context.Background()

	docs := newTree(t, owner, "PDF", "Printed handbook")
	other := newTree(t, owner, "FAR", "Far away")
	guide := docs.add(docs.homeID, "Printed guide")
	publishBody(t, owner, guide, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": 1}, "content": []any{map[string]any{"type": "text", "text": "Setting up"}}},
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Install it, then "}, mathInline(`e^{i\pi} + 1 = 0`)}},
		diagramNode("graph TD; Install --> Configure"),
		mathBlock(`\int_0^1 x^2 \, dx`),
		map[string]any{"type": "codeBlock", "attrs": map[string]any{"language": "go"}, "content": []any{map[string]any{"type": "text", "text": "fmt.Println(\"ready\")"}}},
	}})
	secret := docs.add(docs.homeID, "Printed secret")
	want(t, restrict(t, owner, secret, []any{user(home.user)}, nil), http.StatusOK, "keep the secret to the owner")
	box := folder(docs, docs.homeID, "Printed folder")
	draft := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Printed draft"}), http.StatusCreated, "a draft"), "page")["id"].(string)
	far := other.add(other.homeID, "Far page")
	// The api the render service reaches is the stack's, whose reads may go
	// to a replica that has not seen this test's writes.
	h.settle(t)

	t.Run("a member downloads the published page, printed with every block, and the export is audited", func(t *testing.T) {
		var during struct {
			scopes  []string
			expires time.Duration
			listed  int
			wrote   int
		}
		tap.during = func(req render.Request) {
			var expires, created time.Time
			if err := h.super.QueryRow(ctx, `SELECT scopes, expires_at, created_at FROM api_token WHERE user_id = $1 AND for_render`, home.user).
				Scan(&during.scopes, &expires, &created); err != nil {
				t.Errorf("no print token while printing: %v", err)
				return
			}
			during.expires = expires.Sub(created)
			during.listed = len(list(t, owner.get(t, "/api/v1/tokens"), "tokens"))
			during.wrote = api.withToken(req.Token).post(t, pagePath(guide, "/comments"), map[string]any{"body": commentDoc("Printed by a script.")}).Status
		}
		defer func() { tap.during = nil }()
		resp, data := owner.download(t, pagePath(guide, "/pdf"))
		printed(t, resp, data, "Printed guide", "the guide's PDF")
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, `PDF-printed-guide-`+time.Now().Format("2006-01-02")+`.pdf`) {
			t.Errorf("Content-Disposition = %q, want a download named after the space, the page and the day", got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		asked := tap.last(t)
		if asked.Path != "/print/p/"+guide || asked.Token == "" || strings.Contains(asked.Path, asked.Token) {
			t.Errorf("the service was asked for %q with a token of %d characters", asked.Path, len(asked.Token))
		}
		if len(during.scopes) != 1 || during.scopes[0] != auth.ScopeRead || during.expires > auth.RenderTokenTTL {
			t.Errorf("the print's token read %v for %s", during.scopes, during.expires)
		}
		if during.listed != 0 {
			t.Errorf("the tokens page listed %d tokens during the print", during.listed)
		}
		if during.wrote != http.StatusForbidden {
			t.Errorf("the print's token commented with %d, want it refused as read only", during.wrote)
		}
		if n := h.renderTokens(t, home.user); n != 0 {
			t.Errorf("%d print tokens outlive the print", n)
		}
		if data := h.recordedOnce(t, home.org, "page.exported", &home.user, guide); !strings.Contains(data, `"scope": "pdf"`) {
			t.Errorf("the export's record reads %s", data)
		}
	})

	t.Run("the print is in the reader's language", func(t *testing.T) {
		want(t, ina.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "ina reads German")
		resp, data := ina.download(t, pagePath(guide, "/pdf"))
		printed(t, resp, data, "Printed guide", "ina's PDF")
		if got := tap.last(t).Language; got != "de" {
			t.Errorf("ina's print was asked in %q", got)
		}
	})

	t.Run("a reader without access, a folder and a draft are refused before anything is printed", func(t *testing.T) {
		before := len(tap.asked)
		want(t, ina.get(t, pagePath(secret, "/pdf")), http.StatusNotFound, "ina prints the secret")
		want(t, ina.get(t, pagePath(draft, "/pdf")), http.StatusNotFound, "ina prints the owner's draft")
		if got := want(t, owner.get(t, pagePath(box, "/pdf")), http.StatusConflict, "print a folder"); errorCode(t, got) != "not_printable" {
			t.Errorf("a folder is refused with %s", got.Raw)
		}
		got := want(t, owner.get(t, pagePath(draft, "/pdf")), http.StatusConflict, "print a page never published")
		if errorCode(t, got) != "not_printable" || !strings.Contains(got.Body["error"].(map[string]any)["message"].(string), "Publish it") {
			t.Errorf("a draft is refused with %s", got.Raw)
		}
		want(t, api.anonymous().get(t, pagePath(guide, "/pdf")), http.StatusUnauthorized, "nobody prints a page")
		if len(tap.asked) != before {
			t.Errorf("%d prints were asked for refused pages", len(tap.asked)-before)
		}
		if n := h.renderTokens(t, inaID); n != 0 {
			t.Errorf("a refused print left %d tokens", n)
		}
	})

	t.Run("a script's token prints, unless it is limited to spaces, which may make no token", func(t *testing.T) {
		_, whole := makeToken(t, owner, map[string]any{"name": "printer", "scopes": []string{"read"}})
		resp, data := api.withToken(whole).download(t, pagePath(guide, "/pdf"))
		printed(t, resp, data, "Printed guide", "the script's PDF")
		before := len(tap.asked)
		_, limited := makeToken(t, owner, map[string]any{"name": "limited printer", "spaces": []string{"PDF"}})
		want(t, api.withToken(limited).get(t, pagePath(guide, "/pdf")), http.StatusForbidden, "a limited token prints")
		if len(tap.asked) != before {
			t.Error("a limited token's print was asked for")
		}
	})

	t.Run("the database holds every print token to read only and a few minutes", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, home.user)
		for what, sql := range map[string]string{
			"an hour":     `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, for_render) VALUES ($1, $2, 'x', $3, ARRAY['read'], now() + interval '1 hour', true)`,
			"forever":     `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, for_render) VALUES ($1, $2, 'x', $3, ARRAY['read'], NULL, true)`,
			"able to act": `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, for_render) VALUES ($1, $2, 'x', $3, '{}', now() + interval '1 minute', true)`,
		} {
			_, digest, _ := auth.GenerateAPIToken()
			refused(t, conn, "a print token "+what, sql, home.org, home.user, digest)
		}
		_, digest, _ := auth.GenerateAPIToken()
		var id uuid.UUID
		if err := conn.QueryRow(ctx, `INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, for_render) VALUES ($1, $2, 'x', $3, ARRAY['read'], now() + interval '2 minutes', true) RETURNING id`,
			home.org, home.user, digest).Scan(&id); err != nil {
			t.Fatalf("a print token as the api makes it: %v", err)
		}
		refused(t, conn, "a print token lengthened", `UPDATE api_token SET expires_at = now() + interval '1 day' WHERE id = $1`, id)
		if _, err := conn.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("anybody prints what anybody may read, and nothing else", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/PDF/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open PDF")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
		h.settle(t)
		anon := api.anonymous()
		resp, data := anon.download(t, publicPath(slug, "/pages/", guide, "/pdf"))
		printed(t, resp, data, "Printed guide", "the public PDF")
		if asked := tap.last(t); asked.Token != "" || asked.Path != "/print/public/"+slug+"/p/"+guide {
			t.Errorf("the public print was asked for %q with a token %q", asked.Path, asked.Token)
		}
		for _, page := range []string{secret, far} {
			want(t, anon.get(t, publicPath(slug, "/pages/", page, "/pdf")), http.StatusNotFound, "print a page nobody may read")
		}
	})

	t.Run("a public link prints its page for whoever holds it, until it is revoked", func(t *testing.T) {
		made := want(t, owner.post(t, pagePath(guide, "/public-links"), map[string]any{"label": "Printed"}), http.StatusCreated, "make a link")
		token := made.Body["token"].(string)
		linkID := obj(t, made, "link")["id"].(string)
		h.settle(t)
		anon := api.anonymous()
		resp, data := anon.download(t, linkPath(slug, token, "/pdf"))
		printed(t, resp, data, "Printed guide", "the link's PDF")
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, `filename=printed-guide-`) {
			t.Errorf("the link's PDF is named %q, which should say nothing of its space", got)
		}
		want(t, owner.delete(t, pagePath(guide, "/public-links/", linkID)), http.StatusNoContent, "revoke the link")
		h.settle(t)
		if got := anon.get(t, linkPath(slug, token, "/pdf")); got.Status != http.StatusNotFound || errorCode(t, got) != "link_gone" {
			t.Errorf("the revoked link printed %d %s", got.Status, got.Raw)
		}
	})

	t.Run("without a render service the export says so", func(t *testing.T) {
		off := newAPIServer(t, h)
		got := want(t, off.as(t, home.user, home.org, slug).get(t, pagePath(guide, "/pdf")), http.StatusServiceUnavailable, "print with no service")
		if errorCode(t, got) != "render_unavailable" || !strings.Contains(got.Body["error"].(map[string]any)["message"].(string), "render service") {
			t.Errorf("no service answers %s", got.Raw)
		}
		if n := h.renderTokens(t, home.user); n != 0 {
			t.Errorf("a print that could not start left %d tokens", n)
		}
	})
}
