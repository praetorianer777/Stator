package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/review"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

var (
	// ErrReviewRequired refuses publishing a page held to a review any way
	// but through it.
	ErrReviewRequired = errors.New("this page is published through a review")
	// ErrReviewPending refuses a second review, or any version, while one waits.
	ErrReviewPending = errors.New("a review of this page is pending")
	// ErrNoReviewNeeded refuses submitting a page no rule holds.
	ErrNoReviewNeeded = errors.New("this page needs no review")
	// ErrLivePages refuses a rule over a live page, which leaves nothing to review.
	ErrLivePages = errors.New("a review rule cannot cover a live page")
	// ErrNotAsked refuses confirming a page that does not ask.
	ErrNotAsked = errors.New("this page does not ask its readers to confirm")
	// ErrAskUnpublished refuses asking readers of a page nobody may read yet.
	ErrAskUnpublished = errors.New("publish the page before you ask its readers to confirm reading it")
	// ErrReviewNotFound answers a review that is not the page's.
	ErrReviewNotFound = errors.New("that review was not found")
	// ErrReviewClosed refuses acting on a review decided or withdrawn.
	ErrReviewClosed = errors.New("that review is no longer pending")
	// ErrOwnReview refuses an author deciding on their own draft.
	ErrOwnReview = errors.New("nobody decides on their own draft")
	// ErrNotReviewer refuses a decision by somebody the review does not name.
	ErrNotReviewer = errors.New("only a reviewer the review names decides on it")
	// ErrNotAuthor refuses withdrawing somebody else's review.
	ErrNotAuthor = errors.New("only its author withdraws a review")
	// ErrNoRule answers removing a rule that is not there.
	ErrNoRule = errors.New("there is no review rule here")
	// ErrAuthorCannotPublish refuses publishing a review in the name of an
	// author who may no longer edit the page.
	ErrAuthorCannotPublish = errors.New("the author may no longer edit the page")
)

// Outcomes of a review the author hears of, besides the decisions.
const (
	outcomePublished = "published"
	outcomeSkipped   = "skipped"
)

// ClearToPublish refuses a version that would go around a review: a page
// held to one goes out through it, and nothing goes out while one waits.
func (p *Page) ClearToPublish() error {
	if p.Review.Rule != nil {
		return ErrReviewRequired
	}
	if p.Review.Pending != nil {
		return ErrReviewPending
	}
	return nil
}

const selectRules = `
SELECT r.id, s.key, r.page_id, COALESCE(p.title, ''), r.required, COALESCE(u.name, ''), r.updated_at
FROM review_rule r
JOIN space s ON s.id = review_rule_space(r.space_id, r.page_id)
LEFT JOIN page p ON p.id = r.page_id
LEFT JOIN app_user u ON u.id = r.updated_by`

