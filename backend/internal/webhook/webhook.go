// Package webhook posts the organization's content events to addresses its
// administrators named, signed with a secret each endpoint was shown once,
// with a log of every attempt, retries, redelivery and a test ping. Adapted
// from Armature's internal/webhook.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// MaxAttempts is how many times one event is tried before it is given up.
	MaxAttempts = 6
	// TopicAny subscribes an endpoint to every topic.
	TopicAny = "*"
	// TopicPing is what a test delivery carries.
	TopicPing = "ping"
	// SecretPrefix marks a webhook secret, so one is never taken for a token
	// and never reaches the audit log.
	SecretPrefix = "stator_whs_"
	// DefaultDeliveries and MaxDeliveries bound one read of the log.
	DefaultDeliveries = 50
	MaxDeliveries     = 200
	// MaxName and MaxURL are the database's bounds on an endpoint.
	MaxName = 100
	MaxURL  = 2000
	// DisableAfter is how long an endpoint fails every attempt before the
	// worker turns it off, and DisableAfterFailures the fewest failures that
	// do, so a receiver down for an afternoon is kept.
	DisableAfter         = 24 * time.Hour
	DisableAfterFailures = MaxAttempts
	// The headers a delivery carries, named as Armature names its own.
	SignatureHeader = "X-Stator-Signature-256"
	EventHeader     = "X-Stator-Event"
	DeliveryHeader  = "X-Stator-Delivery"
	userAgent       = "Stator-Webhook"
)

// Backoff is the wait before each retry, by attempt number; a sixth failure
// is the last.
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// Topics are the events an endpoint may take besides TopicAny.
var Topics = []string{events.TopicPagePublished, events.TopicPageMoved, events.TopicPageDeleted, events.TopicCommentCreated}

// Subscribable is every value an endpoint's topics may hold.
var Subscribable = append([]string{TopicAny}, Topics...)

// Reasons an endpoint was turned off by the worker rather than a person.
const ReasonFailing = "failing"

// DisabledReasons lists them for the document.
var DisabledReasons = []string{ReasonFailing}

var (
	// ErrNotFound is an endpoint or delivery that is not here, or not the caller's to see.
	ErrNotFound = errors.New("there is no such webhook")
	// ErrNoKey refuses an endpoint whose secret could not be sealed.
	ErrNoKey = errors.New("webhooks need STATOR_SECRET_KEY to keep their secrets")
)

