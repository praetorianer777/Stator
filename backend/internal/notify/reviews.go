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

// planReviewSubmitted tells the people a review names, and the members of
// the groups it names, except its author. A review decided or withdrawn
// before the worker came to it tells nobody; whether each may view the page
// is decided when their row is written.
func planReviewSubmitted(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.ReviewSubmitted
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		author  *uuid.UUID
		comment string
	)
	err := tx.QueryRow(ctx, `
		SELECT author_id, comment FROM page_review
		WHERE id = $1 AND page_id = $2 AND state = 'pending'`,
		in.ReviewID, in.PageID).Scan(&author, &comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if author == nil || *author != in.ActorID {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT s.user_id FROM page_review_subject s
		WHERE s.review_id = $1 AND s.user_id IS NOT NULL
		UNION
		SELECT gm.user_id FROM page_review_subject s JOIN group_member gm ON gm.group_id = s.group_id
		WHERE s.review_id = $1
		ORDER BY 1`, in.ReviewID)
	if err != nil {
		return nil, err
	}
	people, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	plan := &Plan{Actor: in.ActorID, Subject: Subject{PageID: in.PageID, Excerpt: Excerpt(comment)}}
	for _, id := range people {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindReview})
	}
	return plan, nil
}

// planReviewDecided tells a review's author what became of it, with what the
// reviewer or the administrator said.
func planReviewDecided(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.ReviewDecided
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		author *uuid.UUID
		said   string
	)
	err := tx.QueryRow(ctx, `
		SELECT r.author_id, CASE WHEN $3 = 'skipped' THEN COALESCE(r.skip_reason, '')
		                         ELSE COALESCE((SELECT d.comment FROM page_review_decision d
		                                        WHERE d.org_id = r.org_id AND d.review_id = r.id AND d.reviewer_id = $4), '') END
		FROM page_review r WHERE r.id = $1 AND r.page_id = $2`,
		in.ReviewID, in.PageID, in.Outcome, in.ActorID).Scan(&author, &said)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if author == nil {
		return nil, nil
	}
	return &Plan{
		Actor:   in.ActorID,
		Subject: Subject{PageID: in.PageID, Version: in.Version, Excerpt: Excerpt(said)},
		Tells:   []Tell{{UserID: *author, Kind: KindReviewed}},
	}, nil
}
