package armature

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
)

// appRegistrationLock keeps two members connecting at once from registering
// Stator twice; any constant no other advisory lock uses does.
const appRegistrationLock = 428_0001

var (
	// ErrAppUnavailable is an Armature that does not offer connected apps.
	ErrAppUnavailable = errors.New("the connected Armature does not offer connected apps")
	// ErrOAuthAttempt is a return from Armature this session did not start,
	// or started too long ago.
	ErrOAuthAttempt = errors.New("the return from Armature belongs to no connect this session started")
	// ErrConnectDenied is the person choosing Cancel on Armature's page.
	ErrConnectDenied = errors.New("the person did not allow Stator in Armature")
	// ErrConnectEnded is a code Armature no longer redeems.
	ErrConnectEnded = errors.New("Armature no longer redeems the code")
	// ErrConnectFailed is any other refusal on the way back.
	ErrConnectFailed = errors.New("Armature did not connect the account")
	// ErrOtherInstallation is a grant of another Armature than the one connected.
	ErrOtherInstallation = errors.New("the grant belongs to another Armature installation")
)

// AppRefusedError is Armature refusing to register Stator, in its words.
type AppRefusedError struct{ Message string }

func (e *AppRefusedError) Error() string { return e.Message }

// registrationClosed is said for an Armature whose metadata names no
// registration endpoint, which it leaves out where registration is closed.
const registrationClosed = "This Armature registers connected apps only by hand. Ask an Armature administrator to add Stator under Settings, Connected apps, and a Stator administrator to paste its client id and secret under Settings, Armature."

// metadataCache keeps each Armature's metadata in the process.
type metadataCache struct {
	mu      sync.Mutex
	entries map[string]metadataEntry
}

type metadataEntry struct {
	at   time.Time
	meta *ServerMetadata
}

func (s *Service) serverMetadata(ctx context.Context, base string) (*ServerMetadata, error) {
	s.metadata.mu.Lock()
	e, ok := s.metadata.entries[base]
	s.metadata.mu.Unlock()
	if ok && time.Since(e.at) < MetadataTTL {
		return e.meta, nil
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	meta, err := s.client.ServerMetadata(ctx, base)
	if err != nil {
		return nil, err
	}
	s.metadata.mu.Lock()
	s.metadata.entries[base] = metadataEntry{at: time.Now(), meta: meta}
	s.metadata.mu.Unlock()
	return meta, nil
}

// OffersApp says whether the Armature at base is known to offer connected
// apps; unlike Offers, an Armature that did not answer is not given the
// benefit of the doubt, since the token form works either way.
func (p *Partner) OffersApp() bool {
	return p != nil && p.Status == StatusOK && slices.Contains(p.Capabilities, CapabilityPartnerTokens)
}

// RedirectURI is the one return address Stator registers.
func (s *Service) RedirectURI() string { return s.opts.AppURL + OAuthCallbackPath }

func appScopes() []string { return strings.Fields(AppScopes) }

// appRow is the app as stored, its secret still sealed.
type appRow struct {
	Status       AppStatus
	ClientID     *string
	Secret       []byte
	RegisteredBy AppRegistrar
	Scopes       []string
	Refusal      *string
}

func readApp(ctx context.Context, tx db.DBTX) (*appRow, error) {
	var a appRow
	err := tx.QueryRow(ctx, `SELECT status, client_id, client_secret, registered_by, scopes, refusal FROM armature_app`).
		Scan(&a.Status, &a.ClientID, &a.Secret, &a.RegisteredBy, &a.Scopes, &a.Refusal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature app: %w", err)
	}
	return &a, nil
}

func (s *Service) credentials(site uuid.UUID, a *appRow) (*AppCredentials, error) {
	if a == nil || a.Status == AppRefused || a.ClientID == nil {
		return nil, ErrConnectFailed
	}
	creds := &AppCredentials{ClientID: *a.ClientID}
	if len(a.Secret) > 0 {
		plain, err := s.box.Open(a.Secret, appContext(site))
		if err != nil {
			return nil, fmt.Errorf("open the Armature app's secret: %w", err)
		}
		creds.ClientSecret = string(plain)
	}
	return creds, nil
}

// appCredentials reads the app as the admin role: a member's connect and
// refresh need its secret, which stator_app keeps from members.
func (s *Service) appCredentials(ctx context.Context) (*AppCredentials, error) {
	var creds *AppCredentials
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		a, err := readApp(ctx, tx)
		if err != nil {
			return err
		}
		creds, err = s.credentials(site, a)
		return err
	})
	return creds, err
}

