package armature

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// ErrBadSignature is any delivery that cannot be trusted, one sent to an old
// address naming another site included.
var ErrBadSignature = errors.New("the webhook signature does not match")

// ErrWebhookVersion is a signed delivery in an envelope version the receiver
// does not read; Armature keeps it in the endpoint's log and tries again.
var ErrWebhookVersion = errors.New("the webhook envelope version is not one Stator reads")

// Sign is Armature's webhook.Sign: sha256= and the lower case hex HMAC-SHA256
// of the raw body under the secret.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature compares header with the body's signature in constant time.
func VerifySignature(body []byte, secret, header string) bool {
	return hmac.Equal([]byte(Sign(body, secret)), []byte(header))
}

// absentSecret is signed against when there is no secret to check, so an
// unconnected site costs the same work as a connected one.
const absentSecret = WebhookSecretPrefix + "absent"

// WebhookClears says which issue keys a delivery clears, and whether it
// clears every search; an unknown topic clears nothing.
func WebhookClears(topic string, payload json.RawMessage) (keys []string, searches bool) {
	var p struct {
		Key       string `json:"key"`
		MovedFrom string `json:"movedFrom"`
	}
	_ = json.Unmarshal(payload, &p)
	add := func(raw string) {
		if key, ok := NormalizeKey(raw); ok {
			keys = append(keys, key)
		}
	}
	switch topic {
	case "issue.created", "issue.transitioned":
		add(p.Key)
		searches = true
	case "issue.updated":
		add(p.Key)
		add(p.MovedFrom)
		searches = true
	case "comment.added":
		add(p.Key)
	}
	return keys, searches
}

// webhookTarget is what the receiver needs of the connection.
type webhookTarget struct {
	// SiteID is what the secret was sealed with, and keys the cache.
	SiteID        uuid.UUID
	Sealed        []byte
	ArmatureOrgID *uuid.UUID
}

// Webhook verifies one delivery and clears what it announces; a replay of an
// event already acted on changes nothing.
func (s *Service) Webhook(ctx context.Context, body []byte, signature string) error {
	var target *webhookTarget
	// As the admin role: nobody signs in, and the sender is only known once
	// the secret has vouched for the body.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var t webhookTarget
		err := tx.QueryRow(ctx, `
			SELECT s.id, c.webhook_secret, c.armature_org_id
			FROM site s CROSS JOIN armature_connection c
			WHERE s.archived_at IS NULL AND c.webhook_secret IS NOT NULL`,
		).Scan(&t.SiteID, &t.Sealed, &t.ArmatureOrgID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the webhook secret: %w", err)
		}
		target = &t
		return nil
	})
	if err != nil {
		return err
	}
	secret := absentSecret
	if target != nil {
		if secret, err = s.OpenWebhookSecret(target.SiteID, target.Sealed); err != nil {
			s.opts.Log.Warn("a sealed webhook secret could not be opened", "site_id", target.SiteID, "error", err)
			secret = absentSecret
			target = nil
		}
	}
	if !VerifySignature(body, secret, signature) || target == nil {
		return ErrBadSignature
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.ID == uuid.Nil {
		s.opts.Log.Info("a signed webhook delivery is not Armature's envelope; ignored", "site_id", target.SiteID, "error", err)
		return nil
	}
	if env.Version != 0 && env.Version != WebhookVersion {
		s.opts.Log.Warn("a signed webhook delivery is in an envelope version Stator does not read; register the endpoint in Armature for this version",
			"site_id", target.SiteID, "version", env.Version, "reads", WebhookVersion)
		return ErrWebhookVersion
	}
	if target.ArmatureOrgID != nil && env.OrgID != *target.ArmatureOrgID {
		return ErrBadSignature
	}
	keys, searches := WebhookClears(env.Topic, env.Payload)
	if len(keys) == 0 && !searches {
		return nil
	}
	if !s.cache.FirstDelivery(ctx, target.SiteID, env.ID) {
		return nil
	}
	s.cache.ForgetIssues(ctx, target.SiteID, keys...)
	if searches {
		s.cache.ForgetSearches(ctx, target.SiteID)
	}
	return nil
}
