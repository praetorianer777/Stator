package notify

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
)

// planShared tells the people a share names, with its note. Whether each may
// still view the page is decided again when their row is written.
func planShared(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.PageShared
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		pageID, sharer uuid.UUID
		message        string
	)
	err := tx.QueryRow(ctx, `SELECT page_id, sharer_id, message FROM page_share WHERE id = $1 AND org_id = current_org_id()`,
		in.ShareID).Scan(&pageID, &sharer, &message)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The row, not the event, says who shared what.
	if sharer != in.ActorID || pageID != in.PageID {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT user_id FROM page_share_recipient WHERE share_id = $1 AND org_id = current_org_id() ORDER BY user_id`, in.ShareID)
	if err != nil {
		return nil, err
	}
	people, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	plan := &Plan{Actor: sharer, Subject: Subject{PageID: pageID, Excerpt: Excerpt(message)}}
	for _, id := range people {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindShared})
	}
	return plan, nil
}