// ensureApp is the app, registered now when there is none. A refusal is
// kept with Armature's sentence, so members and administrators read it.
func (s *Service) ensureApp(ctx context.Context, meta *ServerMetadata, scopes string) (*AppCredentials, error) {
	var (
		creds   *AppCredentials
		refusal string
	)
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, appRegistrationLock); err != nil {
			return err
		}
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		a, err := readApp(ctx, tx)
		if err != nil {
			return err
		}
		if a != nil && a.Status != AppRefused {
			creds, err = s.credentials(site, a)
			return err
		}
		if meta.RegistrationEndpoint == "" {
			refusal = registrationClosed
			return s.storeApp(ctx, tx, AppRefused, nil, nil, RegisteredByStator, scopes, &refusal, uuid.Nil)
		}
		call, cancel := context.WithTimeout(ctx, CallTimeout)
		defer cancel()
		reg, err := s.client.Register(call, meta.RegistrationEndpoint, Registration{
			ClientName: AppName, ClientURI: s.opts.AppURL, RedirectURIs: []string{s.RedirectURI()}, Scope: scopes,
			TokenEndpointAuthMethod: "client_secret_basic",
			GrantTypes:              []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
		})
		var refused *OAuthError
		if errors.As(err, &refused) {
			refusal = strings.TrimSpace(refused.Description)
			if refusal == "" {
				refusal = registrationClosed
			}
			s.opts.Log.Info("Armature would not register Stator", "error", refused.Code, "description", refused.Description)
			return s.storeApp(ctx, tx, AppRefused, nil, nil, RegisteredByStator, scopes, &refusal, uuid.Nil)
		}
		if err != nil {
			return err
		}
		var sealed []byte
		if reg.ClientSecret != "" {
			if sealed, err = s.seal(reg.ClientSecret, appContext(site), "clientSecret"); err != nil {
				return err
			}
		}
		if err := s.storeApp(ctx, tx, AppPending, &reg.ClientID, sealed, RegisteredByStator, scopes, nil, uuid.Nil); err != nil {
			return err
		}
		creds = &AppCredentials{ClientID: reg.ClientID, ClientSecret: reg.ClientSecret}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		return nil, &AppRefusedError{Message: refusal}
	}
	return creds, nil
}

