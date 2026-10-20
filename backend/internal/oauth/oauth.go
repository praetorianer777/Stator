// Package oauth hands partner tools a person's Stator token by token
// exchange (RFC 8693), as Armature does since Cloudster1/Armature#280: a
// partner that signed the person in through the shared Keycloak realm trades
// their access token for a grant, with no consent page and no pasted token.
package oauth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/oidc"
)

// CapabilityTokenExchange is what /meta lists for this, with the promise
// Armature's carries (Dynamo's docs/compatibility.md).
const CapabilityTokenExchange = "oauth.token-exchange@1"

// RFC 8693's names, and the grants the token endpoint serves.
const (
	GrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	GrantRefreshToken  = "refresh_token"
	TokenTypeAccess    = "urn:ietf:params:oauth:token-type:access_token"
)

// GrantTypes are the grants the metadata names.
var GrantTypes = []string{GrantTokenExchange, GrantRefreshToken}

// Scopes a partner app may hold. A grant holding read alone may only read;
// one holding write may do whatever its person may, and no more.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// Scopes lists every scope, as the metadata names them.
var Scopes = []string{ScopeRead, ScopeWrite}

const (
	// AccessTokenTTL and RefreshTokenTTL are Armature's: an hour, and 90
	// days from the last refresh.
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 90 * 24 * time.Hour
	// MaxNameLength bounds an app's name, which the token list shows.
	MaxNameLength = 60
	// MaxKeycloakClientIDLength bounds the Keycloak client an app names.
	MaxKeycloakClientIDLength = 255
	// MetadataPath is RFC 8414's document, under the issuer.
	MetadataPath = "/.well-known/oauth-authorization-server"
	// MetadataMaxAge is how long a partner may keep the metadata, as Armature's.
	MetadataMaxAge = 5 * time.Minute
)

// Error is a refusal in OAuth's own shape (RFC 6749 section 5.2).
type Error struct {
	Status      int    `json:"-"`
	Code        string `json:"error"`
	Description string `json:"error_description"`
	// Basic asks for WWW-Authenticate: Basic, for a client that used it.
	Basic bool `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func refuse(status int, code, description string) *Error {
	return &Error{Status: status, Code: code, Description: description}
}

// ended is what a grant that is no more is answered with.
func ended() *Error {
	return refuse(http.StatusBadRequest, "invalid_grant", "This connection has ended. Connect Stator again.")
}

// Client is a partner app, as an administrator registered it.
type Client struct {
	// ID is the client_id the app authenticates with.
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Scopes []string  `json:"scopes"`
	// KeycloakClientID is the realm's client whose access tokens the app
	// trades here; their azp names it.
	KeycloakClientID string `json:"keycloakClientId"`
	// Grants counts the people the app holds a live grant for.
	Grants    int       `json:"grants"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Secret is answered once, when the app is registered.
	Secret string `json:"secret,omitempty"`
}

// ClientInput registers an app; every field is needed.
type ClientInput struct {
	Name             string   `json:"name"`
	Scopes           []string `json:"scopes"`
	KeycloakClientID string   `json:"keycloakClientId"`
}

// ClientChanges changes an app; what is left out keeps its value. Narrowing
// the scopes narrows every grant of the app at once.
type ClientChanges struct {
	Name             *string   `json:"name,omitempty"`
	Scopes           *[]string `json:"scopes,omitempty"`
	KeycloakClientID *string   `json:"keycloakClientId,omitempty"`
}

// FieldError is input that cannot be stored, with the sentence to show by it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// ErrNoSuchClient is an app that is not registered.
var ErrNoSuchClient = errors.New("that connected app is not registered")

// TokenResponse is the token endpoint's answer (RFC 6749 section 5.1,
// RFC 8693 section 2.2.1).
type TokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	RefreshToken    string `json:"refresh_token"`
	Scope           string `json:"scope"`
	IssuedTokenType string `json:"issued_token_type,omitempty"`
}

