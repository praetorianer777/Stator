package page

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Contributor is somebody who published a version of the pages counted.
type Contributor struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
	// Edits counts the versions they published.
	Edits int `json:"edits"`
	// LastEditedAt is when they last published one.
	LastEditedAt time.Time `json:"lastEditedAt"`
}

// Contributors is who published the pages a contributors block counts, those
// with the most versions first; truncated says more did than it names.
type Contributors struct {
	Contributors []Contributor `json:"contributors"`
	Truncated    bool          `json:"truncated"`
}

// ContributorsQuery is what a contributors block counts: the page alone or
// with the pages below it, and how many people it names.
type ContributorsQuery struct {
	Scope string
	Limit int
}

// Check refuses what the document allowlist would refuse in a block.
func (q ContributorsQuery) Check() error {
	if !slices.Contains(document.ContributorScopes, q.Scope) {
		return &FieldError{Field: "scope", Message: "Count this page alone, or this page and the pages below it."}
	}
	if q.Limit < 1 || q.Limit > document.MaxContributors {
		return &FieldError{Field: "limit", Message: fmt.Sprintf("Name 1 to %d people.", document.MaxContributors)}
	}
	return nil
}

// Contributors names the people who published versions of a page, or of it
// and the pages below it the actor may view, as the history would name them.
func (s *Service) Contributors(ctx context.Context, actor perm.Actor, id uuid.UUID, q ContributorsQuery) (*Contributors, error) {
	if err := q.Check(); err != nil {
		return nil, err
	}
	out := &Contributors{Contributors: []Contributor{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		// An archived page counts what was archived with it; any other leaves
		// archived pages out, as the tree and a child pages block do.
		var archived *bool
		if err := tx.QueryRow(ctx, `SELECT (SELECT p.archived_at IS NOT NULL FROM page p WHERE p.id = $1 AND`+live+` AND `+perm.ViewablePage("p", 2)+`)`,
			id, actor.UserID).Scan(&archived); err != nil {
			return err
		}
		if archived == nil {
			return ErrNotFound
		}
		// A page the actor may not view hides the pages below it too, as in
		// the tree; somebody whose account is gone is no longer named.
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE counted (id) AS (
				SELECT $1::uuid
				UNION ALL
				SELECT p.id FROM page p JOIN counted c ON p.parent_id = c.id
				WHERE $3 AND`+live+` AND (p.archived_at IS NULL OR $4) AND `+perm.ViewablePage("p", 2)+`
			)
			SELECT u.id, u.name, COALESCE(u.avatar_url, ''), count(*)::int, max(v.created_at)
			FROM page_version v JOIN counted c ON c.id = v.page_id JOIN app_user u ON u.id = v.created_by
			GROUP BY u.id, u.name, u.avatar_url
			ORDER BY count(*) DESC, max(v.created_at) DESC, lower(u.name), u.id
			LIMIT $5`, id, actor.UserID, q.Scope == document.ContributorsTree, *archived, q.Limit+1)
		if err != nil {
			return fmt.Errorf("count the contributors: %w", err)
		}
		out.Contributors, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Contributor, error) {
			var c Contributor
			err := row.Scan(&c.ID, &c.Name, &c.AvatarURL, &c.Edits, &c.LastEditedAt)
			return c, err
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if out.Contributors == nil {
		out.Contributors = []Contributor{}
	}
	if len(out.Contributors) > q.Limit {
		out.Contributors, out.Truncated = out.Contributors[:q.Limit], true
	}
	return out, nil
}
