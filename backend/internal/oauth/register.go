package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// Dynamic registration (RFC 7591), adapted from Armature's Register in
// backend/internal/oauth/clients.go: an app waits until an administrator allows it.

// RegistrationRequest is RFC 7591's client metadata, in its names.
type RegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

// RegistrationResponse is what a registered app is told, its secret once.
type RegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func invalidMetadata(description string) *Error {
	return refuse(http.StatusBadRequest, "invalid_client_metadata", description)
}

// checkRedirects refuses a return address a code could leak through.
func checkRedirects(uris []string) ([]string, *Error) {
	if len(uris) == 0 || len(uris) > MaxRedirectURIs {
		return nil, refuse(http.StatusBadRequest, "invalid_redirect_uri",
			fmt.Sprintf("Register 1 to %d addresses Stator sends the browser back to.", MaxRedirectURIs))
	}
	out := make([]string, 0, len(uris))
	for _, raw := range uris {
		if !validRedirect(raw) {
			return nil, refuse(http.StatusBadRequest, "invalid_redirect_uri", "The return address "+raw+
				" is neither https nor http on a loopback address, or it has a fragment. Register https addresses, or http://127.0.0.1 or http://localhost at any port.")
		}
		if !slices.Contains(out, raw) {
			out = append(out, raw)
		}
	}
	return out, nil
}

// checkClientURI is the app's own address, or empty.
func checkClientURI(raw string) (string, *Error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if u, err := url.Parse(raw); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", invalidMetadata("Write client_uri as an http or https address, such as https://rotor.example.com, or leave it out.")
	}
	return raw, nil
}

// checkGrants holds the grant types to what Stator offers, authorization_code
// among them; the token exchange is accepted for a partner tool's sake.
func checkGrants(grants []string) ([]string, *Error) {
	if len(grants) == 0 {
		return []string{GrantAuthorizationCode, GrantRefreshToken}, nil
	}
	for _, g := range grants {
		if !slices.Contains(GrantTypes, g) {
			return nil, invalidMetadata(fmt.Sprintf("Stator does not offer the %s grant. Choose from %s.", g, strings.Join(GrantTypes, ", ")))
		}
	}
	if !slices.Contains(grants, GrantAuthorizationCode) {
		return nil, invalidMetadata("An app is connected by authorization_code; name it among grant_types.")
	}
	return slices.Compact(slices.Clone(grants)), nil
}

// Register is RFC 7591 dynamic registration. Nothing the app asks for works
// until an administrator allows it, and it goes after PendingClientTTL.
func (s *Service) Register(ctx context.Context, in RegistrationRequest, ip string) (*RegistrationResponse, error) {
	name, err := validName(in.ClientName)
	if err != nil {
		return nil, invalidMetadata(fmt.Sprintf("Name the app in client_name, in 1 to %d characters, as people will read it on the consent page.", MaxNameLength))
	}
	clientURI, refused := checkClientURI(in.ClientURI)
	if refused != nil {
		return nil, refused
	}
	redirects, refused := checkRedirects(in.RedirectURIs)
	if refused != nil {
		return nil, refused
	}
	// An app that names no scope may ask for both; the person decides.
	scopes := Scopes
	if strings.TrimSpace(in.Scope) != "" {
		if scopes, err = normalScopes("scope", strings.Fields(in.Scope)); err != nil {
			return nil, invalidMetadata(err.Error())
		}
	}
	// RFC 7591 section 2 makes a confidential app the default, as Armature does.
	method := in.TokenEndpointAuthMethod
	if method == "" {
		method = AuthBasic
	}
	if !slices.Contains(AuthMethods, method) {
		return nil, invalidMetadata("Choose none, client_secret_basic or client_secret_post as token_endpoint_auth_method.")
	}
	grants, refused := checkGrants(in.GrantTypes)
	if refused != nil {
		return nil, refused
	}
	for _, r := range in.ResponseTypes {
		if r != ResponseCode {
			return nil, invalidMetadata("Stator answers the code response type only.")
		}
	}
	var (
		secret string
		digest []byte
	)
	if method != AuthNone {
		if secret, digest, err = auth.GeneratePrefixed(auth.ClientSecretPrefix); err != nil {
			return nil, err
		}
	}
	out := &RegistrationResponse{
		ClientSecret: secret, ClientName: name, ClientURI: clientURI, RedirectURIs: redirects, Scope: joinScope(scopes),
		TokenEndpointAuthMethod: method, GrantTypes: grants, ResponseTypes: []string{ResponseCode},
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := deletePending(ctx, tx, s.now()); err != nil {
			return err
		}
		var (
			id      uuid.UUID
			created time.Time
		)
		if err := tx.QueryRow(ctx, `
			INSERT INTO oauth_client (name, scopes, kind, status, client_uri, redirect_uris, auth_method, secret_hash)
			VALUES ($1, $2, 'registered', 'pending', $3, $4, $5, $6)
			RETURNING id, created_at`, name, scopes, clientURI, redirects, method, digest).Scan(&id, &created); err != nil {
			return err
		}
		out.ClientID, out.ClientIDIssuedAt = id.String(), created.Unix()
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthClientRegistered, TargetType: "oauth_client", TargetID: &id, IP: ip,
			Data: map[string]any{"name": name, "clientUri": clientURI, "redirectUris": redirects, "scopes": scopes, "authMethod": method}})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// deletePending removes the apps nobody allowed in time; the sweep does it
// too, this keeps a burst of registrations from piling up between sweeps.
func deletePending(ctx context.Context, tx db.DBTX, now time.Time) error {
	_, err := tx.Exec(ctx, `DELETE FROM oauth_client WHERE status = 'pending' AND created_at < $1`, now.Add(-PendingClientTTL))
	return err
}
