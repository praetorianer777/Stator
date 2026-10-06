package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/mail"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Subject is what one event tells people about: a page, and a thread, a
// comment or a version of it.
type Subject struct {
	PageID    uuid.UUID
	ThreadID  *uuid.UUID
	CommentID *uuid.UUID
	Version   *int
	Excerpt   string
}

// Tell is one person to tell, and why.
type Tell struct {
	UserID uuid.UUID
	Kind   Kind
	// Excerpt, when set, is shown to this person instead of the subject's,
	// as the block that mentions them in a page.
	Excerpt string
}

// Plan is who hears about one event.
type Plan struct {
	Actor   uuid.UUID
	Subject Subject
	Tells   []Tell
}

// Planner reads what an event is about and who could hear of it, in the
// event's organization, as the worker. A nil plan tells nobody.
type Planner func(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error)

// Pick keeps one kind per person, the first of Kinds that applies, and
// leaves out the actor, who is never told about their own act.
func Pick(actor uuid.UUID, candidates []Tell) []Tell {
	best := map[uuid.UUID]Tell{}
	var order []uuid.UUID
	for _, c := range candidates {
		if c.UserID == actor || c.UserID == uuid.Nil {
			continue
		}
		rank := slices.Index(Kinds, c.Kind)
		if rank < 0 {
			continue
		}
		if prev, seen := best[c.UserID]; !seen {
			best[c.UserID] = c
			order = append(order, c.UserID)
		} else if rank < slices.Index(Kinds, prev.Kind) {
			best[c.UserID] = c
		}
	}
	out := make([]Tell, len(order))
	for i, id := range order {
		out[i] = best[id]
	}
	return out
}

// FanOut runs in the worker: each event becomes a row per person told, and a
// mail for those who want one, at once or in their digest.
type FanOut struct {
	db       *db.Cluster
	mailer   mail.Mailer
	appURL   string
	log      *slog.Logger
	planners map[string]Planner
}

// NewFanOut builds the handler. A nil mailer writes the rows alone.
func NewFanOut(cluster *db.Cluster, mailer mail.Mailer, appURL string, log *slog.Logger) *FanOut {
	f := &FanOut{db: cluster, mailer: mailer, appURL: strings.TrimRight(appURL, "/"), log: log, planners: map[string]Planner{}}
	f.planners[events.TopicPagePublished] = planPublished
	f.planners[events.TopicPageAmended] = planAmended
	f.planners[events.TopicCommentCreated] = planCommentCreated
	f.planners[events.TopicCommentEdited] = planCommentEdited
	f.planners[events.TopicThreadResolved] = planThreadResolved
	f.planners[events.TopicThreadReopened] = planThreadResolved
	f.planners[events.TopicVerificationLapsed] = planVerificationLapsed
	f.planners[events.TopicPageShared] = planShared
	f.planners[events.TopicTaskDue] = planTaskDue
	f.planners[events.TopicScheduleFailed] = planScheduleFailed
	return f
}

// Plan adds or replaces who hears about a topic.
func (f *FanOut) Plan(topic string, planner Planner) *FanOut {
	f.planners[topic] = planner
	return f
}

// Handle tells everybody an event concerns. Run twice with the same event, it
// finds every row already written and sends nothing again.
func (f *FanOut) Handle(ctx context.Context, e events.Event) error {
	planner, ok := f.planners[e.Topic]
	if !ok || e.OrgID == uuid.Nil {
		return nil
	}
	org := tenant.WithOrg(db.PinPrimary(ctx), tenant.Org{ID: e.OrgID})
	var plan *Plan
	err := f.db.ReadAdmin(org, func(ctx context.Context, tx db.DBTX) error {
		var err error
		plan, err = planner(ctx, tx, e)
		return err
	})
	if err != nil || plan == nil {
		return err
	}
	for _, t := range Pick(plan.Actor, plan.Tells) {
		if err := f.deliver(db.WithUser(org, t.UserID), e.ID, plan, t); err != nil {
			return fmt.Errorf("tell %s: %w", t.UserID, err)
		}
	}
	return nil
}

