package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// sendTimeout bounds one request; a slow receiver is a failed attempt.
	sendTimeout = 10 * time.Second
	// maxResponseBytes is the most of an answer that is read and dropped,
	// so the connection can be used again.
	maxResponseBytes = 4 << 10
	// ClaimLease is how long a claimed delivery is kept from other senders;
	// one that dies holding it delays the attempt by at most this much.
	ClaimLease = 2 * time.Minute
	// sendBatch is how many due deliveries one pass claims; passes repeat
	// until none is due, so a backlog is drained rather than nibbled.
	sendBatch = 100
	// SendInterval is how often the worker looks for due deliveries, and so
	// the most a first attempt waits.
	SendInterval = 2 * time.Second
	// DeliveryRetention is how long the log keeps an attempt, as Armature's.
	DeliveryRetention = 30 * 24 * time.Hour
	// pruneEvery is how often attempts past DeliveryRetention are deleted.
	pruneEvery = time.Hour
	// MaxCommentText bounds the words of a comment a payload carries.
	MaxCommentText = 1000
)

// The sentences a delivery's log shows for what stopped it.
const (
	whyBlocked    = "This address is inside the server's own network, so Stator may not post to it. Use the receiver's public address, or ask the operator to add the host to STATOR_OUTBOUND_ALLOW."
	whyTimeout    = "The receiver did not answer in time. Check that it is running, then send it again."
	whyUnreached  = "The receiver could not be reached. Check the address, then send it again."
	whyNoSecret   = "The webhook's secret does not open with this server's key. Rotate the secret and give the receiver the new one."
	whyOff        = "The webhook was turned off before this was sent."
	whyNoOwner    = "Nothing was sent: the administrator who saved this webhook has left. Save it again to send as yourself."
	whyNotVisible = "Nothing was sent: the webhook's owner may not view this page, or it no longer exists."
)

// errWithheld is a payload the owner could not read; nothing is sent.
var errWithheld = errors.New("withheld")

// Handle is the outbox's half: every enabled endpoint subscribed to the
// topic gets a delivery due now. A second run for the same event adds none.
func (s *Service) Handle(ctx context.Context, e events.Event) error {
	if e.OrgID == uuid.Nil || !slices.Contains(Topics, e.Topic) {
		return nil
	}
	occurred := e.CreatedAt
	if occurred.IsZero() {
		occurred = s.now()
	}
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	ctx = tenant.WithOrg(db.WithUser(ctx, uuid.Nil), tenant.Org{ID: e.OrgID})
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, event, occurred_at, next_attempt_at)
			SELECT org_id, id, $2, $3, $4, $5, now() FROM webhook_endpoint
			WHERE org_id = $1 AND enabled AND ($3 = ANY (topics) OR $6 = ANY (topics))
			ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`,
			e.OrgID, e.ID, e.Topic, payload, occurred, TopicAny)
		return err
	})
	return err
}

// SendDue posts every delivery whose time has come, across organizations,
// and says how many were tried. Each is leased first, so senders in several
// workers never post one twice.
func (s *Service) SendDue(ctx context.Context) (int, error) {
	type due struct{ orgID, id uuid.UUID }
	tried := 0
	for {
		var found []due
		_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `
				UPDATE webhook_delivery d SET next_attempt_at = now() + make_interval(secs => $2)
				FROM (
				    SELECT id FROM webhook_delivery
				    WHERE state = 'pending' AND next_attempt_at <= now()
				    ORDER BY next_attempt_at
				    LIMIT $1
				    FOR UPDATE SKIP LOCKED
				) claimed
				WHERE d.id = claimed.id
				RETURNING d.org_id, d.id`, sendBatch, ClaimLease.Seconds())
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d due
				if err := rows.Scan(&d.orgID, &d.id); err != nil {
					return err
				}
				found = append(found, d)
			}
			return rows.Err()
		})
		if err != nil {
			return tried, err
		}
		for _, d := range found {
			if ctx.Err() != nil {
				return tried, nil
			}
			if _, _, err := s.attempt(tenant.WithOrg(ctx, tenant.Org{ID: d.orgID}), d.id); err != nil {
				s.opts.Log.Warn("a webhook attempt could not be recorded", "delivery", d.id, "error", err)
			}
		}
		tried += len(found)
		if len(found) < sendBatch || ctx.Err() != nil {
			return tried, nil
		}
	}
}

// Prune deletes the attempts past DeliveryRetention.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	var gone int64
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM webhook_delivery WHERE created_at < now() - make_interval(secs => $1) AND state <> 'pending'`,
			DeliveryRetention.Seconds())
		gone = tag.RowsAffected()
		return err
	})
	return gone, err
}

