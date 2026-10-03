//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/netguard"
)

// Link previews (#47): a page holds a link card's address and view only;
// what the linked page says is read through the outbound guard and kept in
// Valkey, and an allowlisted site's address names its player.

func TestLinkPreviewsAreReadThroughTheGuardAndKept(t *testing.T) {
	h := newHarness(t)
	var title atomic.Value
	title.Store("The first title")
	var fetches atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>%s</title><meta property="og:description" content="What it is about"><meta property="og:site_name" content="Numbat news"></head><body>x</body></html>`, title.Load())
	}))
	t.Cleanup(site.Close)

	// The test's own site is on loopback, which only this api's guard lets through.
	open := newAPIServer(t, h, func(s *httpapi.Server) { s.Unfurl = h.unfurl(t, netguard.ParseAllow("127.0.0.0/8")) })
	guarded := newAPIServer(t, h)
	home := h.makeMember(t, "preview")
	slug := h.slugOf(t, home.org)
	reader := open.as(t, home.user, home.org, slug)
	outsider := guarded.as(t, home.user, home.org, slug)

	preview := func(c *client, addr string) map[string]any {
		t.Helper()
		return obj(t, want(t, c.get(t, "/api/v1/link-preview?url="+url.QueryEscape(addr)), http.StatusOK, "preview "+addr), "preview")
	}
	page := site.URL + "/story?id=" + strings.ReplaceAll(t.Name(), "/", "-")

	t.Run("a page's title, summary and site are read and kept", func(t *testing.T) {
		got := preview(reader, page)
		if got["title"] != "The first title" || got["description"] != "What it is about" || got["siteName"] != "Numbat news" || got["fetched"] != true || got["embed"] != nil {
			t.Fatalf("the preview reads %v", got)
		}
		title.Store("A later title")
		if again := preview(reader, page); again["title"] != "The first title" || fetches.Load() != 1 {
			t.Errorf("a second preview fetched again (%d fetches) and reads %v", fetches.Load(), again)
		}
	})

	t.Run("an address inside the server's network is not read", func(t *testing.T) {
		before := fetches.Load()
		got := preview(outsider, site.URL+"/inside")
		if got["fetched"] != false || got["title"] != "" || got["siteName"] != "127.0.0.1" || fetches.Load() != before {
			t.Errorf("an address inside the network reads %v after %d fetches", got, fetches.Load()-before)
		}
		for _, internal := range []string{"http://valkey:6379/", "http://postgres-primary:5432/", "http://169.254.169.254/latest/meta-data/"} {
			if got := preview(outsider, internal); got["fetched"] != false {
				t.Errorf("%s was read: %v", internal, got)
			}
		}
	})

	t.Run("an allowlisted site's address names its player without asking the site", func(t *testing.T) {
		got := preview(outsider, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		embed, _ := got["embed"].(map[string]any)
		if embed["src"] != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" || embed["kind"] != "video" || embed["provider"] != "YouTube" {
			t.Errorf("the embed is %v", got)
		}
	})

	t.Run("what is not a web page's address is refused, and nobody signed out asks", func(t *testing.T) {
		for _, bad := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "/spaces/x", "ftp://example.test/a"} {
			if got := reader.get(t, "/api/v1/link-preview?url="+url.QueryEscape(bad)); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%q: %d %s", bad, got.Status, got.Raw)
			}
		}
		resp, err := http.Get(open.srv.URL + "/api/v1/link-preview?url=" + url.QueryEscape(page))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("signed out: %d", resp.StatusCode)
		}
	})

	t.Run("a page keeps a card's address and view, and nothing the site said", func(t *testing.T) {
		docs := newTree(t, reader, "LINKS", "Links")
		id := docs.add(docs.homeID, "Reading", map[string]any{"body": docOf(
			map[string]any{"type": "linkCard", "attrs": map[string]any{"url": page, "view": "card"}},
			map[string]any{"type": "linkCard", "attrs": map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ", "view": "embed"}},
		)})
		body := fmt.Sprint(obj(t, reader.get(t, pagePath(id)), "page")["body"])
		if !strings.Contains(body, "youtu.be") || strings.Contains(body, "first title") {
			t.Errorf("the body: %s", body)
		}
		for what, bad := range map[string]map[string]any{
			"a script":  {"url": "javascript:alert(1)", "view": "card"},
			"a frame":   {"url": page, "view": "frame"},
			"a title":   {"url": page, "view": "card", "title": "Stored"},
			"no scheme": {"url": "example.test", "view": "card"},
		} {
			got := reader.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Odd " + what, "body": docOf(map[string]any{"type": "linkCard", "attrs": bad})})
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("a card with %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}
