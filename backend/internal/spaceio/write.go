package spaceio

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// writeArchive writes each page with its versions, in the snapshot's
// transaction, and counts the versions and comments it wrote.
func (s *snapshot) writeArchive(ctx context.Context, tx db.DBTX, zw *zip.Writer, step func()) (Counts, error) {
	counts := Counts{Pages: len(s.pages), Files: s.files}
	for _, p := range s.pages {
		if p.Kind != "folder" {
			var err error
			if p.Versions, err = s.versions(ctx, tx, p.ID); err != nil {
				return counts, err
			}
		}
		counts.Versions += len(p.Versions)
		for _, t := range p.Threads {
			counts.Comments += len(t.Comments)
		}
		if err := writeJSON(zw, pagePath(p.ID), p); err != nil {
			return counts, err
		}
		// The bodies are written; holding them for every page at once is
		// what a large space could not afford.
		p.Versions = nil
		step()
	}
	return counts, nil
}

// writeFiles copies every file's bytes from storage into the archive, after
// the transaction, one at a time.
func (s *snapshot) writeFiles(ctx context.Context, store objectstore.Store, zw *zip.Writer, path func(p *ArchivePage, f File) string, include func(p *ArchivePage, f File) bool, step func()) error {
	for _, p := range s.pages {
		for _, f := range p.Files {
			if !include(p, f) {
				continue
			}
			if err := copyObject(ctx, store, s.keys[f.ID], zw, path(p, f), f); err != nil {
				return err
			}
			step()
		}
	}
	return nil
}

func copyObject(ctx context.Context, store objectstore.Store, key string, zw *zip.Writer, name string, f File) error {
	body, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read the file %s: %w", f.Name, err)
	}
	defer body.Close()
	// Pictures and archives are compressed already; storing them saves
	// the time of compressing them again for nothing.
	out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: f.CreatedAt})
	if err != nil {
		return err
	}
	n, err := io.Copy(out, body)
	if err != nil {
		return fmt.Errorf("read the file %s: %w", f.Name, err)
	}
	if n != f.Size {
		return fmt.Errorf("the file %s holds %d bytes in storage, not %d", f.Name, n, f.Size)
	}
	return nil
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func archiveFilePath(_ *ArchivePage, f File) string { return filePath(f.ID) }

func everyFile(*ArchivePage, File) bool { return true }

// fileIDs is the set of a page's files a document names.
func fileIDs(body json.RawMessage) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	var walk func(v any)
	walk = func(v any) {
		n, ok := v.(map[string]any)
		if !ok {
			return
		}
		if attrs, ok := n["attrs"].(map[string]any); ok {
			if text, ok := attrs["attachmentId"].(string); ok {
				if id, err := uuid.Parse(text); err == nil {
					out[id] = true
				}
			}
		}
		if children, ok := n["content"].([]any); ok {
			for _, c := range children {
				walk(c)
			}
		}
	}
	var root any
	if json.Unmarshal(body, &root) == nil {
		walk(root)
	}
	return out
}