// FieldError is an input a person has to change.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// WebhookOwner is the administrator whose permissions an endpoint's payloads are
// read with: whoever saved it last.
type WebhookOwner struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Webhook is one address the organization posts to.
type Webhook struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	URL     string    `json:"url"`
	Topics  []string  `json:"topics"`
	Enabled bool      `json:"enabled"`
	// DisabledReason says the worker turned it off, and why; null when a
	// person did or it is on.
	DisabledReason *string `json:"disabledReason"`
	// Failures counts the attempts that failed since the last delivered.
	Failures int `json:"failures"`
	// Owner is null once the administrator who saved it left; nothing but a
	// ping is sent until somebody saves it again.
	WebhookOwner *WebhookOwner `json:"owner"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	// Secret is set only on the answer that made or rotated it.
	Secret string `json:"secret,omitempty"`
}

// DeliveryState is where a delivery stands.
type DeliveryState string

const (
	StatePending   DeliveryState = "pending"
	StateDelivered DeliveryState = "delivered"
	StateFailed    DeliveryState = "failed"
	// StateWithheld is an event whose page the owner may not view, or no
	// longer exists: nothing was sent, and nothing will be.
	StateWithheld DeliveryState = "withheld"
	// StateCancelled is an attempt that came due while the endpoint was off.
	StateCancelled DeliveryState = "cancelled"
)

// States lists them for the document.
var States = []DeliveryState{StatePending, StateDelivered, StateFailed, StateWithheld, StateCancelled}

// WebhookDelivery is one attempt at one event.
type WebhookDelivery struct {
	ID        uuid.UUID `json:"id"`
	WebhookID uuid.UUID `json:"webhookId"`
	// EventID is the envelope's id, the same on every attempt and redelivery,
	// so a receiver can tell a repeat.
	EventID uuid.UUID `json:"eventId"`
	Topic   string    `json:"topic"`
	Attempt int       `json:"attempt"`
	// Manual is a test or a redelivery somebody asked for.
	Manual        bool          `json:"manual"`
	DeliveryState DeliveryState `json:"state"`
	// Status is what the receiver answered, null when it was not reached.
	Status        *int       `json:"status"`
	Error         string     `json:"error"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	AttemptedAt   *time.Time `json:"attemptedAt"`
	DeliveredAt   *time.Time `json:"deliveredAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// WebhookInput makes or changes an endpoint; enabled left out keeps it as it is,
// and a new endpoint starts on.
type WebhookInput struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Enabled *bool    `json:"enabled,omitempty"`
}

// Sign is the header value for a body: sha256= and the hex HMAC, Armature's
// shape, so a receiver written for one serves both.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify says whether a signature header matches the body, in constant time.
func Verify(body []byte, secret, header string) bool {
	return hmac.Equal([]byte(Sign(body, secret)), []byte(header))
}

// Options are the settings of the process the service needs.
type Options struct {
	// AppURL is Stator's own origin, for the links in a payload.
	AppURL string
	// Allow is what STATOR_OUTBOUND_ALLOW names.
	Allow netguard.Allow
	Log   *slog.Logger
}

// Service keeps endpoints and deliveries and does the posting.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *http.Client
	opts   Options
	now    func() time.Time
}

// NewService returns the service; a nil box refuses to make an endpoint and
// fails every delivery, since no secret opens.
func NewService(cluster *db.Cluster, box *secret.Box, opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	opts.AppURL = strings.TrimSuffix(opts.AppURL, "/")
	return &Service{db: cluster, box: box, client: netguard.Client(sendTimeout, opts.Allow), opts: opts, now: time.Now}
}

// sealContext binds a sealed secret to its endpoint, so a copy onto another
// row or organization does not open.
func sealContext(orgID, id uuid.UUID) []byte {
	return []byte("webhook:" + orgID.String() + ":" + id.String())
}

func newSecret() (string, error) {
	raw, _, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	return SecretPrefix + raw, nil
}

const selectWebhooks = `
SELECT e.id, e.name, e.url, e.topics, e.enabled, e.disabled_reason, e.failures, e.owner_id, u.name, e.created_at, e.updated_at
FROM webhook_endpoint e LEFT JOIN app_user u ON u.id = e.owner_id`

func scanWebhook(row pgx.Row) (*Webhook, error) {
	var (
		w         Webhook
		ownerID   *uuid.UUID
		ownerName *string
	)
	if err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Topics, &w.Enabled, &w.DisabledReason, &w.Failures, &ownerID, &ownerName, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	if w.Topics == nil {
		w.Topics = []string{}
	}
	if ownerID != nil {
		w.WebhookOwner = &WebhookOwner{ID: *ownerID}
		if ownerName != nil {
			w.WebhookOwner.Name = *ownerName
		}
	}
	return &w, nil
}

func one(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Webhook, error) {
	w, err := scanWebhook(tx.QueryRow(ctx, selectWebhooks+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

// List is the organization's endpoints, by name.
func (s *Service) List(ctx context.Context) ([]Webhook, error) {
	out := []Webhook{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectWebhooks+` ORDER BY lower(e.name), e.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWebhook(rows)
			if err != nil {
				return err
			}
			out = append(out, *w)
		}
		return rows.Err()
	})
	return out, err
}

// clean checks an input and returns it trimmed, its topics deduplicated in
// the order Subscribable gives them.
func (s *Service) clean(in WebhookInput) (WebhookInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > MaxName {
		return in, &FieldError{Field: "name", Message: fmt.Sprintf("Give the webhook a name of at most %d characters, such as the system it feeds.", MaxName)}
	}
	in.URL = strings.TrimSpace(in.URL)
	if err := s.checkURL(in.URL); err != nil {
		return in, err
	}
	if len(in.Topics) == 0 {
		return in, &FieldError{Field: "topics", Message: "Choose at least one event to send."}
	}
	for _, t := range in.Topics {
		if !slices.Contains(Subscribable, t) {
			return in, &FieldError{Field: "topics", Message: fmt.Sprintf("A webhook cannot take the event %q. Choose from %s.", t, strings.Join(Subscribable, ", "))}
		}
	}
	topics := []string{}
	for _, t := range Subscribable {
		if slices.Contains(in.Topics, t) {
			topics = append(topics, t)
		}
	}
	in.Topics = topics
	return in, nil
}

