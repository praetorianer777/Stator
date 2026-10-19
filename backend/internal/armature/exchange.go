package armature

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

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
)

// Connecting a person at their sign-in (#430): Stator trades the access token
// of their sign-in through the identity provider Armature shares for a grant
// of the connected app, as Armature's docs/oauth.md sets out under "A
// signed-in person, with no click". Nobody clicks Connect or pastes a token.

const (
	// CapabilityTokenExchange is Armature exchanging a token of its identity
	// provider for a grant (Cloudster1/Armature#302); Stator asks for nothing
	// at sign-in from an Armature that does not list it.
	CapabilityTokenExchange = "oauth.token-exchange@1"
	// GrantTokenExchange and TokenTypeAccess are RFC 8693's names.
	GrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	TokenTypeAccess    = "urn:ietf:params:oauth:token-type:access_token"
	// SignInTimeout bounds the whole exchange, so a slow Armature delays a
	// sign-in by at most this; the person can still connect by hand.
	SignInTimeout = 10 * time.Second
)

// The sentences the profile shows when Armature did not connect a person at
// sign-in and gave no sentence of its own.
const (
	signInNoApp        = "Stator is not a connected app of this Armature yet, so it could not connect you when you signed in. Connect Armature below, or ask a Stator administrator to set the app up under Settings, Armature."
	signInUnreachable  = "Armature did not answer when you signed in, so you are not connected yet. Connect Armature below, or sign in again later."
	signInOther        = "The account Armature connected at sign-in belongs to another Armature installation than this one. Connect Armature below."
	signInRefusedShape = "Armature would not connect you when you signed in (%s). Connect Armature below."
)

// OffersTokenExchange says whether the Armature connects people at sign-in.
func (p *Partner) OffersTokenExchange() bool {
	return p != nil && p.Status == StatusOK && slices.Contains(p.Capabilities, CapabilityTokenExchange)
}

// ConnectAtSignIn connects the person ctx acts for to Armature with the
// access token of the sign-in they just made, unless they hold a live grant
// or token already. It never fails the sign-in: a refusal is kept for the
// profile to say, and the LSN of what it wrote, zero for nothing, is answered.
func (s *Service) ConnectAtSignIn(ctx context.Context, subjectToken string) db.LSN {
	user, ok := db.UserFrom(ctx)
	if !s.opts.TokenExchange || !ok || subjectToken == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, SignInTimeout)
	defer cancel()
	lsn, err := s.connectAtSignIn(ctx, user, subjectToken)
	if err != nil {
		s.opts.Log.Warn("Armature could not be asked to connect a person at sign-in", "user_id", user, "error", err)
	}
	return lsn
}

func (s *Service) connectAtSignIn(ctx context.Context, user uuid.UUID, subjectToken string) (db.LSN, error) {
	var (
		e    *endpoint
		site uuid.UUID
		live bool
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if e, err = s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		if site, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		live, err = holdsLiveLink(ctx, tx, user)
		return err
	}); err != nil {
		return 0, err
	}
	// A grant or token the person made keeps working as they made it; the
	// exchange only fills in a missing or dead one.
	if e == nil || live {
		return 0, nil
	}
	if p := s.Partner(ctx, e.BaseURL, false); !p.OffersTokenExchange() {
		return 0, nil
	}
	creds, err := s.appCredentials(ctx)
	if errors.Is(err, ErrConnectFailed) {
		return s.noteSignIn(ctx, user, signInNoApp)
	}
	if err != nil {
		return 0, err
	}
	meta, err := s.serverMetadata(ctx, e.BaseURL)
	if err != nil {
		s.opts.Log.Info("Armature's metadata could not be read at sign-in", "error", err)
		return s.noteSignIn(ctx, user, signInUnreachable)
	}
	call, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	set, err := s.client.Token(call, meta.TokenEndpoint, *creds, url.Values{
		"grant_type": {GrantTokenExchange}, "subject_token": {subjectToken},
		"subject_token_type": {TokenTypeAccess}, "scope": {AppScopes},
	})
	var refused *OAuthError
	switch {
	case errors.As(err, &refused):
		s.opts.Log.Info("Armature did not connect a person at sign-in", "user_id", user, "error", refused.Code, "description", refused.Description)
		return s.noteSignIn(ctx, user, signInRefusal(refused))
	case err != nil:
		s.opts.Log.Info("Armature did not answer an exchange at sign-in", "error", err)
		return s.noteSignIn(ctx, user, signInUnreachable)
	}
	me, err := s.me(ctx, e.BaseURL, set.AccessToken)
	switch {
	case err != nil:
		s.revoke(ctx, meta, creds, set.RefreshToken)
		return s.noteSignIn(ctx, user, signInUnreachable)
	case !e.owns(me):
		s.revoke(ctx, meta, creds, set.RefreshToken)
		return s.noteSignIn(ctx, user, signInOther)
	}
	lsn, err := s.storeGrant(ctx, site, user, set, me)
	if err != nil {
		return lsn, err
	}
	s.learnInstallation(ctx, e, me)
	return max(lsn, s.noteAppAllowed(ctx, creds.ClientID)), nil
}

// holdsLiveLink says whether the person has a grant or a token that Armature
// has not refused and that has not run out.
func holdsLiveLink(ctx context.Context, tx db.DBTX, user uuid.UUID) (bool, error) {
	var live bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM armature_grant WHERE user_id = $1 AND status <> $2 AND refresh_expires_at > now())
		    OR (NOT EXISTS (SELECT 1 FROM armature_grant WHERE user_id = $1)
		        AND EXISTS (SELECT 1 FROM armature_token WHERE user_id = $1 AND status <> $2))`,
		user, StatusRejected).Scan(&live)
	if err != nil {
		return false, fmt.Errorf("read the Armature link: %w", err)
	}
	return live, nil
}

// signInRefusal is Armature's own sentence, such as the person having no
// account there, or one of Stator's naming its code.
func signInRefusal(refused *OAuthError) string {
	if said := strings.TrimSpace(refused.Description); said != "" {
		return said
	}
	return fmt.Sprintf(signInRefusedShape, refused.Code)
}

// noteSignIn keeps why the person is not connected, for their profile.
func (s *Service) noteSignIn(ctx context.Context, user uuid.UUID, refusal string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_sign_in (user_id, refusal, refused_at) VALUES ($1, $2, now())
			ON CONFLICT (user_id) DO UPDATE SET refusal = EXCLUDED.refusal, refused_at = EXCLUDED.refused_at`,
			user, refusal)
		if err != nil {
			return fmt.Errorf("keep why Armature did not connect at sign-in: %w", err)
		}
		return nil
	})
}

// signInRefusalOf is the person's last refusal at sign-in, or nil.
func signInRefusalOf(ctx context.Context, tx db.DBTX, user uuid.UUID) (*string, error) {
	var refusal string
	err := tx.QueryRow(ctx, `SELECT refusal FROM armature_sign_in WHERE user_id = $1`, user).Scan(&refusal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read why Armature did not connect at sign-in: %w", err)
	}
	return &refusal, nil
}

// noteAppAllowed marks a pending app active once Armature issued a grant of
// it, which it does for an allowed app only.
func (s *Service) noteAppAllowed(ctx context.Context, clientID string) db.LSN {
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE armature_app SET status = 'active' WHERE status = 'pending' AND client_id = $1`, clientID)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the Armature app could not be marked allowed", "error", err)
		return 0
	}
	return lsn
}
