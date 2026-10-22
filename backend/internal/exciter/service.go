package exciter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// WebhookPath is where Exciter posts the site's events, under the app's origin.
const WebhookPath = "/api/v1/exciter/webhook"

var (
	// ErrNotConfigured is a site without an Exciter connection.
	ErrNotConfigured = errors.New("the site has not connected Exciter")
	// ErrNotConnected is a caller who stored no token.
	ErrNotConnected = errors.New("the caller has not connected their Exciter account")
	// ErrUnreachable is Exciter not answering a call a write waits for.
	ErrUnreachable = errors.New("Exciter could not be reached")

	webhookSecretShape = regexp.MustCompile(`^` + WebhookSecretPrefix + `[A-Za-z0-9_-]{43}$`)
)

// Options are the settings of the process the service needs.
type Options struct {
	// AppURL is Stator's own origin, for the webhook address.
	AppURL string
	// WebhookURL is where Exciter reaches Stator's webhook receiver, when
	// that is not under AppURL; empty is AppURL's.
	WebhookURL string
	// TokenExchange connects people at sign-in where Exciter offers it.
	TokenExchange bool
	// Allow is what STATOR_OUTBOUND_ALLOW names; such a host may be plain http.
	Allow netguard.Allow
	// Development lets any base URL be plain http.
	Development bool
	Log         *slog.Logger
}

// Service keeps the site's connection and each member's token, and hands
// later work the caller as Exciter knows them. Calls go through Armature's
// client, which is Stator's one way out to a partner.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *armature.Client
	cache  *armature.Cache
	opts   Options
	// partners keeps what each Exciter said at /meta; see Partner.
	partners *partners
}

// NewService returns the service. A nil box refuses to store a token or a
// secret; a nil cache keeps nothing. The cache is NewCache's.
func NewService(cluster *db.Cluster, box *secret.Box, client *armature.Client, cache *armature.Cache, opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Service{db: cluster, box: box, client: client, cache: cache, opts: opts,
		partners: &partners{entries: map[string]partnerEntry{}, now: time.Now}}
}

// NewCache is the per-person answer cache, Armature's kept under CachePrefix;
// nil for a nil store.
func NewCache(store redis.UniversalClient, log *slog.Logger) *armature.Cache {
	return armature.NewCacheFor(store, log, CachePrefix, "Exciter")
}

func (s *Service) endpoint(ctx context.Context, tx db.DBTX) (string, error) {
	var base string
	err := tx.QueryRow(ctx, `SELECT base_url FROM exciter_endpoint()`).Scan(&base)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the Exciter connection: %w", err)
	}
	return base, nil
}

// Address is where people open the connected Exciter, empty when none is
// configured; any member may read it, guests included.
func (s *Service) Address(ctx context.Context) (string, error) {
	var base string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		base, err = s.endpoint(ctx, tx)
		return err
	})
	return base, err
}

// Connection is the site's connection for an administrator, or nil. It asks
// Exciter afresh what it offers, so the settings page shows an upgrade at once.
func (s *Service) Connection(ctx context.Context) (*Connection, error) {
	var (
		out  *Connection
		site uuid.UUID
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if out, err = s.readConnection(ctx, tx); err != nil || out == nil {
			return err
		}
		site, err = instance.ID(ctx, tx)
		return err
	})
	if out != nil && err == nil {
		out.Partner = s.Partner(ctx, out.BaseURL, true)
		s.noteApp(ctx, out, false)
		out.LastDelivery, _ = s.cache.Receipt(ctx, site)
	}
	return out, err
}

// webhookURL is the receiver address Stator names for Exciter's webhook.
func (s *Service) webhookURL() string {
	if s.opts.WebhookURL != "" {
		return s.opts.WebhookURL
	}
	return s.opts.AppURL + WebhookPath
}

