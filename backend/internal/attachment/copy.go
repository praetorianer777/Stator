package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// ReferenceNodes are the document nodes that name a file by attachmentId.
var ReferenceNodes = []string{"image", "attachment"}

// PagesCopied gives every copied page a copy of each file on its original,
// objects included, and points the copy's body at its own files.
func (s *Service) PagesCopied(ctx context.Context, tx db.DBTX, copies map[uuid.UUID]uuid.UUID) error {
	originals := make([]uuid.UUID, 0, len(copies))
	for from := range copies {
		originals = append(originals, from)
	}
	rows, err := tx.Query(ctx, `
		SELECT id, page_id, object_key FROM attachment WHERE page_id = ANY($1) ORDER BY created_at, id`, originals)
	if err != nil {
		return err
	}
	type source struct {
		ID, PageID uuid.UUID
		Key        string
	}
	files, err := pgx.CollectRows(rows, pgx.RowToStructByPos[source])
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	renamed := make(map[uuid.UUID]uuid.UUID, len(files))
	for _, f := range files {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		var key, contentType string
		if err := tx.QueryRow(ctx, `
			INSERT INTO attachment (id, org_id, page_id, uploaded_by, file_name, content_type, size_bytes, width, height, created_at)
			SELECT $2, org_id, $3, uploaded_by, file_name, content_type, size_bytes, width, height, created_at
			FROM attachment WHERE id = $1
			RETURNING object_key, content_type`, f.ID, id, copies[f.PageID]).Scan(&key, &contentType); err != nil {
			return fmt.Errorf("copy the file: %w", err)
		}
		if err := s.copyObject(ctx, f.Key, key, contentType); err != nil {
			return err
		}
		renamed[f.ID] = id
	}
	for _, to := range copies {
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM page WHERE id = $1`, to).Scan(&body); err != nil {
			return err
		}
		rewritten, changed, err := RewriteReferences(body, renamed)
		if err != nil {
			return fmt.Errorf("point the copy at its own files: %w", err)
		}
		if changed {
			if _, err := tx.Exec(ctx, `UPDATE page SET body = $2 WHERE id = $1`, to, rewritten); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyObject reads the whole object before writing, which the store needs to
// sign the upload; a file is at most the upload limit.
func (s *Service) copyObject(ctx context.Context, from, to, contentType string) error {
	body, err := s.store.Get(ctx, from)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return s.store.Put(ctx, to, bytes.NewReader(data), int64(len(data)), contentType)
}

// RewriteReferences points every file node of a document named in ids at its
// new id, and reports whether anything changed. Numbers keep their spelling.
func RewriteReferences(body []byte, ids map[uuid.UUID]uuid.UUID) ([]byte, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, false, err
	}
	if !rewrite(root, ids) {
		return body, false, nil
	}
	out, err := json.Marshal(root)
	return out, true, err
}

func rewrite(node any, ids map[uuid.UUID]uuid.UUID) bool {
	n, ok := node.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	if typ, _ := n["type"].(string); isReference(typ) {
		if attrs, ok := n["attrs"].(map[string]any); ok {
			if text, ok := attrs["attachmentId"].(string); ok {
				if old, err := uuid.Parse(text); err == nil {
					if fresh, ok := ids[old]; ok {
						attrs["attachmentId"] = fresh.String()
						changed = true
					}
				}
			}
		}
	}
	if children, ok := n["content"].([]any); ok {
		for _, child := range children {
			if rewrite(child, ids) {
				changed = true
			}
		}
	}
	return changed
}

func isReference(typ string) bool {
	for _, r := range ReferenceNodes {
		if typ == r {
			return true
		}
	}
	return false
}