// readRules reads the rules matching where, the space's first, with whom each names.
func readRules(ctx context.Context, tx db.DBTX, where string, args ...any) ([]review.Rule, error) {
	rows, err := tx.Query(ctx, selectRules+` WHERE `+where+` ORDER BY r.page_id IS NOT NULL, lower(COALESCE(p.title, '')), r.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the review rules: %w", err)
	}
	out := []review.Rule{}
	for rows.Next() {
		var (
			r     review.Rule
			page  *uuid.UUID
			title string
		)
		if err := rows.Scan(&r.ID, &r.SpaceKey, &page, &title, &r.Required, &r.UpdatedByName, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if page != nil {
			r.Page = &review.PageRef{ID: *page, Title: title}
		}
		r.Reviewers = []perm.Subject{}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	ids := make([]uuid.UUID, len(out))
	at := map[uuid.UUID]int{}
	for i, r := range out {
		ids[i], at[r.ID] = r.ID, i
	}
	rows, err = tx.Query(ctx, `SELECT g.rule_id, `+perm.SubjectColumns+` FROM review_rule_subject g`+perm.SubjectJoins+`
		WHERE g.rule_id = ANY($1) ORDER BY `+perm.SubjectOrder, ids)
	if err != nil {
		return nil, fmt.Errorf("read the reviewers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rule uuid.UUID
			s    perm.Subject
		)
		if err := rows.Scan(&rule, &s.Type, &s.ID, &s.Name); err != nil {
			return nil, err
		}
		out[at[rule]].Reviewers = append(out[at[rule]].Reviewers, s)
	}
	return out, rows.Err()
}

// ruleOf is the rule a page is held to: its branch's nearest, or its
// space's; a post only ever the space's, and a folder none.
func ruleOf(ctx context.Context, tx db.DBTX, p *Page) (*review.Rule, error) {
	if p.Kind == KindFolder {
		return nil, nil
	}
	chain := []uuid.UUID{}
	if p.Kind != KindPost {
		// Read past restrictions: a rule on a page above that the reader may
		// not see holds this one all the same.
		rows, err := tx.Query(ctx, `SELECT id FROM perm_page_chain($1)`, p.ID)
		if err != nil {
			return nil, err
		}
		if chain, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return nil, err
		}
	}
	rules, err := readRules(ctx, tx, `(r.space_id = $1 OR r.page_id = ANY($2))`, p.SpaceID, chain)
	if err != nil {
		return nil, err
	}
	var whole *review.Rule
	branches := map[uuid.UUID]*review.Rule{}
	for i := range rules {
		if rules[i].Page == nil {
			whole = &rules[i]
		} else {
			branches[rules[i].Page.ID] = &rules[i]
		}
	}
	return review.Nearest(chain, branches, whole), nil
}

const selectReviews = `
SELECT r.id, r.page_id, r.state, r.author_id, COALESCE(a.name, ''), r.base_version, r.title, r.comment, r.required,
       page_review_approvals(r.id), r.submitted_at, r.finished_at, r.version, COALESCE(r.skip_reason, ''), COALESCE(sb.name, ''),
       page_review_decidable(r.id, $1::uuid)
FROM page_review r
LEFT JOIN app_user a ON a.id = r.author_id
LEFT JOIN app_user sb ON sb.id = r.skipped_by`

// reviewsOf reads the reviews matching where, $1 being the actor, with
// whom each names, what each reviewer said and what the actor may do.
func reviewsOf(ctx context.Context, tx db.DBTX, actor perm.Actor, admin bool, where string, args ...any) ([]review.Review, error) {
	rows, err := tx.Query(ctx, selectReviews+` WHERE `+where+` ORDER BY r.submitted_at DESC, r.id DESC`, append([]any{actor.UserID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("read the reviews: %w", err)
	}
	out := []review.Review{}
	for rows.Next() {
		var r review.Review
		if err := rows.Scan(&r.ID, &r.PageID, &r.State, &r.AuthorID, &r.AuthorName, &r.BaseVersion, &r.Title, &r.Comment, &r.Required,
			&r.Approvals, &r.SubmittedAt, &r.FinishedAt, &r.Version, &r.SkipReason, &r.SkippedByName, &r.Can.Decide); err != nil {
			rows.Close()
			return nil, err
		}
		pending := r.State == review.StatePending
		r.Can.Withdraw = pending && r.AuthorID != nil && *r.AuthorID == actor.UserID
		r.Can.Skip = pending && admin
		r.Reviewers, r.Decisions = []perm.Subject{}, []review.DecisionEntry{}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	ids := make([]uuid.UUID, len(out))
	at := map[uuid.UUID]int{}
	for i, r := range out {
		ids[i], at[r.ID] = r.ID, i
	}
	rows, err = tx.Query(ctx, `SELECT g.review_id, `+perm.SubjectColumns+` FROM page_review_subject g`+perm.SubjectJoins+`
		WHERE g.review_id = ANY($1) ORDER BY `+perm.SubjectOrder, ids)
	if err != nil {
		return nil, fmt.Errorf("read the reviewers: %w", err)
	}
	for rows.Next() {
		var (
			id uuid.UUID
			s  perm.Subject
		)
		if err := rows.Scan(&id, &s.Type, &s.ID, &s.Name); err != nil {
			rows.Close()
			return nil, err
		}
		out[at[id]].Reviewers = append(out[at[id]].Reviewers, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `
		SELECT d.review_id, d.reviewer_id, COALESCE(u.name, ''), d.decision, d.comment, d.decided_at
		FROM page_review_decision d LEFT JOIN app_user u ON u.id = d.reviewer_id
		WHERE d.review_id = ANY($1) ORDER BY d.decided_at, d.id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the decisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			d  review.DecisionEntry
		)
		if err := rows.Scan(&id, &d.ReviewerID, &d.ReviewerName, &d.Decision, &d.Comment, &d.DecidedAt); err != nil {
			return nil, err
		}
		out[at[id]].Decisions = append(out[at[id]].Decisions, d)
	}
	return out, rows.Err()
}

