package armature

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

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// WebhookPath is where Armature posts an organization's events, under the
// app's origin; the organization's slug follows.
const WebhookPath = "/api/v1/armature/webhook/"

// Audit actions of the connection.
const (
	ActionConnectionSaved   = "armature.connection_saved"
	ActionConnectionRemoved = "armature.connection_removed"
)

var (
	// ErrNotConfigured is an organization without an Armature connection.
	ErrNotConfigured = errors.New("the organization has not connected Armature")
	// ErrNotConnected is a caller who stored no token.
	ErrNotConnected = errors.New("the caller has not connected their Armature account")

	slugShape          = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
	webhookSecretShape = regexp.MustCompile(`^` + WebhookSecretPrefix + `[A-Za-z0-9_-]{43}$`)
)

// FieldError is input that cannot be stored, with the sentence to show by it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Options are the settings of the process the service needs.
type Options struct {
	// AppURL is Stator's own origin, for the webhook address.
	AppURL string
	// Allow is what STATOR_OUTBOUND_ALLOW names; such a host may be plain http.
	Allow netguard.Allow
	// Development lets any base URL be plain http.
	Development bool
	Log         *slog.Logger
}

// Service keeps the organization's connection and each member's token, and
// hands later work the caller as Armature knows them.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *Client
	cache  *Cache
	opts   Options
}

// NewService returns the service. A nil box refuses to store a token or a
// secret; a nil cache keeps nothing.
func NewService(cluster *db.Cluster, box *secret.Box, client *Client, cache *Cache, opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Service{db: cluster, box: box, client: client, cache: cache, opts: opts}
}

// Cache is the per-person answer cache, nil without Valkey.
func (s *Service) Cache() *Cache { return s.cache }

// Client is the one outbound client.
func (s *Service) Client() *Client { return s.client }

// endpoint is what any member may know of the connection.
type endpoint struct {
	BaseURL       string
	OrgSlug       string
	ArmatureOrgID *uuid.UUID
}

func (s *Service) endpoint(ctx context.Context, tx db.DBTX) (*endpoint, error) {
	var e endpoint
	err := tx.QueryRow(ctx, `SELECT base_url, org_slug, armature_org_id FROM armature_endpoint()`).
		Scan(&e.BaseURL, &e.OrgSlug, &e.ArmatureOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature connection: %w", err)
	}
	return &e, nil
}

// Connection is the organization's connection for an administrator, or nil.
func (s *Service) Connection(ctx context.Context) (*Connection, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var out *Connection
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		out, err = s.readConnection(ctx, tx, org)
		return err
	})
	return out, err
}

func (s *Service) readConnection(ctx context.Context, tx db.DBTX, org tenant.Org) (*Connection, error) {
	c := Connection{WebhookURL: s.opts.AppURL + WebhookPath + org.Slug, WebhookTopics: WebhookTopics}
	var connected *int
	err := tx.QueryRow(ctx, `
		SELECT base_url, org_slug, armature_org_id, webhook_secret IS NOT NULL, updated_at, armature_connected_count()
		FROM armature_connection WHERE org_id = $1`, org.ID,
	).Scan(&c.BaseURL, &c.OrgSlug, &c.ArmatureOrgID, &c.WebhookSecretSet, &c.UpdatedAt, &connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature connection: %w", err)
	}
	if connected != nil {
		c.Connected = *connected
	}
	return &c, nil
}

