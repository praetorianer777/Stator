package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// AnonymousHit is a page found for somebody who is not signed in: where it is
// and the words around the match, and nothing about who wrote it.
type AnonymousHit struct {
	Page    PageRef   `json:"page"`
	Title   []Segment `json:"title"`
	Snippet []Segment `json:"snippet"`
}

// Anonymous finds published pages anybody may read, the best matches first,
// within one space when spaceKey names one; more says another window follows.
func (s *Service) Anonymous(ctx context.Context, text, spaceKey string, limit, offset int) (hits []AnonymousHit, more bool, err error) {
	if !db.AnonymousFrom(ctx) {
		return nil, false, errors.New("a public search is made as an anonymous reader")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false, &FieldError{"q", "Type the words to look for."}
	}
	if utf8.RuneCountInString(text) > MaxQueryLength {
		return nil, false, &FieldError{"q", fmt.Sprintf("Keep the search to %d characters.", MaxQueryLength)}
	}
	type found struct {
		id               uuid.UUID
		title, key, name string
		body             []byte
	}
	hits = []AnonymousHit{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		tsq := `websearch_to_tsquery(` + config + `, $1)`
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, p.body FROM page p JOIN space s ON s.id = p.space_id
			WHERE p.trashed_at IS NULL AND p.version > 0 AND p.kind = 'page' AND p.search_vector @@ `+tsq+`
			  AND perm_page_viewable(p.id, NULL::uuid) AND perm_space_holds(NULL::uuid, s.id, 'view')
			  AND ($2 = '' OR s.key = upper(btrim($2)))
			ORDER BY to_tsvector(`+config+`, p.title) @@ `+tsq+` DESC, ts_rank(p.search_vector, `+tsq+`) DESC, p.id
			LIMIT $3 OFFSET $4`, text, spaceKey, limit+1, offset)
		if err != nil {
			return fmt.Errorf("search the public pages: %w", err)
		}
		var all []found
		for rows.Next() {
			var f found
			if err := rows.Scan(&f.id, &f.title, &f.key, &f.name, &f.body); err != nil {
				rows.Close()
				return err
			}
			all = append(all, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if more = len(all) > limit; more {
			all = all[:limit]
		}
		if len(all) == 0 {
			return nil
		}
		// The index holds the names of the people a page mentions, which an
		// anonymous reader is never shown: each hit is matched again, and its
		// snippet cut, from the words with those names taken out.
		titles, texts := make([]string, len(all)), make([]string, len(all))
		for i, f := range all {
			titles[i] = f.title
			if root, err := document.Parse(f.body); err == nil {
				texts[i] = document.PlainText(document.ForAnonymous(root))
			}
		}
		marked, err := tx.Query(ctx, `
			SELECT to_tsvector(`+config+`, u.title) @@ `+tsq+` OR to_tsvector(`+config+`, left(u.body, `+fmt.Sprint(headlineChars)+`)) @@ `+tsq+`,
			       ts_headline(`+config+`, `+unmarked(`u.title`)+`, `+tsq+`, $4),
			       ts_headline(`+config+`, `+unmarked(`left(u.body, `+fmt.Sprint(headlineChars)+`)`)+`, `+tsq+`, $5)
			FROM unnest($2::text[], $3::text[]) WITH ORDINALITY AS u (title, body, i) ORDER BY u.i`,
			text, titles, texts, titleOptions, headlineOptions)
		if err != nil {
			return fmt.Errorf("mark the public hits: %w", err)
		}
		defer marked.Close()
		for i := 0; marked.Next(); i++ {
			var (
				still         bool
				title, around string
			)
			if err := marked.Scan(&still, &title, &around); err != nil {
				return err
			}
			if !still {
				continue
			}
			f := all[i]
			hits = append(hits, AnonymousHit{
				Page:  PageRef{ID: f.id, Title: f.title, SpaceKey: f.key, SpaceName: f.name},
				Title: Split(title), Snippet: Split(around),
			})
		}
		return marked.Err()
	})
	if err != nil {
		return nil, false, err
	}
	return hits, more, nil
}