func (s *Service) storeApp(ctx context.Context, tx db.DBTX, status AppStatus, clientID *string, secret []byte, by AppRegistrar, scopes string, refusal *string, actor uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO armature_app (status, client_id, client_secret, registered_by, scopes, refusal, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (singleton) DO UPDATE SET
		    status = EXCLUDED.status, client_id = EXCLUDED.client_id, client_secret = EXCLUDED.client_secret,
		    registered_by = EXCLUDED.registered_by, scopes = EXCLUDED.scopes, refusal = EXCLUDED.refusal,
		    updated_by = EXCLUDED.updated_by`,
		status, clientID, secret, by, strings.Fields(scopes), refusal, nullable(actor))
	if err != nil {
		return fmt.Errorf("store the Armature app: %w", err)
	}
	return nil
}

// SaveApp stores the app an Armature administrator registered by hand. A new
// client id is a new row, so every grant of the old one goes with it.
func (s *Service) SaveApp(ctx context.Context, in AppInput, ip string) (*Connection, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	clientID := strings.TrimSpace(in.ClientID)
	secret := strings.TrimSpace(in.ClientSecret)
	if clientID == "" || len(clientID) > MaxClientIDLength || strings.ContainsAny(clientID, " \t\r\n") {
		return nil, 0, &FieldError{Field: "clientId", Message: "Paste the client id Armature showed when the app was added under Settings, Connected apps."}
	}
	if secret != "" && (!strings.HasPrefix(secret, ClientSecretPrefix) || len(secret) == len(ClientSecretPrefix)) {
		return nil, 0, &FieldError{Field: "clientSecret", Message: "That is not an Armature client secret. Paste the secret Armature showed once for the app; it starts with " + ClientSecretPrefix + ", and is empty only for a public app."}
	}
	var out *Connection
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		e, err := s.readConnection(ctx, tx)
		if err != nil {
			return err
		}
		if e == nil {
			return ErrNotConfigured
		}
		var sealed []byte
		if secret != "" {
			if sealed, err = s.seal(secret, appContext(site), "clientSecret"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM armature_app WHERE client_id IS DISTINCT FROM $1`, clientID); err != nil {
			return fmt.Errorf("forget the old Armature app: %w", err)
		}
		if err := s.storeApp(ctx, tx, AppActive, &clientID, sealed, RegisteredByAdmin, AppScopes, nil, actor); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionArmatureAppSaved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"clientId": clientID, "public": secret == ""},
		}); err != nil {
			return err
		}
		out, err = s.readConnection(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Partner = s.Partner(ctx, out.BaseURL, false)
	out.App.Offered = out.Partner.OffersApp()
	out.Webhook.Offered = out.Partner.OffersPartnerWebhook()
	return out, lsn, nil
}

// RemoveApp forgets the app, and every grant of it with it; the next member
// to connect registers Stator again.
func (s *Service) RemoveApp(ctx context.Context, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var clientID *string
		err := tx.QueryRow(ctx, `DELETE FROM armature_app RETURNING client_id`).Scan(&clientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("forget the Armature app: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionArmatureAppRemoved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"clientId": deref(clientID)},
		})
	})
}

// connectionApp is the app as an administrator reads it, in the
// connection's transaction; Offered is filled in from Partner after it.
func (s *Service) connectionApp(ctx context.Context, tx db.DBTX) (ConnectionApp, error) {
	out := ConnectionApp{Status: AppNone, RedirectURI: s.RedirectURI(), Scopes: appScopes()}
	a, err := readApp(ctx, tx)
	if err != nil || a == nil {
		return out, err
	}
	by := a.RegisteredBy
	out.Status, out.ClientID, out.RegisteredBy, out.Refusal = a.Status, a.ClientID, &by, a.Refusal
	return out, nil
}

// noteApp fills in what the caller may do with the app, outside any
// transaction of theirs: the app is read as the admin role.
func (s *Service) noteApp(ctx context.Context, a *Account, p Partner) {
	var row *appRow
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		row, err = readApp(ctx, tx)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the Armature app could not be read", "error", err)
		return
	}
	a.App.Offered = p.OffersApp() && (row == nil || row.Status != AppRefused)
	if row != nil {
		a.App.Pending = row.Status == AppPending
		a.App.Refusal = row.Refusal
	}
}

// ConnectPurpose is what a trip to Armature's consent page is for.
type ConnectPurpose string

const (
	// PurposeAccount connects the caller's own account.
	PurposeAccount ConnectPurpose = "account"
	// PurposeWebhook is an administrator connecting Armature in one step:
	// their account, and Stator's own webhook made with the same grant.
	PurposeWebhook ConnectPurpose = "webhook"
)

