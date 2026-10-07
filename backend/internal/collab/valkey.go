package collab

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ValkeyBus passes frames between api processes over Valkey's publish and
// subscribe, one channel per page under a prefix.
type ValkeyBus struct {
	client *redis.Client
	prefix string
}

// NewValkeyBus speaks on channels named prefix plus a page id.
func NewValkeyBus(client *redis.Client, prefix string) *ValkeyBus {
	return &ValkeyBus{client: client, prefix: prefix}
}

func (b *ValkeyBus) Publish(ctx context.Context, page uuid.UUID, envelope []byte) error {
	return b.client.Publish(ctx, b.prefix+page.String(), envelope).Err()
}

// Listen subscribes to every page's channel and hands what arrives to the
// hub until ctx ends. It returns once the subscription stands, so no
// connection is served before this process can hear the others.
func (b *ValkeyBus) Listen(ctx context.Context, hub *Hub) error {
	sub := b.client.PSubscribe(ctx, b.prefix+"*")
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return fmt.Errorf("subscribe to the shared drafts' channel: %w", err)
	}
	go func() {
		defer func() { _ = sub.Close() }()
		// The client subscribes again by itself after losing Valkey, and says
		// so with a subscription message, which is when to catch up.
		messages := sub.ChannelWithSubscriptions()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-messages:
				if !ok {
					return
				}
				switch m := m.(type) {
				case *redis.Subscription:
					hub.CatchUp()
				case *redis.Message:
					page, err := uuid.Parse(strings.TrimPrefix(m.Channel, b.prefix))
					if err != nil {
						continue
					}
					hub.Receive(page, []byte(m.Payload))
				}
			}
		}
	}()
	return nil
}
