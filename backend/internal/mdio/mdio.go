// Package mdio moves pages in and out as Markdown: a page or a subtree as
// an archive with its files, and Markdown files and folders as new pages.
package mdio

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/markdown"
	"github.com/praetorianer777/stator/backend/internal/page"
)

const (
	// DefaultMaxImportBytes is what one import may weigh, its files included.
	DefaultMaxImportBytes int64 = 100 << 20
	// MaxImportFiles bounds the files of one import, an archive's counted.
	MaxImportFiles = 1000
	// MaxImportPages bounds the pages one import makes.
	MaxImportPages = 200
	// filesSuffix names the folder beside a page's Markdown that holds its files.
	filesSuffix = ".files"
	// maxWarnings bounds what one import reports it changed.
	maxWarnings = 50
)

// InvalidError refuses an upload in a sentence that says what to change.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// TooLargeError refuses an upload over the limit, naming it.
type TooLargeError struct{ Limit int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("this upload is over %s; import a smaller folder, or its parts one at a time", attachment.Size(e.Limit))
}

var (
	// ErrNoMarkdown refuses an upload without a Markdown file in it.
	ErrNoMarkdown = errors.New("there is no Markdown file in the upload; choose .md files, a folder holding them, or a .zip of one")
	// ErrTooManyBelow refuses an export of more pages than MaxExportPages.
	ErrTooManyBelow = fmt.Errorf("this page has more than %d pages below it; export the pages under it one at a time", page.MaxBelow)
)

// File is one file of an upload: its path in the folder or archive it came
// from, slash separated, and its bytes.
type File struct {
	Path string
	Data []byte
}

// isMarkdown says whether a path names a Markdown file.
func isMarkdown(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".md" || ext == ".markdown"
}

// cleanPath makes an uploaded path relative and slash separated, and says
// whether to keep it: never a hidden file or a system's leftovers.
func cleanPath(p string) (string, bool, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", false, invalid("the upload names the path %q, which leaves its folder; upload the folder itself", p)
	}
	clean := path.Clean(strings.TrimLeft(p, "/"))
	if clean == "." {
		return "", false, nil
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") || part == "__MACOSX" {
			return "", false, nil
		}
	}
	return clean, true, nil
}

// collect cleans the paths of an upload and opens its archives, keeping the
// order the files came in, which is the order the pages take.
func collect(in []File, limit int64) ([]File, error) {
	var out []File
	seen := map[string]bool{}
	var total int64
	add := func(f File) error {
		p, keep, err := cleanPath(f.Path)
		if err != nil || !keep || seen[p] {
			return err
		}
		seen[p] = true
		total += int64(len(f.Data))
		if total > limit {
			return &TooLargeError{Limit: limit}
		}
		if len(out) >= MaxImportFiles {
			return invalid("the upload holds more than %d files; import its folders one at a time", MaxImportFiles)
		}
		out = append(out, File{Path: p, Data: f.Data})
		return nil
	}
	for _, f := range in {
		if strings.EqualFold(path.Ext(f.Path), ".zip") {
			if err := unzip(f, limit-total, add); err != nil {
				return nil, err
			}
			continue
		}
		if err := add(f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// unzip reads an archive's files, holding what it unpacks to the limit
// whatever the archive's directory claims about their sizes.
func unzip(f File, limit int64, add func(File) error) error {
	r, err := zip.NewReader(bytes.NewReader(f.Data), int64(len(f.Data)))
	if err != nil {
		return invalid("the archive %s could not be read; make the archive again and upload it", path.Base(f.Path))
	}
	if len(r.File) > MaxImportFiles {
		return invalid("the upload holds more than %d files; import its folders one at a time", MaxImportFiles)
	}
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return invalid("the archive %s holds %s, which could not be read; make the archive again and upload it", path.Base(f.Path), entry.Name)
		}
		data, err := io.ReadAll(io.LimitReader(rc, limit+1))
		_ = rc.Close()
		if err != nil {
			return invalid("the archive %s holds %s, which could not be read; make the archive again and upload it", path.Base(f.Path), entry.Name)
		}
		if int64(len(data)) > limit {
			return &TooLargeError{Limit: limit}
		}
		limit -= int64(len(data))
		if err := add(File{Path: entry.Name, Data: data}); err != nil {
			return err
		}
	}
	return nil
}

// resolvePath reads a link target written in a Markdown file as a path of
// the upload, and the fragment after it.
func resolvePath(from, dest string) (string, string, bool) {
	fragment := ""
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		dest, fragment = dest[:i], dest[i:]
	}
	if i := strings.IndexByte(dest, '?'); i >= 0 {
		dest = dest[:i]
	}
	if unescaped, err := url.PathUnescape(dest); err == nil {
		dest = unescaped
	}
	if dest == "" {
		return "", "", false
	}
	joined := path.Join(path.Dir(from), dest)
	if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
		return "", "", false
	}
	return strings.TrimSuffix(joined, "/"), fragment, true
}

// titleFrom makes a title of a file or folder name, as a person would write it.
func titleFrom(name string) string {
	if isMarkdown(name) {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	title := clip(strings.NewReplacer("-", " ", "_", " ").Replace(name))
	if title == "" {
		return untitled
	}
	return title
}

// untitled names a page whose file name has no words.
const untitled = "Untitled"

// clip trims a title to what a page takes.
func clip(title string) string {
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > page.MaxTitleLength {
		title = strings.TrimSpace(string(runes[:page.MaxTitleLength]))
	}
	return title
}

// relPath is the path from a folder to a file, both relative to the archive.
func relPath(fromDir, to string) string {
	from := splitDir(fromDir)
	target := strings.Split(to, "/")
	common := 0
	for common < len(from) && common < len(target)-1 && from[common] == target[common] {
		common++
	}
	parts := slices.Repeat([]string{".."}, len(from)-common)
	parts = append(parts, target[common:]...)
	return markdown.PathRef(strings.Join(parts, "/"))
}

func splitDir(dir string) []string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}
