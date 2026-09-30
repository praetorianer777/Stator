// Package events is the transactional outbox, as in Armature: an event is
// committed with the change that caused it, and the worker drains the table.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// Topics are dotted names: what happened, to what.
const (
	TopicPagePublished  = "page.published"
	TopicCommentCreated = "comment.created"
	TopicCommentEdited  = "comment.edited"
	TopicThreadResolved = "thread.resolved"
	TopicThreadReopened = "thread.reopened"
)

// Topics lists every topic the product emits.
var Topics = []string{TopicPagePublished, TopicCommentCreated, TopicCommentEdited, TopicThreadResolved, TopicThreadReopened}

// Event is one committed domain event.
type Event struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Topic     string
	Payload   json.RawMessage
	CreatedAt time.Time
	// Trace is the W3C traceparent of the request that emitted it, empty when
	// none was sampled.
	Trace string
}

// PagePublished is a new version of a page, by any path but a copy.
type PagePublished struct {
	PageID  uuid.UUID `json:"pageId"`
	Version int       `json:"version"`
	ActorID uuid.UUID `json:"actorId"`
	// NotifyWatchers is the flag the publish carried; only publish and
	// restore can set it.
	NotifyWatchers bool `json:"notifyWatchers"`
	// First is the page's first publish, which its watchers hear as created.
	First bool `json:"first"`
	// Mentioned are the people mentioned in this version and not the one
	// before.
	Mentioned []uuid.UUID `json:"mentioned"`
}

// CommentCreated is a new thread or a reply.
type CommentCreated struct {
	CommentID uuid.UUID `json:"commentId"`
	ThreadID  uuid.UUID `json:"threadId"`
	PageID    uuid.UUID `json:"pageId"`
	ActorID   uuid.UUID `json:"actorId"`
	// Reply is a comment at the end of a thread somebody else may have begun.
	Reply bool `json:"reply"`
	// Mentioned are the people the comment names.
	Mentioned []uuid.UUID `json:"mentioned"`
}

// ThreadResolved is an inline thread resolved, or reopened, by its topic.
type ThreadResolved struct {
	ThreadID uuid.UUID `json:"threadId"`
	PageID   uuid.UUID `json:"pageId"`
	ActorID  uuid.UUID `json:"actorId"`
}

// Emit writes an event in the caller's transaction, the one that makes the
// change. The payload's actorId must be the person the transaction acts for.
func Emit(ctx context.Context, tx db.DBTX, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", topic, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
		VALUES (current_org_id(), $1, $2, NULLIF($3, ''))`,
		topic, body, traceParent(ctx)); err != nil {
		return fmt.Errorf("emit %s: %w", topic, err)
	}
	return nil
}

func traceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier["traceparent"]
}

// continueTrace puts the emitting request's trace on ctx, so the work done
// for the event joins it.
func continueTrace(ctx context.Context, e Event) context.Context {
	if e.Trace == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": e.Trace})
}