// planPublished tells the newly mentioned, and the watchers when the
// publish asked for it: of the page, or of the places above a new page.
func planPublished(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.PagePublished
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		comment string
		body    []byte
	)
	err := tx.QueryRow(ctx, `SELECT comment, body FROM page_version WHERE page_id = $1 AND number = $2 AND org_id = current_org_id()`,
		in.PageID, in.Version).Scan(&comment, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	version := in.Version
	plan := &Plan{Actor: in.ActorID, Subject: Subject{PageID: in.PageID, Version: &version, Excerpt: Excerpt(comment)}}
	if err := tellAssignedAndMentioned(ctx, tx, plan, in.PageID, in.Version, nil, body, in.Mentioned); err != nil {
		return nil, err
	}
	if !in.NotifyWatchers {
		return plan, nil
	}
	kind := KindPublished
	if in.First {
		kind = KindCreated
	}
	rows, err := tx.Query(ctx, `SELECT user_id FROM page_watch_coverage($1, $2)`, in.PageID, in.First)
	if err != nil {
		return nil, err
	}
	watchers, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range watchers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: kind})
	}
	return plan, nil
}

// planAmended tells whom a save into a live page's open version newly
// mentioned or assigned, and nobody else: the version was announced when it began.
func planAmended(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.PageAmended
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var body []byte
	err := tx.QueryRow(ctx, `SELECT body FROM page_version WHERE page_id = $1 AND number = $2 AND org_id = current_org_id()`,
		in.PageID, in.Version).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	version := in.Version
	plan := &Plan{Actor: in.ActorID, Subject: Subject{PageID: in.PageID, Version: &version}}
	if err := tellAssignedAndMentioned(ctx, tx, plan, in.PageID, in.Version, &in.At, body, in.Mentioned); err != nil {
		return nil, err
	}
	return plan, nil
}

// tellAssignedAndMentioned adds the assignees of the version's open tasks,
// only those assigned at a given moment when one is named, and the mentioned.
func tellAssignedAndMentioned(ctx context.Context, tx db.DBTX, plan *Plan, pageID uuid.UUID, version int, at *time.Time, body []byte, mentioned []uuid.UUID) error {
	blocks := map[uuid.UUID]string{}
	for _, m := range document.MentionsIn(body) {
		blocks[m.ID] = m.Block
	}
	// The rows say who this version assigned, so a later version that
	// assigned the task to somebody else, or saw it done, tells nobody here.
	rows, err := tx.Query(ctx, `
		SELECT assignee_id, summary FROM page_task
		WHERE org_id = current_org_id() AND page_id = $1 AND assigned_version = $2 AND NOT done AND assignee_id IS NOT NULL
		  AND ($3::timestamptz IS NULL OR assigned_at = $3)
		ORDER BY position`, pageID, version, at)
	if err != nil {
		return err
	}
	var (
		assignee uuid.UUID
		summary  string
	)
	if _, err := pgx.ForEachRow(rows, []any{&assignee, &summary}, func() error {
		plan.Tells = append(plan.Tells, Tell{UserID: assignee, Kind: KindAssigned, Excerpt: Excerpt(summary)})
		return nil
	}); err != nil {
		return err
	}
	for _, id := range mentioned {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindMentioned, Excerpt: Excerpt(blocks[id])})
	}
	return nil
}

// planVerificationLapsed tells the page's owner, or whoever verified it when
// it has no owner who may still view it, so a lapse reaches somebody who can
// act. A verification renewed since tells nobody.
func planVerificationLapsed(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.VerificationLapsed
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		owner, verifier *uuid.UUID
		ownerViews      bool
		version         int
	)
	err := tx.QueryRow(ctx, `
		SELECT o.user_id, COALESCE(perm_page_viewable(v.page_id, o.user_id), false), v.verified_by, v.version
		FROM page_verification v
		LEFT JOIN page_owner o ON o.org_id = v.org_id AND o.page_id = v.page_id
		WHERE v.org_id = current_org_id() AND v.page_id = $1 AND v.expires_at <= now() AND v.expires_at = $2`,
		in.PageID, in.ExpiresAt).Scan(&owner, &ownerViews, &verifier, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan := &Plan{Subject: Subject{PageID: in.PageID, Version: &version}}
	switch {
	case owner != nil && ownerViews:
		plan.Tells = []Tell{{UserID: *owner, Kind: KindExpired}}
	case verifier != nil:
		plan.Tells = []Tell{{UserID: *verifier, Kind: KindExpired}}
	}
	return plan, nil
}

