package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Service runs the sign-in flow and keeps the site's provider, with
// discovery cached per issuer; the library refreshes keys it has not seen.
type Service struct {
	db          *db.Cluster
	box         *secret.Box
	redirectURL string
	// http reaches the provider; nil is the default client.
	http *http.Client

	mu         sync.Mutex
	discovered map[string]*coreoidc.Provider
}

// NewService returns the service. box seals client secrets; a nil box refuses
// to store one.
func NewService(cluster *db.Cluster, box *secret.Box, redirectURL string) *Service {
	return &Service{
		db:          cluster,
		box:         box,
		redirectURL: redirectURL,
		discovered:  map[string]*coreoidc.Provider{},
	}
}

// Provider reads the site's configuration, through row level security like
// any other data.
func (s *Service) Provider(ctx context.Context) (*Provider, error) {
	var p Provider
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT issuer, client_id, client_secret IS NOT NULL, groups_claim, scopes,
			       create_groups, enabled, updated_at
			FROM oidc_provider`,
		).Scan(&p.Issuer, &p.ClientID, &p.HasSecret, &p.GroupsClaim, &p.Scopes,
			&p.CreateGroups, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	return &p, nil
}

// Save writes the site's configuration. An empty secret keeps the stored one,
// so a form that never shows it cannot erase it.
func (s *Service) Save(ctx context.Context, in Provider) (*Provider, db.LSN, error) {
	siteID, err := s.siteID(ctx, s.db.ReadPrimary)
	if err != nil {
		return nil, 0, err
	}
	_, lsn, err := s.store(ctx, s.db.Write, siteID, in, false)
	if err != nil {
		return nil, lsn, err
	}
	saved, err := s.Provider(ctx)
	return saved, lsn, err
}

// Bootstrap points the site at the operator's provider unless it has one,
// and reports whether it did. It acts for no member, so as stator_admin.
func (s *Service) Bootstrap(ctx context.Context, in Provider) (bool, error) {
	siteID, err := s.siteID(ctx, s.db.ReadAdmin)
	if err != nil {
		return false, err
	}
	created, _, err := s.store(ctx, s.db.WriteAdmin, siteID, in, true)
	return created, err
}

// Configure writes the site's provider for the operator, over any already
// there. It acts for no member, so as stator_admin.
func (s *Service) Configure(ctx context.Context, in Provider) error {
	siteID, err := s.siteID(ctx, s.db.ReadAdmin)
	if err != nil {
		return err
	}
	_, _, err = s.store(ctx, s.db.WriteAdmin, siteID, in, false)
	return err
}

// siteID reads what the client secret is sealed with: the id it was sealed
// with when the site was an organization, so stored secrets keep opening.
func (s *Service) siteID(ctx context.Context, read func(context.Context, func(context.Context, db.DBTX) error) error) (uuid.UUID, error) {
	var id uuid.UUID
	err := read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		id, err = instance.ID(ctx, tx)
		return err
	})
	return id, err
}

// store writes a provider through write and reports whether it did; keep
// leaves a provider already there as it is.
func (s *Service) store(
	ctx context.Context,
	write func(context.Context, func(context.Context, db.DBTX) error) (db.LSN, error),
	siteID uuid.UUID, in Provider, keep bool,
) (bool, db.LSN, error) {
	in, err := normalize(in)
	if err != nil {
		return false, 0, err
	}
	var sealed []byte
	if in.ClientSecret != "" {
		sealed, err = s.box.Seal([]byte(in.ClientSecret), siteID[:])
		if errors.Is(err, secret.ErrNoKey) {
			return false, 0, &ValidationError{Field: "clientSecret", Message: "The server has no STATOR_SECRET_KEY, so it cannot store a client secret. Set one and try again."}
		}
		if err != nil {
			return false, 0, err
		}
	}

	stored := false
	lsn, err := write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if keep {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oidc_provider)`).Scan(&exists); err != nil || exists {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider (issuer, client_id, client_secret, groups_claim, scopes, create_groups, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (singleton) DO UPDATE SET
			    issuer = EXCLUDED.issuer,
			    client_id = EXCLUDED.client_id,
			    client_secret = COALESCE(EXCLUDED.client_secret, oidc_provider.client_secret),
			    groups_claim = EXCLUDED.groups_claim,
			    scopes = EXCLUDED.scopes,
			    create_groups = EXCLUDED.create_groups,
			    enabled = EXCLUDED.enabled`,
			in.Issuer, in.ClientID, sealed, in.GroupsClaim, in.Scopes, in.CreateGroups, in.Enabled)
		if err != nil {
			return err
		}
		stored = true
		secretChange := "kept"
		if sealed != nil {
			secretChange = "set"
		}
		actor, _ := db.UserFrom(ctx)
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionSSOProviderSaved, TargetType: "oidc_provider", Actor: actor,
			Data: map[string]any{
				"issuer": in.Issuer, "clientId": in.ClientID, "clientSecret": secretChange, "groupsClaim": in.GroupsClaim,
				"scopes": in.Scopes, "createGroups": in.CreateGroups, "enabled": in.Enabled,
			},
		})
	})
	if err != nil {
		return false, lsn, fmt.Errorf("save the identity provider: %w", err)
	}

	// A changed issuer means the cached discovery is about somewhere else.
	s.mu.Lock()
	delete(s.discovered, in.Issuer)
	s.mu.Unlock()
	return stored, lsn, nil
}

// normalize trims a provider's settings, fills in the defaults and refuses
// what cannot work.
func normalize(in Provider) (Provider, error) {
	in.Issuer = strings.TrimSuffix(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.GroupsClaim = strings.TrimSpace(in.GroupsClaim)
	in.Scopes = strings.Join(strings.Fields(in.Scopes), " ")
	if u, err := url.Parse(in.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return in, &ValidationError{Field: "issuer", Message: "Enter the provider's issuer URL, such as https://id.example.com/realms/acme."}
	}
	if in.ClientID == "" {
		return in, &ValidationError{Field: "clientId", Message: "Enter the client ID the provider gave Stator."}
	}
	if in.GroupsClaim == "" {
		in.GroupsClaim = DefaultGroupsClaim
	}
	if in.Scopes == "" {
		in.Scopes = DefaultScopes
	}
	return in, nil
}

// signInProvider reads the provider for a sign-in, which happens before
// anybody is known, and opens its secret.
func (s *Service) signInProvider(ctx context.Context) (*Provider, error) {
	var (
		p      Provider
		sealed []byte
		siteID uuid.UUID
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if siteID, err = instance.ID(ctx, tx); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT issuer, client_id, client_secret, groups_claim, scopes, create_groups, enabled, updated_at
			FROM oidc_provider`,
		).Scan(&p.Issuer, &p.ClientID, &sealed, &p.GroupsClaim, &p.Scopes, &p.CreateGroups, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	if !p.Enabled {
		return nil, ErrNotConfigured
	}
	if sealed != nil {
		plain, err := s.box.Open(sealed, siteID[:])
		if err != nil {
			return nil, fmt.Errorf("open the client secret: %w", err)
		}
		p.ClientSecret, p.HasSecret = string(plain), true
	}
	return &p, nil
}

// discover returns the provider's endpoints and key set, fetching them once.
func (s *Service) discover(ctx context.Context, issuer string) (*coreoidc.Provider, error) {
	s.mu.Lock()
	cached, ok := s.discovered[issuer]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	found, err := coreoidc.NewProvider(s.providerContext(ctx), issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: ask %s who it is: %w", ErrUnreachable, issuer, err)
	}
	s.mu.Lock()
	s.discovered[issuer] = found
	s.mu.Unlock()
	return found, nil
}

func (s *Service) oauthConfig(p *Provider, discovered *coreoidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  s.redirectURL,
		Scopes:       p.ScopeList(),
	}
}

