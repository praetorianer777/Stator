package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/openapi"
	"github.com/praetorianer777/stator/backend/internal/theme"
)

// The API described in terms of the code that serves it. Every route in Routes
// has a row in operations, and a test refuses a router and a table that
// disagree; the document is then derived from the rows by reflection.

// env is a response envelope: the one or two keys a handler wraps its result
// in. A nil value is an untyped JSON value; a typed nil pointer is nullable.
type env map[string]any

// param is a query parameter.
type param struct {
	name, description string
	schema            *openapi.Schema
	repeated          bool
}

// operation is one row of the table.
type operation struct {
	method, path string
	handler      string
	// id names the operation for clients when the handler's name would not
	// be unique, as when one handler serves two paths.
	id      string
	summary string
	tag     string
	query   []param
	// request is the JSON body type the handler decodes; multipart says the
	// body is a file upload in a part named file instead.
	request   any
	multipart bool
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session; binary ones answer with a file.
	public bool
	binary bool
	// redirect routes answer with a Location header rather than a body.
	redirect bool
}

// operations is the table. Order is by area, then by path; paths are relative
// to APIPrefix except the probes, which live at the root.
var operations = []operation{
	{method: "GET", path: "/healthz", handler: "handleLiveness", tag: "health", summary: "Whether the process is running.", public: true,
		responses: ok(statusResponse{})},
	{method: "GET", path: "/readyz", handler: "handleReadiness", tag: "health", summary: "Whether the process can serve traffic, and how reads are routed.", public: true,
		responses: map[int]any{200: readinessResponse{}, 503: statusResponse{}}},
	{method: "GET", path: "/openapi.json", handler: "handleOpenAPI", tag: "health", summary: "This document.", public: true,
		responses: ok(openapi.Document{})},

	{method: "POST", path: "/auth/login", handler: "handleLogin", tag: "auth", summary: "Sign in a local account with email and password, and set the session cookie.", public: true,
		request: loginRequest{}, responses: ok(meResponse{})},
	{method: "GET", path: "/auth/oidc/{orgSlug}/start", handler: "handleOIDCStart", tag: "auth", summary: "Begin signing in through the organization's identity provider.", public: true, redirect: true,
		query: []param{{name: "next", description: "A path of this application to land on afterwards."}}, responses: map[int]any{}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", summary: "Where the identity provider sends the browser back; sets the session cookie.", public: true, redirect: true,
		query: []param{{name: "state"}, {name: "code"}, {name: "error"}}, responses: map[int]any{}},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", summary: "End the session.", responses: none()},
	{method: "GET", path: "/auth/me", handler: "handleMe", tag: "auth", summary: "Who is signed in, and the organizations they may act in.",
		responses: ok(meResponse{})},
	{method: "POST", path: "/auth/switch-org", handler: "handleSwitchOrg", tag: "auth", summary: "Move the session to another organization.",
		request: switchOrgRequest{}, responses: ok(env{"organization": auth.CurrentOrg{}})},

	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", tag: "access", summary: "The organization's identity provider, if one is configured. For administrators.",
		responses: ok(providerView{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", tag: "access", summary: "Configure the organization's identity provider. For administrators.",
		request: saveOIDCProviderRequest{}, responses: ok(providerView{})},
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", tag: "access", summary: "Who signed in through the identity provider and is waiting to be let in. For administrators.",
		responses: ok(env{"requests": []auth.JoinRequest{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", tag: "access", summary: "Let a waiting person in with the standing given. For administrators.",
		request: admitRequest{}, responses: ok(env{"membership": auth.Membership{}})},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", tag: "access", summary: "Turn a waiting person away; they may ask again. For administrators.",
		responses: none()},
	{method: "GET", path: "/org/tokens", handler: "handleListOrgAPITokens", tag: "access", summary: "Every personal access token in the organization, with whose it is. For administrators.",
		responses: ok(env{"tokens": []auth.OrgAPIToken{}})},
	{method: "DELETE", path: "/org/tokens/{tokenID}", handler: "handleRevokeOrgAPIToken", tag: "access", summary: "Revoke anybody's token in the organization. For administrators.",
		responses: none()},

	{method: "GET", path: "/tokens", handler: "handleListAPITokens", tag: "tokens", summary: "The caller's personal access tokens in this organization, without their secrets.",
		responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", tag: "tokens", summary: "Make a personal access token; the secret is in this answer and never again. Needs a session.",
		request: createTokenRequest{}, responses: created(env{"token": auth.APIToken{}})},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", tag: "tokens", summary: "Revoke one of the caller's tokens; it stops working at once.",
		responses: none()},

	// Themes, as Armature serves them.
	{method: "GET", path: "/themes", handler: "handleListThemes", tag: "themes", summary: "Themes the caller may use: theirs, then the shared ones.", responses: ok(env{"themes": []theme.Theme{}})},
	{method: "POST", path: "/themes", handler: "handleCreateTheme", tag: "themes", summary: "Make a theme.", request: theme.Input{}, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/examples", handler: "handleThemeExamples", tag: "themes", summary: "The themes shipped with the product, to start a theme from.", responses: ok(env{"examples": []theme.Example{}})},
	{method: "GET", path: "/themes/active", handler: "handleActiveTheme", tag: "themes", summary: "The theme the caller sees: chosen, the organization's default, or null for the built-in one.", responses: ok(env{"theme": (*theme.Theme)(nil), "source": ""})},
	{method: "PUT", path: "/themes/active", handler: "handleChooseTheme", tag: "themes", summary: "Use a theme; null returns to the organization's default, null with builtIn keeps the built-in one.", request: chooseThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "PUT", path: "/themes/default", handler: "handleSetDefaultTheme", tag: "themes", summary: "Name the shared theme everybody sees until they choose, or null for the built-in one.", request: defaultThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "POST", path: "/themes/import", handler: "handleImportTheme", tag: "themes", summary: "Make a theme of the caller's own from an exported theme file, sent as a multipart part named file.", multipart: true, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/{themeID}/export", handler: "handleExportTheme", tag: "themes", summary: "The theme as one file, its pictures and fonts inside, as a download.", binary: true, responses: ok(nil)},
	{method: "GET", path: "/themes/{themeID}", handler: "handleGetTheme", tag: "themes", summary: "One theme.", responses: ok(env{"theme": theme.Theme{}})},
	{method: "PATCH", path: "/themes/{themeID}", handler: "handleUpdateTheme", tag: "themes", summary: "Change a theme; the owner's to do, or an administrator's once shared.", request: theme.Input{}, responses: ok(env{"theme": theme.Theme{}})},
	{method: "DELETE", path: "/themes/{themeID}", handler: "handleDeleteTheme", tag: "themes", summary: "Delete a theme; everybody using it returns to the built-in one.", responses: none()},
	{method: "POST", path: "/themes/{themeID}/assets", handler: "handleUploadThemeAsset", tag: "themes", summary: "Put a picture or a font on a theme, as a multipart part named file.", multipart: true, responses: created(env{"asset": theme.Asset{}})},
	{method: "GET", path: "/themes/{themeID}/assets/{assetID}", handler: "handleThemeAsset", tag: "themes", summary: "The bytes of a theme's file, as a download.", binary: true, responses: ok(nil)},
	{method: "DELETE", path: "/themes/{themeID}/assets/{assetID}", handler: "handleDeleteThemeAsset", tag: "themes", summary: "Take a file off a theme it no longer uses.", responses: none()},
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := openapi.NewBuilder()
	b.FieldOverrides["Backdrop.fit"] = &openapi.Schema{Type: "string", Enum: theme.BackdropFits}
	scopes := &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: []string{auth.ScopeRead}}}
	b.FieldOverrides["APIToken.scopes"] = scopes
	b.FieldOverrides["OrgAPIToken.scopes"] = scopes
	b.FieldOverrides["CreateTokenRequest.scopes"] = scopes
	roles := make([]string, len(auth.OrgRoles))
	for i, role := range auth.OrgRoles {
		roles[i] = string(role)
	}
	b.Enums[reflect.TypeOf(auth.OrgRole(""))] = roles

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   "Stator",
			Version: "1",
			Description: "The wiki's HTTP API. Every endpoint answers JSON; errors share one envelope. " +
				"Sign in with a session cookie or an API token sent as a bearer token.",
		},
		Servers: []openapi.Server{{URL: APIPrefix, Description: "This deployment"}},
		Paths:   map[string]openapi.PathItem{},
		Components: openapi.Components{
			SecuritySchemes: map[string]openapi.SecurityScheme{
				"session": {Type: "apiKey", In: "cookie", Name: config.DefaultSessionCookie, Description: "The cookie a sign-in sets."},
				"token": {Type: "http", Scheme: "bearer", BearerFormat: auth.APITokenPrefix + "<43 characters>",
					Description: "A personal access token, made under Tokens in the account menu; a token with the read scope is refused every write."},
			},
		},
		Security: []map[string][]string{{"session": {}}, {"token": {}}},
	}

	errorSchema := b.Schema(reflect.TypeOf(errorEnvelope{}))
	tags := map[string]bool{}
	for _, op := range operations {
		item := doc.Paths[op.path]
		if item == nil {
			item = openapi.PathItem{}
			doc.Paths[op.path] = item
		}
		o := &openapi.Operation{
			OperationID: op.operationID(),
			Summary:     op.summary,
			Tags:        []string{op.tag},
			Responses:   map[string]*openapi.Response{},
		}
		tags[op.tag] = true
		for _, name := range pathParams(op.path) {
			o.Parameters = append(o.Parameters, openapi.Parameter{
				Name: name, In: "path", Required: true, Schema: pathParamSchema(name),
			})
		}
		for _, q := range op.query {
			schema := q.schema
			if schema == nil {
				schema = &openapi.Schema{Type: "string"}
			}
			if q.repeated {
				schema = &openapi.Schema{Type: "array", Items: schema}
			}
			o.Parameters = append(o.Parameters, openapi.Parameter{Name: q.name, In: "query", Description: q.description, Schema: schema})
		}
		switch {
		case op.multipart:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"multipart/form-data": {Schema: &openapi.Schema{Type: "object", Required: []string{"file"},
					Properties: map[string]*openapi.Schema{"file": {Type: "string", Format: "binary"}}}},
			}}
		case op.request != nil:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: b.SchemaOf(op.request)},
			}}
		}
		for status, body := range op.responses {
			r := &openapi.Response{Description: http.StatusText(status)}
			switch {
			case op.binary && status == http.StatusOK:
				r.Content = map[string]openapi.MediaType{"*/*": {Schema: &openapi.Schema{Type: "string", Format: "binary"}}}
			case body != nil:
				r.Content = map[string]openapi.MediaType{"application/json": {Schema: envelopeSchema(b, body)}}
			}
			o.Responses[strconv.Itoa(status)] = r
		}
		if op.redirect {
			o.Responses["302"] = &openapi.Response{Description: "Found: the browser is sent on."}
		}
		o.Responses["default"] = &openapi.Response{Description: "An error, in the one shape every endpoint uses.",
			Content: map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}}
		if op.public {
			o.Security = []map[string][]string{}
		}
		item[strings.ToLower(op.method)] = o
	}
	for _, tag := range openapi.SortedKeys(tags) {
		doc.Tags = append(doc.Tags, openapi.Tag{Name: tag})
	}
	doc.Components.Schemas = b.Components()
	return doc
}

