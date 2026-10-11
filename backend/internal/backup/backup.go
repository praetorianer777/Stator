// Package backup writes a whole site, its database and every stored object,
// into one archive, and brings a site back from one.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// Format is the archive layout this package writes and reads. A change to the
// layout raises it, so an old tool refuses an archive it would misread.
const Format = 1

const (
	manifestEntry = "manifest.json"
	databaseEntry = "database.sql.gz"
	objectsPrefix = "objects/"

	archivePrefix = "stator-"
	archiveSuffix = ".tar"
	partialSuffix = ".partial"
	// Sorts as it reads, so the newest archive in a directory is the last name.
	stampLayout = "20060102T150405Z"

	keyCheckLabel = "stator backup key check\x00"
	keyCheckBytes = 16

	entryMode = 0o600
)

// Manifest says what an archive holds and what it takes to restore it.
type Manifest struct {
	Format    int       `json:"format"`
	Version   string    `json:"version"`
	Commit    string    `json:"commit,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Migrations are the schema versions applied when the dump was taken.
	Migrations []int64 `json:"migrations"`
	// KeyCheck tells whether a restore runs with the key that sealed the
	// stored secrets, without the archive carrying the key.
	KeyCheck string `json:"keyCheck,omitempty"`
}

// KeyCheck derives the fingerprint of STATOR_SECRET_KEY a manifest records.
func KeyCheck(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(append([]byte(keyCheckLabel), key...))
	return hex.EncodeToString(sum[:keyCheckBytes])
}

// Compatible refuses an archive this build cannot restore faithfully: another
// layout, a schema it does not know, or a different secret key.
func (m Manifest) Compatible(known []int64, key []byte) error {
	if m.Format != Format {
		return fmt.Errorf("This backup is in archive format %d, which this version of Stator cannot read. Restore it with Stator %s.", m.Format, m.Version)
	}
	for _, v := range m.Migrations {
		if !slices.Contains(known, v) {
			return fmt.Errorf("This backup was taken with Stator %s, whose database has changes this version does not know (migration %d). Restore it with Stator %s or later.", m.Version, v, m.Version)
		}
	}
	switch {
	case m.KeyCheck == "":
	case len(key) == 0:
		return errors.New("The backed-up site sealed its stored secrets with STATOR_SECRET_KEY. Set STATOR_SECRET_KEY to that key and restore again.")
	case KeyCheck(key) != m.KeyCheck:
		return errors.New("STATOR_SECRET_KEY is not the key the backed-up site ran with, so its stored secrets could not be opened. Set it to that key and restore again.")
	}
	return nil
}

// Source is what Create reads a site from.
type Source struct {
	Version    string
	Commit     string
	Migrations []int64
	Key        []byte
	// Dump writes the database as SQL.
	Dump func(ctx context.Context, w io.Writer) error
	// Store is nil for a site without file storage.
	Store objectstore.Store
	// TempDir holds the dump and one object at a time while they are written.
	TempDir string
	Now     func() time.Time
}

// Summary is what a backup or a restore did.
type Summary struct {
	Objects int
	// Missing are objects listed but gone before they were read: removed
	// while the backup ran, such as by the worker emptying the trash.
	Missing []string
	// Removed are objects in the bucket a restore found no copy of.
	Removed int
}

// Create writes the archive to w. The database is dumped before the objects
// are listed, so every file a dumped row names was there to be copied.
func Create(ctx context.Context, w io.Writer, src Source) (Summary, error) {
	var sum Summary
	dump, err := os.CreateTemp(src.TempDir, "dump-*.sql.gz")
	if err != nil {
		return sum, err
	}
	defer os.Remove(dump.Name())
	defer dump.Close()
	zw := gzip.NewWriter(dump)
	if err := src.Dump(ctx, zw); err != nil {
		return sum, fmt.Errorf("dump the database: %w", err)
	}
	if err := zw.Close(); err != nil {
		return sum, err
	}

	var keys []string
	if src.Store != nil {
		if keys, err = src.Store.List(ctx, ""); err != nil {
			return sum, fmt.Errorf("list stored files: %w", err)
		}
		sort.Strings(keys)
	}

	tw := tar.NewWriter(w)
	manifest, err := json.MarshalIndent(Manifest{
		Format:     Format,
		Version:    src.Version,
		Commit:     src.Commit,
		CreatedAt:  src.Now().UTC(),
		Migrations: src.Migrations,
		KeyCheck:   KeyCheck(src.Key),
	}, "", "  ")
	if err != nil {
		return sum, err
	}
	if err := writeEntry(tw, manifestEntry, int64(len(manifest)), strings.NewReader(string(manifest))); err != nil {
		return sum, err
	}
	if err := writeFile(tw, databaseEntry, dump); err != nil {
		return sum, err
	}

	for _, key := range keys {
		missing, err := copyObject(ctx, tw, src, key)
		if err != nil {
			return sum, err
		}
		if missing {
			sum.Missing = append(sum.Missing, key)
			continue
		}
		sum.Objects++
	}
	return sum, tw.Close()
}

func copyObject(ctx context.Context, tw *tar.Writer, src Source, key string) (missing bool, err error) {
	body, err := src.Store.Get(ctx, key)
	if errors.Is(err, objectstore.ErrNoObject) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read stored file %s: %w", key, err)
	}
	defer body.Close()
	tmp, err := os.CreateTemp(src.TempDir, "object-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, body); err != nil {
		return false, fmt.Errorf("read stored file %s: %w", key, err)
	}
	return false, writeFile(tw, objectsPrefix+key, tmp)
}

func writeFile(tw *tar.Writer, name string, f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return writeEntry(tw, name, info.Size(), f)
}

func writeEntry(tw *tar.Writer, name string, size int64, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: entryMode, Size: size, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.Copy(tw, r)
	return err
}

// CreateFile writes a new archive into dir, named by when it was taken. A
// backup that fails leaves nothing behind that a restore could mistake for one.
func CreateFile(ctx context.Context, dir string, src Source) (string, Summary, error) {
	name := filepath.Join(dir, archivePrefix+src.Now().UTC().Format(stampLayout)+archiveSuffix)
	partial := name + partialSuffix
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entryMode)
	if err != nil {
		return "", Summary{}, err
	}
	sum, err := Create(ctx, f, src)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(partial, name)
	}
	if err != nil {
		os.Remove(partial)
		return "", sum, err
	}
	return name, sum, nil
}

// Latest names the newest archive in dir.
func Latest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var newest string
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(n, archivePrefix) && strings.HasSuffix(n, archiveSuffix) && n > newest {
			newest = n
		}
	}
	if newest == "" {
		return "", fmt.Errorf("There is no backup in %s. Name the archive to restore with -from.", dir)
	}
	return filepath.Join(dir, newest), nil
}

// Target is what Restore brings a site back into.
type Target struct {
	// Check runs once the manifest is read and before anything is changed.
	Check func(Manifest) error
	// Load replaces the database with the SQL it is given.
	Load  func(ctx context.Context, r io.Reader) error
	Store objectstore.Store
	// TempDir holds one object at a time, since a store wants its size first.
	TempDir string
}

// Restore replaces the database, then the stored objects, with the archive's.
// Objects the archive does not hold are removed, so the bucket ends as it was.
func Restore(ctx context.Context, r io.Reader, dst Target) (Manifest, Summary, error) {
	var (
		m   Manifest
		sum Summary
	)
	tr := tar.NewReader(r)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != manifestEntry {
		return m, sum, errors.New("This file is not a Stator backup: it does not start with a manifest.")
	}
	if err := json.NewDecoder(tr).Decode(&m); err != nil {
		return m, sum, fmt.Errorf("read the backup's manifest: %w", err)
	}
	if err := dst.Check(m); err != nil {
		return m, sum, err
	}

	hdr, err = tr.Next()
	if err != nil || hdr.Name != databaseEntry {
		return m, sum, errors.New("The backup holds no database after its manifest, so it is incomplete. Restore another one.")
	}
	zr, err := gzip.NewReader(tr)
	if err != nil {
		return m, sum, fmt.Errorf("read the backup's database: %w", err)
	}
	if err := dst.Load(ctx, zr); err != nil {
		return m, sum, fmt.Errorf("restore the database: %w", err)
	}

	restored := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, sum, fmt.Errorf("read the backup: %w", err)
		}
		key, ok := strings.CutPrefix(hdr.Name, objectsPrefix)
		if !ok || key == "" {
			return m, sum, fmt.Errorf("The backup holds %s, which is not part of a Stator backup.", hdr.Name)
		}
		if dst.Store == nil {
			return m, sum, errors.New("The backup holds stored files but no file storage is configured. Set STATOR_S3_ENDPOINT and restore again.")
		}
		if err := putObject(ctx, dst, key, tr); err != nil {
			return m, sum, err
		}
		restored[key] = true
		sum.Objects++
	}

	if dst.Store == nil {
		return m, sum, nil
	}
	keys, err := dst.Store.List(ctx, "")
	if err != nil {
		return m, sum, fmt.Errorf("list stored files: %w", err)
	}
	for _, key := range keys {
		if restored[key] {
			continue
		}
		if err := dst.Store.Delete(ctx, key); err != nil {
			return m, sum, fmt.Errorf("remove stored file %s: %w", key, err)
		}
		sum.Removed++
	}
	return m, sum, nil
}

func putObject(ctx context.Context, dst Target, key string, r io.Reader) error {
	tmp, err := os.CreateTemp(dst.TempDir, "object-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, r)
	if err != nil {
		return fmt.Errorf("read stored file %s from the backup: %w", key, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := dst.Store.Put(ctx, key, tmp, size, "application/octet-stream"); err != nil {
		return fmt.Errorf("restore stored file %s: %w", key, err)
	}
	return nil
}