// Start begins a sign-in and returns the URL to send the browser to.
func (s *Service) Start(ctx context.Context, redirect string) (string, error) {
	provider, err := s.signInProvider(ctx)
	if err != nil {
		return "", err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	// Three values guard the round trip: the state ties the callback to this
	// attempt, the nonce ties the id token to it, and the PKCE verifier proves
	// that whoever redeems the code is who asked for it.
	verifier := oauth2.GenerateVerifier()

	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Abandoned attempts are dead weight, and the table is small enough
		// that tidying on write is the whole story.
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_login (state, nonce, code_verifier, redirect, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			state, nonce, verifier, redirect, time.Now().Add(howLongALoginMayTake))
		return err
	}); err != nil {
		return "", fmt.Errorf("remember this sign-in: %w", err)
	}

	return authCodeURL(s.oauthConfig(provider, discovered), state, nonce, verifier), nil
}

// authCodeURL carries the state, the nonce and the S256 challenge; the verifier
// itself stays behind for the code exchange.
func authCodeURL(config *oauth2.Config, state, nonce, verifier string) string {
	return config.AuthCodeURL(state, coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the code for a verified id token and returns its identity,
// with where the browser was headed before it left.
func (s *Service) Exchange(ctx context.Context, state, code string) (*Identity, string, error) {
	var (
		nonce    string
		verifier string
		redirect string
	)
	// Consumed as it is read: a state is good for exactly one sign-in, so a
	// replayed callback cannot open a second session.
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM oidc_login WHERE state = $1 AND expires_at > now()
			RETURNING nonce, code_verifier, redirect`, state,
		).Scan(&nonce, &verifier, &redirect)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownLogin
		}
		return err
	})
	if err != nil {
		return nil, "", err
	}

	provider, err := s.signInProvider(ctx)
	if err != nil {
		return nil, "", err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, "", err
	}

	ctx = s.providerContext(ctx)
	tokens, err := s.oauthConfig(provider, discovered).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, "", errors.New("the identity provider returned no id token")
	}

	// Verified against the issuer's published keys, for this client, unexpired.
	// The library refuses an unsigned token and one signed with a symmetric key
	// it was not given, the two ways this check is usually skipped.
	verified, err := discovered.Verifier(&coreoidc.Config{ClientID: provider.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, "", fmt.Errorf("verify the id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(nonce)) != 1 {
		return nil, "", errors.New("the id token belongs to a different sign-in")
	}

	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, "", fmt.Errorf("read the token's claims: %w", err)
	}
	identity, err := identityFromClaims(verified.Issuer, verified.Subject, claims, provider.GroupsClaim)
	if err != nil {
		return nil, "", err
	}
	identity.AccessToken = tokens.AccessToken
	return identity, redirect, nil
}

// randomToken is 32 bytes of randomness, which is what both the state and the
// nonce need to be unguessable.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