// reviewOn is one review of a page, with its draft's body when asked.
func reviewOn(ctx context.Context, tx db.DBTX, actor perm.Actor, admin bool, pageID, id uuid.UUID, body bool) (*review.Review, error) {
	found, err := reviewsOf(ctx, tx, actor, admin, `r.page_id = $2 AND r.id = $3`, pageID, id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrReviewNotFound
	}
	r := found[0]
	if body {
		if err := tx.QueryRow(ctx, `SELECT body FROM page_review WHERE id = $1`, id).Scan(&r.Body); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

// reviewStatus is a page's standing with reviews as the actor sees it.
func reviewStatus(ctx context.Context, tx db.DBTX, actor perm.Actor, admin bool, p *Page) (review.Status, error) {
	var (
		out review.Status
		err error
	)
	if out.Rule, err = ruleOf(ctx, tx, p); err != nil {
		return out, err
	}
	pending, err := reviewsOf(ctx, tx, actor, admin, `r.page_id = $2 AND r.state = 'pending'`, p.ID)
	if err != nil {
		return out, err
	}
	if len(pending) > 0 {
		out.Pending = &pending[0]
	}
	return out, nil
}

// readAskOf is whether a page asks its readers to confirm, and whether the
// actor did since it asked.
func readAskOf(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) (review.ReadAsk, error) {
	var out review.ReadAsk
	err := tx.QueryRow(ctx, `
		SELECT a.since, (SELECT c.confirmed_at FROM page_read_confirmation c
		                 WHERE c.org_id = a.org_id AND c.page_id = a.page_id AND c.user_id = $2 AND c.version >= a.since)
		FROM page_read_ask a WHERE a.page_id = $1`, pageID, actor.UserID).Scan(&out.Since, &out.ConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return review.ReadAsk{}, nil
	}
	return out, err
}

// actAs makes the rest of the transaction act for somebody else, so what a
// review publishes is its author's own publish, held to their rights.
func actAs(ctx context.Context, tx db.DBTX, user uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, db.UserVar, user.String())
	return err
}

// skipping is an administrator publishing a review without its approvals.
type skipping struct {
	reason string
	by     uuid.UUID
}

// goOut publishes a pending review's draft as its author's next version,
// acting for them, and closes the review on it; the author's draft goes if it
// is still what they submitted. The actor is who completed it.
func goOut(ctx context.Context, tx db.DBTX, actor perm.Actor, p *Page, r *review.Review, skip *skipping) (*VersionEntry, error) {
	if r.AuthorID == nil {
		return nil, ErrAuthorCannotPublish
	}
	author := *r.AuthorID
	var (
		editable bool
		role     string
		body     json.RawMessage
		notify   bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT perm_page_editable(r.page_id, r.author_id),
		       COALESCE((SELECT m.org_role FROM org_member m WHERE m.org_id = current_org_id() AND m.user_id = r.author_id), ''),
		       r.body, r.notify_watchers
		FROM page_review r WHERE r.id = $1`, r.ID).Scan(&editable, &role, &body, &notify); err != nil {
		return nil, err
	}
	if !editable || role == "" {
		return nil, ErrAuthorCannotPublish
	}
	// Nothing publishes around a pending review, so this holds unless the
	// page was written past the database's back.
	if p.Version != r.BaseVersion {
		return nil, ErrPublishConflict
	}
	if err := actAs(ctx, tx, author); err != nil {
		return nil, err
	}
	entry, err := publish(ctx, tx, perm.Actor{UserID: author, Role: auth.OrgRole(role)}, p,
		release{title: r.Title, body: body, comment: r.Comment, notify: notify, reviewID: &r.ID})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM page_draft d USING page_review r
		WHERE d.page_id = $1 AND d.user_id = current_actor_id() AND r.id = $2 AND d.title = r.title AND d.body = r.body`,
		p.ID, r.ID); err != nil {
		return nil, fmt.Errorf("let the published draft go: %w", err)
	}
	if err := actAs(ctx, tx, actor.UserID); err != nil {
		return nil, err
	}
	state, reason, by := review.StatePublished, (*string)(nil), (*uuid.UUID)(nil)
	if skip != nil {
		state, reason, by = review.StateSkipped, &skip.reason, &skip.by
	}
	if _, err := tx.Exec(ctx, `
		UPDATE page_review SET state = $2, version = $3, finished_at = now(), skip_reason = $4, skipped_by = $5 WHERE id = $1`,
		r.ID, state, entry.Number, reason, by); err != nil {
		return nil, fmt.Errorf("close the review: %w", err)
	}
	return versionEntry(ctx, tx, p.ID, entry.Number)
}

// SubmitReview puts the caller's draft of a page held to a review to its
// reviewers, as a snapshot; the draft stays theirs to go on editing.
func (s *Service) SubmitReview(ctx context.Context, actor perm.Actor, id uuid.UUID, in review.SubmitInput) (*review.Review, db.LSN, error) {
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *review.Review
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = submitReview(ctx, tx, actor, id, comment, in.NotifyWatchers)
		return err
	})
	return out, lsn, err
}

