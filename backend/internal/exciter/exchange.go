package exciter

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
)

// Connecting a person at their sign-in (#462), as Stator does toward
// Armature (#430): Stator trades the access token of their sign-in through
// the identity provider Exciter shares for an Exciter grant, at Exciter's
// token endpoint (Cloudster1/Exciter#273), as the partner app an Exciter
// administrator registered for Stator. Nobody pastes an exciter_pat_.

const (
	// CapabilityTokenExchange is Exciter exchanging a token of its identity
	// provider for a grant; nothing is asked at sign-in without it.
	CapabilityTokenExchange = armature.CapabilityTokenExchange
	// TokenPath and RevokePath are Exciter's OAuth endpoints under its origin.
	TokenPath  = "/api/v1/oauth/token"
	RevokePath = "/api/v1/oauth/revoke"
	// ClientSecretPrefix starts every secret Exciter shows for an app.
	ClientSecretPrefix = "exciter_ocs_"
	// MaxClientIDLength bounds a pasted client id.
	MaxClientIDLength = 200
)

// The sentences the profile shows when Exciter did not connect a person at
// sign-in and gave no sentence of its own.
const (
	signInNoApp        = "Stator is not a partner app of this Exciter yet, so it could not connect you when you signed in. Paste an Exciter token below, or ask a Stator administrator to store the app under Settings, Exciter."
	signInUnreachable  = "Exciter did not answer when you signed in, so you are not connected yet. Paste an Exciter token below, or sign in again later."
	signInRefusedShape = "Exciter would not connect you when you signed in (%s). Paste an Exciter token below."
)

// OffersTokenExchange says whether the Exciter connects people at sign-in.
func (p *Partner) OffersTokenExchange() bool {
	return p != nil && p.Status == StatusOK && slices.Contains(p.Capabilities, CapabilityTokenExchange)
}

// appContext binds the app's secret to its site.
func appContext(site uuid.UUID) []byte {
	return append([]byte("exciter.app:"), site[:]...)
}

// refreshContext binds a grant's refresh token to its site and person; the
// access token is sealed as a pasted token is.
func refreshContext(site, user uuid.UUID) []byte {
	return append(append([]byte("exciter.refresh:"), site[:]...), user[:]...)
}

func (s *Service) tokenEndpoint(base string) string { return strings.TrimSuffix(base, "/") + TokenPath }
func (s *Service) revokeEndpoint(base string) string {
	return strings.TrimSuffix(base, "/") + RevokePath
}

// SaveApp stores the partner app an Exciter administrator registered for
// Stator. A new client id ends every grant of the old one.
func (s *Service) SaveApp(ctx context.Context, in AppInput, ip string) (*Connection, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	clientID := strings.TrimSpace(in.ClientID)
	secret := strings.TrimSpace(in.ClientSecret)
	if clientID == "" || len(clientID) > MaxClientIDLength || strings.ContainsAny(clientID, " \t\r\n") {
		return nil, 0, &armature.FieldError{Field: "clientId", Message: "Paste the client id Exciter showed when its administrator registered Stator as a partner app."}
	}
	if !strings.HasPrefix(secret, ClientSecretPrefix) || len(secret) == len(ClientSecretPrefix) {
		return nil, 0, &armature.FieldError{Field: "clientSecret", Message: "That is not an Exciter client secret. Paste the secret Exciter showed once for the app; it starts with " + ClientSecretPrefix + "."}
	}
	var out *Connection
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		if out, err = s.readConnection(ctx, tx); err != nil {
			return err
		}
		if out == nil {
			return ErrNotConfigured
		}
		sealed, err := s.seal(secret, appContext(site), "clientSecret")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exciter_app WHERE client_id <> $1`, clientID); err != nil {
			return fmt.Errorf("forget the old Exciter app: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO exciter_app (client_id, client_secret, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (singleton) DO UPDATE SET client_secret = EXCLUDED.client_secret, updated_by = EXCLUDED.updated_by`,
			clientID, sealed, nullable(actor)); err != nil {
			return fmt.Errorf("store the Exciter app: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionExciterAppSaved, TargetType: "exciter_connection", Actor: actor, IP: ip,
			Data: map[string]any{"clientId": clientID}}); err != nil {
			return err
		}
		out, err = s.readConnection(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Partner = s.Partner(ctx, out.BaseURL, false)
	s.noteApp(ctx, out, false)
	return out, lsn, nil
}

