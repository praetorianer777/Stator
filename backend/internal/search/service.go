package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service finds pages among those the caller may view. Visibility is part of
// the SQL that finds them, so a total never counts what is left out.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// found is what every search reads: published pages out of the trash that the
// actor, parameter $1, may view, with the version they are at.
const found = `
FROM page p
JOIN space s ON s.id = p.space_id
JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id AND v.number = p.version
LEFT JOIN app_user vu ON vu.id = v.created_by
WHERE p.trashed_at IS NULL AND p.version > 0`

// config is the text search configuration of migration 00100.
const config = `'stator_search'::regconfig`

// spacedName readies a file name for ts_headline, which would read
// "plan_v2.pdf" as one path; Split undoes it.
func spacedName(column string) string {
	return `regexp_replace(translate(` + column + `, '` + startSel + stopSel + openAngle + nameGap + `', ''), '([^[:alnum:][:space:]])', '` + nameGap + `\1` + nameGap + `', 'g')`
}

// unmarked readies a text column for ts_headline; Split undoes it.
func unmarked(column string) string {
	return `translate(translate(` + column + `, '` + startSel + stopSel + openAngle + `', ''), '<', '` + openAngle + `')`
}

// pathOf lists the titles above page p, the home page first.
const pathOf = `ARRAY(
	WITH RECURSIVE up (id, parent_id, title, depth) AS (
		SELECT a.id, a.parent_id, a.title, 0 FROM page a WHERE a.id = p.parent_id
		UNION ALL
		SELECT a.id, a.parent_id, a.title, up.depth + 1 FROM page a JOIN up ON a.id = up.parent_id
	)
	SELECT title FROM up ORDER BY depth DESC)`

// pageHits and attachmentHits are the two kinds of hit a search reads, each a
// row of the same shape, among published pages out of the trash that the actor,
// parameter $1, may view. An attachment carries the version its page is at.
const (
	pageHits = `
SELECT 'page' AS kind, NULL::uuid AS attachment_id, p.id AS page_id, p.title AS page_title,
       p.title AS title, p.body, s.key, s.name, v.created_at AS changed_at, COALESCE(vu.name, '') AS by_name, %s
FROM page p
JOIN space s ON s.id = p.space_id
JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id AND v.number = p.version
LEFT JOIN app_user vu ON vu.id = v.created_by
WHERE p.trashed_at IS NULL AND p.version > 0`
	attachmentHits = `
SELECT 'attachment', a.id, p.id, p.title, a.file_name, NULL::jsonb, s.key, s.name, a.created_at, COALESCE(au.name, ''), %s
FROM attachment a
JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
JOIN space s ON s.id = p.space_id
LEFT JOIN app_user au ON au.id = a.uploaded_by
WHERE p.trashed_at IS NULL AND p.version > 0`
)