// operationID is what a client calls the operation: the handler's name unless
// the table says otherwise.
func (op operation) operationID() string {
	if op.id != "" {
		return op.id
	}
	id := strings.TrimPrefix(op.handler, "handle")
	return strings.ToLower(id[:1]) + id[1:]
}

// envelopeSchema describes a response body: an env becomes an inline object
// with every key required, anything else is the type itself.
func envelopeSchema(b *openapi.Builder, body any) *openapi.Schema {
	e, ok := body.(env)
	if !ok {
		return b.SchemaOf(body)
	}
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, key := range openapi.SortedKeys(e) {
		schema := b.SchemaOf(e[key])
		if schema == nil {
			schema = &openapi.Schema{Description: "A JSON value."}
		}
		s.Properties[key] = schema
		s.Required = append(s.Required, key)
	}
	return s
}

var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

func pathParams(path string) []string {
	var out []string
	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

// pathParamSchema types a path parameter by its name: ids are uuids, the rest
// strings.
func pathParamSchema(name string) *openapi.Schema {
	if strings.HasSuffix(name, "ID") {
		return &openapi.Schema{Type: "string", Format: "uuid"}
	}
	return &openapi.Schema{Type: "string"}
}

func ok(body any) map[int]any      { return map[int]any{200: body} }
func created(body any) map[int]any { return map[int]any{201: body} }
func none() map[int]any            { return map[int]any{204: nil} }

// Route is one operation as the integration suite sees it: which request it
// answers, and whether its answer is a file or a redirect.
type Route struct {
	Method, Path, ID string
	Public, Binary   bool
	Redirect         bool
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		out = append(out, Route{Method: op.method, Path: op.path, ID: op.operationID(), Public: op.public, Binary: op.binary, Redirect: op.redirect})
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