// submitReview snapshots the actor's draft, or a page never published as it
// was made when there is no draft, as publishDraft would publish it.
func submitReview(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID, comment string, notify bool) (*review.Review, error) {
	p, sp, err := load(ctx, tx, actor, id, true)
	if err != nil {
		return nil, err
	}
	if err := p.must(perm.EditPages); err != nil {
		return nil, err
	}
	if p.Kind == KindFolder {
		return nil, ErrFolder
	}
	if p.Mode == ModeLive {
		return nil, ErrLivePage
	}
	rule := p.Review.Rule
	switch {
	case rule == nil:
		return nil, ErrNoReviewNeeded
	case p.Review.Pending != nil:
		return nil, ErrReviewPending
	}
	draft, err := draftOf(ctx, tx, actor, id, false)
	if err != nil {
		return nil, err
	}
	title, body := p.Title, p.Body
	switch {
	case draft != nil && draft.BaseVersion != p.Version:
		return nil, ErrPublishConflict
	case draft != nil:
		title, body = draft.Title, draft.Body
	case !p.Unpublished:
		return nil, ErrNoDraft
	}
	reviewID := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_review (org_id, id, page_id, author_id, base_version, title, body, comment, notify_watchers, required)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		reviewID, id, actor.UserID, p.Version, title, body, comment, notify, rule.Required); err != nil {
		return nil, fmt.Errorf("submit the draft: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO page_review_subject (org_id, review_id, subject_type, user_id, group_id)
		SELECT current_org_id(), $1, subject_type, user_id, group_id FROM review_rule_subject WHERE rule_id = $2`,
		reviewID, rule.ID); err != nil {
		return nil, fmt.Errorf("name the reviewers: %w", err)
	}
	if err := events.Emit(ctx, tx, events.TopicReviewSubmitted, events.ReviewSubmitted{
		ReviewID: reviewID, PageID: id, ActorID: actor.UserID}); err != nil {
		return nil, err
	}
	return reviewOn(ctx, tx, actor, sp.Can.Administer, id, reviewID, false)
}

// Reviews lists a page's reviews, the latest first, without their bodies.
func (s *Service) Reviews(ctx context.Context, actor perm.Actor, id uuid.UUID) ([]review.Review, error) {
	var out []review.Review
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		out, err = reviewsOf(ctx, tx, actor, sp.Can.Administer, `r.page_id = $2`, id)
		return err
	})
	return out, err
}

// GetReview is one review of a page with its draft's body.
func (s *Service) GetReview(ctx context.Context, actor perm.Actor, pageID, id uuid.UUID) (*review.Review, error) {
	var out *review.Review
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, sp, err := load(ctx, tx, actor, pageID, false)
		if err != nil {
			return err
		}
		out, err = reviewOn(ctx, tx, actor, sp.Can.Administer, pageID, id, true)
		return err
	})
	return out, err
}

// pendingReview loads a page for a change to one of its reviews, refusing a
// review that is not pending or a page archived meanwhile.
func pendingReview(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID, id uuid.UUID) (*Page, *space.Space, *review.Review, error) {
	p, sp, err := load(ctx, tx, actor, pageID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := reviewOn(ctx, tx, actor, sp.Can.Administer, pageID, id, false)
	if err != nil {
		return nil, nil, nil, err
	}
	if r.State != review.StatePending {
		return nil, nil, nil, ErrReviewClosed
	}
	if p.access.Archived != perm.NotArchived {
		return nil, nil, nil, p.Refusal(perm.EditPages)
	}
	return p, sp, r, nil
}

// DecideReview records a named reviewer's approval of a pending review, or
// their ask for changes, which ends it. The approval that completes the
// count publishes the draft as its author's next version, answered too.
func (s *Service) DecideReview(ctx context.Context, actor perm.Actor, pageID, id uuid.UUID, in review.DecisionInput) (*review.Review, *VersionEntry, db.LSN, error) {
	comment := strings.TrimSpace(in.Comment)
	switch {
	case in.Decision != review.DecisionApprove && in.Decision != review.DecisionChanges:
		return nil, nil, 0, &FieldError{Field: "decision", Message: "Approve the draft, or ask for changes."}
	case in.Decision == review.DecisionChanges && comment == "":
		return nil, nil, 0, &FieldError{Field: "comment", Message: "Say what to change, so the author knows what to do next."}
	case utf8.RuneCountInString(comment) > review.MaxCommentLength:
		return nil, nil, 0, &FieldError{Field: "comment", Message: fmt.Sprintf("Keep the comment to %d characters.", review.MaxCommentLength)}
	}
	var (
		out     *review.Review
		version *VersionEntry
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, r, err := pendingReview(ctx, tx, actor, pageID, id)
		if err != nil {
			return err
		}
		switch {
		case r.AuthorID != nil && *r.AuthorID == actor.UserID:
			return ErrOwnReview
		case !r.Can.Decide:
			return ErrNotReviewer
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_review_decision (org_id, review_id, reviewer_id, decision, comment)
			VALUES (current_org_id(), $1, $2, $3, $4)
			ON CONFLICT (org_id, review_id, reviewer_id) DO UPDATE
			SET decision = EXCLUDED.decision, comment = EXCLUDED.comment, decided_at = now()`,
			id, actor.UserID, in.Decision, comment); err != nil {
			return fmt.Errorf("record the decision: %w", err)
		}
		outcome := string(in.Decision)
		var number *int
		switch in.Decision {
		case review.DecisionChanges:
			if _, err := tx.Exec(ctx, `UPDATE page_review SET state = $2, finished_at = now() WHERE id = $1`, id, review.StateChanges); err != nil {
				return fmt.Errorf("send the draft back: %w", err)
			}
		case review.DecisionApprove:
			var approvals int
			if err := tx.QueryRow(ctx, `SELECT page_review_approvals($1)`, id).Scan(&approvals); err != nil {
				return err
			}
			if review.Complete(approvals, r.Required) {
				if version, err = goOut(ctx, tx, actor, p, r, nil); err != nil {
					return err
				}
				outcome, number = outcomePublished, &version.Number
			}
		}
		if err := events.Emit(ctx, tx, events.TopicReviewDecided, events.ReviewDecided{
			ReviewID: id, PageID: pageID, ActorID: actor.UserID, Outcome: outcome, Version: number}); err != nil {
			return err
		}
		out, err = reviewOn(ctx, tx, actor, sp.Can.Administer, pageID, id, false)
		return err
	})
	return out, version, lsn, err
}