// StartConnect begins a connect for the caller's session: it registers
// Stator when it is not yet, and answers the consent page's address. The
// state and the verifier stay behind, bound to the session.
func (s *Service) StartConnect(ctx context.Context, session uuid.UUID, returnTo string, purpose ConnectPurpose) (string, error) {
	user, ok := db.UserFrom(ctx)
	if !ok || session == uuid.Nil {
		return "", ErrNotConnected
	}
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
		return "", err
	}
	if e == nil {
		return "", ErrNotConfigured
	}
	p := s.Partner(ctx, e.BaseURL, false)
	if !p.OffersApp() {
		return "", ErrAppUnavailable
	}
	if purpose == PurposeWebhook && !p.OffersPartnerWebhook() {
		return "", ErrWebhookUnavailable
	}
	// Always the full set the person may hold: a new consent replaces their
	// grant's scopes (Cloudster1/Armature#298), so an administrator who made
	// the webhook is never asked for less when connecting their account.
	scopes := AppScopes
	if purpose == PurposeWebhook || (grant != nil && slices.Contains(grant.Scopes, ScopeWebhooks)) {
		scopes = AdminScopes
	}
	registered := AppScopes
	if p.OffersPartnerWebhook() {
		registered = AdminScopes
	}
	meta, err := s.serverMetadata(ctx, e.BaseURL)
	if err != nil {
		return "", err
	}
	creds, err := s.ensureApp(ctx, meta, registered)
	if err != nil {
		return "", err
	}
	state, err := randomState()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Abandoned trips are dead weight; tidying on write is the whole story.
		if _, err := tx.Exec(ctx, `DELETE FROM armature_oauth_attempt WHERE user_id = $1 AND expires_at < now()`, user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_oauth_attempt (state_digest, user_id, session_id, code_verifier, return_to, expires_at, purpose)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			digest(state), user, session, verifier, returnTo, time.Now().Add(OAuthAttemptTTL), purpose)
		return err
	}); err != nil {
		return "", fmt.Errorf("remember the connect: %w", err)
	}
	target, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("%w: the authorization endpoint %q: %w", ErrUnreachable, meta.AuthorizationEndpoint, err)
	}
	q := target.Query()
	q.Set("response_type", "code")
	q.Set("client_id", creds.ClientID)
	q.Set("redirect_uri", s.RedirectURI())
	q.Set("scope", scopes)
	q.Set("state", state)
	q.Set("code_challenge", oauth2.S256ChallengeFromVerifier(verifier))
	q.Set("code_challenge_method", pkceMethod)
	target.RawQuery = q.Encode()
	return target.String(), nil
}

// Finished is where a connect leaves the person: the path they were headed
// for, and what the trip was for.
type Finished struct {
	Back    string
	Purpose ConnectPurpose
}