// RemoveApp forgets the app, and every grant of it with it.
func (s *Service) RemoveApp(ctx context.Context, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var clientID string
		err := tx.QueryRow(ctx, `DELETE FROM exciter_app RETURNING client_id`).Scan(&clientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("forget the Exciter app: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionExciterAppRemoved, TargetType: "exciter_connection", Actor: actor, IP: ip,
			Data: map[string]any{"clientId": clientID}})
	})
}

// connectionApp is the app as an administrator reads it, in the
// connection's transaction; noteApp fills in what Exciter offers.
func connectionApp(ctx context.Context, tx db.DBTX) (ConnectionApp, error) {
	var out ConnectionApp
	var clientID string
	err := tx.QueryRow(ctx, `SELECT client_id FROM exciter_app`).Scan(&clientID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return out, nil
	case err != nil:
		return out, fmt.Errorf("read the Exciter app: %w", err)
	}
	out.ClientID, out.SecretSet = &clientID, true
	return out, nil
}

func (s *Service) noteApp(ctx context.Context, c *Connection, fresh bool) {
	if c == nil {
		return
	}
	p := s.Partner(ctx, c.BaseURL, fresh)
	c.App.Offered = p.OffersTokenExchange()
	c.App.Exchanging = s.opts.TokenExchange && c.App.Offered && c.App.ClientID != nil
}

// appCredentials reads the app as the admin role: a member's sign-in and
// refresh need its secret, which stator_app keeps from members. Nil for none.
func (s *Service) appCredentials(ctx context.Context) (*armature.AppCredentials, error) {
	var creds *armature.AppCredentials
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		site, err := instance.ID(ctx, tx)
		if err != nil {
			return err
		}
		var (
			clientID string
			sealed   []byte
		)
		err = tx.QueryRow(ctx, `SELECT client_id, client_secret FROM exciter_app`).Scan(&clientID, &sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the Exciter app: %w", err)
		}
		plain, err := s.box.Open(sealed, appContext(site))
		if err != nil {
			return fmt.Errorf("open the Exciter app's secret: %w", err)
		}
		creds = &armature.AppCredentials{ClientID: clientID, ClientSecret: string(plain)}
		return nil
	})
	return creds, err
}

// ConnectAtSignIn connects the person ctx acts for to Exciter with the
// access token of the sign-in they just made, unless they hold a live token
// or grant already. It never fails the sign-in: a refusal is kept for the
// profile to say, and the LSN of what it wrote, zero for nothing, is answered.
func (s *Service) ConnectAtSignIn(ctx context.Context, subjectToken string) db.LSN {
	user, ok := db.UserFrom(ctx)
	if !s.opts.TokenExchange || !ok || subjectToken == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, armature.SignInTimeout)
	defer cancel()
	lsn, err := s.connectAtSignIn(ctx, user, subjectToken)
	if err != nil {
		s.opts.Log.Warn("Exciter could not be asked to connect a person at sign-in", "user_id", user, "error", err)
	}
	return lsn
}

