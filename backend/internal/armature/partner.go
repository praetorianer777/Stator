package armature

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
)

// Stator's own webhook in Armature (#429), as Armature's docs/oauth.md sets
// out "The partner's own webhook": an administrator's grant that holds
// webhooks.partner makes it, and the secret Armature answers is kept as a
// pasted one is.

const (
	// CapabilityPartnerWebhooks is Armature letting a connected app keep its
	// own webhook; Connect is offered when /meta lists it beside
	// oauth.partner-tokens@1, and the pasted secret otherwise.
	CapabilityPartnerWebhooks = "webhooks.partner@1"
	// ScopeWebhooks lets a grant manage the app's webhook, for an Armature
	// administrator only.
	ScopeWebhooks = "webhooks.partner"
	// AdminScopes are what an administrator's consent asks for, every scope
	// at once: a narrower consent replaces the grant (Cloudster1/Armature#298).
	AdminScopes = AppScopes + " " + ScopeWebhooks
	// PartnerWebhookPath is the app's own endpoint, relative to /api/v1.
	PartnerWebhookPath = "/webhooks/partner"
	// PartnerTimeout bounds the calls one change of the webhook takes, a
	// test delivery Armature makes while it answers among them.
	PartnerTimeout = 20 * time.Second
)

// Armature's error codes the partner webhook is refused with.
const (
	codePartnerExists     = "partner_webhook_exists"
	codeInsufficientScope = "insufficient_scope"
	codeForbidden         = "forbidden"
)

var (
	// ErrWebhookUnavailable is an Armature that does not let Stator make its
	// own webhook; its administrator pastes the secret instead.
	ErrWebhookUnavailable = errors.New("the connected Armature does not let Stator make its own webhook")
	// ErrWebhookAdmin is a grant that may not manage the webhook: it lacks
	// webhooks.partner, its person no longer administers Armature, or it ended.
	ErrWebhookAdmin = errors.New("an Armature administrator has to connect Stator again to manage its webhook")
	// ErrNoPartnerWebhook is Armature having no webhook of Stator's.
	ErrNoPartnerWebhook = errors.New("Armature has no webhook of Stator's")
	// ErrScopeRefused is Armature's consent page refusing webhooks.partner,
	// which the app registered in Armature may not ask for.
	ErrScopeRefused = errors.New("Armature's app for Stator may not ask to send its events")
)

// WebhookRefusedError is Armature refusing to make or change Stator's
// webhook, in its own words, such as its outbound guard refusing the address.
type WebhookRefusedError struct{ Message string }

func (e *WebhookRefusedError) Error() string { return e.Message }

// PartnerWebhook is Stator's own webhook in Armature, for administrators.
type PartnerWebhook struct {
	// Offered says Armature lists webhooks.partner@1 beside
	// oauth.partner-tokens@1, so Connect makes the webhook.
	Offered bool `json:"offered"`
	// Connected says the stored secret is of the webhook Stator made.
	Connected bool `json:"connected"`
	// URL is the address Stator gives Armature to post to.
	URL string `json:"url"`
	// Refusal is Armature's sentence the last time it would not make it.
	Refusal *string `json:"refusal"`
	// Manageable says the caller's own grant may rotate, test and remove
	// it; otherwise they connect as an administrator first.
	Manageable bool `json:"manageable"`
}

// PartnerWebhookTest is a ping Armature posted to Stator's receiver now.
type PartnerWebhookTest struct {
	// Status is what the receiver answered, null when Armature reached nothing.
	Status *int `json:"status"`
	// Error is why Armature could not deliver, as it logged it.
	Error       *string    `json:"error"`
	DeliveredAt *time.Time `json:"deliveredAt"`
}

// OffersPartnerWebhook says whether the Armature lets Stator make its own
// webhook through the connected app.
func (p *Partner) OffersPartnerWebhook() bool {
	return p.OffersApp() && slices.Contains(p.Capabilities, CapabilityPartnerWebhooks)
}

func (s *Service) webhookURL() string {
	if s.opts.WebhookURL != "" {
		return s.opts.WebhookURL
	}
	return s.opts.AppURL + WebhookPath
}

type partnerEndpoint struct {
	ID      uuid.UUID `json:"id"`
	URL     string    `json:"url"`
	Topics  []string  `json:"topics"`
	Enabled bool      `json:"enabled"`
	Version int       `json:"version"`
	Secret  string    `json:"secret,omitempty"`
}

type partnerInput struct {
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Version int      `json:"version"`
}

type partnerChanges struct {
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
}

type partnerAnswer struct {
	Webhook *partnerEndpoint `json:"webhook"`
}

