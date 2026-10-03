// Package unfurl reads what a web page says about itself, its title, its
// summary and its site, so a link can be shown as a card; and names the
// player a page of an allowlisted video or design site is embedded with.
package unfurl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/praetorianer777/stator/backend/internal/document"
)

const (
	// FetchTimeout bounds one page's fetch, redirects included, so a slow
	// site holds a reader's card for seconds, not minutes.
	FetchTimeout = 5 * time.Second
	// MaxHeadBytes is as much of a page as is read looking for its head;
	// what a page says about itself comes before its body.
	MaxHeadBytes = 512 << 10
	// MaxTitleLength and MaxDescriptionLength cut what a page says to what
	// a card shows.
	MaxTitleLength       = 300
	MaxDescriptionLength = 500
	// CacheTTL is how long a page's card is kept; FailureTTL how long a page
	// that could not be read is left alone before it is tried again.
	CacheTTL   = time.Hour
	FailureTTL = 5 * time.Minute
	// CachePrefix keeps the cards apart from anything else in Valkey.
	CachePrefix = "stator:unfurl:"
)

// ErrBadURL refuses an address that is not a web page's.
var ErrBadURL = errors.New("give the full address of a web page, starting with https:// or http://")

// LinkPreview is what a page says about itself. Fetched is false when the page
// could not be read: it is unreachable, inside the server's network, or not
// a web page; the card then names its site by host.
type LinkPreview struct {
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	SiteName    string     `json:"siteName"`
	Fetched     bool       `json:"fetched"`
	Embed       *LinkEmbed `json:"embed"`
}

// Service unfurls links through a client that refuses the server's own
// network, and keeps what it read in Valkey when there is one.
type Service struct {
	client *http.Client
	cache  redis.UniversalClient
	log    *slog.Logger
}

// NewService takes the guarded client to fetch with; a nil cache reads
// every page afresh.
func NewService(client *http.Client, cache redis.UniversalClient, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{client: client, cache: cache, log: log}
}

// Parse admits an absolute http or https address that a page may link to.
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if !document.SafeHref(raw) {
		return nil, ErrBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, ErrBadURL
	}
	u.Fragment = ""
	return u, nil
}

// Preview is the card for an address, from the cache when it is there.
func (s *Service) Preview(ctx context.Context, raw string) (*LinkPreview, error) {
	u, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	addr := u.String()
	key := CachePrefix + digest(addr)
	if out, ok := s.cached(ctx, key); ok {
		return out, nil
	}
	out := &LinkPreview{URL: addr, SiteName: u.Hostname(), Embed: EmbedFor(u)}
	ttl := FailureTTL
	if head, err := s.fetch(ctx, addr); err != nil {
		s.log.Debug("unfurl", "url", addr, "err", err)
	} else {
		out.Fetched = true
		ttl = CacheTTL
		out.Title = clip(first(head["og:title"], head["twitter:title"], head["title"]), MaxTitleLength)
		out.Description = clip(first(head["og:description"], head["twitter:description"], head["description"]), MaxDescriptionLength)
		if site := clip(head["og:site_name"], MaxTitleLength); site != "" {
			out.SiteName = site
		}
	}
	s.store(ctx, key, out, ttl)
	return out, nil
}

func (s *Service) fetch(ctx context.Context, addr string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "Stator link preview")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the page answered %d", resp.StatusCode)
	}
	if media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); media != "text/html" && media != "application/xhtml+xml" {
		return nil, fmt.Errorf("the address is %q, not a web page", media)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxHeadBytes))
	if err != nil {
		return nil, err
	}
	return ReadHead(body), nil
}

// ReadHead collects a page's title and the meta tags a card reads, keyed by
// their property or name in lower case, from the head alone.
func ReadHead(page []byte) map[string]string {
	out := map[string]string{}
	z := html.NewTokenizer(bytes.NewReader(page))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.EndTagToken:
			name, _ := z.TagName()
			switch atom.Lookup(name) {
			case atom.Head:
				return out
			case atom.Title:
				inTitle = false
			}
		case html.TextToken:
			if inTitle && out["title"] == "" {
				out["title"] = string(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch atom.Lookup(name) {
			case atom.Body:
				return out
			case atom.Title:
				inTitle = true
			case atom.Meta:
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						key = strings.ToLower(string(v))
					case "content":
						content = string(v)
					}
				}
				if key != "" && out[key] == "" {
					out[key] = content
				}
			}
		}
	}
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// clip tidies what a page says into one line of valid text, cut at max.
func clip(s string, max int) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-3]) + "..."
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// A Valkey that fails is a miss, never a refusal: the card is read afresh.
func (s *Service) cached(ctx context.Context, key string) (*LinkPreview, bool) {
	if s.cache == nil {
		return nil, false
	}
	raw, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			s.log.Warn("read a link preview from Valkey", "err", err)
		}
		return nil, false
	}
	var out LinkPreview
	if json.Unmarshal(raw, &out) != nil {
		return nil, false
	}
	return &out, true
}

func (s *Service) store(ctx context.Context, key string, p *LinkPreview, ttl time.Duration) {
	if s.cache == nil {
		return
	}
	raw, _ := json.Marshal(p)
	if err := s.cache.Set(ctx, key, raw, ttl).Err(); err != nil {
		s.log.Warn("keep a link preview in Valkey", "err", err)
	}
}
