// Package objectstore keeps the bytes of uploaded files in any bucket that
// speaks the S3 protocol. Adapted from Armature's attachment store.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"sync"
	"unicode"
)

// Store is where the bytes go. A domain knows nothing about S3 beyond this, so
// a test can hold the bytes in memory and a deployment can point at any bucket.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// ErrUnavailable is returned when no object store is configured. It is a
// deployment problem, not a user's, and the message says which setting fixes it.
var ErrUnavailable = errors.New("file storage is not configured: set STATOR_S3_ENDPOINT to an S3 compatible store")

// ErrNoObject is returned when a row names an object whose bytes are gone.
var ErrNoObject = errors.New("the file is missing from storage")

// Config is what it takes to reach a bucket.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Configured reports whether a store was asked for at all.
func (c Config) Configured() bool { return c.Endpoint != "" }

// Open returns the bucket a configuration names, or a store that refuses with
// the setting to fix. It does not pretend.
func Open(cfg Config) (Store, error) {
	if !cfg.Configured() {
		return Unavailable{}, nil
	}
	return NewS3(cfg)
}

// IsUnavailable reports whether a store is the one that refuses everything.
func IsUnavailable(s Store) bool {
	if s == nil {
		return true
	}
	_, off := s.(Unavailable)
	return off
}

// Memory holds objects in a map. It is for tests, and for nothing else.
type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemory() *Memory { return &Memory{objects: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNoObject
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// Len is how many objects are held, for a test to count.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

// Unavailable is the store a deployment gets when none was configured. Every
// call fails the same way, so an upload is refused with the setting to fix.
type Unavailable struct{}

func (Unavailable) Put(context.Context, string, io.Reader, int64, string) error {
	return ErrUnavailable
}
func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }
func (Unavailable) Delete(context.Context, string) error               { return ErrUnavailable }

// MaxNameLength keeps a file name short enough for a header and a listing.
const MaxNameLength = 200

// CleanName reduces a file name to something safe to store and to send back
// in a header: no directories, no control characters, never empty.
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if runes := []rune(name); len(runes) > MaxNameLength {
		name = string(runes[:MaxNameLength])
	}
	return name
}
