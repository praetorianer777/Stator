package page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// ErrListLimit refuses a list of pages longer than document.MaxListedPages, or empty.
var ErrListLimit = fmt.Errorf("list 1 to %d pages", document.MaxListedPages)

// UpdatedPage is a page as a recently updated list shows it: where it lives,
// when it was last published and by whom.
type UpdatedPage struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	SpaceKey    string    `json:"spaceKey"`
	SpaceName   string    `json:"spaceName"`
	PublishedAt time.Time `json:"publishedAt"`
	AuthorName  string    `json:"authorName"`
}

// RecentlyUpdated lists the pages published last that the actor may read, in
// one space or across the organization, leaving out folders, the trash and
// the archive. It names whoever published each page's current version.
func (s *Service) RecentlyUpdated(ctx context.Context, actor perm.Actor, spaceKey string, limit int) ([]UpdatedPage, error) {
	if limit < 1 || limit > document.MaxListedPages {
		return nil, ErrListLimit
	}
	out := []UpdatedPage{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		args := []any{actor.UserID, limit}
		within := ` AND s.archived_at IS NULL`
		if key := strings.TrimSpace(spaceKey); key != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
			if err != nil {
				return err
			}
			args = append(args, sp.ID)
			within = ` AND p.space_id = $3`
		}
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, p.published_at, COALESCE(u.name, '')
			FROM page p
			JOIN space s ON s.id = p.space_id
			LEFT JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id AND v.number = p.version
			LEFT JOIN app_user u ON u.id = v.created_by
			WHERE p.published_at IS NOT NULL AND`+live+` AND p.archived_at IS NULL AND p.kind <> 'folder'
			  AND `+perm.ViewablePage("p", 1)+within+`
			ORDER BY p.published_at DESC, p.id DESC
			LIMIT $2`, args...)
		if err != nil {
			return fmt.Errorf("list the pages updated lately: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[UpdatedPage])
		return err
	})
	if err == nil && out == nil {
		out = []UpdatedPage{}
	}
	return out, err
}
