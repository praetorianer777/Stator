package db

import (
	"context"
	"encoding/hex"
	"strings"

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

// TokenSpacesVar is the setting that limits the person to the spaces of the
// token they act with, read by the permission functions; unset is no limit.
const TokenSpacesVar = "app.token_spaces"

// AnonymousVar is the setting that makes a transaction read for somebody
// who is not signed in, read by current_anonymous().
const AnonymousVar = "app.anonymous"

// LinkVar is the setting that holds the digest of the public link an
// anonymous reader reads through, read by current_link_digest().
const LinkVar = "app.page_link"

// acting is whom the transactions act for and, for a token limited to some
// spaces, which; one value so that naming another person drops the limit,
// and naming anybody drops anonymity.
type acting struct {
	user       uuid.UUID
	spacesOnly bool
	spaces     []uuid.UUID
	anonymous  bool
	// link is the hex digest of a public link's token, for an anonymous reader.
	link string
}

// WithUser names the person the transactions made with ctx act for, so row
// level security holds them to that person's permissions.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, acting{user: id})
}

// WithUserInSpaces is WithUser for a token that reaches only the spaces named,
// none when the list is empty.
func WithUserInSpaces(ctx context.Context, id uuid.UUID, spaces []uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, acting{user: id, spacesOnly: true, spaces: spaces})
}

// WithAnonymous makes the transactions made with ctx read for somebody who
// is not signed in, whom the database holds to the spaces open to anybody.
func WithAnonymous(ctx context.Context) context.Context {
	return context.WithValue(ctx, userKey{}, acting{anonymous: true})
}

// WithLink is WithAnonymous for a reader holding a public link, whom the
// database also lets read the page the link opens; digest is its token's.
func WithLink(ctx context.Context, digest []byte) context.Context {
	return context.WithValue(ctx, userKey{}, acting{anonymous: true, link: hex.EncodeToString(digest)})
}

// LinkFrom says whether WithLink made ctx.
func LinkFrom(ctx context.Context) bool {
	a, _ := ctx.Value(userKey{}).(acting)
	return a.anonymous && a.link != ""
}

// AnonymousFrom says whether WithAnonymous or WithLink made ctx.
func AnonymousFrom(ctx context.Context) bool {
	a, _ := ctx.Value(userKey{}).(acting)
	return a.anonymous
}

// UserFrom is the person WithUser named, if any.
func UserFrom(ctx context.Context) (uuid.UUID, bool) {
	a, ok := ctx.Value(userKey{}).(acting)
	return a.user, ok && a.user != uuid.Nil
}

// SpacesFrom is the spaces WithUserInSpaces limited the person to; ok is
// false when nothing limits them.
func SpacesFrom(ctx context.Context) (spaces []uuid.UUID, ok bool) {
	a, _ := ctx.Value(userKey{}).(acting)
	return a.spaces, a.spacesOnly
}

// spacesSetting spells the spaces as the Postgres array the setting holds.
func spacesSetting(spaces []uuid.UUID) string {
	parts := make([]string, len(spaces))
	for i, s := range spaces {
		parts[i] = s.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
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