// WithdrawReview takes the author's pending review back; the draft stays.
func (s *Service) WithdrawReview(ctx context.Context, actor perm.Actor, pageID, id uuid.UUID) (*review.Review, db.LSN, error) {
	var out *review.Review
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := load(ctx, tx, actor, pageID, true)
		if err != nil {
			return err
		}
		r, err := reviewOn(ctx, tx, actor, sp.Can.Administer, p.ID, id, false)
		if err != nil {
			return err
		}
		switch {
		case r.State != review.StatePending:
			return ErrReviewClosed
		case r.AuthorID == nil || *r.AuthorID != actor.UserID:
			return ErrNotAuthor
		}
		if _, err := tx.Exec(ctx, `UPDATE page_review SET state = $2, finished_at = now() WHERE id = $1`, id, review.StateWithdrawn); err != nil {
			return fmt.Errorf("withdraw the review: %w", err)
		}
		out, err = reviewOn(ctx, tx, actor, sp.Can.Administer, pageID, id, false)
		return err
	})
	return out, lsn, err
}

// SkipReview publishes a pending review without its approvals, as its
// author's next version, for an administrator of the space with a reason.
func (s *Service) SkipReview(ctx context.Context, actor perm.Actor, pageID, id uuid.UUID, in review.SkipInput) (*review.Review, *VersionEntry, db.LSN, error) {
	reason := strings.TrimSpace(in.Reason)
	switch {
	case reason == "":
		return nil, nil, 0, &FieldError{Field: "reason", Message: "Say why the draft goes out without its review; the reason is kept in the audit log."}
	case utf8.RuneCountInString(reason) > review.MaxReasonLength:
		return nil, nil, 0, &FieldError{Field: "reason", Message: fmt.Sprintf("Keep the reason to %d characters.", review.MaxReasonLength)}
	}
	var (
		out     *review.Review
		version *VersionEntry
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, r, err := pendingReview(ctx, tx, actor, pageID, id)
		if err != nil {
			return err
		}
		if !sp.Can.Administer {
			return perm.Refuse(perm.SkipReview, perm.NotArchived)
		}
		if version, err = goOut(ctx, tx, actor, p, r, &skipping{reason: reason, by: actor.UserID}); err != nil {
			return err
		}
		if err := record(ctx, tx, actor, audit.ActionReviewSkipped, pageID, map[string]any{
			"space": sp.Key, "title": p.Title, "review": id, "author": r.AuthorName, "reason": reason, "version": version.Number,
		}); err != nil {
			return err
		}
		number := version.Number
		if err := events.Emit(ctx, tx, events.TopicReviewDecided, events.ReviewDecided{
			ReviewID: id, PageID: pageID, ActorID: actor.UserID, Outcome: outcomeSkipped, Version: &number}); err != nil {
			return err
		}
		out, err = reviewOn(ctx, tx, actor, sp.Can.Administer, pageID, id, false)
		return err
	})
	return out, version, lsn, err
}

