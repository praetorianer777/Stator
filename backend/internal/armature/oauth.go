package armature

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Stator as a connected app of Armature (#428): Armature's authorization code
// flow with PKCE, as its docs/oauth.md sets it out.

// CapabilityPartnerTokens is Armature offering connected apps: the Connect
// button is shown only when /meta lists it.
const CapabilityPartnerTokens = "oauth.partner-tokens@1"

const (
	// ServerMetadataPath is RFC 8414's document under Armature's issuer, which
	// names every other address; Stator builds none of them itself.
	ServerMetadataPath = "/.well-known/oauth-authorization-server"
	// OAuthStartPath and OAuthCallbackPath are Stator's own, under its origin;
	// the callback is the one return address Stator registers.
	OAuthStartPath    = "/api/v1/armature/oauth/start"
	OAuthCallbackPath = "/api/v1/armature/oauth/callback"
	// AppName is what Armature's consent page and token list call Stator.
	AppName = "Stator"
	// AppScopes are what Stator asks for: linking a page to an issue takes
	// issue.write, and Armature wants read beside every other scope.
	AppScopes = "read issue.write"
	// ClientSecretPrefix starts every secret Armature shows for an app.
	ClientSecretPrefix = "armature_ocs_"
	// MaxClientIDLength bounds a pasted client id; Armature's are uuids.
	MaxClientIDLength = 200
	// OAuthAttemptTTL is how long a trip to the consent page may take.
	OAuthAttemptTTL = 10 * time.Minute
	// RefreshAhead is how long before its end an access token is renewed, so
	// a call is not sent with one that runs out on the way.
	RefreshAhead = 2 * time.Minute
	// MetadataTTL is how long an Armature's metadata is kept; Armature
	// itself says it may be cached for five minutes.
	MetadataTTL = 5 * time.Minute
	// stateBytes makes a state of 256 bits; Armature asks for at least 128.
	stateBytes = 32
	// pkceMethod is the one method Armature takes.
	pkceMethod = "S256"
)

// OAuth error codes Stator acts on, as RFC 6749 and RFC 7591 name them.
const (
	oauthAccessDenied  = "access_denied"
	oauthInvalidGrant  = "invalid_grant"
	oauthInvalidClient = "invalid_client"
)

// ServerMetadata is what Stator reads of Armature's RFC 8414 document.
type ServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	ISSParameter          bool     `json:"authorization_response_iss_parameter_supported"`
}

// OAuthError is a refusal in OAuth's own shape, with Armature's sentence.
type OAuthError struct {
	Status      int
	Code        string
	Description string
}

func (e *OAuthError) Error() string {
	return fmt.Sprintf("Armature refused with %d %s: %s", e.Status, e.Code, e.Description)
}

// Registration is an RFC 7591 request, in its own names.
type Registration struct {
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

// Registered is what Armature answers a registration with.
type Registered struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope,omitempty"`
}

// AppCredentials authenticate Stator at the token and revocation endpoints;
// a public app has no secret.
type AppCredentials struct {
	ClientID     string
	ClientSecret string
}

// TokenSet is the token endpoint's answer.
type TokenSet struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope,omitempty"`
}

// ServerMetadata reads the document under baseURL and holds it to what Stator
// needs: the issuer is the address the site connected, and PKCE is S256.
func (c *Client) ServerMetadata(ctx context.Context, baseURL string) (*ServerMetadata, error) {
	base := strings.TrimSuffix(baseURL, "/")
	var m ServerMetadata
	if err := c.do(ctx, http.MethodGet, base+ServerMetadataPath, "", nil, &m); err != nil {
		return nil, err
	}
	if strings.TrimSuffix(m.Issuer, "/") != base || m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return nil, fmt.Errorf("%w: the metadata at %s names the issuer %q", ErrUnreachable, base, m.Issuer)
	}
	supported := false
	for _, method := range m.CodeChallengeMethods {
		supported = supported || method == pkceMethod
	}
	if !supported {
		return nil, fmt.Errorf("%w: Armature at %s does not offer PKCE with S256", ErrUnreachable, base)
	}
	return &m, nil
}

// Register asks Armature to register Stator; an Armature that registers apps
// only by hand answers an OAuthError with access_denied.
func (c *Client) Register(ctx context.Context, endpoint string, r Registration) (*Registered, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var out Registered
	if err := c.oauth(ctx, endpoint, "application/json", bytes.NewReader(body), nil, &out); err != nil {
		return nil, err
	}
	if out.ClientID == "" {
		return nil, fmt.Errorf("%w: the registration named no client", ErrUnreachable)
	}
	return &out, nil
}

// Token posts form to the token endpoint as app.
func (c *Client) Token(ctx context.Context, endpoint string, app AppCredentials, form url.Values) (*TokenSet, error) {
	var out TokenSet
	if err := c.oauth(ctx, endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &app, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" || out.ExpiresIn <= 0 {
		return nil, fmt.Errorf("%w: the token endpoint answered no usable tokens", ErrUnreachable)
	}
	return &out, nil
}

// Revoke ends the grant token belongs to; Armature answers every token alike.
func (c *Client) Revoke(ctx context.Context, endpoint string, app AppCredentials, token string) error {
	form := url.Values{"token": {token}, "token_type_hint": {"refresh_token"}}
	return c.oauth(ctx, endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &app, nil)
}

// oauth posts to one of Armature's OAuth endpoints, which answer OAuth's
// error shape rather than Armature's envelope. A confidential app
// authenticates with HTTP Basic, the id and secret form-encoded first
// (RFC 6749 section 2.3.1); a public one names its id in the form.
func (c *Client) oauth(ctx context.Context, endpoint, kind string, body io.Reader, app *AppCredentials, out any) error {
	if app != nil && app.ClientSecret == "" {
		raw, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		form, _ := url.ParseQuery(string(raw))
		form.Set("client_id", app.ClientID)
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", kind)
	if app != nil && app.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(raw) > MaxResponseBytes {
		return fmt.Errorf("%w: the answer of %s could not be read", ErrUnreachable, req.URL.Host)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	case resp.StatusCode >= http.StatusBadRequest:
		var refused struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(raw, &refused) != nil || refused.Error == "" {
			return fmt.Errorf("%w: %s answered %d without an OAuth error", ErrUnreachable, req.URL.Host, resp.StatusCode)
		}
		return &OAuthError{Status: resp.StatusCode, Code: refused.Error, Description: refused.Description}
	case resp.StatusCode >= http.StatusMultipleChoices:
		return fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %w: %w", ErrUnreachable, errUnreadable, err)
	}
	return nil
}

// isOAuth says whether err is Armature refusing with code.
func isOAuth(err error, code string) bool {
	var refused *OAuthError
	return errors.As(err, &refused) && refused.Code == code
}
