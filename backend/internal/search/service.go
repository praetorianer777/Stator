package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// viewable is true when the actor, parameter $actorParam, may view page alias.
// It stands in for perm.ViewablePage, whose signature it has; the trash is the caller's.
//
// Until #19 a member sees every space, and an unpublished page and everything
// below it are its creator's alone. TODO(#19): use perm.ViewablePage instead.
func viewable(alias string, actorParam int) string {
	actor := "$" + strconv.Itoa(actorParam) + "::uuid"
	return `(EXISTS (SELECT 1 FROM org_member vm WHERE vm.user_id = ` + actor + `)
		AND NOT EXISTS (
			WITH RECURSIVE up (id, parent_id, version, created_by) AS (
				SELECT v.id, v.parent_id, v.version, v.created_by FROM page v WHERE v.id = ` + alias + `.id
				UNION ALL
				SELECT v.id, v.parent_id, v.version, v.created_by FROM page v JOIN up ON v.id = up.parent_id
			)
			SELECT 1 FROM up WHERE up.version = 0 AND up.created_by IS DISTINCT FROM ` + actor + `))`
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

// Search answers a full search with one page of hits and how many there are.
func (s *Service) Search(ctx context.Context, actor perm.Actor, q Query) ([]Hit, int, error) {
	args := []any{actor.UserID}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	conds := []string{viewable("p", 1)}
	var tsq string
	if q.Text != "" {
		tsq = `websearch_to_tsquery(` + config + `, ` + bind(q.Text) + `)`
		conds = append(conds, `p.search_vector @@ `+tsq)
	}
	// Labels arrive with #17; until then no page carries one.
	if !q.wants(HitPage) || len(q.Labels) > 0 {
		conds = append(conds, `FALSE`)
	}
	if len(q.Spaces) > 0 {
		conds = append(conds, `s.key = ANY(`+bind(q.Spaces)+`)`)
	}
	if len(q.Authors) > 0 {
		conds = append(conds, `EXISTS (SELECT 1 FROM page_version a WHERE a.org_id = p.org_id AND a.page_id = p.id AND a.created_by = ANY(`+bind(q.Authors)+`))`)
	}
	if q.After != nil {
		conds = append(conds, `v.created_at >= `+bind(*q.After))
	}
	if q.Before != nil {
		conds = append(conds, `v.created_at < `+bind(*q.Before))
	}
	where := found + ` AND ` + strings.Join(conds, ` AND `)

	order := `v.created_at DESC, p.id`
	if !q.ByUpdate && tsq != "" {
		order = `to_tsvector(` + config + `, p.title) @@ ` + tsq + ` DESC, ts_rank(p.search_vector, ` + tsq + `) DESC, ` + order
	}
	countArgs := len(args)
	title, snippet := `h.title`, `left(page_plain_text(h.body), `+strconv.Itoa(snippetScanChars)+`)`
	if tsq != "" {
		title = `ts_headline(` + config + `, ` + unmarked(`h.title`) + `, ` + tsq + `, ` + bind(titleOptions) + `)`
		snippet = `ts_headline(` + config + `, ` + unmarked(`left(page_plain_text(h.body), `+strconv.Itoa(headlineChars)+`)`) + `, ` + tsq + `, ` + bind(headlineOptions) + `)`
	}
	limit, offset := bind(q.Limit), bind(q.Offset)
	sql := `
		WITH h AS (
			SELECT p.id, p.title, p.body, s.key, s.name, v.created_at, COALESCE(vu.name, '') AS by_name,
			       row_number() OVER (ORDER BY ` + order + `) AS ord
			` + where + `
			ORDER BY ` + order + `
			LIMIT ` + limit + ` OFFSET ` + offset + `
		)
		SELECT h.id, h.key, h.name, h.title, ` + title + `, ` + snippet + `, h.created_at, h.by_name
		FROM h ORDER BY h.ord`

	hits := []Hit{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) `+where, args[:countArgs]...).Scan(&total); err != nil {
			return fmt.Errorf("count the hits: %w", err)
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				h             Hit
				marked, body  string
				updatedAt     time.Time
				updatedByName string
			)
			if err := rows.Scan(&h.Page.ID, &h.Page.SpaceKey, &h.Page.SpaceName, &h.Page.Title, &marked, &body, &updatedAt, &updatedByName); err != nil {
				return err
			}
			h.Type = HitPage
			h.Labels = []string{}
			h.UpdatedAt, h.UpdatedByName = updatedAt, updatedByName
			if tsq != "" {
				h.Title, h.Snippet = Split(marked), Split(body)
			} else {
				h.Title, h.Snippet = Plain(marked), Plain(firstWords(body, snippetMinWords))
			}
			hits = append(hits, h)
		}
		return rows.Err()
	})
	return hits, total, err
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
			`+found+` AND `+viewable("p", 1)+` AND p.search_vector @@ `+query+within+`
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
			WHERE r.user_id = $1 AND p.trashed_at IS NULL AND `+viewable("p", 1)+`
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
			WHERE p.id = $2 AND p.trashed_at IS NULL AND `+viewable("p", 1)+`
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