// WaitingReviews lists the pending reviews the caller may decide, across
// every space of the organization, the oldest first.
func (s *Service) WaitingReviews(ctx context.Context, actor perm.Actor) ([]review.Waiting, error) {
	out := []review.Waiting{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT r.id, p.id, p.title, s.key, COALESCE(perm_space_holds($1, p.space_id, 'administer'), false)
			FROM page_review r JOIN page p ON p.id = r.page_id JOIN space s ON s.id = p.space_id
			WHERE r.state = 'pending' AND p.trashed_at IS NULL AND page_review_decidable(r.id, $1)
			ORDER BY r.submitted_at, r.id`, actor.UserID)
		if err != nil {
			return fmt.Errorf("read the reviews waiting: %w", err)
		}
		var (
			ids   []uuid.UUID
			admin = map[uuid.UUID]bool{}
		)
		for rows.Next() {
			var (
				w  review.Waiting
				id uuid.UUID
				ok bool
			)
			if err := rows.Scan(&id, &w.Page.ID, &w.Page.Title, &w.SpaceKey, &ok); err != nil {
				rows.Close()
				return err
			}
			ids, admin[id] = append(ids, id), ok
			out = append(out, w)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		found, err := reviewsOf(ctx, tx, actor, false, `r.id = ANY($2)`, ids)
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]review.Review{}
		for _, r := range found {
			r.Can.Skip = admin[r.ID]
			byID[r.ID] = r
		}
		for i, id := range ids {
			out[i].Review = byID[id]
		}
		return nil
	})
	return out, err
}

// ReviewRules lists a space's rules: the whole space's first, then each
// branch's, for whoever may view the space.
func (s *Service) ReviewRules(ctx context.Context, actor perm.Actor, spaceKey string) ([]review.Rule, error) {
	var out []review.Rule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		out, err = readRules(ctx, tx, `review_rule_space(r.space_id, r.page_id) = $1`, sp.ID)
		return err
	})
	return out, err
}

// cleanRule checks a rule's reviewers and count: people and groups who are
// together at least as many people as the approvals it asks for.
func cleanRule(ctx context.Context, tx db.DBTX, in review.RuleInput) ([]perm.Subject, error) {
	if in.Required < 1 || in.Required > review.MaxRequired {
		return nil, &FieldError{Field: "required", Message: fmt.Sprintf("Ask for between 1 and %d approvals.", review.MaxRequired)}
	}
	if len(in.Reviewers) == 0 {
		return nil, &FieldError{Field: "reviewers", Message: "Name at least one person or group to review the pages."}
	}
	for _, ref := range in.Reviewers {
		if ref.Type != perm.SubjectUser && ref.Type != perm.SubjectGroup {
			return nil, &FieldError{Field: "reviewers", Message: "Name people and groups to review the pages; a review is somebody's judgement, so everyone cannot be named."}
		}
	}
	subjects, err := perm.ResolveSubjects(ctx, tx, "reviewers", in.Reviewers, false)
	if err != nil {
		return nil, err
	}
	users, groups := subjectIDs(subjects)
	var people int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT unnest($1::uuid[])
			UNION
			SELECT gm.user_id FROM group_member gm WHERE gm.org_id = current_org_id() AND gm.group_id = ANY($2)
		) named`, users, groups).Scan(&people); err != nil {
		return nil, fmt.Errorf("count the reviewers: %w", err)
	}
	if people < in.Required {
		return nil, &FieldError{Field: "required", Message: fmt.Sprintf(
			"The people and groups named are %d people, so the rule can ask for at most %d approvals. Name more reviewers, or ask for fewer approvals.", people, people)}
	}
	return subjects, nil
}

