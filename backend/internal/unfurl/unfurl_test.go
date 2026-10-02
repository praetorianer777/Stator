package unfurl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/netguard"
)

func TestParseAdmitsWebPagesOnly(t *testing.T) {
	for _, ok := range []string{"https://example.test/a?b=c", "http://example.test", " HTTPS://Example.test/x#part "} {
		if _, err := Parse(ok); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "example.test", "/spaces/x", "javascript:alert(1)", "mailto:a@example.test", "ftp://example.test", "https://user:pw@example.test", "https://", "file:///etc/passwd"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q was admitted", bad)
		}
	}
	u, _ := Parse("https://example.test/a#part")
	if u.String() != "https://example.test/a" {
		t.Errorf("the fragment stays: %s", u)
	}
}

func TestReadHeadReadsTheHeadOnly(t *testing.T) {
	head := ReadHead([]byte(`<!doctype html><html><head>
		<title> The  plain
		title </title>
		<meta property="og:title" content="The card title">
		<META NAME="Description" content="Said in the head">
		<meta property="og:site_name" content="Example">
		<meta property="og:title" content="A second one is ignored">
		</head><body><meta name="description" content="In the body"><title>Not this</title></body></html>`))
	want := map[string]string{"og:title": "The card title", "description": "Said in the head", "og:site_name": "Example"}
	for k, v := range want {
		if head[k] != v {
			t.Errorf("%s = %q, want %q", k, head[k], v)
		}
	}
	if got := clip(head["title"], MaxTitleLength); got != "The plain title" {
		t.Errorf("the title reads %q", got)
	}
}

func TestClipKeepsOneLineOfValidText(t *testing.T) {
	if got := clip("a\n\tb \xff c", 100); got != "a b c" {
		t.Errorf("clip = %q", got)
	}
	if got := clip(strings.Repeat("é", 20), 10); got != strings.Repeat("é", 7)+"..." {
		t.Errorf("clip cut = %q", got)
	}
}

func TestEmbedsComeOnlyFromAllowlistedSites(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10": "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                     "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://m.youtube.com/shorts/dQw4w9WgXcQ":         "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://vimeo.com/76979871":                       "https://player.vimeo.com/video/76979871",
		"https://www.figma.com/design/AbCdEfGhIj12/Plan?node-id=1-2": "https://www.figma.com/embed?embed_host=stator&url=" +
			url.QueryEscape("https://www.figma.com/design/AbCdEfGhIj12/Plan?node-id=1-2"),
		"https://www.youtube.com/watch?v=short":                    "",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ%22onload%3D1": "",
		"https://vimeo.com/channels/staffpicks":                    "",
		"https://youtube.com.evil.test/watch?v=dQw4w9WgXcQ":        "",
		"https://www.figma.com/community/plugin/1":                 "",
		"https://example.test/embed/dQw4w9WgXcQ":                   "",
	}
	for raw, want := range cases {
		u, err := Parse(raw)
		if err != nil {
			t.Fatal(raw, err)
		}
		got := ""
		if e := EmbedFor(u); e != nil {
			got = e.Src
			var allowed bool
			for _, frame := range FrameSources() {
				allowed = allowed || strings.HasPrefix(e.Src, frame+"/")
			}
			if !allowed {
				t.Errorf("%s embeds from %s, which frame-src does not allow", raw, e.Src)
			}
		}
		if got != want {
			t.Errorf("%s embeds %q, want %q", raw, got, want)
		}
	}
}

// The browser loads an embed only from an origin the policy names, in the
// compose stack and in the chart alike.
func TestThePolicyLetsEveryEmbedLoad(t *testing.T) {
	for _, path := range []string{"../../../deploy/nginx.conf", "../../../deploy/charts/stator/templates/configmap-nginx.yaml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "frame-ancestors 'none'") {
				continue
			}
			if want := "frame-src " + strings.Join(FrameSources(), " ") + ";"; !strings.Contains(line, want) {
				t.Errorf("%s lacks %q in %s", path, want, strings.TrimSpace(line))
			}
		}
	}
}

func TestPreviewReadsAPageThroughTheGuard(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>Plain</title><meta property="og:description" content="About it"></head></html>`))
		case "/file":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer page.Close()
	ctx := context.Background()

	open := NewService(netguard.Client(FetchTimeout, netguard.ParseAllow("127.0.0.0/8")), nil, nil)
	got, err := open.Preview(ctx, page.URL+"/page")
	if err != nil || !got.Fetched || got.Title != "Plain" || got.Description != "About it" || got.SiteName != "127.0.0.1" {
		t.Fatalf("the page reads %+v, %v", got, err)
	}
	for _, path := range []string{"/file", "/missing"} {
		if got, _ := open.Preview(ctx, page.URL+path); got.Fetched || got.Title != "" {
			t.Errorf("%s reads %+v", path, got)
		}
	}

	guarded := NewService(netguard.Client(FetchTimeout, netguard.ParseAllow("")), nil, nil)
	if got, err := guarded.Preview(ctx, page.URL+"/page"); err != nil || got.Fetched || got.Title != "" {
		t.Errorf("an address inside the network reads %+v, %v", got, err)
	}
}