// Search answers a full search with one page of hits and how many there are.
func (s *Service) Search(ctx context.Context, actor perm.Actor, q Query) ([]Hit, int, error) {
	args := []any{actor.UserID}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	pages := []string{perm.ViewablePage("p", 1)}
	files := []string{perm.ViewablePage("p", 1)}
	var tsq string
	pageScore, fileScore := `FALSE, 0::real`, `FALSE, 0::real`
	if q.Text != "" {
		tsq = `websearch_to_tsquery(` + config + `, ` + bind(q.Text) + `)`
		pages = append(pages, `p.search_vector @@ `+tsq)
		files = append(files, `a.search_vector @@ `+tsq)
		pageScore = `to_tsvector(` + config + `, p.title) @@ ` + tsq + `, ts_rank(p.search_vector, ` + tsq + `)`
		fileScore = `TRUE, ts_rank(a.search_vector, ` + tsq + `)`
	}
	if !q.wants(HitPage) {
		pages = append(pages, `FALSE`)
	}
	// Labels are on pages; a file carries none, so a label filter leaves files out.
	if !q.wants(HitAttachment) || len(q.Labels) > 0 {
		files = append(files, `FALSE`)
	}
	if len(q.Labels) > 0 {
		pages = append(pages, `EXISTS (SELECT 1 FROM page_label pl WHERE pl.org_id = p.org_id AND pl.page_id = p.id AND pl.name = ANY(`+bind(q.Labels)+`))`)
	}
	if len(q.Spaces) > 0 {
		keys := bind(q.Spaces)
		pages = append(pages, `s.key = ANY(`+keys+`)`)
		files = append(files, `s.key = ANY(`+keys+`)`)
	}
	if len(q.Authors) > 0 {
		authors := bind(q.Authors)
		pages = append(pages, `EXISTS (SELECT 1 FROM page_version pa WHERE pa.org_id = p.org_id AND pa.page_id = p.id AND pa.created_by = ANY(`+authors+`))`)
		files = append(files, `a.uploaded_by = ANY(`+authors+`)`)
	}
	if q.After != nil {
		after := bind(*q.After)
		pages = append(pages, `v.created_at >= `+after)
		files = append(files, `a.created_at >= `+after)
	}
	if q.Before != nil {
		before := bind(*q.Before)
		pages = append(pages, `v.created_at < `+before)
		files = append(files, `a.created_at < `+before)
	}
	hits := `WITH hit (kind, attachment_id, page_id, page_title, title, body, key, name, changed_at, by_name, title_match, rank) AS (` +
		fmt.Sprintf(pageHits, pageScore) + ` AND ` + strings.Join(pages, ` AND `) + `
		UNION ALL` +
		fmt.Sprintf(attachmentHits, fileScore) + ` AND ` + strings.Join(files, ` AND `) + `
	)`

	order := `h.changed_at DESC, h.kind DESC, COALESCE(h.attachment_id, h.page_id)`
	if !q.ByUpdate && tsq != "" {
		order = `h.title_match DESC, h.rank DESC, ` + order
	}
	countArgs := len(args)
	title := `h.title`
	snippet := `CASE WHEN h.kind = 'page' THEN left(page_plain_text(h.body), ` + strconv.Itoa(snippetScanChars) + `) ELSE '' END`
	if tsq != "" {
		titleOpts, snippetOpts := bind(titleOptions), bind(headlineOptions)
		title = `CASE WHEN h.kind = 'page' THEN ts_headline(` + config + `, ` + unmarked(`h.title`) + `, ` + tsq + `, ` + titleOpts + `)
			ELSE ts_headline(` + config + `, ` + spacedName(`h.title`) + `, ` + tsq + `, ` + titleOpts + `) END`
		snippet = `CASE WHEN h.kind = 'page' THEN ts_headline(` + config + `, ` + unmarked(`left(page_plain_text(h.body), `+strconv.Itoa(headlineChars)+`)`) + `, ` + tsq + `, ` + snippetOpts + `) ELSE '' END`
	}
	limit, offset := bind(q.Limit), bind(q.Offset)
	sql := hits + `, chosen AS (
			SELECT h.*, row_number() OVER (ORDER BY ` + order + `) AS ord FROM hit h
			ORDER BY ` + order + `
			LIMIT ` + limit + ` OFFSET ` + offset + `
		)
		SELECT h.kind, h.attachment_id, h.page_id, h.page_title, h.key, h.name, ` + title + `, ` + snippet + `, h.changed_at, h.by_name,
		       CASE WHEN h.kind = 'page' THEN ARRAY(SELECT pl.name FROM page_label pl WHERE pl.page_id = h.page_id ORDER BY pl.name) ELSE '{}' END
		FROM chosen h ORDER BY h.ord`

	out := []Hit{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, hits+` SELECT count(*) FROM hit`, args[:countArgs]...).Scan(&total); err != nil {
			return fmt.Errorf("count the hits: %w", err)
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				h            Hit
				marked, body string
			)
			if err := rows.Scan(&h.Type, &h.AttachmentID, &h.Page.ID, &h.Page.Title, &h.Page.SpaceKey, &h.Page.SpaceName,
				&marked, &body, &h.UpdatedAt, &h.UpdatedByName, &h.Labels); err != nil {
				return err
			}
			if h.Labels == nil {
				h.Labels = []string{}
			}
			if tsq != "" {
				h.Title, h.Snippet = Split(marked), Split(body)
			} else {
				h.Title, h.Snippet = Plain(marked), Plain(firstWords(body, snippetMinWords))
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, total, err
}

// How much of a body is read for its snippet. A headline reads its whole
// text, so a long page is cut first; its opening is what a hit mostly shows.
const (
	headlineChars    = 100000
	snippetScanChars = 4000
)

// Quick finds pages whose title words start with each word typed, the best
// title matches first; spaceKey, when set, stays inside one space.
func (s *Service) Quick(ctx context.Context, actor perm.Actor, typed, spaceKey string, limit int) ([]PageHit, error) {
	out := []PageHit{}
	tsq := PrefixQuery(typed)
	if tsq == "" {
		return out, nil
	}
	args := []any{actor.UserID, tsq, strings.TrimSpace(typed), limit}
	within := ``
	if spaceKey = strings.TrimSpace(spaceKey); spaceKey != "" {
		args = append(args, spaceKey)
		within = ` AND s.key = upper($5)`
	}
	query := `to_tsquery(` + config + `, $2)`
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, `+pathOf+`
			`+found+` AND `+perm.ViewablePage("p", 1)+` AND p.search_vector @@ `+query+within+`
			ORDER BY starts_with(lower(p.title), lower($3)) DESC, ts_rank(p.search_vector, `+query+`) DESC,
			         char_length(p.title), v.created_at DESC, p.id
			LIMIT $4`, args...)
		if err != nil {
			return fmt.Errorf("quick search: %w", err)
		}
		out, err = pgx.CollectRows(rows, scanPageHit)
		return err
	})
	return out, err
}

func scanPageHit(row pgx.CollectableRow) (PageHit, error) {
	var h PageHit
	err := row.Scan(&h.ID, &h.Title, &h.SpaceKey, &h.SpaceName, &h.Path)
	if h.Path == nil {
		h.Path = []string{}
	}
	return h, err
}

// Recent lists the pages the actor visited last that they may still view.
func (s *Service) Recent(ctx context.Context, actor perm.Actor, limit int) ([]RecentPage, error) {
	out := []RecentPage{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, `+pathOf+`, r.visited_at
			FROM page_visit r
			JOIN page p ON p.org_id = r.org_id AND p.id = r.page_id
			JOIN space s ON s.id = p.space_id
			WHERE r.user_id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`
			ORDER BY r.visited_at DESC, p.id
			LIMIT $2`, actor.UserID, limit)
		if err != nil {
			return fmt.Errorf("list recent pages: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RecentPage, error) {
			var r RecentPage
			err := row.Scan(&r.ID, &r.Title, &r.SpaceKey, &r.SpaceName, &r.Path, &r.VisitedAt)
			if r.Path == nil {
				r.Path = []string{}
			}
			return r, err
		})
		return err
	})
	return out, err
}

// Visit notes that the actor opened a page, refusing with page.ErrNotFound one
// they may not view.
func (s *Service) Visit(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO page_visit (org_id, user_id, page_id)
			SELECT p.org_id, $1, p.id FROM page p
			WHERE p.id = $2 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+`
			ON CONFLICT (org_id, user_id, page_id) DO UPDATE SET visited_at = now()`, actor.UserID, id)
		if err != nil {
			return fmt.Errorf("note the visit: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return page.ErrNotFound
		}
		return nil
	})
}