// partnerError names what Armature's refusal means for Stator's webhook.
func partnerError(err error) error {
	var refused *RefusedError
	switch {
	case errors.Is(err, ErrRejected):
		return ErrWebhookAdmin
	case !errors.As(err, &refused):
		return err
	case refused.Status == http.StatusNotFound:
		return ErrNoPartnerWebhook
	case refused.Status == http.StatusForbidden && (refused.Code == codeInsufficientScope || refused.Code == codeForbidden):
		return ErrWebhookAdmin
	case refused.Status == http.StatusUnprocessableEntity, refused.Status == http.StatusConflict && refused.Code != codePartnerExists:
		message := refused.Message
		if field := refused.Fields["url"]; field != "" && !strings.Contains(message, strings.TrimSuffix(field, ".")) {
			message += " " + field
		}
		return &WebhookRefusedError{Message: strings.TrimSpace(message)}
	}
	return err
}

func isPartnerExists(err error) bool {
	var refused *RefusedError
	return errors.As(err, &refused) && refused.Status == http.StatusConflict && refused.Code == codePartnerExists
}

// partnerAttempts bounds how often a webhook that goes between two calls is
// made again, so two administrators connecting at once cannot loop forever.
const partnerAttempts = 3

// makePartnerWebhook makes Stator's webhook in Armature, or brings the one
// it has to Stator's address and topics, and answers its new secret: a
// webhook found already is given a new secret, since the old one is not
// known to be the one stored.
func makePartnerWebhook(ctx context.Context, c *Caller, in partnerInput) (string, error) {
	for range partnerAttempts {
		var made partnerAnswer
		err := c.Send(ctx, http.MethodPost, PartnerWebhookPath, in, &made)
		if err == nil {
			if made.Webhook == nil || made.Webhook.Secret == "" {
				return "", fmt.Errorf("%w: Armature made the webhook without answering its secret", ErrUnreachable)
			}
			return made.Webhook.Secret, nil
		}
		if !isPartnerExists(err) {
			return "", partnerError(err)
		}
		var found partnerAnswer
		if err := c.Get(ctx, PartnerWebhookPath, nil, &found); err != nil {
			return "", partnerError(err)
		}
		if found.Webhook == nil {
			continue
		}
		var changed partnerAnswer
		err = c.Send(ctx, http.MethodPatch, PartnerWebhookPath, partnerChanges{URL: in.URL, Topics: in.Topics, Version: in.Version, Enabled: true}, &changed)
		if err = partnerError(err); errors.Is(err, ErrNoPartnerWebhook) {
			continue
		} else if err != nil {
			return "", err
		}
		secret, err := rotatePartnerSecret(ctx, c)
		if errors.Is(err, ErrNoPartnerWebhook) {
			continue
		}
		return secret, err
	}
	return "", fmt.Errorf("%w: Stator's webhook in Armature went each time it was found", ErrUnreachable)
}

func rotatePartnerSecret(ctx context.Context, c *Caller) (string, error) {
	var rotated partnerAnswer
	if err := c.Send(ctx, http.MethodPost, PartnerWebhookPath+"/rotate-secret", nil, &rotated); err != nil {
		return "", partnerError(err)
	}
	if rotated.Webhook == nil || rotated.Webhook.Secret == "" {
		return "", fmt.Errorf("%w: Armature rotated the webhook's secret without answering it", ErrUnreachable)
	}
	return rotated.Webhook.Secret, nil
}

// partnerCaller is the caller's own grant, when it may manage the webhook.
func (s *Service) partnerCaller(ctx context.Context) (*Caller, *endpoint, error) {
	var (
		e     *endpoint
		grant *grantRow
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		grant, err = s.ownGrant(ctx, tx)
		return err
	}); err != nil {
		return nil, nil, err
	}
	if e == nil {
		return nil, nil, ErrNotConfigured
	}
	if grant == nil || grant.Status == StatusRejected || !slices.Contains(grant.Scopes, ScopeWebhooks) {
		return nil, e, ErrWebhookAdmin
	}
	caller, err := s.grantCaller(ctx, e.BaseURL, grant)
	if errors.Is(err, ErrRejected) {
		return nil, e, ErrWebhookAdmin
	}
	return caller, e, err
}

// setUpWebhook makes Stator's webhook with the caller's grant and stores its
// secret; Armature's refusal is kept for the settings page as well.
func (s *Service) setUpWebhook(ctx context.Context, ip string) (db.LSN, error) {
	caller, _, err := s.partnerCaller(ctx)
	if err != nil {
		return 0, err
	}
	call, cancel := context.WithTimeout(ctx, PartnerTimeout)
	defer cancel()
	secret, err := makePartnerWebhook(call, caller, partnerInput{URL: s.webhookURL(), Topics: WebhookTopics, Version: WebhookVersion})
	return s.keepWebhookSecret(ctx, secret, err, "made", ip)
}