func (s *Service) readConnection(ctx context.Context, tx db.DBTX) (*Connection, error) {
	c := Connection{WebhookURL: s.webhookURL(), WebhookTopics: WebhookTopics}
	var connected *int
	err := tx.QueryRow(ctx, `
		SELECT base_url, webhook_secret IS NOT NULL, updated_at, exciter_connected_count()
		FROM exciter_connection`,
	).Scan(&c.BaseURL, &c.WebhookSecretSet, &c.UpdatedAt, &connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Exciter connection: %w", err)
	}
	if connected != nil {
		c.Connected = *connected
	}
	if c.App, err = connectionApp(ctx, tx); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConnection sets the site's Exciter. A new address forgets every stored
// token in the same transaction.
func (s *Service) SaveConnection(ctx context.Context, in ConnectionInput, ip string) (*Connection, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	base, err := s.normalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	plain := ""
	secretChange := "kept"
	if in.WebhookSecret != nil {
		secretChange = "removed"
		if plain = strings.TrimSpace(*in.WebhookSecret); plain != "" {
			if !webhookSecretShape.MatchString(plain) {
				return nil, 0, &armature.FieldError{Field: "webhookSecret", Message: "That is not an Exciter webhook secret. Paste the secret Exciter showed when you added the webhook; it starts with " + WebhookSecretPrefix + "."}
			}
			secretChange = "set"
		}
	}

	var before *Connection
	var siteID uuid.UUID
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if siteID, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		before, err = s.readConnection(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	var sealed []byte
	if plain != "" {
		if sealed, err = s.seal(plain, webhookContext(siteID), "webhookSecret"); err != nil {
			return nil, 0, err
		}
	}
	moved := before == nil || before.BaseURL != base
	if moved {
		if _, err := s.probe(ctx, base); err != nil {
			return nil, 0, err
		}
	}

	var out *Connection
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		forgotten := 0
		if moved && before != nil {
			if err := tx.QueryRow(ctx, `SELECT coalesce(exciter_connected_count(), 0)`).Scan(&forgotten); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO exciter_connection (base_url, webhook_secret, updated_by)
			VALUES ($1, $2, $3)
			ON CONFLICT (singleton) DO UPDATE SET
			    base_url = EXCLUDED.base_url,
			    webhook_secret = CASE WHEN $4 THEN exciter_connection.webhook_secret ELSE EXCLUDED.webhook_secret END,
			    updated_by = EXCLUDED.updated_by`,
			base, sealed, nullable(actor), in.WebhookSecret == nil)
		if err != nil {
			return fmt.Errorf("save the Exciter connection: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionExciterConnectionSaved, TargetType: "exciter_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "webhookSecret": secretChange, "moved": moved, "tokensForgotten": forgotten},
		}); err != nil {
			return err
		}
		out, err = s.readConnection(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Partner = s.Partner(ctx, base, !moved)
	out.LastDelivery, _ = s.cache.Receipt(ctx, siteID)
	return out, lsn, nil
}

// RemoveConnection disconnects Exciter; every stored token goes with it.
func (s *Service) RemoveConnection(ctx context.Context, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var forgotten int
		if err := tx.QueryRow(ctx, `SELECT coalesce(exciter_connected_count(), 0)`).Scan(&forgotten); err != nil {
			return err
		}
		var base string
		err := tx.QueryRow(ctx, `DELETE FROM exciter_connection RETURNING base_url`).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove the Exciter connection: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionExciterConnectionRemoved, TargetType: "exciter_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "tokensForgotten": forgotten},
		})
	})
}

// CheckConnection says whether an Exciter answers at the address, and what it
// offers, without storing anything; a refusal is a FieldError on baseUrl.
func (s *Service) CheckConnection(ctx context.Context, in ConnectionCheckInput) (*ConnectionCheck, error) {
	base, err := s.normalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	partner, err := s.probe(ctx, base)
	if err != nil {
		return nil, err
	}
	return &ConnectionCheck{BaseURL: base, Partner: partner}, nil
}

// probe asks base at /meta whether it is an Exciter, and keeps what it offers.
func (s *Service) probe(ctx context.Context, base string) (Partner, error) {
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	meta, err := s.client.Meta(ctx, base)
	var refused *armature.RefusedError
	notExciter := errors.New("not an Exciter")
	switch {
	case err == nil && !strings.EqualFold(meta.Product, MetaProduct):
		err = notExciter
	case errors.Is(err, armature.ErrNoMeta), errors.As(err, &refused), errors.Is(err, armature.ErrRejected), errors.Is(err, armature.ErrUnreadable):
		err = notExciter
	}
	switch {
	case err == nil:
	case errors.Is(err, netguard.ErrBlocked):
		s.opts.Log.Info("the SSRF guard refused an Exciter address", "base_url", base, "error", err)
		host := armature.HostOf(base)
		return Partner{}, &armature.FieldError{Field: "baseUrl", Message: armature.BlockedSentenceFor("Exciter", host, armature.Resolve(ctx, host))}
	case errors.Is(err, notExciter):
		return Partner{}, &armature.FieldError{Field: "baseUrl", Message: "Something answers at this address, but it is not Exciter. Check the address, and leave out any path such as /api/v1."}
	default:
		s.opts.Log.Info("Exciter did not answer at an address being connected", "base_url", base, "error", err)
		return Partner{}, &armature.FieldError{Field: "baseUrl", Message: "Exciter did not answer at this address. Check the address, and that Exciter is running, then try again."}
	}
	p := partnerOf(meta, nil)
	s.partners.put(base, p)
	return p, nil
}

// normalizeBaseURL keeps the origin only: Exciter's API is always under
// /api/v1 of it, and its requirements under /p/.
func (s *Service) normalizeBaseURL(raw string) (string, error) {
	invalid := &armature.FieldError{Field: "baseUrl", Message: "Enter the address people open Exciter at, such as https://exciter.example.com, without a path."}
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") || (u.Path != "" && u.Path != "/") {
		return "", invalid
	}
	if u.Scheme == "http" && !s.opts.Development && !s.opts.Allow.Names(u.Hostname()) {
		return "", &armature.FieldError{Field: "baseUrl", Message: "Use an https address for Exciter, so the members' tokens travel encrypted."}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// Account is the caller's own link to Exciter.
func (s *Service) Account(ctx context.Context) (*Account, error) {
	var out *Account
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.account(ctx, tx)
		return err
	})
	s.noteMissing(ctx, out, false)
	return out, err
}

// noteMissing lists what the account's Exciter does not offer, outside any
// transaction, since asking Exciter may take a while.
func (s *Service) noteMissing(ctx context.Context, a *Account, fresh bool) {
	if a == nil || a.BaseURL == nil {
		return
	}
	a.Missing = s.Partner(ctx, *a.BaseURL, fresh).Missing
}

func (s *Service) account(ctx context.Context, tx db.DBTX) (*Account, error) {
	base, err := s.endpoint(ctx, tx)
	if err != nil {
		return nil, err
	}
	if base == "" {
		return &Account{Status: StatusNotConfigured, Missing: []MissingFeature{}}, nil
	}
	a := &Account{Configured: true, BaseURL: &base, Status: StatusNotConnected, Missing: []MissingFeature{}}
	if user, ok := db.UserFrom(ctx); ok {
		if a.SignInRefusal, err = signInRefusalOf(ctx, tx, user); err != nil {
			return nil, err
		}
	}
	row, err := s.ownToken(ctx, tx)
	if err != nil || row == nil {
		return a, err
	}
	via := ViaToken
	if row.Refresh != nil {
		via = ViaSignIn
	}
	a.Connected, a.Via, a.Status, a.User, a.CheckedAt = true, &via, row.Status, row.User, &row.CheckedAt
	return a, nil
}

// tokenRow is the caller's stored token, still sealed.
type tokenRow struct {
	ID uuid.UUID
	// SiteID is part of what the token was sealed with.
	SiteID    uuid.UUID
	Sealed    []byte
	Status    Status
	User      *armature.AccountUser
	CheckedAt time.Time
	// Refresh and AccessExpires are set for a grant the exchange at sign-in
	// stored, whose token is its access token.
	Refresh       []byte
	AccessExpires *time.Time
}

const tokenColumns = `id, (SELECT id FROM site), token, status, exciter_user_id, exciter_user_name, exciter_user_email, checked_at,
	refresh_token, access_expires_at`

func scanToken(r pgx.Row) (*tokenRow, error) {
	var (
		row               tokenRow
		exciterUser       *uuid.UUID
		exciterName, mail *string
	)
	err := r.Scan(&row.ID, &row.SiteID, &row.Sealed, &row.Status, &exciterUser, &exciterName, &mail, &row.CheckedAt,
		&row.Refresh, &row.AccessExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Exciter token: %w", err)
	}
	if exciterUser != nil {
		row.User = &armature.AccountUser{ID: *exciterUser, Name: deref(exciterName), Email: deref(mail)}
	}
	return &row, nil
}

func (s *Service) ownToken(ctx context.Context, tx db.DBTX) (*tokenRow, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, ErrNotConnected
	}
	return scanToken(tx.QueryRow(ctx, `SELECT `+tokenColumns+` FROM exciter_token WHERE user_id = $1`, user))
}

// Connect stores the caller's token once Exciter accepts it.
func (s *Service) Connect(ctx context.Context, token string) (*Account, db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, 0, ErrNotConnected
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) || len(token) == len(TokenPrefix) {
		return nil, 0, &armature.FieldError{Field: "token", Message: "That is not an Exciter token. Make one under Your account, Access tokens in Exciter and paste it here; it starts with " + TokenPrefix + "."}
	}
	var base string
	var siteID uuid.UUID
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if siteID, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		base, err = s.endpoint(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if base == "" {
		return nil, 0, ErrNotConfigured
	}

	me, err := s.me(ctx, base, token)
	switch {
	case errors.Is(err, armature.ErrUnreachable):
		return nil, 0, fmt.Errorf("%w: %w", ErrUnreachable, err)
	case err != nil:
		return nil, 0, &armature.FieldError{Field: "token", Message: "Exciter did not accept this token. Make a new one under Your account, Access tokens in Exciter and paste it here."}
	}
	sealed, err := s.seal(token, tokenContext(siteID, user), "token")
	if err != nil {
		return nil, 0, err
	}

	var (
		out  *Account
		gone *tokenRow
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A new row rather than an update, so the row id that keys the
		// person's cache is new with the token.
		var err error
		if gone, err = scanToken(tx.QueryRow(ctx, `DELETE FROM exciter_token WHERE user_id = $1 RETURNING `+tokenColumns, user)); err != nil {
			return fmt.Errorf("forget the old Exciter token: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exciter_sign_in WHERE user_id = $1`, user); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO exciter_token (user_id, token, exciter_user_id, exciter_user_name, exciter_user_email, status, checked_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())`,
			user, sealed, me.ID, me.Name, me.Email, StatusOK); err != nil {
			return fmt.Errorf("store the Exciter token: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	s.revokeGrant(ctx, base, gone)
	s.noteMissing(ctx, out, false)
	return out, lsn, nil
}

// Check asks Exciter now whether the caller's stored token still works, and
// records what it said; ok clears a rejected mark.
func (s *Service) Check(ctx context.Context) (*Account, db.LSN, error) {
	var (
		base string
		row  *tokenRow
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if base, err = s.endpoint(ctx, tx); err != nil || base == "" {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if base == "" || row == nil {
		out, err := s.Account(db.PinPrimary(ctx))
		return out, 0, err
	}

	status, user := StatusRejected, row.User
	me, err := s.checkRow(ctx, base, row)
	switch {
	case errors.Is(err, armature.ErrUnreachable):
		status = StatusUnreachable
	case err == nil:
		status, user = StatusOK, me
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id, name, email any
		if user != nil {
			id, name, email = user.ID, user.Name, user.Email
		}
		if _, err := tx.Exec(ctx, `
			UPDATE exciter_token SET status = $2, checked_at = now(),
			    exciter_user_id = coalesce($3, exciter_user_id),
			    exciter_user_name = coalesce($4, exciter_user_name),
			    exciter_user_email = coalesce($5, exciter_user_email)
			WHERE id = $1`, row.ID, status, id, name, email); err != nil {
			return fmt.Errorf("record the Exciter check: %w", err)
		}
		var err error
		out, err = s.account(ctx, tx)
		return err
	})
	s.noteMissing(ctx, out, true)
	return out, lsn, err
}

// checkRow asks Exciter who a stored token or grant acts as; a grant is
// renewed first when it runs out, as any call with it is.
func (s *Service) checkRow(ctx context.Context, base string, row *tokenRow) (*armature.AccountUser, error) {
	if row.Refresh == nil {
		token, err := s.open(ctx, row)
		if err != nil {
			return nil, armature.ErrRejected
		}
		return s.me(ctx, base, token)
	}
	caller, err := s.grantCaller(ctx, base, row)
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		User armature.AccountUser `json:"user"`
	}
	if err := caller.Get(call, "/auth/me", nil, &answer); err != nil {
		return nil, err
	}
	return &answer.User, nil
}

// Disconnect forgets the caller's token or grant, and revokes a grant at
// Exciter so it ends there too; forgetting none is not an error.
func (s *Service) Disconnect(ctx context.Context) (db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return 0, ErrNotConnected
	}
	var (
		base string
		gone *tokenRow
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if base, err = s.endpoint(ctx, tx); err != nil {
			return err
		}
		gone, err = scanToken(tx.QueryRow(ctx, `DELETE FROM exciter_token WHERE user_id = $1 RETURNING `+tokenColumns, user))
		return err
	})
	if err == nil {
		s.revokeGrant(ctx, base, gone)
	}
	return lsn, err
}

// Viewer is the caller as Exciter knows them. TokenID keys their entries in
// the cache, and Caller makes every call with their own token.
type Viewer struct {
	SiteID  uuid.UUID
	UserID  uuid.UUID
	TokenID uuid.UUID
	Caller  *armature.Caller
}

// Viewer returns the person ctx acts for, ready to call Exciter, or the
// status saying why not: not_configured, not_connected or rejected.
func (s *Service) Viewer(ctx context.Context) (*Viewer, Status, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, StatusNotConnected, nil
	}
	var (
		base string
		row  *tokenRow
	)
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if base, err = s.endpoint(ctx, tx); err != nil || base == "" {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, "", err
	}
	switch {
	case base == "":
		return nil, StatusNotConfigured, nil
	case row == nil:
		return nil, StatusNotConnected, nil
	case row.Status == StatusRejected:
		return nil, StatusRejected, nil
	}
	if row.Refresh != nil {
		caller, err := s.grantCaller(ctx, base, row)
		if err != nil {
			return nil, armature.StatusOf(err), nil
		}
		return &Viewer{SiteID: row.SiteID, UserID: user, TokenID: row.ID, Caller: caller}, StatusOK, nil
	}
	token, err := s.open(ctx, row)
	if err != nil {
		return nil, StatusRejected, nil
	}
	return &Viewer{SiteID: row.SiteID, UserID: user, TokenID: row.ID, Caller: s.client.As(base, token)}, StatusOK, nil
}

// noteRejected marks the viewer's token rejected after Exciter answered it
// 401, so it is not sent again until its owner stores a new one or checks it.
func (s *Service) noteRejected(ctx context.Context, v *Viewer) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE exciter_token SET status = $2, checked_at = now() WHERE id = $1`, v.TokenID, StatusRejected)
		return err
	})
	return err
}