func (s *Service) connectAtSignIn(ctx context.Context, user uuid.UUID, subjectToken string) (db.LSN, error) {
	var (
		base string
		site uuid.UUID
		live bool
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if base, err = s.endpoint(ctx, tx); err != nil || base == "" {
			return err
		}
		if site, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exciter_token WHERE user_id = $1 AND status <> $2)`, user, StatusRejected).Scan(&live)
	}); err != nil {
		return 0, err
	}
	// A token the person pasted keeps working as they made it; the exchange
	// only fills in a missing or dead one.
	if base == "" || live {
		return 0, nil
	}
	if p := s.Partner(ctx, base, false); !p.OffersTokenExchange() {
		return 0, nil
	}
	creds, err := s.appCredentials(ctx)
	if err != nil {
		return 0, err
	}
	if creds == nil {
		return s.noteSignIn(ctx, user, signInNoApp)
	}
	call, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	set, err := s.client.Token(call, s.tokenEndpoint(base), *creds, url.Values{
		"grant_type": {armature.GrantTokenExchange}, "subject_token": {subjectToken},
		"subject_token_type": {armature.TokenTypeAccess},
	})
	var refused *armature.OAuthError
	switch {
	case errors.As(err, &refused):
		s.opts.Log.Info("Exciter did not connect a person at sign-in", "user_id", user, "error", refused.Code, "description", refused.Description)
		return s.noteSignIn(ctx, user, signInRefusal(refused))
	case err != nil:
		s.opts.Log.Info("Exciter did not answer an exchange at sign-in", "error", err)
		return s.noteSignIn(ctx, user, signInUnreachable)
	}
	me, err := s.me(ctx, base, set.AccessToken)
	if err != nil {
		s.revoke(ctx, base, creds, set.RefreshToken)
		return s.noteSignIn(ctx, user, signInUnreachable)
	}
	access, err := s.seal(set.AccessToken, tokenContext(site, user), "token")
	if err != nil {
		return 0, err
	}
	refresh, err := s.seal(set.RefreshToken, refreshContext(site, user), "token")
	if err != nil {
		return 0, err
	}
	// As the admin role: a member may store a token of their own but never
	// write a grant's tokens, which only Exciter's answers may be.
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM exciter_token WHERE user_id = $1`, user); err != nil {
			return fmt.Errorf("forget the old Exciter token: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exciter_sign_in WHERE user_id = $1`, user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO exciter_token (user_id, token, refresh_token, access_expires_at, app,
			                           exciter_user_id, exciter_user_name, exciter_user_email, status, checked_at)
			VALUES ($1, $2, $3, $4, true, $5, $6, $7, $8, now())`,
			user, access, refresh, time.Now().Add(time.Duration(set.ExpiresIn)*time.Second), me.ID, me.Name, me.Email, StatusOK)
		if err != nil {
			return fmt.Errorf("store the Exciter grant: %w", err)
		}
		return nil
	})
}

// signInRefusal is Exciter's own sentence, or one of Stator's naming its code.
func signInRefusal(refused *armature.OAuthError) string {
	if said := strings.TrimSpace(refused.Description); said != "" {
		return said
	}
	return fmt.Sprintf(signInRefusedShape, refused.Code)
}

// noteSignIn keeps why the person is not connected, for their profile.
func (s *Service) noteSignIn(ctx context.Context, user uuid.UUID, refusal string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO exciter_sign_in (user_id, refusal, refused_at) VALUES ($1, $2, now())
			ON CONFLICT (user_id) DO UPDATE SET refusal = EXCLUDED.refusal, refused_at = EXCLUDED.refused_at`,
			user, refusal)
		if err != nil {
			return fmt.Errorf("keep why Exciter did not connect at sign-in: %w", err)
		}
		return nil
	})
}

func signInRefusalOf(ctx context.Context, tx db.DBTX, user uuid.UUID) (*string, error) {
	var refusal string
	err := tx.QueryRow(ctx, `SELECT refusal FROM exciter_sign_in WHERE user_id = $1`, user).Scan(&refusal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read why Exciter did not connect at sign-in: %w", err)
	}
	return &refusal, nil
}

// grantCaller is a caller with the grant's access token, renewed first when
// it runs out within armature.RefreshAhead and again after a 401.
func (s *Service) grantCaller(ctx context.Context, base string, row *tokenRow) (*armature.Caller, error) {
	access, err := s.open(ctx, row)
	if err != nil {
		return nil, armature.ErrRejected
	}
	if left := time.Until(*row.AccessExpires); left < armature.RefreshAhead {
		fresh, err := s.refreshGrant(ctx, base, "")
		switch {
		case err == nil:
			access = fresh
		case errors.Is(err, armature.ErrRejected) || left <= 0:
			return nil, err
		}
	}
	return s.client.Renewing(base, access, func(ctx context.Context, rejected string) (string, error) {
		return s.refreshGrant(ctx, base, rejected)
	}), nil
}