// keepWebhookSecret stores what making or rotating the webhook came to: its
// secret, or Armature's sentence refusing it.
func (s *Service) keepWebhookSecret(ctx context.Context, secret string, made error, change, ip string) (db.LSN, error) {
	var refused *WebhookRefusedError
	if made != nil && !errors.As(made, &refused) {
		return 0, made
	}
	actor, _ := db.UserFrom(ctx)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if refused != nil {
			_, err := tx.Exec(ctx, `UPDATE armature_connection SET webhook_refusal = $1`, refused.Message)
			return err
		}
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		sealed, err := s.seal(secret, webhookContext(site), "webhookSecret")
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE armature_connection SET webhook_secret = $1, webhook_partner = true, webhook_refusal = NULL, updated_by = $2`,
			sealed, nullable(actor))
		if err != nil {
			return fmt.Errorf("store the webhook secret: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotConfigured
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: ActionConnectionSaved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"webhookSecret": change, "partnerWebhook": true},
		})
	})
	if err != nil {
		return lsn, err
	}
	return lsn, made
}

// RotateWebhook gives Stator's webhook in Armature a new secret and stores
// it; a webhook Armature no longer has is made again.
func (s *Service) RotateWebhook(ctx context.Context, ip string) (*Connection, db.LSN, error) {
	caller, _, err := s.partnerCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	call, cancel := context.WithTimeout(ctx, PartnerTimeout)
	defer cancel()
	secret, err := rotatePartnerSecret(call, caller)
	change := "rotated"
	if errors.Is(err, ErrNoPartnerWebhook) {
		change = "made"
		secret, err = makePartnerWebhook(call, caller, partnerInput{URL: s.webhookURL(), Topics: WebhookTopics, Version: WebhookVersion})
	}
	lsn, err := s.keepWebhookSecret(ctx, secret, err, change, ip)
	if err != nil {
		return nil, lsn, err
	}
	out, err := s.Connection(db.PinPrimary(ctx))
	return out, lsn, err
}

// TestWebhook has Armature post a ping to Stator's receiver now and answers
// how it went; a webhook Armature no longer has is made again first.
func (s *Service) TestWebhook(ctx context.Context, ip string) (*PartnerWebhookTest, db.LSN, error) {
	caller, _, err := s.partnerCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	call, cancel := context.WithTimeout(ctx, PartnerTimeout)
	defer cancel()
	var answer struct {
		Delivery struct {
			Status      *int       `json:"status"`
			Error       *string    `json:"error"`
			DeliveredAt *time.Time `json:"deliveredAt"`
		} `json:"delivery"`
	}
	test := func() error {
		return partnerError(caller.Send(patiently(call), http.MethodPost, PartnerWebhookPath+"/test", nil, &answer))
	}
	var lsn db.LSN
	err = test()
	if errors.Is(err, ErrNoPartnerWebhook) {
		secret, made := makePartnerWebhook(call, caller, partnerInput{URL: s.webhookURL(), Topics: WebhookTopics, Version: WebhookVersion})
		if lsn, err = s.keepWebhookSecret(ctx, secret, made, "made", ip); err == nil {
			err = test()
		}
	}
	if err != nil {
		return nil, lsn, err
	}
	d := answer.Delivery
	return &PartnerWebhookTest{Status: d.Status, Error: d.Error, DeliveredAt: d.DeliveredAt}, lsn, nil
}

// removePartnerWebhook removes Stator's webhook from the Armature it is
// leaving, with the caller's grant. Where that cannot be done it is logged:
// the secret is forgotten here either way, so nothing Armature sends to the
// old webhook is taken.
func (s *Service) removePartnerWebhook(ctx context.Context) {
	caller, _, err := s.partnerCaller(ctx)
	if err != nil {
		s.opts.Log.Info("Stator's webhook in Armature was left there; an Armature administrator removes it under Admin, Webhooks", "error", err)
		return
	}
	call, cancel := context.WithTimeout(ctx, PartnerTimeout)
	defer cancel()
	if err := partnerError(caller.Send(call, http.MethodDelete, PartnerWebhookPath, nil, nil)); err != nil {
		s.opts.Log.Info("Stator's webhook in Armature could not be removed; an Armature administrator removes it under Admin, Webhooks", "error", err)
	}
}

// partnerWebhook is the webhook as the connection's reader sees it, in the
// connection's transaction; Offered is filled in from Partner after it.
func (s *Service) partnerWebhook(ctx context.Context, tx db.DBTX, connected bool, refusal *string) (PartnerWebhook, error) {
	out := PartnerWebhook{Connected: connected, Refusal: refusal, URL: s.webhookURL()}
	grant, err := s.ownGrant(ctx, tx)
	if err != nil {
		return out, err
	}
	out.Manageable = grant != nil && grant.Status != StatusRejected && slices.Contains(grant.Scopes, ScopeWebhooks)
	return out, nil
}
