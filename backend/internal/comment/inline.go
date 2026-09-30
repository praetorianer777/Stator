package comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// StartInline begins a thread on a passage, marked in the page's body by
// anybody who may comment; nothing else in the body may change.
func (s *Service) StartInline(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in InlineThreadInput) (*Thread, db.LSN, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, 0, err
	}
	if in.ThreadID == uuid.Nil {
		return nil, 0, &FieldError{Field: "threadId", Message: "Send the id the passage's mark names."}
	}
	quote, _, err := passage(in.PageBody, in.ThreadID)
	if err != nil {
		return nil, 0, err
	}
	var out *Thread
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := loadPage(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if err := p.canComment(); err != nil {
			return err
		}
		var stored []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM page WHERE id = $1 FOR UPDATE`, pageID).Scan(&stored); err != nil {
			return fmt.Errorf("read the page: %w", err)
		}
		// The id is claimed before the bodies are compared, so a taken one
		// is refused as such rather than as a changed page.
		if _, err := tx.Exec(ctx, `
			INSERT INTO comment_thread (org_id, id, page_id, kind, created_by, quote)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)`, in.ThreadID, pageID, KindInline, actor.UserID, quote); err != nil {
			if isUnique(err) {
				return ErrThreadTaken
			}
			return fmt.Errorf("start the thread: %w", err)
		}
		if _, err := NewAnchor(stored, in.PageBody, in.ThreadID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET body = $2 WHERE id = $1`, pageID, []byte(in.PageBody)); err != nil {
			return fmt.Errorf("mark the passage: %w", err)
		}
		if err := add(ctx, tx, actor, in.ThreadID, in.ThreadID, pageID, body, false); err != nil {
			return err
		}
		out, err = thread(ctx, tx, actor, p, in.ThreadID)
		return err
	})
	return out, lsn, err
}

// SetResolved resolves or reopens the inline thread a comment belongs to.
// Either twice is no change, and tells nobody.
func (s *Service) SetResolved(ctx context.Context, actor perm.Actor, commentID uuid.UUID, resolved bool) (*Thread, db.LSN, error) {
	var out *Thread
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, p, err := locate(ctx, tx, actor, commentID, false)
		if err != nil {
			return err
		}
		current, err := thread(ctx, tx, actor, p, h.thread)
		if err != nil {
			return err
		}
		if current.Kind != KindInline {
			return ErrNotInline
		}
		if err := p.canComment(); err != nil {
			return err
		}
		var was bool
		if err := tx.QueryRow(ctx, `SELECT resolved_at IS NOT NULL FROM comment_thread WHERE id = $1 FOR UPDATE`, h.thread).Scan(&was); err != nil {
			return fmt.Errorf("read the thread: %w", err)
		}
		if was != resolved {
			topic := events.TopicThreadReopened
			sql := `UPDATE comment_thread SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`
			args := []any{h.thread}
			if resolved {
				topic = events.TopicThreadResolved
				sql = `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`
				args = append(args, actor.UserID)
			}
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				return fmt.Errorf("resolve the thread: %w", err)
			}
			if err := events.Emit(ctx, tx, topic, events.ThreadResolved{ThreadID: h.thread, PageID: h.page, ActorID: actor.UserID}); err != nil {
				return err
			}
		}
		out, err = thread(ctx, tx, actor, p, h.thread)
		return err
	})
	return out, lsn, err
}

// SettleAnchors answers the body to publish with its threads settled, and
// detaches for good those whose passage is gone.
func SettleAnchors(ctx context.Context, tx db.DBTX, pageID uuid.UUID, body json.RawMessage) (json.RawMessage, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.quote FROM comment_thread t
		WHERE t.page_id = $1 AND t.kind = 'inline' AND t.detached_at IS NULL
		  AND EXISTS (SELECT 1 FROM comment c WHERE c.org_id = t.org_id AND c.thread_id = t.id AND c.deleted_at IS NULL)`, pageID)
	if err != nil {
		return nil, fmt.Errorf("read the inline threads: %w", err)
	}
	live := map[uuid.UUID]string{}
	var (
		id    uuid.UUID
		quote string
	)
	if _, err := pgx.ForEachRow(rows, []any{&id, &quote}, func() error {
		live[id] = quote
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read the inline threads: %w", err)
	}
	settled, err := Settle(body, live)
	if err != nil {
		return nil, err
	}
	if len(settled.Detached) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE comment_thread SET detached_at = now() WHERE id = ANY($1)`, settled.Detached); err != nil {
			return nil, fmt.Errorf("detach the threads: %w", err)
		}
	}
	return settled.Body, nil
}