// refreshGrant renews the caller's grant under a lock on its row: Exciter
// ends a grant whose rotated refresh token is presented again, so two
// requests never refresh it at once; the second uses the first's tokens.
// rejected is the access token Exciter answered 401, empty when it is only
// running out.
func (s *Service) refreshGrant(ctx context.Context, base, rejected string) (string, error) {
	var (
		fresh string
		ended bool
	)
	user, _ := db.UserFrom(ctx)
	// As the admin role, for the person ctx acts for alone: a stored token is
	// never rewritten in place by a member's own role (00820).
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		row, err := scanToken(tx.QueryRow(ctx, `SELECT `+tokenColumns+` FROM exciter_token WHERE user_id = $1 FOR UPDATE`, user))
		if err != nil {
			return err
		}
		if row == nil || row.Refresh == nil || row.Status == StatusRejected {
			ended = true
			return nil
		}
		access, err := s.open(ctx, row)
		if err != nil {
			return armature.ErrRejected
		}
		if access != rejected && (rejected != "" || time.Until(*row.AccessExpires) >= armature.RefreshAhead) {
			fresh = access
			return nil
		}
		plain, err := s.box.Open(row.Refresh, refreshContext(row.SiteID, user))
		if err != nil {
			return armature.ErrRejected
		}
		creds, err := s.appCredentials(ctx)
		if err != nil {
			return err
		}
		if creds == nil {
			ended = true
			return nil
		}
		call, cancel := context.WithTimeout(ctx, armature.CallTimeout)
		defer cancel()
		set, err := s.client.Token(call, s.tokenEndpoint(base), *creds, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {string(plain)}})
		var refused *armature.OAuthError
		if errors.As(err, &refused) && (refused.Code == "invalid_grant" || refused.Code == "invalid_client") {
			// The grant ended in Exciter: disconnected there, the app removed,
			// or the person switched off.
			ended = true
			_, err := tx.Exec(ctx, `UPDATE exciter_token SET status = $2, checked_at = now() WHERE id = $1`, row.ID, StatusRejected)
			return err
		}
		if err != nil {
			return err
		}
		sealedAccess, err := s.seal(set.AccessToken, tokenContext(row.SiteID, user), "token")
		if err != nil {
			return err
		}
		sealedRefresh, err := s.seal(set.RefreshToken, refreshContext(row.SiteID, user), "token")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE exciter_token SET token = $2, refresh_token = $3, access_expires_at = $4, status = $5
			WHERE id = $1`, row.ID, sealedAccess, sealedRefresh, time.Now().Add(time.Duration(set.ExpiresIn)*time.Second), StatusOK); err != nil {
			return fmt.Errorf("store the renewed Exciter grant: %w", err)
		}
		fresh = set.AccessToken
		return nil
	})
	if err != nil {
		if !errors.Is(err, armature.ErrRejected) {
			s.opts.Log.Info("an Exciter grant could not be renewed", "error", err)
		}
		return "", err
	}
	if ended {
		return "", armature.ErrRejected
	}
	return fresh, nil
}

// revoke ends a grant at Exciter; Exciter not answering is logged, and the
// grant ends there unused.
func (s *Service) revoke(ctx context.Context, base string, creds *armature.AppCredentials, token string) {
	call, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	if err := s.client.Revoke(call, s.revokeEndpoint(base), *creds, token); err != nil {
		s.opts.Log.Info("an Exciter grant could not be revoked", "error", err)
	}
}

// revokeGrant ends a grant row's refresh token at Exciter, after the row is
// forgotten here.
func (s *Service) revokeGrant(ctx context.Context, base string, row *tokenRow) {
	if row == nil || row.Refresh == nil || base == "" {
		return
	}
	user, _ := db.UserFrom(ctx)
	plain, err := s.box.Open(row.Refresh, refreshContext(row.SiteID, user))
	if err != nil {
		return
	}
	creds, err := s.appCredentials(ctx)
	if err != nil || creds == nil {
		return
	}
	s.revoke(ctx, base, creds, string(plain))
}
