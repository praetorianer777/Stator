package label

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
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Service puts labels on pages and finds pages by them. Whether the actor may
// see or change a label is decided on its page.
type Service struct {
	db *db.Cluster
}

// NewService makes the service and has it told of every page copy, so the
// copy's labels come along.
func NewService(cluster *db.Cluster, pages *page.Service) *Service {
	s := &Service{db: cluster}
	if pages != nil {
		pages.ObserveCopies(s)
	}
	return s
}

func onPage(ctx context.Context, tx db.DBTX, pageID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT name FROM page_label WHERE page_id = $1 ORDER BY name`, pageID)
	if err != nil {
		return nil, fmt.Errorf("read the labels: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if names == nil {
		names = []string{}
	}
	return names, err
}

// OnPage is the labels on a page, by name.
func (s *Service) OnPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID) ([]string, error) {
	var out []string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := page.Load(ctx, tx, actor, pageID); err != nil {
			return err
		}
		var err error
		out, err = onPage(ctx, tx, pageID)
		return err
	})
	return out, err
}

// editable loads a page the actor may change the labels of.
func editable(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) error {
	p, _, err := page.Load(ctx, tx, actor, pageID)
	if err != nil {
		return err
	}
	if !p.Can.Edit {
		return &perm.DeniedError{Action: perm.EditPages}
	}
	return nil
}

// Add puts a label on a page and answers the page's labels; a label the page
// carries already is no change.
func (s *Service) Add(ctx context.Context, actor perm.Actor, pageID uuid.UUID, raw string) ([]string, db.LSN, error) {
	name, err := Normalize(raw)
	if err != nil {
		return nil, 0, fieldError("name", err)
	}
	var out []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := editable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		// The page row is locked so two tabs adding at once cannot pass the limit together.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM page WHERE id = $1 FOR NO KEY UPDATE`, pageID); err != nil {
			return err
		}
		current, err := onPage(ctx, tx, pageID)
		if err != nil {
			return err
		}
		for _, have := range current {
			if have == name {
				out = current
				return nil
			}
		}
		if len(current) >= MaxPerPage {
			return fieldError("name", ErrTooMany)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_label (org_id, page_id, name, created_by)
			SELECT p.org_id, p.id, $2, $3 FROM page p WHERE p.id = $1
			ON CONFLICT DO NOTHING`, pageID, name, actor.UserID); err != nil {
			return fmt.Errorf("add the label: %w", err)
		}
		out, err = onPage(ctx, tx, pageID)
		return err
	})
	return out, lsn, err
}

// Remove takes a label off a page; one the page does not carry is no change.
func (s *Service) Remove(ctx context.Context, actor perm.Actor, pageID uuid.UUID, raw string) (db.LSN, error) {
	name, err := Normalize(raw)
	if err != nil {
		return 0, fieldError("labelName", err)
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := editable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM page_label WHERE page_id = $1 AND name = $2`, pageID, name); err != nil {
			return fmt.Errorf("remove the label: %w", err)
		}
		return nil
	})
}

// likePrefix matches what starts with typed, taken literally.
func likePrefix(typed string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(typed) + "%"
}

// typedPrefix is what somebody typed so far, read the way Normalize reads a name.
func typedPrefix(typed string) string {
	prefix := strings.Join(strings.Fields(strings.ToLower(typed)), "-")
	if strings.HasSuffix(typed, " ") && prefix != "" {
		prefix += "-"
	}
	return prefix
}

// Suggest offers the labels on pages the actor may view that start with what
// was typed, the most used first; spaceKey, when set, stays inside one space.
func (s *Service) Suggest(ctx context.Context, actor perm.Actor, typed, spaceKey string, limit int) ([]Suggestion, error) {
	args := []any{actor.UserID, likePrefix(typedPrefix(typed)), limit}
	within := ``
	if spaceKey = strings.TrimSpace(spaceKey); spaceKey != "" {
		args = append(args, spaceKey)
		within = ` AND s.key = upper($4)`
	}
	out := []Suggestion{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT l.name, count(*)::int
			FROM page_label l
			JOIN page p ON p.org_id = l.org_id AND p.id = l.page_id
			JOIN space s ON s.id = p.space_id
			WHERE l.name LIKE $2 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 1)+within+`
			GROUP BY l.name
			ORDER BY count(*) DESC, l.name
			LIMIT $3`, args...)
		if err != nil {
			return fmt.Errorf("suggest labels: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Suggestion])
		return err
	})
	return out, err
}

// pathOf lists the titles above page p, the home page first.
const pathOf = `ARRAY(
	WITH RECURSIVE up (id, parent_id, title, depth) AS (
		SELECT a.id, a.parent_id, a.title, 0 FROM page a WHERE a.id = p.parent_id
		UNION ALL
		SELECT a.id, a.parent_id, a.title, up.depth + 1 FROM page a JOIN up ON a.id = up.parent_id
	)
	SELECT title FROM up ORDER BY depth DESC)`

// Pages lists the pages out of the trash that carry a label and that the
// actor may view, by title, with how many there are; spaceKey narrows them.
func (s *Service) Pages(ctx context.Context, actor perm.Actor, raw, spaceKey string, limit, offset int) ([]LabeledPage, int, error) {
	name, err := Normalize(raw)
	if err != nil {
		return nil, 0, fieldError("labelName", err)
	}
	args := []any{actor.UserID, name}
	where := `
		FROM page_label l
		JOIN page p ON p.org_id = l.org_id AND p.id = l.page_id
		JOIN space s ON s.id = p.space_id
		LEFT JOIN app_user u ON u.id = p.updated_by
		WHERE l.name = $2 AND p.trashed_at IS NULL AND ` + perm.ViewablePage("p", 1)
	out := []LabeledPage{}
	var total int
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if spaceKey = strings.TrimSpace(spaceKey); spaceKey != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
			if err != nil {
				return err
			}
			args = append(args, sp.ID)
			where += ` AND p.space_id = $3`
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) `+where, args...).Scan(&total); err != nil {
			return fmt.Errorf("count the pages: %w", err)
		}
		n := len(args)
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, `+pathOf+`,
			       ARRAY(SELECT o.name FROM page_label o WHERE o.org_id = p.org_id AND o.page_id = p.id ORDER BY o.name),
			       p.version = 0, COALESCE(u.name, ''), p.updated_at
			`+where+`
			ORDER BY lower(p.title), p.id
			LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2), append(args, limit, offset)...)
		if err != nil {
			return fmt.Errorf("list the pages: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LabeledPage])
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if out[i].Path == nil {
			out[i].Path = []string{}
		}
		if out[i].Labels == nil {
			out[i].Labels = []string{}
		}
	}
	return out, total, nil
}

// PagesCopied puts each copied page's labels on its copy. The copier is the
// transaction's actor, whom the policies hold to editing the copy.
func (s *Service) PagesCopied(ctx context.Context, tx db.DBTX, copies map[uuid.UUID]uuid.UUID) error {
	olds, news := make([]uuid.UUID, 0, len(copies)), make([]uuid.UUID, 0, len(copies))
	for from, to := range copies {
		olds, news = append(olds, from), append(news, to)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_label (org_id, page_id, name, created_by)
		SELECT l.org_id, m.new_id, l.name, current_actor_id()
		FROM page_label l JOIN unnest($1::uuid[], $2::uuid[]) AS m (old_id, new_id) ON l.page_id = m.old_id`, olds, news); err != nil {
		return fmt.Errorf("copy the labels: %w", err)
	}
	return nil
}
