// Package diagram draws diagrams written as text through the installation's
// Kroki and keeps each drawing in Valkey (#466). Mermaid is not drawn here:
// readers' browsers draw it.
package diagram

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/document"
)

const (
	// CallTimeout is what one drawing may take, waiting for a free slot
	// included. Kroki cuts a drawing off after about nine seconds itself.
	CallTimeout = 10 * time.Second
	// Concurrency is how many drawings one api process asks Kroki for at once.
	Concurrency = 4
	// MaxSVGBytes is the largest drawing taken from Kroki.
	MaxSVGBytes = 2 << 20
	// CacheTTL is how long a drawing is kept; FailureTTL how long a diagram
	// that could not be drawn is left alone before it is tried again.
	CacheTTL   = 30 * 24 * time.Hour
	FailureTTL = time.Minute
	// CachePrefix keeps the drawings apart from anything else in Valkey.
	CachePrefix = "stator:diagram:"
	// MaxReasonLength cuts Kroki's reason for a refusal to what a block shows.
	MaxReasonLength = 300
)

// refusalBodyLimit is how much of Kroki's refusal is read for its first line.
const refusalBodyLimit = 4 << 10

var (
	// ErrLanguage is a language that Kroki is not asked to draw.
	ErrLanguage = errors.New("diagram language not drawn by Kroki")
	// ErrOff is a server without Kroki, which draws Mermaid alone.
	ErrOff = errors.New("this server draws Mermaid diagrams alone")
	// ErrEmpty and ErrTooLong refuse a source before Kroki sees it.
	ErrEmpty   = errors.New("the diagram has no text")
	ErrTooLong = errors.New("the diagram's text is too long")
	// ErrBusy is every slot of this process taken until the deadline.
	ErrBusy = errors.New("too many diagrams are being drawn right now")
	// ErrTimeout is a drawing that ran out of time, at Kroki or here.
	ErrTimeout = errors.New("the diagram took too long to draw")
	// ErrUnavailable is a Kroki that could not be reached or failed.
	ErrUnavailable = errors.New("the diagram cannot be drawn right now")
	// ErrTooLarge is a drawing larger than MaxSVGBytes.
	ErrTooLarge = errors.New("the drawing is larger than this server takes")
)

// UnreadableError is a diagram Kroki could not read, with the first line of
// its reason.
type UnreadableError struct {
	Reason string
}

func (e *UnreadableError) Error() string { return "the diagram cannot be read: " + e.Reason }

// Hash names a diagram by its language and its source, as the cache and the
// readers' endpoints do: the hex SHA-256 of the language, a line break and
// the source.
func Hash(language, source string) string {
	sum := sha256.Sum256([]byte(language + "\n" + source))
	return hex.EncodeToString(sum[:])
}

// Drawn reports whether Kroki is asked to draw a language at all.
func Drawn(language string) bool {
	return language != document.DiagramMermaid && slices.Contains(document.DiagramLanguages, language)
}

// Service draws through the Kroki at an address. Its address is the
// operator's, like the render service's, so it is not held to the outbound
// guard; what keeps page content in is that Kroki has no way out.
type Service struct {
	base    string
	http    *http.Client
	timeout time.Duration
	cache   redis.UniversalClient
	slots   chan struct{}
	log     *slog.Logger
}

// New makes a service drawing through the Kroki at baseURL; a blank one
// draws nothing, and a nil cache asks Kroki every time.
func New(baseURL string, cache redis.UniversalClient, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		base: strings.TrimRight(baseURL, "/"), http: &http.Client{}, timeout: CallTimeout, cache: cache,
		slots: make(chan struct{}, Concurrency), log: log,
	}
}

// Languages are the diagram languages this server draws: Mermaid always, in
// the browser, and Kroki's when there is one.
func (s *Service) Languages() []string {
	if !s.On() {
		return []string{document.DiagramMermaid}
	}
	return slices.Clone(document.DiagramLanguages)
}

// On reports whether there is a Kroki to draw through.
func (s *Service) On() bool { return s != nil && s.base != "" }

// Check refuses what Kroki is never sent: a language it does not draw here,
// and a source that is blank or too long.
func (s *Service) Check(language, source string) error {
	switch {
	case !Drawn(language):
		return ErrLanguage
	case !s.On():
		return ErrOff
	case strings.TrimSpace(source) == "":
		return ErrEmpty
	case utf8.RuneCountInString(source) > document.MaxDiagramLength:
		return ErrTooLong
	}
	return nil
}