// setRule writes a rule of a space or of a branch, whichever is named, with
// its reviewers, over the one there was, and records it.
func setRule(ctx context.Context, tx db.DBTX, actor perm.Actor, spaceID, pageID *uuid.UUID, required int, subjects []perm.Subject, target uuid.UUID, about map[string]any) (*review.Rule, error) {
	var ruleID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM review_rule WHERE space_id IS NOT DISTINCT FROM $1 AND page_id IS NOT DISTINCT FROM $2`,
		spaceID, pageID).Scan(&ruleID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		ruleID = uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO review_rule (org_id, id, space_id, page_id, required, updated_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)`, ruleID, spaceID, pageID, required, actor.UserID); err != nil {
			return nil, fmt.Errorf("save the review rule: %w", err)
		}
	case err != nil:
		return nil, err
	default:
		if _, err := tx.Exec(ctx, `UPDATE review_rule SET required = $2, updated_by = $3, updated_at = now() WHERE id = $1`,
			ruleID, required, actor.UserID); err != nil {
			return nil, fmt.Errorf("save the review rule: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM review_rule_subject WHERE rule_id = $1`, ruleID); err != nil {
			return nil, fmt.Errorf("save the reviewers: %w", err)
		}
	}
	var types, users, groups []string
	for _, sub := range subjects {
		types = append(types, string(sub.Type))
		users = append(users, idText(sub.UserID()))
		groups = append(groups, idText(sub.GroupID()))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO review_rule_subject (org_id, rule_id, subject_type, user_id, group_id)
		SELECT current_org_id(), $1, t, NULLIF(u, '')::uuid, NULLIF(g, '')::uuid
		FROM unnest($2::text[], $3::text[], $4::text[]) AS s (t, u, g)`, ruleID, types, users, groups); err != nil {
		return nil, fmt.Errorf("save the reviewers: %w", err)
	}
	about["reviewers"], about["required"] = perm.SubjectLog(subjects), required
	if err := recordRule(ctx, tx, actor, audit.ActionReviewRuleSet, spaceID == nil, target, about); err != nil {
		return nil, err
	}
	rules, err := readRules(ctx, tx, `r.id = $1`, ruleID)
	if err != nil {
		return nil, err
	}
	return &rules[0], nil
}

// recordRule notes a rule set or removed against the space, or the branch's
// top page.
func recordRule(ctx context.Context, tx db.DBTX, actor perm.Actor, action string, branch bool, target uuid.UUID, data map[string]any) error {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return err
	}
	targetType := "space"
	if branch {
		targetType = "page"
	}
	return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: targetType, TargetID: &target, Actor: actor.UserID, Data: data})
}

// SetSpaceReviewRule holds every page and post of a space to a review, for
// its administrators, while none of its pages is live.
func (s *Service) SetSpaceReviewRule(ctx context.Context, actor perm.Actor, spaceKey string, in review.RuleInput) (*review.Rule, db.LSN, error) {
	var out *review.Rule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		if !sp.Can.Administer {
			return perm.Refuse(perm.KeepReviewRules, perm.NotArchived)
		}
		subjects, err := cleanRule(ctx, tx, in)
		if err != nil {
			return err
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT page_live_within($1, NULL)`, sp.ID).Scan(&live); err != nil {
			return err
		}
		if live {
			return ErrLivePages
		}
		out, err = setRule(ctx, tx, actor, &sp.ID, nil, in.Required, subjects, sp.ID, map[string]any{"space": sp.Key})
		return err
	})
	return out, lsn, err
}

// RemoveSpaceReviewRule stops holding a space to a review; branches keep
// their own rules, and pending reviews stay to be decided.
func (s *Service) RemoveSpaceReviewRule(ctx context.Context, actor perm.Actor, spaceKey string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		if !sp.Can.Administer {
			return perm.Refuse(perm.KeepReviewRules, perm.NotArchived)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM review_rule WHERE space_id = $1`, sp.ID)
		if err != nil {
			return fmt.Errorf("remove the review rule: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNoRule
		}
		return recordRule(ctx, tx, actor, audit.ActionReviewRuleRemoved, false, sp.ID, map[string]any{"space": sp.Key})
	})
}

// branchFor loads the top page of a branch for a change of its rule.
func branchFor(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Page, *space.Space, error) {
	p, sp, err := load(ctx, tx, actor, id, true)
	if err != nil {
		return nil, nil, err
	}
	if !sp.Can.Administer {
		return nil, nil, perm.Refuse(perm.KeepReviewRules, perm.NotArchived)
	}
	if p.Kind == KindPost {
		return nil, nil, ErrPostPlace
	}
	return p, sp, nil
}

// SetPageReviewRule holds a page and every page below it to a review in
// place of the space's rule, for the space's administrators.
func (s *Service) SetPageReviewRule(ctx context.Context, actor perm.Actor, id uuid.UUID, in review.RuleInput) (*review.Rule, db.LSN, error) {
	var out *review.Rule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := branchFor(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		subjects, err := cleanRule(ctx, tx, in)
		if err != nil {
			return err
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT page_live_within(NULL, $1)`, id).Scan(&live); err != nil {
			return err
		}
		if live {
			return ErrLivePages
		}
		out, err = setRule(ctx, tx, actor, nil, &id, in.Required, subjects, id, map[string]any{"space": sp.Key, "title": p.Title})
		return err
	})
	return out, lsn, err
}

