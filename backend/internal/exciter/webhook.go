package exciter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The receiver refuses with Armature's errors, so the API answers both
// partners in the same words.
var (
	ErrBadSignature   = armature.ErrBadSignature
	ErrWebhookStale   = armature.ErrWebhookStale
	ErrWebhookVersion = armature.ErrWebhookVersion
)

// absentSecret is signed against when there is no secret to check, so an
// unconnected site costs the same work as a connected one.
const absentSecret = WebhookSecretPrefix + "absent"

// WebhookClears says which requirement keys a delivery clears, and the id of
// the requirement it names, whose former keys are cleared too; an unknown
// topic clears nothing.
func WebhookClears(topic string, payload json.RawMessage) (keys []string, id uuid.UUID) {
	var p struct {
		Requirement struct {
			ID  uuid.UUID `json:"id"`
			Key string    `json:"key"`
		} `json:"requirement"`
		FromKey string `json:"fromKey"`
		ToKey   string `json:"toKey"`
	}
	if !slices.Contains(WebhookTopics, topic) {
		return nil, uuid.Nil
	}
	_ = json.Unmarshal(payload, &p)
	for _, raw := range []string{p.Requirement.Key, p.FromKey, p.ToKey} {
		if key, ok := NormalizeKey(raw); ok && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys, p.Requirement.ID
}

// Webhook verifies one delivery and clears what it announces; a replay of an
// event already acted on changes nothing. The payload is read as the
// webhook's owner in Exciter, so nothing of it is ever shown.
func (s *Service) Webhook(ctx context.Context, body []byte, h WebhookHeaders) error {
	var (
		site   uuid.UUID
		sealed []byte
		found  bool
	)
	// As the admin role: nobody signs in, and the sender is only known once
	// the secret has vouched for the body.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT s.id, c.webhook_secret
			FROM site s CROSS JOIN exciter_connection c
			WHERE s.archived_at IS NULL AND c.webhook_secret IS NOT NULL`,
		).Scan(&site, &sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the Exciter webhook secret: %w", err)
		}
		found = true
		return nil
	})
	if err != nil {
		return err
	}
	secret := absentSecret
	if found {
		if secret, err = s.OpenWebhookSecret(site, sealed); err != nil {
			s.opts.Log.Warn("a sealed Exciter webhook secret could not be opened", "site_id", site, "error", err)
			secret, found = absentSecret, false
		}
	}
	if !armature.VerifySignature(body, secret, h.Signature) || !found {
		return ErrBadSignature
	}
	// Exciter signs the time of every attempt, so one without it is no
	// delivery of Exciter's, and one outside the window is a replay.
	signed, fresh := armature.VerifyTimestamp(body, secret, h.Timestamp, h.TimestampedSignature, time.Now())
	if !signed {
		return ErrBadSignature
	}
	if !fresh {
		s.opts.Log.Warn("a signed Exciter webhook delivery names a time too far from this server's clock; a replay, or a clock that is wrong",
			"site_id", site, "timestamp", h.Timestamp, "tolerance", armature.WebhookTimestampTolerance)
		return ErrWebhookStale
	}
	if h.Version != "" && h.Version != strconv.Itoa(WebhookVersion) {
		s.opts.Log.Warn("a signed Exciter webhook delivery is in an envelope version Stator does not read; register the webhook in Exciter for version 1",
			"site_id", site, "version", h.Version, "reads", WebhookVersion)
		return ErrWebhookVersion
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.ID == uuid.Nil {
		s.opts.Log.Info("a signed webhook delivery is not Exciter's envelope; ignored", "site_id", site, "error", err)
		return nil
	}
	if env.Version != WebhookVersion {
		s.opts.Log.Warn("a signed Exciter webhook delivery is in an envelope version Stator does not read; register the webhook in Exciter for version 1",
			"site_id", site, "version", env.Version, "reads", WebhookVersion)
		return ErrWebhookVersion
	}
	if env.Source != WebhookSource {
		s.opts.Log.Info("a signed webhook delivery names another source than Exciter; ignored", "site_id", site, "source", env.Source)
		return nil
	}
	version := env.Version
	s.cache.PutReceipt(ctx, site, armature.WebhookReceipt{ReceivedAt: time.Now().UTC(), Topic: env.Topic, Version: &version, Timestamped: true})
	keys, id := WebhookClears(env.Topic, env.Payload)
	if len(keys) == 0 && id == uuid.Nil {
		return nil
	}
	if !s.cache.FirstDelivery(ctx, site, env.ID) {
		return nil
	}
	// Any change may change what a list's query finds, which no key tells.
	forget := make([]string, 0, len(keys)+3)
	forget = append(forget, s.listsKey(site))
	if id != uuid.Nil {
		for _, former := range s.cache.Members(ctx, s.idKey(site, id)) {
			if !slices.Contains(keys, former) {
				keys = append(keys, former)
			}
		}
		forget = append(forget, s.idKey(site, id))
	}
	for _, key := range keys {
		forget = append(forget, s.requirementKey(site, key))
	}
	s.cache.Forget(ctx, forget...)
	return nil
}
