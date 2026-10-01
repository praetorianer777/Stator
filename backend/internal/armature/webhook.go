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

// ErrBadSignature is any delivery that cannot be trusted, an unknown
// organization included, so the receiver never says which ones exist.
var ErrBadSignature = errors.New("the webhook signature does not match")

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
// unknown organization costs the same work as a known one.
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

// webhookTarget is what the receiver needs of an organization by its slug.
type webhookTarget struct {
	OrgID         uuid.UUID
	Sealed        []byte
	ArmatureOrgID *uuid.UUID
}

// Webhook verifies one delivery to orgSlug and clears what it announces; a
// replay of an event already acted on changes nothing.
func (s *Service) Webhook(ctx context.Context, orgSlug string, body []byte, signature string) error {
	var target *webhookTarget
	// As the admin role: nobody signs in, and the organization is only
	// known once its secret has vouched for the body.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var t webhookTarget
		err := tx.QueryRow(ctx, `
			SELECT o.id, c.webhook_secret, c.armature_org_id
			FROM org o JOIN armature_connection c ON c.org_id = o.id
			WHERE o.slug = $1 AND o.archived_at IS NULL AND c.webhook_secret IS NOT NULL`, orgSlug,
		).Scan(&t.OrgID, &t.Sealed, &t.ArmatureOrgID)
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
		if secret, err = s.OpenWebhookSecret(target.OrgID, target.Sealed); err != nil {
			s.opts.Log.Warn("a sealed webhook secret could not be opened", "org_id", target.OrgID, "error", err)
			secret = absentSecret
			target = nil
		}
	}
	if !VerifySignature(body, secret, signature) || target == nil {
		return ErrBadSignature
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.ID == uuid.Nil {
		s.opts.Log.Info("a signed webhook delivery is not Armature's envelope; ignored", "org_id", target.OrgID, "error", err)
		return nil
	}
	if target.ArmatureOrgID != nil && env.OrgID != *target.ArmatureOrgID {
		return ErrBadSignature
	}
	keys, searches := WebhookClears(env.Topic, env.Payload)
	if len(keys) == 0 && !searches {
		return nil
	}
	if !s.cache.FirstDelivery(ctx, target.OrgID, env.ID) {
		return nil
	}
	s.cache.ForgetIssues(ctx, target.OrgID, keys...)
	if searches {
		s.cache.ForgetSearches(ctx, target.OrgID)
	}
	return nil
}
