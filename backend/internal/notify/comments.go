package notify

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/events"
)

// planCommentCreated tells the people the comment names, everybody who
// wrote in the thread when it is a reply, and the page's watchers. A comment
// deleted before the worker came to it tells nobody.
func planCommentCreated(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.CommentCreated
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var body []byte
	err := tx.QueryRow(ctx, `
		SELECT body FROM comment WHERE id = $1 AND org_id = current_org_id() AND deleted_at IS NULL`,
		in.CommentID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var excerpt string
	if root, err := document.ParseComment(body); err == nil {
		excerpt = Excerpt(document.PlainText(root))
	}
	thread, comment := in.ThreadID, in.CommentID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{PageID: in.PageID, ThreadID: &thread, CommentID: &comment, Excerpt: excerpt}}
	for _, id := range in.Mentioned {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindMentioned})
	}
	if in.Reply {
		writers, err := people(ctx, tx, `
			SELECT DISTINCT author_id FROM comment
			WHERE thread_id = $1 AND org_id = current_org_id() AND author_id IS NOT NULL`, in.ThreadID)
		if err != nil {
			return nil, err
		}
		for _, id := range writers {
			plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindReplied})
		}
	}
	watchers, err := people(ctx, tx, `SELECT user_id FROM page_watch_coverage($1, false)`, in.PageID)
	if err != nil {
		return nil, err
	}
	for _, id := range watchers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindCommented})
	}
	return plan, nil
}

// planThreadResolved tells everybody who wrote in an inline thread that it
// was resolved or reopened, quoting its passage. A thread wholly deleted
// before the worker came to it tells nobody.
func planThreadResolved(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.ThreadResolved
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var quote string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(t.quote, '') FROM comment_thread t
		WHERE t.id = $1 AND t.org_id = current_org_id()
		  AND EXISTS (SELECT 1 FROM comment c WHERE c.org_id = t.org_id AND c.thread_id = t.id AND c.deleted_at IS NULL)`,
		in.ThreadID).Scan(&quote)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	thread := in.ThreadID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{PageID: in.PageID, ThreadID: &thread, Excerpt: Excerpt(quote)}}
	writers, err := people(ctx, tx, `
		SELECT DISTINCT author_id FROM comment
		WHERE thread_id = $1 AND org_id = current_org_id() AND author_id IS NOT NULL`, in.ThreadID)
	if err != nil {
		return nil, err
	}
	for _, id := range writers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindResolved})
	}
	return plan, nil
}

func people(ctx context.Context, tx db.DBTX, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
