package armature

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// ErrBadSignature is any delivery that cannot be trusted, one sent to an old
// address naming another site included.
var ErrBadSignature = errors.New("the webhook signature does not match")

// ErrWebhookStale is a signed delivery whose signed time is too far from this
// server's clock: a replay, or a clock that is wrong on one side.
var ErrWebhookStale = errors.New("the webhook delivery was signed too long ago")

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

// SignTimestamped is Armature's webhook.SignTimestamped: Sign over the time
// in Unix seconds, a dot and the raw body.
func SignTimestamped(body []byte, secret string, at time.Time) string {
	return Sign(append([]byte(strconv.FormatInt(at.Unix(), 10)+"."), body...), secret)
}

// VerifyTimestamp says whether the timestamped signature matches the time the
// timestamp header names and the body, and whether that time is within
// WebhookTimestampTolerance of now; a time that is no Unix seconds signs nothing.
func VerifyTimestamp(body []byte, secret, timestamp, header string, now time.Time) (signed, fresh bool) {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp {
		return false, false
	}
	at := time.Unix(seconds, 0)
	drift := now.Sub(at)
	return hmac.Equal([]byte(SignTimestamped(body, secret, at)), []byte(header)),
		drift <= WebhookTimestampTolerance && drift >= -WebhookTimestampTolerance
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
func (s *Service) Webhook(ctx context.Context, body []byte, h WebhookHeaders) error {
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
	if !VerifySignature(body, secret, h.Signature) || target == nil {
		return ErrBadSignature
	}
	// An Armature from before #242 signs no time; one that does is held to
	// it, so a replay outside the window is refused rather than acted on.
	timestamped := h.Timestamp != "" || h.TimestampedSignature != ""
	if timestamped {
		signed, fresh := VerifyTimestamp(body, secret, h.Timestamp, h.TimestampedSignature, time.Now())
		if !signed {
			return ErrBadSignature
		}
		if !fresh {
			s.opts.Log.Warn("a signed webhook delivery names a time too far from this server's clock; a replay, or a clock that is wrong",
				"site_id", target.SiteID, "timestamp", h.Timestamp, "tolerance", WebhookTimestampTolerance)
			return ErrWebhookStale
		}
	}
	if h.Version != "" && h.Version != strconv.Itoa(WebhookVersion) {
		s.opts.Log.Warn("a signed webhook delivery is in an envelope version Stator does not read; register the endpoint in Armature for this version",
			"site_id", target.SiteID, "version", h.Version, "reads", WebhookVersion)
		return ErrWebhookVersion
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
	receipt := WebhookReceipt{ReceivedAt: time.Now().UTC(), Topic: env.Topic, Timestamped: timestamped}
	if env.Version != 0 {
		receipt.Version = &env.Version
	}
	s.cache.PutReceipt(ctx, target.SiteID, receipt)
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
