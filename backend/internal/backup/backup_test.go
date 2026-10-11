package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

const dumpSQL = "CREATE TABLE page (id int);\n"

var (
	key      = bytes.Repeat([]byte{7}, 32)
	otherKey = bytes.Repeat([]byte{8}, 32)
	takenAt  = time.Date(2026, 10, 11, 19, 2, 3, 0, time.UTC)
)

func put(t *testing.T, s objectstore.Store, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, s objectstore.Store, key string) string {
	t.Helper()
	r, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func source(t *testing.T, store objectstore.Store) Source {
	return Source{
		Version:    "1.4.0",
		Migrations: []int64{1, 2, 680},
		Key:        key,
		Dump: func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, dumpSQL)
			return err
		},
		Store:   store,
		TempDir: t.TempDir(),
		Now:     func() time.Time { return takenAt },
	}
}

func accept(Manifest) error { return nil }

func TestRoundTripReplacesDatabaseAndBucket(t *testing.T) {
	ctx := context.Background()
	src := objectstore.NewMemory()
	put(t, src, "org/a/attachments/1", "first file")
	put(t, src, "org/b/theme/logo.png", "second file")

	var archive bytes.Buffer
	sum, err := Create(ctx, &archive, source(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Objects != 2 {
		t.Fatalf("backed up %d objects, want 2", sum.Objects)
	}

	dst := objectstore.NewMemory()
	put(t, dst, "org/a/attachments/1", "changed since")
	put(t, dst, "org/c/stray", "not in the backup")
	var loaded string
	m, rsum, err := Restore(ctx, &archive, Target{
		Check: accept,
		Load: func(_ context.Context, r io.Reader) error {
			b, err := io.ReadAll(r)
			loaded = string(b)
			return err
		},
		Store:   dst,
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded != dumpSQL {
		t.Errorf("loaded %q, want the dump", loaded)
	}
	if m.Version != "1.4.0" || !m.CreatedAt.Equal(takenAt) || m.KeyCheck != KeyCheck(key) {
		t.Errorf("manifest = %+v", m)
	}
	if rsum.Objects != 2 || rsum.Removed != 1 {
		t.Errorf("restored %d and removed %d objects, want 2 and 1", rsum.Objects, rsum.Removed)
	}
	if got := read(t, dst, "org/a/attachments/1"); got != "first file" {
		t.Errorf("restored object holds %q", got)
	}
	if got := read(t, dst, "org/b/theme/logo.png"); got != "second file" {
		t.Errorf("restored object holds %q", got)
	}
	if dst.Len() != 2 {
		t.Errorf("bucket holds %d objects after the restore, want 2", dst.Len())
	}
}

func TestRefusalLeavesEverythingAlone(t *testing.T) {
	ctx := context.Background()
	var archive bytes.Buffer
	if _, err := Create(ctx, &archive, source(t, objectstore.NewMemory())); err != nil {
		t.Fatal(err)
	}
	dst := objectstore.NewMemory()
	put(t, dst, "org/a/kept", "untouched")
	refused := errors.New("refused")
	_, _, err := Restore(ctx, &archive, Target{
		Check: func(Manifest) error { return refused },
		Load: func(context.Context, io.Reader) error {
			t.Error("the database was loaded after the check refused")
			return nil
		},
		Store: dst,
	})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the check's refusal", err)
	}
	if dst.Len() != 1 {
		t.Error("the bucket changed after the check refused")
	}
}

func TestFailedLoadLeavesBucketAlone(t *testing.T) {
	ctx := context.Background()
	src := objectstore.NewMemory()
	put(t, src, "org/a/new", "from the backup")
	var archive bytes.Buffer
	if _, err := Create(ctx, &archive, source(t, src)); err != nil {
		t.Fatal(err)
	}
	dst := objectstore.NewMemory()
	put(t, dst, "org/a/kept", "untouched")
	_, _, err := Restore(ctx, &archive, Target{
		Check: accept,
		Load:  func(context.Context, io.Reader) error { return errors.New("psql failed") },
		Store: dst,
	})
	if err == nil {
		t.Fatal("restore went on after the database failed to load")
	}
	if dst.Len() != 1 || read(t, dst, "org/a/kept") != "untouched" {
		t.Error("the bucket changed although the database was not restored")
	}
}

func TestRestoreRefusesWhatIsNotABackup(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := writeEntry(tw, "readme.txt", 2, strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	_, _, err := Restore(context.Background(), &buf, Target{Check: accept})
	if err == nil || !strings.Contains(err.Error(), "not a Stator backup") {
		t.Fatalf("err = %v", err)
	}
}

func TestObjectsNeedAStore(t *testing.T) {
	ctx := context.Background()
	src := objectstore.NewMemory()
	put(t, src, "org/a/file", "x")
	var archive bytes.Buffer
	if _, err := Create(ctx, &archive, source(t, src)); err != nil {
		t.Fatal(err)
	}
	_, _, err := Restore(ctx, &archive, Target{
		Check: accept,
		Load:  func(context.Context, io.Reader) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "STATOR_S3_ENDPOINT") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompatible(t *testing.T) {
	known := []int64{1, 2, 680}
	base := Manifest{Format: Format, Version: "1.4.0", Migrations: []int64{1, 2}, KeyCheck: KeyCheck(key)}
	tests := []struct {
		name   string
		change func(*Manifest)
		key    []byte
		want   string
	}{
		{name: "same schema and key", key: key},
		{name: "older schema", change: func(m *Manifest) { m.Migrations = []int64{1} }, key: key},
		{name: "no sealed secrets", change: func(m *Manifest) { m.KeyCheck = "" }},
		{name: "newer schema", change: func(m *Manifest) { m.Migrations = append(m.Migrations, 690) }, key: key, want: "migration 690"},
		{name: "other format", change: func(m *Manifest) { m.Format = Format + 1 }, key: key, want: "archive format"},
		{name: "no key", want: "Set STATOR_SECRET_KEY"},
		{name: "other key", key: otherKey, want: "not the key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base
			m.Migrations = append([]int64(nil), base.Migrations...)
			if tt.change != nil {
				tt.change(&m)
			}
			err := m.Compatible(known, tt.key)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("err = %v, want one naming %q", err, tt.want)
			}
		})
	}
}

func TestKeyCheckDoesNotCarryTheKey(t *testing.T) {
	check := KeyCheck(key)
	if KeyCheck(otherKey) == check || KeyCheck(nil) != "" {
		t.Fatal("key checks do not tell keys apart")
	}
	if strings.Contains(check, string(key)) || len(check) != 2*keyCheckBytes {
		t.Errorf("key check %q", check)
	}
}

func TestCreateFileAndLatest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := Latest(dir); err == nil {
		t.Fatal("Latest found a backup in an empty directory")
	}
	src := source(t, objectstore.NewMemory())
	first, _, err := CreateFile(ctx, dir, src)
	if err != nil {
		t.Fatal(err)
	}
	src.Now = func() time.Time { return takenAt.Add(time.Hour) }
	second, _, err := CreateFile(ctx, dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "stator-20261011T190203Z.tar" {
		t.Errorf("archive named %s", first)
	}
	if err := os.WriteFile(filepath.Join(dir, "stator-29990101T000000Z.tar.partial"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	latest, err := Latest(dir)
	if err != nil || latest != second {
		t.Errorf("Latest = %s, %v; want %s", latest, err, second)
	}
}

func TestFailedCreateLeavesNoArchive(t *testing.T) {
	dir := t.TempDir()
	src := source(t, objectstore.NewMemory())
	src.Dump = func(context.Context, io.Writer) error { return errors.New("pg_dump failed") }
	if _, _, err := CreateFile(context.Background(), dir, src); err == nil {
		t.Fatal("a failed dump made a backup")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), archivePrefix) {
			t.Errorf("left %s behind", e.Name())
		}
	}
}

func TestKnownReadsTheEmbeddedMigrations(t *testing.T) {
	known, err := Known()
	if err != nil {
		t.Fatal(err)
	}
	if len(known) == 0 || known[0] != 1 {
		t.Fatalf("known = %v", known)
	}
}