// RemovePageReviewRule removes a branch's own rule, so the space's holds there.
func (s *Service) RemovePageReviewRule(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, sp, err := branchFor(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM review_rule WHERE page_id = $1`, id)
		if err != nil {
			return fmt.Errorf("remove the review rule: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNoRule
		}
		return recordRule(ctx, tx, actor, audit.ActionReviewRuleRemoved, true, id, map[string]any{"space": sp.Key, "title": p.Title})
	})
}

// liveUnderRule refuses a page that is live, or has a live page below it,
// where a rule holds it, as a move may bring about.
func liveUnderRule(ctx context.Context, tx db.DBTX, p *Page) error {
	var live bool
	if err := tx.QueryRow(ctx, `SELECT page_live_within(NULL, $1)`, p.ID).Scan(&live); err != nil || !live {
		return err
	}
	rule, err := ruleOf(ctx, tx, p)
	if err != nil {
		return err
	}
	if rule != nil {
		return ErrLivePages
	}
	return nil
}

// SetReadAsk asks a page's readers to confirm they read it from its current
// version on, again when it asked before, or stops asking; for its editors.
func (s *Service) SetReadAsk(ctx context.Context, actor perm.Actor, id uuid.UUID, in review.ReadAskInput) (*review.ReadAsk, db.LSN, error) {
	var out review.ReadAsk
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if p.Kind == KindFolder {
			return ErrFolder
		}
		switch {
		case !in.Ask:
			if _, err := tx.Exec(ctx, `DELETE FROM page_read_ask WHERE page_id = $1`, id); err != nil {
				return fmt.Errorf("stop asking: %w", err)
			}
		case p.Version == 0:
			return ErrAskUnpublished
		default:
			tag, err := tx.Exec(ctx, `UPDATE page_read_ask SET since = $2, asked_by = $3, asked_at = now() WHERE page_id = $1`,
				id, p.Version, actor.UserID)
			if err != nil {
				return fmt.Errorf("ask the readers again: %w", err)
			}
			if tag.RowsAffected() == 0 {
				if _, err := tx.Exec(ctx, `
					INSERT INTO page_read_ask (org_id, page_id, since, asked_by) VALUES (current_org_id(), $1, $2, $3)`,
					id, p.Version, actor.UserID); err != nil {
					return fmt.Errorf("ask the readers: %w", err)
				}
			}
		}
		out, err = readAskOf(ctx, tx, actor, id)
		return err
	})
	return &out, lsn, err
}

// ConfirmRead confirms the caller read the page as it is now.
func (s *Service) ConfirmRead(ctx context.Context, actor perm.Actor, id uuid.UUID) (*review.ReadAsk, db.LSN, error) {
	var out review.ReadAsk
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if p.ReadAsk.Since == nil {
			return ErrNotAsked
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_read_confirmation (org_id, page_id, user_id, version)
			VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (org_id, page_id, user_id) DO UPDATE SET version = EXCLUDED.version, confirmed_at = now()`,
			id, actor.UserID, p.Version); err != nil {
			return fmt.Errorf("confirm reading the page: %w", err)
		}
		out, err = readAskOf(ctx, tx, actor, id)
		return err
	})
	return &out, lsn, err
}

// ReadConfirmations is who confirmed reading a page since it asked, and who
// of those who may read it has not, for those who see who read it.
func (s *Service) ReadConfirmations(ctx context.Context, actor perm.Actor, id uuid.UUID) (*review.Readers, error) {
	var out *review.Readers
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		var listable bool
		if err := tx.QueryRow(ctx, `SELECT perm_page_readers_listable($1, $2)`, id, actor.UserID).Scan(&listable); err != nil {
			return err
		}
		if !listable {
			return &perm.DeniedError{Action: perm.ListReaders}
		}
		if p.ReadAsk.Since == nil {
			return ErrNotAsked
		}
		since := *p.ReadAsk.Since
		out = &review.Readers{Since: since, Confirmed: []review.ReaderEntry{}, Pending: []perm.Person{}}
		rows, err := tx.Query(ctx, `
			SELECT c.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), c.version, c.confirmed_at
			FROM page_read_confirmation c JOIN app_user u ON u.id = c.user_id
			WHERE c.page_id = $1 AND c.version >= $2
			ORDER BY c.confirmed_at DESC, c.user_id`, id, since)
		if err != nil {
			return fmt.Errorf("read the confirmations: %w", err)
		}
		for rows.Next() {
			var e review.ReaderEntry
			if err := rows.Scan(&e.Person.ID, &e.Person.Name, &e.Person.Email, &e.Version, &e.ConfirmedAt); err != nil {
				rows.Close()
				return err
			}
			out.Confirmed = append(out.Confirmed, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT m.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), count(*) OVER ()
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND u.is_active AND perm_page_viewable($1, m.user_id)
			  AND NOT EXISTS (SELECT 1 FROM page_read_confirmation c WHERE c.page_id = $1 AND c.user_id = m.user_id AND c.version >= $2)
			ORDER BY lower(COALESCE(u.name, '')), m.user_id
			LIMIT $3`, id, since, review.MaxPendingReaders)
		if err != nil {
			return fmt.Errorf("read who has not confirmed: %w", err)
		}
		defer rows.Close()
		total := 0
		for rows.Next() {
			var person perm.Person
			if err := rows.Scan(&person.ID, &person.Name, &person.Email, &total); err != nil {
				return err
			}
			out.Pending = append(out.Pending, person)
		}
		out.More = total - len(out.Pending)
		return rows.Err()
	})
	return out, err
}