// SaveConnection sets the organization's Armature. A new address or
// organization forgets every stored token in the same transaction.
func (s *Service) SaveConnection(ctx context.Context, in ConnectionInput, ip string) (*Connection, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	actor, _ := db.UserFrom(ctx)
	base, err := s.normalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	slug := strings.ToLower(strings.TrimSpace(in.OrgSlug))
	if !slugShape.MatchString(slug) {
		return nil, 0, &FieldError{Field: "orgSlug", Message: "Enter the slug of your organization in Armature, such as acme: 3 to 40 lower case letters, digits and hyphens."}
	}
	var sealed []byte
	secretChange := "kept"
	if in.WebhookSecret != nil {
		secretChange = "removed"
		if value := strings.TrimSpace(*in.WebhookSecret); value != "" {
			if !webhookSecretShape.MatchString(value) {
				return nil, 0, &FieldError{Field: "webhookSecret", Message: "That is not an Armature webhook secret. Paste the secret Armature showed when you added the endpoint; it starts with " + WebhookSecretPrefix + "."}
			}
			if sealed, err = s.seal(value, webhookContext(org.ID), "webhookSecret"); err != nil {
				return nil, 0, err
			}
			secretChange = "set"
		}
	}

	var before *Connection
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err = s.readConnection(ctx, tx, org)
		return err
	}); err != nil {
		return nil, 0, err
	}
	moved := before == nil || before.BaseURL != base || before.OrgSlug != slug
	if before == nil || before.BaseURL != base {
		if err := s.describe(ctx, base); err != nil {
			return nil, 0, err
		}
	}

	var out *Connection
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		forgotten := 0
		if moved && before != nil {
			if err := tx.QueryRow(ctx, `SELECT coalesce(armature_connected_count(), 0)`).Scan(&forgotten); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_connection (org_id, base_url, org_slug, webhook_secret, updated_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (org_id) DO UPDATE SET
			    base_url = EXCLUDED.base_url,
			    org_slug = EXCLUDED.org_slug,
			    webhook_secret = CASE WHEN $6 THEN armature_connection.webhook_secret ELSE EXCLUDED.webhook_secret END,
			    updated_by = EXCLUDED.updated_by`,
			org.ID, base, slug, sealed, nullable(actor), in.WebhookSecret == nil)
		if err != nil {
			return fmt.Errorf("save the Armature connection: %w", err)
		}
		if err := audit.Write(ctx, tx, org.ID, audit.Entry{
			Action: ActionConnectionSaved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "orgSlug": slug, "webhookSecret": secretChange, "moved": moved, "tokensForgotten": forgotten},
		}); err != nil {
			return err
		}
		out, err = s.readConnection(ctx, tx, org)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// RemoveConnection disconnects Armature; every stored token goes with it.
func (s *Service) RemoveConnection(ctx context.Context, ip string) (db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return 0, err
	}
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var forgotten int
		if err := tx.QueryRow(ctx, `SELECT coalesce(armature_connected_count(), 0)`).Scan(&forgotten); err != nil {
			return err
		}
		var base string
		err := tx.QueryRow(ctx, `DELETE FROM armature_connection WHERE org_id = $1 RETURNING base_url`, org.ID).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove the Armature connection: %w", err)
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{
			Action: ActionConnectionRemoved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "tokensForgotten": forgotten},
		})
	})
}

// normalizeBaseURL keeps the origin only: Armature's API is always under
// /api/v1 of it, and its issues under /issues.
func (s *Service) normalizeBaseURL(raw string) (string, error) {
	invalid := &FieldError{Field: "baseUrl", Message: "Enter the address people open Armature at, such as https://armature.example.com, without a path."}
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") || (u.Path != "" && u.Path != "/") {
		return "", invalid
	}
	if u.Scheme == "http" && !s.opts.Development && !s.opts.Allow.Names(u.Hostname()) {
		return "", &FieldError{Field: "baseUrl", Message: "Use an https address for Armature, so the members' tokens travel encrypted."}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// describe checks that an Armature answers at base before anybody's token is
// sent there.
func (s *Service) describe(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	err := s.client.Describe(ctx, base)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, netguard.ErrBlocked):
		s.opts.Log.Info("the SSRF guard refused an Armature address", "base_url", base, "error", err)
		return &FieldError{Field: "baseUrl", Message: "Stator may not reach this address, because it is inside the server's own network. Use Armature's public address, or ask the operator to add the host to STATOR_OUTBOUND_ALLOW."}
	case errors.Is(err, ErrNotArmature):
		return &FieldError{Field: "baseUrl", Message: "Something answers at this address, but it is not Armature. Check the address, and leave out any path such as /api/v1."}
	default:
		s.opts.Log.Info("Armature did not answer at an address being connected", "base_url", base, "error", err)
		return &FieldError{Field: "baseUrl", Message: "Armature did not answer at this address. Check the address, and that Armature is running, then save again."}
	}
}

// Account is the caller's own link to Armature.
func (s *Service) Account(ctx context.Context) (*Account, error) {
	var out *Account
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.account(ctx, tx)
		return err
	})
	return out, err
}

func (s *Service) account(ctx context.Context, tx db.DBTX) (*Account, error) {
	e, err := s.endpoint(ctx, tx)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return &Account{Status: StatusNotConfigured}, nil
	}
	a := &Account{Configured: true, BaseURL: &e.BaseURL, Status: StatusNotConnected}
	row, err := s.ownToken(ctx, tx)
	if err != nil || row == nil {
		return a, err
	}
	a.Connected, a.Status, a.User, a.CheckedAt = true, row.Status, row.User, &row.CheckedAt
	return a, nil
}

// tokenRow is the caller's stored token, still sealed.
type tokenRow struct {
	ID        uuid.UUID
	Sealed    []byte
	Status    Status
	User      *AccountUser
	CheckedAt time.Time
}

func (s *Service) ownToken(ctx context.Context, tx db.DBTX) (*tokenRow, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, ErrNotConnected
	}
	var (
		row                      tokenRow
		armatureUser             *uuid.UUID
		armatureName, armatureAt *string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, token, status, armature_user_id, armature_user_name, armature_user_email, checked_at
		FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user,
	).Scan(&row.ID, &row.Sealed, &row.Status, &armatureUser, &armatureName, &armatureAt, &row.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature token: %w", err)
	}
	if armatureUser != nil {
		row.User = &AccountUser{ID: *armatureUser, Name: deref(armatureName), Email: deref(armatureAt)}
	}
	return &row, nil
}

// Connect stores the caller's token once Armature accepts it as somebody in
// the connected organization.
func (s *Service) Connect(ctx context.Context, token string) (*Account, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, 0, ErrNotConnected
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) || len(token) == len(TokenPrefix) {
		return nil, 0, &FieldError{Field: "token", Message: "That is not an Armature token. Make one under Tokens in Armature and paste it here; it starts with " + TokenPrefix + "."}
	}
	var e *endpoint
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err = s.endpoint(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if e == nil {
		return nil, 0, ErrNotConfigured
	}

	me, err := s.me(ctx, e.BaseURL, token)
	switch {
	case errors.Is(err, ErrUnreachable):
		return nil, 0, err
	case err != nil:
		return nil, 0, &FieldError{Field: "token", Message: "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here."}
	case !e.owns(me):
		return nil, 0, &FieldError{Field: "token", Message: fmt.Sprintf("This token belongs to another Armature organization than %s. Make one while signed in to %s in Armature and paste it here.", e.OrgSlug, e.OrgSlug)}
	}
	sealed, err := s.seal(token, tokenContext(org.ID, user), "token")
	if err != nil {
		return nil, 0, err
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A new row rather than an update, so the row id that keys the
		// person's cache is new with the token.
		if _, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the old Armature token: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO armature_token (org_id, user_id, token, armature_user_id, armature_user_name, armature_user_email, status, checked_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, now())`,
			user, sealed, me.User.ID, me.User.Name, me.User.Email, StatusOK); err != nil {
			return fmt.Errorf("store the Armature token: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	s.learnOrg(ctx, org.ID, e, me)
	return out, lsn, nil
}

// Check asks Armature now whether the caller's stored token still works, and
// records what it said; ok clears a rejected mark.
func (s *Service) Check(ctx context.Context) (*Account, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	var (
		e   *endpoint
		row *tokenRow
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	if e == nil || row == nil {
		out, err := s.Account(db.PinPrimary(ctx))
		return out, 0, err
	}

	status, user := StatusRejected, row.User
	token, openErr := s.open(ctx, row, org.ID)
	if openErr == nil {
		me, err := s.me(ctx, e.BaseURL, token)
		switch {
		case errors.Is(err, ErrUnreachable):
			status = StatusUnreachable
		case err == nil && e.owns(me):
			status, user = StatusOK, &me.User
			s.learnOrg(ctx, org.ID, e, me)
		}
	}

	var out *Account
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id, name, email any
		if user != nil {
			id, name, email = user.ID, user.Name, user.Email
		}
		if _, err := tx.Exec(ctx, `
			UPDATE armature_token SET status = $2, checked_at = now(),
			    armature_user_id = coalesce($3, armature_user_id),
			    armature_user_name = coalesce($4, armature_user_name),
			    armature_user_email = coalesce($5, armature_user_email)
			WHERE id = $1`, row.ID, status, id, name, email); err != nil {
			return fmt.Errorf("record the Armature check: %w", err)
		}
		out, err = s.account(ctx, tx)
		return err
	})
	return out, lsn, err
}

// Disconnect forgets the caller's token; forgetting none is not an error.
func (s *Service) Disconnect(ctx context.Context) (db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return 0, ErrNotConnected
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE org_id = current_org_id() AND user_id = $1`, user)
		return err
	})
}

// Viewer is the caller as Armature knows them. TokenID keys their entries in
// the Cache, and Caller makes every call with their own token.
type Viewer struct {
	OrgID   uuid.UUID
	UserID  uuid.UUID
	TokenID uuid.UUID
	Caller  *Caller
}

// Viewer returns the person ctx acts for, ready to call Armature, or the
// status saying why not: not_configured, not_connected or rejected.
func (s *Service) Viewer(ctx context.Context) (*Viewer, Status, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, "", err
	}
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, StatusNotConnected, nil
	}
	var (
		e   *endpoint
		row *tokenRow
	)
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		row, err = s.ownToken(ctx, tx)
		return err
	}); err != nil {
		return nil, "", err
	}
	switch {
	case e == nil:
		return nil, StatusNotConfigured, nil
	case row == nil:
		return nil, StatusNotConnected, nil
	case row.Status == StatusRejected:
		return nil, StatusRejected, nil
	}
	token, err := s.open(ctx, row, org.ID)
	if err != nil {
		return nil, StatusRejected, nil
	}
	return &Viewer{OrgID: org.ID, UserID: user, TokenID: row.ID, Caller: s.client.As(e.BaseURL, token)}, StatusOK, nil
}

// NoteRejected marks the viewer's token rejected after Armature answered it
// 401, so it is not sent again until its owner stores a new one or checks it.
func (s *Service) NoteRejected(ctx context.Context, v *Viewer) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE armature_token SET status = $2, checked_at = now() WHERE id = $1`, v.TokenID, StatusRejected)
		return err
	})
	return err
}

