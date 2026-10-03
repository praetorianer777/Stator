package label

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

var (
	// ErrListMatch and ErrListSort refuse a choice a list does not offer.
	ErrListMatch = errors.New("match all of the labels or any of them")
	ErrListSort  = errors.New("sort by title or by when the page was last updated")
)

// ListInput is what a content by label block asks for: pages carrying all or
// any of the labels, in one space or all, in an order, up to a number.
type ListInput struct {
	Labels   []string
	Match    string
	SpaceKey string
	Sort     string
	Limit    int
}

func checkList(in ListInput) ([]string, error) {
	if len(in.Labels) == 0 {
		return nil, fieldError("label", errors.New("name at least one label; the list shows the pages that carry them"))
	}
	if len(in.Labels) > document.MaxReportLabels {
		return nil, fieldError("label", ErrReportLabels)
	}
	var labels []string
	for _, raw := range in.Labels {
		name, err := Normalize(raw)
		if err != nil {
			return nil, fieldError("label", err)
		}
		if !slices.Contains(labels, name) {
			labels = append(labels, name)
		}
	}
	if !slices.Contains(document.ListMatches, in.Match) {
		return nil, fieldError("match", ErrListMatch)
	}
	if !slices.Contains(document.ListSorts, in.Sort) {
		return nil, fieldError("sort", ErrListSort)
	}
	if in.Limit < 1 || in.Limit > document.MaxListedPages {
		return nil, fieldError("limit", page.ErrListLimit)
	}
	return labels, nil
}

// Listed is the published pages out of the trash and the archive that carry
// the labels, all or any, and that the actor may read.
func (s *Service) Listed(ctx context.Context, actor perm.Actor, in ListInput) ([]LabeledPage, error) {
	labels, err := checkList(in)
	if err != nil {
		return nil, err
	}
	// Any label is at least one; all is as many as were asked for.
	need := 1
	if in.Match == document.MatchAll {
		need = len(labels)
	}
	order := `p.published_at DESC, p.id`
	if in.Sort == document.SortTitle {
		order = `lower(p.title), p.id`
	}
	out := []LabeledPage{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		args := []any{actor.UserID, labels, need, in.Limit}
		within := ` AND s.archived_at IS NULL`
		if key := strings.TrimSpace(in.SpaceKey); key != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
			if err != nil {
				return err
			}
			args = append(args, sp.ID)
			within = ` AND p.space_id = $5`
		}
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, `+pathOf+`,
			       ARRAY(SELECT o.name FROM page_label o WHERE o.org_id = p.org_id AND o.page_id = p.id ORDER BY o.name),
			       false, COALESCE(u.name, ''), p.published_at
			FROM page p
			JOIN space s ON s.id = p.space_id
			LEFT JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id AND v.number = p.version
			LEFT JOIN app_user u ON u.id = v.created_by
			WHERE p.published_at IS NOT NULL AND p.trashed_at IS NULL AND p.archived_at IS NULL
			  AND (SELECT count(*) FROM page_label l WHERE l.org_id = p.org_id AND l.page_id = p.id AND l.name = ANY($2)) >= $3
			  AND `+perm.ViewablePage("p", 1)+within+`
			ORDER BY `+order+`
			LIMIT $4`, args...)
		if err != nil {
			return fmt.Errorf("list the pages by label: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LabeledPage])
		return err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Path == nil {
			out[i].Path = []string{}
		}
	}
	return out, nil
}