// TokenRequest is the token endpoint's form, in OAuth's names.
type TokenRequest struct {
	GrantType          string `json:"grant_type"`
	SubjectToken       string `json:"subject_token,omitempty"`
	SubjectTokenType   string `json:"subject_token_type,omitempty"`
	RequestedTokenType string `json:"requested_token_type,omitempty"`
	RefreshToken       string `json:"refresh_token,omitempty"`
	Scope              string `json:"scope,omitempty"`
	ClientID           string `json:"client_id,omitempty"`
	ClientSecret       string `json:"client_secret,omitempty"`
}

// RevokeRequest is the revocation endpoint's form (RFC 7009).
type RevokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

// Metadata is the RFC 8414 document.
type Metadata struct {
	Issuer                        string   `json:"issuer"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RevocationEndpoint            string   `json:"revocation_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported"`
	TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethods []string `json:"revocation_endpoint_auth_methods_supported"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	ServiceDocumentation          string   `json:"service_documentation"`
}

// authMethods are how an app authenticates: HTTP Basic or the form.
var authMethods = []string{"client_secret_basic", "client_secret_post"}

// Service keeps the partner apps and serves the token endpoint.
type Service struct {
	db  *db.Cluster
	sso *oidc.Service
	// issuer is STATOR_APP_URL, the base of every address in the metadata.
	issuer string
	log    *slog.Logger
	now    func() time.Time
}

// NewService returns the service; sso verifies the provider's tokens.
func NewService(cluster *db.Cluster, sso *oidc.Service, issuer string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: cluster, sso: sso, issuer: strings.TrimSuffix(issuer, "/"), log: log, now: time.Now}
}

// Metadata is the document partners read every address from.
func (s *Service) Metadata(apiPrefix string) Metadata {
	return Metadata{
		Issuer:                        s.issuer,
		TokenEndpoint:                 s.issuer + apiPrefix + "/oauth/token",
		RevocationEndpoint:            s.issuer + apiPrefix + "/oauth/revoke",
		ScopesSupported:               Scopes,
		GrantTypesSupported:           GrantTypes,
		TokenEndpointAuthMethods:      authMethods,
		RevocationEndpointAuthMethods: authMethods,
		// No authorization endpoint: a person is never sent to a consent page.
		ResponseTypesSupported: []string{},
		ServiceDocumentation:   s.issuer + apiPrefix + "/openapi.json",
	}
}

// splitScope reads a scope parameter, each scope once.
func splitScope(raw string) []string {
	return slices.Compact(slices.Sorted(slices.Values(strings.Fields(raw))))
}

func joinScope(scopes []string) string { return strings.Join(scopes, " ") }

// normalScopes are scopes checked as an app may hold them: known, read
// among them, sorted and each once.
func normalScopes(field string, scopes []string) ([]string, error) {
	out := splitScope(strings.Join(scopes, " "))
	for _, scope := range out {
		if !slices.Contains(Scopes, scope) {
			return nil, &FieldError{Field: field, Message: fmt.Sprintf("%s is not a scope. Name read, and write for an app that changes pages.", scope)}
		}
	}
	if !slices.Contains(out, ScopeRead) {
		return nil, &FieldError{Field: field, Message: "Name read among the scopes; every app reads."}
	}
	return out, nil
}

// tokenScopes are what an api_token holds for a grant's scopes: read alone
// is a reading token, write one that may do what its person may.
func tokenScopes(scopes []string) []string {
	if slices.Contains(scopes, ScopeWrite) {
		return []string{}
	}
	return []string{ScopeRead}
}

// grantScopes are the scopes a grant's api_token holds, in OAuth's names.
func grantScopes(token []string) []string {
	if slices.Contains(token, ScopeRead) {
		return []string{ScopeRead}
	}
	return []string{ScopeRead, ScopeWrite}
}