// Test posts a ping to the endpoint now and returns the attempt as logged.
// It is sent even while the endpoint is off, to check it before turning it on.
func (s *Service) Test(ctx context.Context, id uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	if err := s.visible(ctx, id); err != nil {
		return nil, 0, err
	}
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	var made uuid.UUID
	system := asSystem(ctx, org.ID)
	lsn, err := s.db.WriteAdmin(system, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, event, occurred_at, manual, next_attempt_at)
			VALUES ($1, $2, $3, $4, '{}', now(), true, now() + make_interval(secs => $5))
			RETURNING id`, org.ID, id, uuid.New(), TopicPing, ClaimLease.Seconds()).Scan(&made)
	})
	if err != nil {
		return nil, 0, err
	}
	sent, recorded, err := s.attempt(system, made)
	return sent, max(lsn, recorded), err
}

// Redeliver tries a logged delivery's event again, now, as the next attempt.
// The payload is read afresh, as the owner may view it now.
func (s *Service) Redeliver(ctx context.Context, webhookID, deliveryID uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_delivery WHERE id = $1 AND endpoint_id = $2)`, deliveryID, webhookID).Scan(&found)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	var made uuid.UUID
	system := asSystem(ctx, org.ID)
	lsn, err := s.db.WriteAdmin(system, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, event, occurred_at, attempt, manual, next_attempt_at)
			SELECT d.org_id, d.endpoint_id, d.event_id, d.topic, d.event, d.occurred_at,
			       (SELECT max(attempt) FROM webhook_delivery x WHERE x.endpoint_id = d.endpoint_id AND x.event_id = d.event_id) + 1,
			       true, now() + make_interval(secs => $3)
			FROM webhook_delivery d WHERE d.id = $1 AND d.org_id = $2
			RETURNING id`, deliveryID, org.ID, ClaimLease.Seconds()).Scan(&made)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	sent, recorded, err := s.attempt(system, made)
	return sent, max(lsn, recorded), err
}

// visible says whether the caller may see the endpoint, through the policies.
func (s *Service) visible(ctx context.Context, id uuid.UUID) error {
	return s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE id = $1)`, id).Scan(&found)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
}

// asSystem is ctx in the organization acting for nobody: the log and the
// counts are the worker's, and a request's caller must not become the
// endpoint's owner by sending a test.
func asSystem(ctx context.Context, orgID uuid.UUID) context.Context {
	return db.PinPrimary(tenant.WithOrg(db.WithUser(ctx, uuid.Nil), tenant.Org{ID: orgID}))
}

// claimed is a delivery about to be attempted, with its endpoint.
type claimed struct {
	orgID, endpointID, eventID uuid.UUID
	url, topic                 string
	sealed                     []byte
	enabled, manual            bool
	owner                      *uuid.UUID
	event                      json.RawMessage
	occurred                   time.Time
	attempt                    int
	state                      DeliveryState
}

// outcome is how one attempt went.
type outcome struct {
	state  DeliveryState
	status *int
	why    string
	// counts says whether it moves the endpoint's count of failures.
	counts bool
}

