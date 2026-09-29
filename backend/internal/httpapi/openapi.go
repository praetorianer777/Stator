package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/openapi"
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
	// request is the JSON body type the handler decodes.
	request any
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session.
	public bool
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
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := openapi.NewBuilder()

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
				"session": {Type: "apiKey", In: "cookie", Name: "stator_session", Description: "The cookie a sign-in sets."},
				"token":   {Type: "http", Scheme: "bearer", Description: "A personal access token."},
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
		if op.request != nil {
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: b.SchemaOf(op.request)},
			}}
		}
		for status, body := range op.responses {
			r := &openapi.Response{Description: http.StatusText(status)}
			if body != nil {
				r.Content = map[string]openapi.MediaType{"application/json": {Schema: envelopeSchema(b, body)}}
			}
			o.Responses[strconv.Itoa(status)] = r
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

func ok(body any) map[int]any { return map[int]any{200: body} }

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
