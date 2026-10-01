package perm

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// MentionablePeople are the members with use matching q as People does, each
// saying whether they may view the page once published; the caller may view it.
func MentionablePeople(ctx context.Context, tx db.DBTX, pageID uuid.UUID, q string, limit int) ([]Mentionable, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.name, u.email::text, perm_page_viewable_published($1, u.id)
		FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = current_org_id() AND perm_global_holds(u.id, 'use')
		  AND (u.name ILIKE $2 OR u.name ILIKE '% ' || $2 OR u.email::text ILIKE $2)
		ORDER BY lower(u.name), u.email LIMIT $3`, pageID, LikePrefix(q), PickerLimit(limit))
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Mentionable, error) {
		var m Mentionable
		err := row.Scan(&m.ID, &m.Name, &m.Email, &m.CanView)
		return m, err
	})
	return nonNil(out), err
}

// MentionsToTell narrows the people a page or comment names to those a
// mention tells: members who may view the page now, in the order given.
func MentionsToTell(ctx context.Context, tx db.DBTX, pageID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id FROM unnest($2::uuid[]) WITH ORDINALITY AS m (id, n)
		WHERE perm_is_member(m.id) AND perm_page_viewable($1, m.id)
		ORDER BY m.n`, pageID, ids)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return nonNil(out), err
}