// planTaskDue reminds the assignee of a task whose day came. A task done, given
// another day or another assignee since, or on an archived page, tells nobody.
func planTaskDue(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.TaskDue
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var (
		summary string
		version int
	)
	err := tx.QueryRow(ctx, `
		SELECT t.summary, p.version FROM page_task t
		JOIN page p ON p.org_id = t.org_id AND p.id = t.page_id
		JOIN space s ON s.id = p.space_id
		WHERE t.org_id = current_org_id() AND t.page_id = $1 AND t.task_id = $2 AND t.assignee_id = $3
		  AND t.due_on = $4::date AND NOT t.done AND p.archived_at IS NULL AND s.archived_at IS NULL`,
		in.PageID, in.TaskID, in.AssigneeID, in.DueOn).Scan(&summary, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Plan{
		Subject: Subject{PageID: in.PageID, Version: &version, Excerpt: Excerpt(summary)},
		Tells:   []Tell{{UserID: in.AssigneeID, Kind: KindDue}},
	}, nil
}

// planScheduleFailed tells the author of a scheduled publish that was refused.
// A schedule set again, or called off, since tells nobody.
func planScheduleFailed(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in events.ScheduleFailed
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var version int
	err := tx.QueryRow(ctx, `
		SELECT p.version FROM page_schedule sc JOIN page p ON p.org_id = sc.org_id AND p.id = sc.page_id
		WHERE sc.org_id = current_org_id() AND sc.page_id = $1 AND sc.user_id = $2
		  AND sc.failed_at IS NOT NULL AND sc.publish_at = $3`,
		in.PageID, in.AuthorID, in.PublishAt).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Plan{
		Subject: Subject{PageID: in.PageID, Version: &version},
		Tells:   []Tell{{UserID: in.AuthorID, Kind: KindFailed}},
	}, nil
}

// deliver writes one row acting for its recipient, so the database refuses
// a row about a page they may not view, and mails or queues it as they prefer.
func (f *FanOut) deliver(ctx context.Context, eventID uuid.UUID, plan *Plan, t Tell) error {
	subject := plan.Subject
	if t.Excerpt != "" {
		subject.Excerpt = t.Excerpt
	}
	var out *mail.Mail
	_, err := f.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var about mailed
		err := tx.QueryRow(ctx, `
			SELECT p.title, s.key FROM page p JOIN space s ON s.id = p.space_id
			WHERE p.id = $1 AND p.trashed_at IS NULL AND perm_page_viewable(p.id, $2::uuid)`,
			subject.PageID, t.UserID).Scan(&about.title, &about.spaceKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		prefs, err := readPreferences(ctx, tx, t.UserID)
		if err != nil {
			return err
		}
		if !prefs.InApp.On(t.Kind) {
			return nil
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO notification (org_id, user_id, event_id, kind, actor_id, page_id, thread_id, comment_id, version, excerpt)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (event_id, user_id) DO NOTHING
			RETURNING id`,
			t.UserID, eventID, t.Kind, nullable(plan.Actor), subject.PageID, subject.ThreadID,
			subject.CommentID, subject.Version, subject.Excerpt).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if f.mailer == nil || !prefs.Email.On(t.Kind) {
			return nil
		}
		if prefs.Digest != DigestOff {
			_, err := tx.Exec(ctx, `INSERT INTO notification_digest (org_id, user_id, notification_id) VALUES (current_org_id(), $1, $2)`, t.UserID, id)
			return err
		}
		var to string
		if err := tx.QueryRow(ctx, `
			SELECT u.email, COALESCE((SELECT a.name FROM app_user a WHERE a.id = $2), '')
			FROM app_user u WHERE u.id = $1`, t.UserID, plan.Actor).Scan(&to, &about.actor); err != nil {
			return err
		}
		about.kind, about.subject = t.Kind, subject
		m := about.single(to, f.appURL)
		out = &m
		return nil
	})
	if err != nil || out == nil {
		return err
	}
	// The row is committed, so a failed send is logged rather than retried:
	// a second run would find the row and could not tell whether it went.
	if err := f.mailer.Send(ctx, *out); err != nil {
		f.log.Warn("a notification could not be mailed", "event", eventID, "user", t.UserID, "error", err)
	}
	return nil
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