// FinishConnect takes Armature's answer for the caller's session: it checks
// the state and the issuer, redeems the code and stores the grant, and for an
// administrator's connect makes Stator's webhook with it. It answers where
// the person was headed, also when it fails.
func (s *Service) FinishConnect(ctx context.Context, session uuid.UUID, answer url.Values, ip string) (Finished, db.LSN, error) {
	user, ok := db.UserFrom(ctx)
	if !ok || session == uuid.Nil {
		return Finished{}, 0, ErrNotConnected
	}
	state := answer.Get("state")
	if state == "" {
		return Finished{}, 0, ErrOAuthAttempt
	}
	var (
		verifier string
		out      Finished
	)
	// Consumed as it is read, so a replayed return finds nothing.
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM armature_oauth_attempt
			WHERE state_digest = $1 AND user_id = $2 AND session_id = $3 AND expires_at > now()
			RETURNING code_verifier, return_to, purpose`, digest(state), user, session).Scan(&verifier, &out.Back, &out.Purpose)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOAuthAttempt
		}
		return err
	}); err != nil {
		return Finished{}, 0, err
	}
	lsn, err := s.finishConnect(ctx, user, out.Purpose, verifier, answer, ip)
	return out, lsn, err
}

func (s *Service) finishConnect(ctx context.Context, user uuid.UUID, purpose ConnectPurpose, verifier string, answer url.Values, ip string) (db.LSN, error) {
	if refused := answer.Get("error"); refused != "" {
		switch {
		case refused == oauthAccessDenied:
			return 0, ErrConnectDenied
		case refused == oauthInvalidScope && purpose == PurposeWebhook:
			return 0, ErrScopeRefused
		}
		s.opts.Log.Info("Armature refused a connect", "error", refused, "description", answer.Get("error_description"))
		return 0, ErrConnectFailed
	}

	var (
		e    *endpoint
		site uuid.UUID
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if site, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		e, err = s.endpoint(ctx, tx)
		return err
	}); err != nil {
		return 0, err
	}
	if e == nil {
		return 0, ErrNotConfigured
	}
	meta, err := s.serverMetadata(ctx, e.BaseURL)
	if err != nil {
		return 0, err
	}
	// RFC 9207: a code from another authorization server is never redeemed here.
	if iss := answer.Get("iss"); (meta.ISSParameter || iss != "") && strings.TrimSuffix(iss, "/") != strings.TrimSuffix(meta.Issuer, "/") {
		s.opts.Log.Warn("a connect came back from another issuer", "iss", iss, "issuer", meta.Issuer)
		return 0, ErrConnectFailed
	}
	creds, err := s.appCredentials(ctx)
	if err != nil {
		return 0, err
	}
	call, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	set, err := s.client.Token(call, meta.TokenEndpoint, *creds, url.Values{
		"grant_type": {"authorization_code"}, "code": {answer.Get("code")},
		"redirect_uri": {s.RedirectURI()}, "code_verifier": {verifier},
	})
	switch {
	case isOAuth(err, oauthInvalidGrant):
		return 0, ErrConnectEnded
	case errors.Is(err, ErrUnreachable):
		return 0, err
	case err != nil:
		s.opts.Log.Warn("Armature did not redeem a code", "error", err)
		return 0, ErrConnectFailed
	}
	me, err := s.me(ctx, e.BaseURL, set.AccessToken)
	switch {
	case errors.Is(err, ErrUnreachable):
		return 0, err
	case err != nil:
		return 0, ErrConnectFailed
	case !e.owns(me):
		s.revoke(ctx, meta, creds, set.RefreshToken)
		return 0, ErrOtherInstallation
	}
	lsn, err := s.storeGrant(ctx, site, user, set, me)
	if err != nil {
		return lsn, err
	}
	// The later of the two writes is what the person's next read waits for,
	// so the settings page they come back to says the app is allowed.
	allowed, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE armature_app SET status = 'active' WHERE status = 'pending' AND client_id = $1`, creds.ClientID)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the Armature app could not be marked allowed", "error", err)
	} else if allowed > lsn {
		lsn = allowed
	}
	s.learnInstallation(ctx, e, me)
	if purpose != PurposeWebhook {
		return lsn, nil
	}
	made, err := s.setUpWebhook(ctx, ip)
	return max(lsn, made), err
}