// checkURL refuses what is no address to post to, and an address that names
// the server's own network outright. A name is resolved only when it is
// posted to, where netguard decides again on every attempt.
func (s *Service) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || len(raw) > MaxURL {
		return &FieldError{Field: "url", Message: "Enter the address to post to, starting with https://, without a user name or a # part."}
	}
	host := strings.ToLower(u.Hostname())
	if s.opts.Allow.Names(host) {
		return nil
	}
	addr, notAddr := netip.ParseAddr(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || (notAddr == nil && netguard.Reserved(addr)) {
		return &FieldError{Field: "url", Message: "Stator may not post to an address inside the server's own network. Use the receiver's public address, or ask the operator to add the host to STATOR_OUTBOUND_ALLOW."}
	}
	return nil
}

// hostOf is what the audit log keeps of an address: a receiver's path or
// query often carries a token of its own.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func actorOf(ctx context.Context) uuid.UUID {
	id, _ := db.UserFrom(ctx)
	return id
}

func isUniqueName(err error) bool {
	return err != nil && strings.Contains(err.Error(), "webhook_endpoint_name_idx")
}

var errNameTaken = &FieldError{Field: "name", Message: "A webhook with that name is already here. Choose another name."}

// Create makes an endpoint and returns it with its secret, this once.
func (s *Service) Create(ctx context.Context, in WebhookInput) (*Webhook, db.LSN, error) {
	in, err := s.clean(in)
	if err != nil {
		return nil, 0, err
	}
	if s.box == nil {
		return nil, 0, ErrNoKey
	}
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, 0, err
	}
	plain, err := newSecret()
	if err != nil {
		return nil, 0, err
	}
	sealed, err := s.box.Seal([]byte(plain), sealContext(org.ID, id))
	if err != nil {
		return nil, 0, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO webhook_endpoint (id, org_id, name, url, secret_sealed, topics, enabled)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6)`, id, in.Name, in.URL, sealed, in.Topics, enabled)
		if isUniqueName(err) {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionWebhookCreated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": in.Name, "host": hostOf(in.URL), "topics": in.Topics, "enabled": enabled, "secret": "set"}})
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// Update changes the name, address, topics or whether it is on, and makes
// the caller its owner.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in WebhookInput) (*Webhook, db.LSN, error) {
	in, err := s.clean(in)
	if err != nil {
		return nil, 0, err
	}
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE webhook_endpoint SET name = $2, url = $3, topics = $4, enabled = COALESCE($5, enabled)
			WHERE id = $1`, id, in.Name, in.URL, in.Topics, in.Enabled)
		if isUniqueName(err) {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionWebhookUpdated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": out.Name, "host": hostOf(out.URL), "topics": out.Topics, "enabled": out.Enabled}})
	})
	return out, lsn, err
}

// RotateSecret issues a new secret; the old one stops working at once.
func (s *Service) RotateSecret(ctx context.Context, id uuid.UUID) (*Webhook, db.LSN, error) {
	if s.box == nil {
		return nil, 0, ErrNoKey
	}
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	plain, err := newSecret()
	if err != nil {
		return nil, 0, err
	}
	sealed, err := s.box.Seal([]byte(plain), sealContext(org.ID, id))
	if err != nil {
		return nil, 0, err
	}
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET secret_sealed = $2 WHERE id = $1`, id, sealed)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionWebhookSecretRotated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": out.Name, "secret": "set"}})
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// Delete removes the endpoint and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name, address string
		err := tx.QueryRow(ctx, `DELETE FROM webhook_endpoint WHERE id = $1 RETURNING name, url`, id).Scan(&name, &address)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionWebhookDeleted, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": name, "host": hostOf(address)}})
	})
}

const selectDeliveries = `
SELECT id, endpoint_id, event_id, topic, attempt, manual, state, status, error, next_attempt_at, attempted_at, delivered_at, created_at
FROM webhook_delivery`

func scanDelivery(row pgx.Row) (*WebhookDelivery, error) {
	var d WebhookDelivery
	if err := row.Scan(&d.ID, &d.WebhookID, &d.EventID, &d.Topic, &d.Attempt, &d.Manual, &d.DeliveryState, &d.Status, &d.Error,
		&d.NextAttemptAt, &d.AttemptedAt, &d.DeliveredAt, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// Deliveries is an endpoint's log, newest first.
func (s *Service) Deliveries(ctx context.Context, id uuid.UUID, limit int) ([]WebhookDelivery, error) {
	limit = min(max(limit, 1), MaxDeliveries)
	out := []WebhookDelivery{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE id = $1)`, id).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, selectDeliveries+` WHERE endpoint_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDelivery(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}