// kept is a drawing, or why there is none, as Valkey keeps it.
type kept struct {
	SVG    string `json:"svg,omitempty"`
	Fail   string `json:"fail,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// The failures a kept entry names.
const (
	failUnreadable  = "unreadable"
	failTimeout     = "timeout"
	failUnavailable = "unavailable"
	failTooLarge    = "tooLarge"
)

func (k kept) result() ([]byte, error) {
	switch k.Fail {
	case "":
		return []byte(k.SVG), nil
	case failUnreadable:
		return nil, &UnreadableError{Reason: k.Reason}
	case failTimeout:
		return nil, ErrTimeout
	case failTooLarge:
		return nil, ErrTooLarge
	}
	return nil, ErrUnavailable
}

func keptFailure(err error) (kept, bool) {
	var unreadable *UnreadableError
	switch {
	case errors.As(err, &unreadable):
		return kept{Fail: failUnreadable, Reason: unreadable.Reason}, true
	case errors.Is(err, ErrTimeout):
		return kept{Fail: failTimeout}, true
	case errors.Is(err, ErrTooLarge):
		return kept{Fail: failTooLarge}, true
	case errors.Is(err, ErrUnavailable):
		return kept{Fail: failUnavailable}, true
	}
	return kept{}, false
}

// Draw answers a diagram's SVG, from Valkey when it is kept there, else from
// Kroki, keeping what Kroki said for the next reader.
func (s *Service) Draw(ctx context.Context, language, source string) ([]byte, error) {
	if err := s.Check(language, source); err != nil {
		return nil, err
	}
	key := CachePrefix + Hash(language, source)
	if k, ok := s.cached(ctx, key); ok {
		return k.result()
	}
	svg, err := s.ask(ctx, language, source)
	switch {
	case err == nil:
		s.store(ctx, key, kept{SVG: string(svg)}, CacheTTL)
		return svg, nil
	case ctx.Err() != nil:
		// The reader went away; what Kroki would have said is not known.
		return nil, err
	}
	if k, ok := keptFailure(err); ok {
		s.store(ctx, key, k, FailureTTL)
	}
	return nil, err
}

// ask waits for a free slot and asks Kroki, both within CallTimeout.
func (s *Service) ask(parent context.Context, language, source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, ErrBusy
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/"+language+"/svg", strings.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Accept", "image/svg+xml")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, s.failed(parent, ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, refusalBodyLimit))
		return nil, refusal(resp.StatusCode, said)
	}
	svg, err := io.ReadAll(io.LimitReader(resp.Body, MaxSVGBytes+1))
	if err != nil {
		return nil, s.failed(parent, ctx, err)
	}
	if len(svg) > MaxSVGBytes {
		return nil, ErrTooLarge
	}
	if !bytes.Contains(svg, []byte("<svg")) {
		return nil, fmt.Errorf("%w: Kroki answered no SVG", ErrUnavailable)
	}
	return svg, nil
}

// failed says what a call that broke off means: the reader leaving, the
// drawing running out of time, or Kroki out of reach.
func (s *Service) failed(parent, ctx context.Context, err error) error {
	switch {
	case parent.Err() != nil:
		return parent.Err()
	case ctx.Err() != nil:
		return ErrTimeout
	}
	s.log.Warn("reach Kroki", "err", err)
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// refusal reads a refusal of Kroki's. A 4xx is the diagram at fault, said in
// its first line; a 500 with no reason is a drawing Kroki cut off.
func refusal(status int, said []byte) error {
	reason := firstLine(said)
	switch {
	case status >= 400 && status < 500 && reason != "":
		return &UnreadableError{Reason: reason}
	case status == http.StatusInternalServerError && reason == "":
		return ErrTimeout
	case status == http.StatusInternalServerError:
		return &UnreadableError{Reason: reason}
	case status == http.StatusGatewayTimeout:
		return ErrTimeout
	}
	return fmt.Errorf("%w: Kroki answered %d", ErrUnavailable, status)
}

// firstLine is the first line of a refusal that says something, without the
// status Kroki puts before it, cut to MaxReasonLength.
func firstLine(said []byte) string {
	lines := bufio.NewScanner(bytes.NewReader(said))
	for lines.Scan() {
		line := strings.TrimSpace(strings.ToValidUTF8(lines.Text(), ""))
		if head, rest, ok := strings.Cut(line, ":"); ok && strings.HasPrefix(head, "Error ") {
			line = strings.TrimSpace(rest)
		}
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > MaxReasonLength {
			line = string([]rune(line)[:MaxReasonLength-3]) + "..."
		}
		return line
	}
	return ""
}

// A Valkey that fails is a miss, never a refusal: the diagram is drawn afresh.
func (s *Service) cached(ctx context.Context, key string) (kept, bool) {
	if s.cache == nil {
		return kept{}, false
	}
	raw, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			s.log.Warn("read a diagram from Valkey", "err", err)
		}
		return kept{}, false
	}
	var k kept
	if json.Unmarshal(raw, &k) != nil {
		return kept{}, false
	}
	return k, true
}

func (s *Service) store(ctx context.Context, key string, k kept, ttl time.Duration) {
	if s.cache == nil {
		return
	}
	raw, _ := json.Marshal(k)
	if err := s.cache.Set(context.WithoutCancel(ctx), key, raw, ttl).Err(); err != nil {
		s.log.Warn("keep a diagram in Valkey", "err", err)
	}
}
