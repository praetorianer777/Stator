package db

import (
	"context"

	"github.com/google/uuid"
)

type (
	pinKey    struct{}
	systemKey struct{}
	userKey   struct{}
)

// UserVar is the setting the permission policies read through
// current_actor_id(): whom the transaction acts for, within its organization.
const UserVar = "app.user_id"

// WithUser names the person the transactions made with ctx act for, so row
// level security holds them to that person's permissions.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, id)
}

// UserFrom is the person WithUser named, if any.
func UserFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// pin describes a freshness requirement placed on reads made with a context.
type pin struct {
	// forcePrimary sends every read to the primary regardless of replica state.
	forcePrimary bool
	// requiredLSN is the WAL position a replica must have replayed before it
	// may serve this context's reads.
	requiredLSN LSN
}

// PinPrimary forces every read made with the returned context to the primary,
// for the rare read that must see the absolute latest state.
func PinPrimary(ctx context.Context) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	p.forcePrimary = true
	return context.WithValue(ctx, pinKey{}, p)
}

// PinLSN requires any replica serving this context to have replayed lsn, so a
// user never reads past their own write; otherwise the read goes to the primary.
func PinLSN(ctx context.Context, lsn LSN) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	if lsn > p.requiredLSN {
		p.requiredLSN = lsn
	}
	return context.WithValue(ctx, pinKey{}, p)
}

func pinFrom(ctx context.Context) pin {
	p, _ := ctx.Value(pinKey{}).(pin)
	return p
}

// WithSystem marks ctx as deliberately un-tenanted, for migrations, health
// checks and jobs that span tenants. Without it an unscoped query is refused.
func WithSystem(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemKey{}, true)
}

func isSystem(ctx context.Context) bool {
	v, _ := ctx.Value(systemKey{}).(bool)
	return v
}