// attempt posts one logged delivery and records how it went: delivered,
// withheld, cancelled, or failed with the next attempt due later.
func (s *Service) attempt(ctx context.Context, id uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	ctx = asSystem(ctx, org.ID)
	var c claimed
	err = s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT d.org_id, d.endpoint_id, d.event_id, e.url, d.topic, e.secret_sealed, e.enabled, d.manual, e.owner_id,
			       d.event, d.occurred_at, d.attempt, d.state
			FROM webhook_delivery d JOIN webhook_endpoint e ON e.org_id = d.org_id AND e.id = d.endpoint_id
			WHERE d.id = $1 AND d.org_id = $2`, id, org.ID).
			Scan(&c.orgID, &c.endpointID, &c.eventID, &c.url, &c.topic, &c.sealed, &c.enabled, &c.manual, &c.owner,
				&c.event, &c.occurred, &c.attempt, &c.state)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	if c.state != StatePending {
		done, err := s.read(ctx, id)
		return done, 0, err
	}
	return s.record(ctx, id, c, s.send(ctx, id, c))
}

// send builds the body as the owner may read it, signs it and posts it.
func (s *Service) send(ctx context.Context, id uuid.UUID, c claimed) outcome {
	if !c.enabled && !c.manual {
		return outcome{state: StateCancelled, why: whyOff}
	}
	body, err := s.body(ctx, c)
	if errors.Is(err, errWithheld) {
		why := whyNotVisible
		if c.owner == nil {
			why = whyNoOwner
		}
		return outcome{state: StateWithheld, why: why}
	}
	if err != nil {
		s.opts.Log.Warn("a webhook payload could not be read", "delivery", id, "error", err)
		return outcome{state: StateFailed, why: whyUnreached, counts: true}
	}
	plain, err := s.box.Open(c.sealed, sealContext(c.orgID, c.endpointID))
	if err != nil {
		s.opts.Log.Warn("a webhook secret could not be opened", "webhook", c.endpointID, "error", err)
		return outcome{state: StateFailed, why: whyNoSecret, counts: true}
	}
	status, err := s.post(ctx, c.url, string(plain), c.topic, id, body)
	if err != nil {
		s.opts.Log.Info("a webhook delivery did not reach its receiver", "delivery", id, "error", err)
		return outcome{state: StateFailed, why: why(err), counts: true}
	}
	if status < 200 || status > 299 {
		return outcome{state: StateFailed, status: &status, why: fmt.Sprintf("The receiver answered %d. Check its log, then send it again.", status), counts: true}
	}
	return outcome{state: StateDelivered, status: &status}
}

// why says what stopped a request in a sentence. The error itself names
// hosts and ports this server can reach, which the log may say and a
// delivery row may not.
func why(err error) string {
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return whyBlocked
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "timeout"):
		return whyTimeout
	default:
		return whyUnreached
	}
}

// record writes how an attempt went, schedules the next after a failure,
// and turns the endpoint off once it has failed for DisableAfter.
func (s *Service) record(ctx context.Context, id uuid.UUID, c claimed, o outcome) (*WebhookDelivery, db.LSN, error) {
	var out *WebhookDelivery
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			UPDATE webhook_delivery
			SET state = $2, status = $3, error = $4, next_attempt_at = NULL, attempted_at = now(),
			    delivered_at = CASE WHEN $2 = 'delivered' THEN now() END
			WHERE id = $1 AND org_id = $5`, id, o.state, o.status, o.why, c.orgID); err != nil {
			return err
		}
		switch {
		case o.state == StateDelivered:
			if _, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET failures = 0, failing_since = NULL WHERE id = $1 AND org_id = $2`, c.endpointID, c.orgID); err != nil {
				return err
			}
		case o.counts && !c.manual:
			disabled, err := s.countFailure(ctx, tx, c)
			if err != nil {
				return err
			}
			if !disabled && c.attempt < MaxAttempts {
				if _, err := tx.Exec(ctx, `
					INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, event, occurred_at, attempt, next_attempt_at)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
					ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`,
					c.orgID, c.endpointID, c.eventID, c.topic, c.event, c.occurred, c.attempt+1, s.now().Add(NextWait(c.attempt))); err != nil {
					return err
				}
			}
		}
		var err error
		out, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE id = $1`, id))
		return err
	})
	return out, lsn, err
}

// NextWait is how long after a failed attempt the next one is due.
func NextWait(attempt int) time.Duration {
	return Backoff[min(max(attempt, 1)-1, len(Backoff)-1)]
}

// ShouldDisable says whether an endpoint that has failed failures times in a
// row, since since, has failed long enough to be turned off at now.
func ShouldDisable(failures int, since, now time.Time) bool {
	return failures >= DisableAfterFailures && !since.After(now.Add(-DisableAfter))
}

// countFailure adds a failure to the endpoint and turns it off when it has
// failed long enough, cancelling what was still due and noting it in the
// audit log with nobody as the actor.
func (s *Service) countFailure(ctx context.Context, tx db.DBTX, c claimed) (bool, error) {
	var (
		failures int
		since    time.Time
		enabled  bool
		name     string
	)
	err := tx.QueryRow(ctx, `
		UPDATE webhook_endpoint SET failures = failures + 1, failing_since = COALESCE(failing_since, now())
		WHERE id = $1 AND org_id = $2
		RETURNING failures, failing_since, enabled, name`, c.endpointID, c.orgID).Scan(&failures, &since, &enabled, &name)
	if err != nil {
		return false, err
	}
	if !enabled {
		return true, nil
	}
	if !ShouldDisable(failures, since, s.now()) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET enabled = false, disabled_reason = $3 WHERE id = $1 AND org_id = $2`,
		c.endpointID, c.orgID, ReasonFailing); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_delivery SET state = 'cancelled', error = $3, next_attempt_at = NULL
		WHERE endpoint_id = $1 AND org_id = $2 AND state = 'pending' AND NOT manual`, c.endpointID, c.orgID, whyOff); err != nil {
		return false, err
	}
	id := c.endpointID
	return true, audit.Write(ctx, tx, c.orgID, audit.Entry{Action: audit.ActionWebhookDisabled, TargetType: "webhook", TargetID: &id,
		Data: map[string]any{"name": name, "reason": ReasonFailing, "failures": failures}})
}

func (s *Service) read(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error) {
	var out *WebhookDelivery
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

// post sends one request and returns the status, or the error that stopped it.
func (s *Service) post(ctx context.Context, address, secret, topic string, deliveryID uuid.UUID, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(EventHeader, topic)
	req.Header.Set(DeliveryHeader, deliveryID.String())
	req.Header.Set(SignatureHeader, Sign(body, secret))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, nil
}