func (s *Service) me(ctx context.Context, base, token string) (*Me, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	me, err := s.client.As(base, token).Me(ctx)
	if errors.Is(err, ErrUnreachable) {
		s.opts.Log.Info("Armature did not answer a token check", "base_url", base, "error", err)
	}
	return me, err
}

// owns says whether a token's identity belongs to the connected organization,
// by slug and, once learned, by id.
func (e *endpoint) owns(me *Me) bool {
	if me == nil || me.OrgSlug != e.OrgSlug {
		return false
	}
	return e.ArmatureOrgID == nil || *e.ArmatureOrgID == me.OrgID
}

// learnOrg keeps the Armature organization's id from the first token that
// checks out. It writes as the admin role, since a member's token teaches it,
// and only while the address is still the one that was asked.
func (s *Service) learnOrg(ctx context.Context, org uuid.UUID, e *endpoint, me *Me) {
	if e.ArmatureOrgID != nil || me.OrgID == uuid.Nil {
		return
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE armature_connection SET armature_org_id = $2
			WHERE org_id = $1 AND armature_org_id IS NULL AND base_url = $3 AND org_slug = $4`,
			org, me.OrgID, e.BaseURL, e.OrgSlug)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the Armature organization's id could not be kept", "org_id", org, "error", err)
	}
}

func (s *Service) seal(value string, context []byte, field string) ([]byte, error) {
	sealed, err := s.box.Seal([]byte(value), context)
	if errors.Is(err, secret.ErrNoKey) {
		return nil, &FieldError{Field: field, Message: "The server has no STATOR_SECRET_KEY, so it cannot store this. Ask the operator to set one and try again."}
	}
	return sealed, err
}

func (s *Service) open(ctx context.Context, row *tokenRow, org uuid.UUID) (string, error) {
	user, _ := db.UserFrom(ctx)
	plain, err := s.box.Open(row.Sealed, tokenContext(org, user))
	if err != nil {
		s.opts.Log.Warn("a stored Armature token could not be opened", "token_id", row.ID, "error", err)
		return "", err
	}
	return string(plain), nil
}

// tokenContext binds a sealed token to its organization and person, so a
// copy moved onto another row does not open.
func tokenContext(org, user uuid.UUID) []byte {
	return append(append([]byte("armature.token:"), org[:]...), user[:]...)
}

// webhookContext binds the webhook secret to its organization.
func webhookContext(org uuid.UUID) []byte {
	return append([]byte("armature.webhook:"), org[:]...)
}

// OpenWebhookSecret opens a sealed webhook secret of org, for the receiver.
func (s *Service) OpenWebhookSecret(org uuid.UUID, sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed, webhookContext(org))
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