// me asks Exciter's GET /auth/me who the token acts as.
func (s *Service) me(ctx context.Context, base, token string) (*armature.AccountUser, error) {
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		User armature.AccountUser `json:"user"`
	}
	err := s.client.As(base, token).Get(ctx, "/auth/me", nil, &answer)
	if errors.Is(err, armature.ErrUnreachable) {
		s.opts.Log.Info("Exciter did not answer a token check", "base_url", base, "error", err)
	}
	if err != nil {
		return nil, err
	}
	return &answer.User, nil
}

func (s *Service) seal(value string, context []byte, field string) ([]byte, error) {
	sealed, err := s.box.Seal([]byte(value), context)
	if errors.Is(err, secret.ErrNoKey) {
		return nil, &armature.FieldError{Field: field, Message: "The server has no STATOR_SECRET_KEY, so it cannot store this. Ask the operator to set one and try again."}
	}
	return sealed, err
}

func (s *Service) open(ctx context.Context, row *tokenRow) (string, error) {
	user, _ := db.UserFrom(ctx)
	plain, err := s.box.Open(row.Sealed, tokenContext(row.SiteID, user))
	if err != nil {
		s.opts.Log.Warn("a stored Exciter token could not be opened", "token_id", row.ID, "error", err)
		return "", err
	}
	return string(plain), nil
}

// tokenContext binds a sealed token to its site and person, so a copy moved
// onto another row does not open.
func tokenContext(site, user uuid.UUID) []byte {
	return append(append([]byte("exciter.token:"), site[:]...), user[:]...)
}

// webhookContext binds the webhook secret to its site.
func webhookContext(site uuid.UUID) []byte {
	return append([]byte("exciter.webhook:"), site[:]...)
}

// OpenWebhookSecret opens the site's sealed webhook secret, for the receiver.
func (s *Service) OpenWebhookSecret(site uuid.UUID, sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed, webhookContext(site))
	return string(plain), err
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