// storeGrant replaces whatever the person had stored with the new grant, as
// a new row so their cache is new with it.
func (s *Service) storeGrant(ctx context.Context, site, user uuid.UUID, set *TokenSet, me *Me) (db.LSN, error) {
	access, err := s.seal(set.AccessToken, grantContext(site, user, "access"), "token")
	if err != nil {
		return 0, err
	}
	refresh, err := s.seal(set.RefreshToken, grantContext(site, user, "refresh"), "token")
	if err != nil {
		return 0, err
	}
	now := time.Now()
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM armature_token WHERE user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the Armature token: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM armature_grant WHERE user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the old Armature grant: %w", err)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_grant (user_id, access_token, refresh_token, access_expires_at, refresh_expires_at, scopes,
			                            armature_user_id, armature_user_name, armature_user_email, status, checked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())`,
			user, access, refresh, now.Add(time.Duration(set.ExpiresIn)*time.Second), now.Add(GrantLifetime),
			strings.Fields(set.Scope), me.User.ID, me.User.Name, me.User.Email, StatusOK)
		if err != nil {
			return fmt.Errorf("store the Armature grant: %w", err)
		}
		return nil
	})
}

// GrantLifetime is how long Armature keeps a grant nobody refreshed.
const GrantLifetime = 90 * 24 * time.Hour

// grantRow is the caller's grant, its tokens still sealed.
type grantRow struct {
	ID            uuid.UUID
	SiteID        uuid.UUID
	Access        []byte
	Refresh       []byte
	AccessExpires time.Time
	Scopes        []string
	Status        Status
	User          *AccountUser
	CheckedAt     time.Time
}

const grantColumns = `id, (SELECT id FROM site), access_token, refresh_token, access_expires_at, scopes, status,
	armature_user_id, armature_user_name, armature_user_email, checked_at`

func scanGrant(row pgx.Row) (*grantRow, error) {
	var (
		g                   grantRow
		armatureUser        *uuid.UUID
		armatureName, email *string
	)
	err := row.Scan(&g.ID, &g.SiteID, &g.Access, &g.Refresh, &g.AccessExpires, &g.Scopes, &g.Status,
		&armatureUser, &armatureName, &email, &g.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature grant: %w", err)
	}
	if armatureUser != nil {
		g.User = &AccountUser{ID: *armatureUser, Name: deref(armatureName), Email: deref(email)}
	}
	return &g, nil
}

func (s *Service) ownGrant(ctx context.Context, tx db.DBTX) (*grantRow, error) {
	user, ok := db.UserFrom(ctx)
	if !ok {
		return nil, ErrNotConnected
	}
	return scanGrant(tx.QueryRow(ctx, `SELECT `+grantColumns+` FROM armature_grant WHERE user_id = $1`, user))
}

func (s *Service) openGrant(ctx context.Context, g *grantRow, kind string) (string, error) {
	user, _ := db.UserFrom(ctx)
	sealed := g.Access
	if kind == "refresh" {
		sealed = g.Refresh
	}
	plain, err := s.box.Open(sealed, grantContext(g.SiteID, user, kind))
	if err != nil {
		s.opts.Log.Warn("a stored Armature grant could not be opened", "grant_id", g.ID, "error", err)
		return "", err
	}
	return string(plain), nil
}

// grantCaller is a caller with the grant's access token, renewed first when
// it runs out within RefreshAhead and again after a 401.
func (s *Service) grantCaller(ctx context.Context, base string, g *grantRow) (*Caller, error) {
	access, err := s.openGrant(ctx, g, "access")
	if err != nil {
		return nil, ErrRejected
	}
	if left := time.Until(g.AccessExpires); left < RefreshAhead {
		fresh, err := s.refreshGrant(ctx, base, "")
		switch {
		case err == nil:
			access = fresh
		case errors.Is(err, ErrRejected) || left <= 0:
			return nil, err
		}
	}
	return s.client.Renewing(base, access, func(ctx context.Context, rejected string) (string, error) {
		return s.refreshGrant(ctx, base, rejected)
	}), nil
}

// refreshGrant renews the caller's grant under a lock on its row: Armature
// ends a grant whose rotated refresh token is presented again, so two
// requests must never refresh it at once. The second finds the first's
// tokens and uses them. rejected is the access token Armature answered 401,
// empty when the token is only running out.
func (s *Service) refreshGrant(ctx context.Context, base, rejected string) (string, error) {
	var (
		fresh string
		ended bool
	)
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		user, _ := db.UserFrom(ctx)
		g, err := scanGrant(tx.QueryRow(ctx, `SELECT `+grantColumns+` FROM armature_grant WHERE user_id = $1 FOR UPDATE`, user))
		if err != nil {
			return err
		}
		if g == nil || g.Status == StatusRejected {
			ended = true
			return nil
		}
		access, err := s.openGrant(ctx, g, "access")
		if err != nil {
			return ErrRejected
		}
		if access != rejected && (rejected != "" || time.Until(g.AccessExpires) >= RefreshAhead) {
			fresh = access
			return nil
		}
		refresh, err := s.openGrant(ctx, g, "refresh")
		if err != nil {
			return ErrRejected
		}
		meta, err := s.serverMetadata(ctx, base)
		if err != nil {
			return err
		}
		creds, err := s.appCredentials(ctx)
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, CallTimeout)
		defer cancel()
		// No scope: a refresh that names fewer narrows the grant for good.
		set, err := s.client.Token(call, meta.TokenEndpoint, *creds, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
		if isOAuth(err, oauthInvalidGrant) || isOAuth(err, oauthInvalidClient) {
			// The grant ended in Armature: it was disconnected there, the app
			// was removed, or the person is switched off.
			ended = true
			_, err := tx.Exec(ctx, `UPDATE armature_grant SET status = $2, checked_at = now() WHERE id = $1`, g.ID, StatusRejected)
			return err
		}
		if err != nil {
			return err
		}
		sealedAccess, err := s.seal(set.AccessToken, grantContext(g.SiteID, user, "access"), "token")
		if err != nil {
			return err
		}
		sealedRefresh, err := s.seal(set.RefreshToken, grantContext(g.SiteID, user, "refresh"), "token")
		if err != nil {
			return err
		}
		now := time.Now()
		if _, err := tx.Exec(ctx, `
			UPDATE armature_grant SET access_token = $2, refresh_token = $3, access_expires_at = $4,
			    refresh_expires_at = $5, scopes = $6, status = $7, refreshed_at = now()
			WHERE id = $1`,
			g.ID, sealedAccess, sealedRefresh, now.Add(time.Duration(set.ExpiresIn)*time.Second), now.Add(GrantLifetime),
			strings.Fields(set.Scope), StatusOK); err != nil {
			return fmt.Errorf("store the renewed Armature grant: %w", err)
		}
		fresh = set.AccessToken
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrRejected) {
			s.opts.Log.Info("an Armature grant could not be renewed", "error", err)
		}
		return "", err
	}
	if ended {
		return "", ErrRejected
	}
	return fresh, nil
}

// endGrant forgets the caller's grant and revokes it at Armature, which ends
// it there too. Armature not answering is logged: the grant is forgotten
// here either way, and ends in Armature unused within its lifetime.
func (s *Service) endGrant(ctx context.Context, tx db.DBTX, user uuid.UUID) (func(context.Context), error) {
	g, err := scanGrant(tx.QueryRow(ctx, `DELETE FROM armature_grant WHERE user_id = $1 RETURNING `+grantColumns, user))
	if err != nil || g == nil {
		return func(context.Context) {}, err
	}
	refresh, openErr := s.openGrant(ctx, g, "refresh")
	return func(ctx context.Context) {
		if openErr != nil {
			return
		}
		var base string
		if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			e, err := s.endpoint(ctx, tx)
			if e != nil {
				base = e.BaseURL
			}
			return err
		}); err != nil || base == "" {
			return
		}
		meta, err := s.serverMetadata(ctx, base)
		if err != nil {
			s.opts.Log.Info("an Armature grant could not be revoked", "error", err)
			return
		}
		creds, err := s.appCredentials(ctx)
		if err != nil {
			return
		}
		s.revoke(ctx, meta, creds, refresh)
	}, nil
}

func (s *Service) revoke(ctx context.Context, meta *ServerMetadata, creds *AppCredentials, token string) {
	if meta.RevocationEndpoint == "" {
		return
	}
	call, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	if err := s.client.Revoke(call, meta.RevocationEndpoint, *creds, token); err != nil {
		s.opts.Log.Info("an Armature grant could not be revoked", "error", err)
	}
}

func randomState() (string, error) {
	buf := make([]byte, stateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func digest(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// appContext binds the app's secret to its site.
func appContext(site uuid.UUID) []byte {
	return append([]byte("armature.app:"), site[:]...)
}

// grantContext binds a grant's token to its site, its person and its kind,
// so neither a copy onto another row nor a swap of the two opens.
func grantContext(site, user uuid.UUID, kind string) []byte {
	return append(append([]byte("armature.grant."+kind+":"), site[:]...), user[:]...)
}
