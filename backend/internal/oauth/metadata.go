package oauth

import "time"

// The metadata documents, adapted from Armature's
// backend/internal/oauth/metadata.go; Stator protects its MCP endpoint alone.

// The addresses partners read from the metadata rather than build themselves.
const (
	// APIPrefix is the API's, as the router mounts it.
	APIPrefix = "/api/v1"
	// MCPPath is the MCP endpoint under the API, the protected resource.
	MCPPath       = "/mcp"
	AuthorizePath = "/oauth/authorize"
	TokenPath     = "/oauth/token"
	RevokePath    = "/oauth/revoke"
	RegisterPath  = "/oauth/register"
	// MetadataPath is RFC 8414's document, under the issuer.
	MetadataPath = "/.well-known/oauth-authorization-server"
	// ResourceMetadataPath is RFC 9728's document; the resource's own path
	// follows it, and the bare path answers the same.
	ResourceMetadataPath = "/.well-known/oauth-protected-resource"
	// MetadataMaxAge is how long a partner may keep the metadata, as Armature's.
	MetadataMaxAge = 5 * time.Minute
)

// Client authentication methods (RFC 7591 section 2).
const (
	AuthBasic = "client_secret_basic"
	AuthPost  = "client_secret_post"
	AuthNone  = "none"
)

// AuthMethods are every way an app may authenticate at the token endpoint.
var AuthMethods = []string{AuthNone, AuthBasic, AuthPost}

// ResponseCode is the one response type, and ChallengeS256 the one PKCE method.
const (
	ResponseCode  = "code"
	ChallengeS256 = "S256"
)

// Metadata is the RFC 8414 document.
type Metadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethods          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethods     []string `json:"revocation_endpoint_auth_methods_supported"`
	AuthorizationResponseIssParameter bool     `json:"authorization_response_iss_parameter_supported"`
	ServiceDocumentation              string   `json:"service_documentation"`
}

// ResourceMetadata is RFC 9728's protected resource metadata, for MCP clients.
type ResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
	ResourceDocumentation  string   `json:"resource_documentation"`
}

// resourceName is what an MCP client may show a person for the endpoint.
const resourceName = "Stator MCP"

// Metadata is the document partners read every address from.
func (s *Service) Metadata() Metadata {
	return Metadata{
		Issuer:                            s.issuer,
		AuthorizationEndpoint:             s.issuer + APIPrefix + AuthorizePath,
		TokenEndpoint:                     s.issuer + APIPrefix + TokenPath,
		RevocationEndpoint:                s.issuer + APIPrefix + RevokePath,
		RegistrationEndpoint:              s.issuer + APIPrefix + RegisterPath,
		ScopesSupported:                   Scopes,
		ResponseTypesSupported:            []string{ResponseCode},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               GrantTypes,
		CodeChallengeMethodsSupported:     []string{ChallengeS256},
		TokenEndpointAuthMethods:          AuthMethods,
		RevocationEndpointAuthMethods:     AuthMethods,
		AuthorizationResponseIssParameter: true,
		ServiceDocumentation:              s.issuer + APIPrefix + "/openapi.json",
	}
}

// ResourceMetadata is the MCP endpoint's document.
func (s *Service) ResourceMetadata() ResourceMetadata {
	return ResourceMetadata{
		Resource:               s.Resource(),
		AuthorizationServers:   []string{s.issuer},
		ScopesSupported:        Scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           resourceName,
		ResourceDocumentation:  s.issuer + APIPrefix + "/openapi.json",
	}
}

// ResourceMetadataURL is where a 401 at the MCP endpoint points a client.
func (s *Service) ResourceMetadataURL() string {
	return s.issuer + ResourceMetadataPath + APIPrefix + MCPPath
}
