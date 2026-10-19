package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Armature as an authorization server (docs/oauth.md there): metadata,
// registration, the consent page, the token and revocation endpoints. The
// stub has no sessions, so its consent page asks who is allowing, by the
// organization and the name a personal token of the stub would carry.

const (
	oauthPrefix    = "/_stub-oauth"
	authorizePath  = "/oauth/authorize"
	wellKnownPath  = "/.well-known/oauth-authorization-server"
	accessPrefix   = "armature_oat_"
	refreshPrefix  = "armature_ort_"
	codePrefix     = "armature_oac_"
	clientSecretOf = "armature_ocs_"
	// accessLifetime is Armature's hour; a suite ends tokens with a control.
	accessLifetime = time.Hour
	codeLifetime   = 60 * time.Second
	secretBytes    = 32

	closedSentence  = "This Armature registers connected apps only by hand. Ask an Armature administrator to add Stator under Settings, Connected apps."
	endedSentence   = "This connection has ended. Connect Armature again."
	pendingSentence = "Stator is not connected to Armature yet. An Armature administrator connects it first."
	unknownSentence = "This app is not connected to Armature. Ask an Armature administrator to connect it."
)

type oauthClient struct {
	ID, Secret, Name string
	RedirectURIs     []string
	Scope            string
	Pending          bool
}

type oauthCode struct {
	client, redirect, challenge, scope, tenant, person string
	expires                                            time.Time
}

type oauthGrant struct {
	client, tenant, person, scope string
	access, refresh               string
	refreshes                     int
	ended                         bool
}

type oauthServer struct {
	mu      sync.Mutex
	clients map[string]*oauthClient
	codes   map[string]*oauthCode
	access  map[string]*oauthGrant
	refresh map[string]*oauthGrant
	// rotated are refresh tokens already traded; one presented again ends
	// its grant, as Armature has no grace window.
	rotated map[string]*oauthGrant
	grants  []*oauthGrant
	// closed are the hosts registration is closed for.
	closed map[string]bool
}

func newOAuth() *oauthServer {
	return &oauthServer{clients: map[string]*oauthClient{}, codes: map[string]*oauthCode{}, access: map[string]*oauthGrant{},
		refresh: map[string]*oauthGrant{}, rotated: map[string]*oauthGrant{}, closed: map[string]bool{}}
}

func secretWith(prefix string) string {
	buf := make([]byte, secretBytes)
	_, _ = rand.Read(buf)
	return prefix + base64.RawURLEncoding.EncodeToString(buf)
}

// holder is whom an access token acts for.
func (o *oauthServer) holder(token string) (string, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.access[token]
	if !ok || g.ended {
		return "", "", false
	}
	return g.tenant, g.person, true
}

// caller resolves a bearer: a grant's access token or a personal one.
func (s *stub) caller(token string) (*tenant, *person, bool) {
	if strings.HasPrefix(token, accessPrefix) {
		slug, name, ok := s.oauth.holder(token)
		if !ok {
			return nil, nil, false
		}
		t := s.world.tenant(slug)
		return t, t.person(name), true
	}
	return s.world.caller(token)
}

func issuerOf(r *http.Request) string { return "http://" + r.Host }

func (s *stub) serverMetadata(c *call) {
	s.serveMetadata(c.w, c.r)
}

func (s *stub) serveMetadata(w http.ResponseWriter, r *http.Request) {
	issuer := issuerOf(r)
	s.oauth.mu.Lock()
	closed := s.oauth.closed[strings.ToLower(r.Host)]
	s.oauth.mu.Unlock()
	meta := map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + authorizePath,
		"token_endpoint":                                 issuer + apiPrefix + "/oauth/token",
		"revocation_endpoint":                            issuer + apiPrefix + "/oauth/revoke",
		"scopes_supported":                               []string{"read", "issue.write", "issue.transition", "comment.write", "webhooks.partner"},
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:token-exchange"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"client_secret_basic", "client_secret_post", "none"},
		"revocation_endpoint_auth_methods_supported":     []string{"client_secret_basic", "client_secret_post", "none"},
		"authorization_response_iss_parameter_supported": true,
		"service_documentation":                          issuer + apiPrefix + "/openapi.json",
	}
	if !closed {
		meta["registration_endpoint"] = issuer + apiPrefix + "/oauth/register"
	}
	respond(w, http.StatusOK, meta)
}

func oauthRefuse(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	respond(w, status, map[string]string{"error": code, "error_description": description})
}

func (s *stub) register(c *call) {
	var req struct {
		ClientName              string   `json:"client_name"`
		ClientURI               string   `json:"client_uri"`
		RedirectURIs            []string `json:"redirect_uris"`
		Scope                   string   `json:"scope"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	if err := json.NewDecoder(io.LimitReader(c.r.Body, maxBody)).Decode(&req); err != nil {
		oauthRefuse(c.w, http.StatusBadRequest, "invalid_client_metadata", "The registration is not JSON.")
		return
	}
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	if s.oauth.closed[strings.ToLower(c.r.Host)] {
		oauthRefuse(c.w, http.StatusForbidden, "access_denied", closedSentence)
		return
	}
	if strings.TrimSpace(req.ClientName) == "" || len(req.RedirectURIs) == 0 {
		oauthRefuse(c.w, http.StatusBadRequest, "invalid_client_metadata", "Name the app and at least one redirect URI.")
		return
	}
	for _, u := range req.RedirectURIs {
		if parsed, err := url.Parse(u); err != nil || parsed.Host == "" || parsed.Fragment != "" {
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_redirect_uri", "Each redirect URI is an absolute address without a fragment.")
			return
		}
	}
	if req.Scope == "" {
		req.Scope = "read"
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "client_secret_basic"
	}
	client := &oauthClient{ID: uuid.NewString(), Name: req.ClientName, RedirectURIs: req.RedirectURIs, Scope: req.Scope, Pending: true}
	if req.TokenEndpointAuthMethod != "none" {
		client.Secret = secretWith(clientSecretOf)
	}
	s.oauth.clients[client.ID] = client
	answer := map[string]any{
		"client_id": client.ID, "client_id_issued_at": time.Now().Unix(), "client_secret_expires_at": 0,
		"client_name": req.ClientName, "client_uri": req.ClientURI, "redirect_uris": req.RedirectURIs, "scope": req.Scope,
		"token_endpoint_auth_method": req.TokenEndpointAuthMethod,
		"grant_types":                []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	}
	if client.Secret != "" {
		answer["client_secret"] = client.Secret
	}
	respond(c.w, http.StatusCreated, answer)
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Connect {{.App}} to Armature</title></head>
<body><main>
<h1>Connect {{.App}} to Armature</h1>
{{if .Refusal}}<p role="alert">{{.Refusal}}</p>{{end}}
{{if .Form}}<form method="post" action="` + authorizePath + `">
<input type="hidden" name="query" value="{{.Query}}">
<p>{{.App}} wants to act as you in Armature: {{.Scopes}}.</p>
<label>Armature organization <input name="tenant" value="{{.Tenant}}"></label>
<label>Your name in Armature <input name="person" value="{{.Person}}"></label>
<button type="submit" name="decision" value="allow">Allow</button>
<button type="submit" name="decision" value="deny">Cancel</button>
</form>{{end}}
</main></body></html>`))

type consentView struct {
	App, Query, Scopes, Tenant, Person, Refusal string
	Form                                        bool
}

// authorization checks a request for the consent page as Armature does,
// answering the client and nothing else when it may be asked at all.
func (s *stub) authorization(q url.Values) (*oauthClient, string) {
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	client, ok := s.oauth.clients[q.Get("client_id")]
	if !ok || !slices.Contains(client.RedirectURIs, q.Get("redirect_uri")) {
		return nil, unknownSentence
	}
	return client, ""
}

func backTo(w http.ResponseWriter, r *http.Request, redirect string, q url.Values) {
	target, _ := url.Parse(redirect)
	values := target.Query()
	for k, v := range q {
		values[k] = v
	}
	values.Set("iss", issuerOf(r))
	target.RawQuery = values.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func consentAnswer(w http.ResponseWriter, status int, v consentView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = consentPage.Execute(w, v)
}

func (s *stub) authorizePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, refusal := s.authorization(q)
	if client == nil {
		consentAnswer(w, http.StatusBadRequest, consentView{App: "This app", Refusal: refusal})
		return
	}
	redirect := q.Get("redirect_uri")
	switch {
	case q.Get("response_type") != "code":
		backTo(w, r, redirect, url.Values{"error": {"unsupported_response_type"}, "error_description": {"Armature answers response_type code only."}, "state": {q.Get("state")}})
		return
	case q.Get("state") == "" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		backTo(w, r, redirect, url.Values{"error": {"invalid_request"}, "error_description": {"Armature requires PKCE with S256."}, "state": {q.Get("state")}})
		return
	}
	consentAnswer(w, http.StatusOK, consentView{App: client.Name, Query: r.URL.RawQuery, Scopes: q.Get("scope"), Form: true})
}

func (s *stub) authorizeDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "The form could not be read.", http.StatusBadRequest)
		return
	}
	q, err := url.ParseQuery(r.PostForm.Get("query"))
	if err != nil {
		http.Error(w, "The form names no request.", http.StatusBadRequest)
		return
	}
	client, refusal := s.authorization(q)
	if client == nil {
		consentAnswer(w, http.StatusBadRequest, consentView{App: "This app", Refusal: refusal})
		return
	}
	state, redirect := q.Get("state"), q.Get("redirect_uri")
	if r.PostForm.Get("decision") != "allow" {
		backTo(w, r, redirect, url.Values{"error": {"access_denied"}, "error_description": {"The person did not allow the app."}, "state": {state}})
		return
	}
	tenant, person := strings.TrimSpace(r.PostForm.Get("tenant")), strings.TrimSpace(r.PostForm.Get("person"))
	view := consentView{App: client.Name, Query: r.PostForm.Get("query"), Scopes: q.Get("scope"), Tenant: tenant, Person: person, Form: true}
	if tenant == "" || person == "" {
		view.Refusal = "Name the organization and yourself, as a personal token of the stub would."
		consentAnswer(w, http.StatusUnprocessableEntity, view)
		return
	}
	s.oauth.mu.Lock()
	if client.Pending && person != adminName {
		s.oauth.mu.Unlock()
		consentAnswer(w, http.StatusOK, consentView{App: client.Name, Refusal: pendingSentence})
		return
	}
	client.Pending = false
	code := secretWith(codePrefix)
	s.oauth.codes[code] = &oauthCode{client: client.ID, redirect: redirect, challenge: q.Get("code_challenge"), scope: q.Get("scope"),
		tenant: tenant, person: person, expires: time.Now().Add(codeLifetime)}
	s.oauth.mu.Unlock()
	backTo(w, r, redirect, url.Values{"code": {code}, "state": {state}})
}

// authenticate finds the app a token or revocation request comes from, by
// HTTP Basic or the form, never both.
func (o *oauthServer) authenticate(w http.ResponseWriter, r *http.Request) *oauthClient {
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
		if r.PostForm.Has("client_id") || r.PostForm.Has("client_secret") {
			oauthRefuse(w, http.StatusBadRequest, "invalid_request", "Authenticate one way, not both.")
			return nil
		}
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	client, ok := o.clients[id]
	if !ok || client.Pending || client.Secret != secret {
		if basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="armature"`)
		}
		oauthRefuse(w, http.StatusUnauthorized, "invalid_client", "Armature does not know this app, or it is not allowed yet.")
		return nil
	}
	return client
}

func (o *oauthServer) issue(w http.ResponseWriter, g *oauthGrant) {
	if g.access != "" {
		delete(o.access, g.access)
	}
	if g.refresh != "" {
		delete(o.refresh, g.refresh)
		o.rotated[g.refresh] = g
	}
	g.access, g.refresh = secretWith(accessPrefix), secretWith(refreshPrefix)
	o.access[g.access], o.refresh[g.refresh] = g, g
	w.Header().Set("Cache-Control", "no-store")
	respond(w, http.StatusOK, map[string]any{"access_token": g.access, "token_type": "Bearer", "expires_in": int(accessLifetime.Seconds()),
		"refresh_token": g.refresh, "scope": g.scope})
}

func (o *oauthServer) end(g *oauthGrant) {
	g.ended = true
	delete(o.access, g.access)
	delete(o.refresh, g.refresh)
}

func (s *stub) token(c *call) {
	if err := c.r.ParseForm(); err != nil {
		oauthRefuse(c.w, http.StatusBadRequest, "invalid_request", "Send the request as a form.")
		return
	}
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	client := s.oauth.authenticate(c.w, c.r)
	if client == nil {
		return
	}
	form := c.r.PostForm
	switch form.Get("grant_type") {
	case "authorization_code":
		code, ok := s.oauth.codes[form.Get("code")]
		delete(s.oauth.codes, form.Get("code"))
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		if !ok || code.client != client.ID || code.redirect != form.Get("redirect_uri") || time.Now().After(code.expires) ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != code.challenge {
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_grant", endedSentence)
			return
		}
		var g *oauthGrant
		for _, each := range s.oauth.grants {
			if !each.ended && each.client == client.ID && each.tenant == code.tenant && each.person == code.person {
				g = each
			}
		}
		if g == nil {
			g = &oauthGrant{client: client.ID, tenant: code.tenant, person: code.person}
			s.oauth.grants = append(s.oauth.grants, g)
		}
		g.scope = code.scope
		s.oauth.issue(c.w, g)
	case "refresh_token":
		presented := form.Get("refresh_token")
		if g, reused := s.oauth.rotated[presented]; reused {
			s.oauth.end(g)
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_grant", endedSentence)
			return
		}
		g, ok := s.oauth.refresh[presented]
		if !ok || g.ended || g.client != client.ID {
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_grant", endedSentence)
			return
		}
		g.refreshes++
		s.oauth.issue(c.w, g)
	default:
		oauthRefuse(c.w, http.StatusBadRequest, "unsupported_grant_type", "The stub trades codes and refresh tokens.")
	}
}

func (s *stub) revoke(c *call) {
	if err := c.r.ParseForm(); err != nil {
		oauthRefuse(c.w, http.StatusBadRequest, "invalid_request", "Send the request as a form.")
		return
	}
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	client := s.oauth.authenticate(c.w, c.r)
	if client == nil {
		return
	}
	token := c.r.PostForm.Get("token")
	for _, g := range []*oauthGrant{s.oauth.access[token], s.oauth.refresh[token]} {
		if g != nil && g.client == client.ID {
			s.oauth.end(g)
		}
	}
	c.w.WriteHeader(http.StatusOK)
}

// oauthControls are how a suite steers the authorization server: close
// registration for a host, end a person's access tokens so the next call
// renews them, end their grants as Armature's Disconnect would, and read
// what the grants went through.
func (s *stub) oauthControls(mux *http.ServeMux) {
	mux.HandleFunc("PUT "+oauthPrefix+"/hosts/{host}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Registration string `json:"registration"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&body); err != nil || (body.Registration != "closed" && body.Registration != "approve") {
			refuse(w, http.StatusBadRequest, "bad_request", "Send registration closed or approve.")
			return
		}
		s.oauth.mu.Lock()
		s.oauth.closed[strings.ToLower(r.PathValue("host"))] = body.Registration == "closed"
		s.oauth.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	each := func(fn func(*oauthGrant)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.oauth.mu.Lock()
			defer s.oauth.mu.Unlock()
			for _, g := range s.oauth.grants {
				if g.tenant == r.PathValue("tenant") && g.person == r.PathValue("person") && !g.ended {
					fn(g)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("POST "+oauthPrefix+"/{tenant}/people/{person}/expire", each(func(g *oauthGrant) { delete(s.oauth.access, g.access) }))
	mux.HandleFunc("POST "+oauthPrefix+"/{tenant}/people/{person}/end", each(s.oauth.end))
	mux.HandleFunc("GET "+oauthPrefix+"/{tenant}/grants", func(w http.ResponseWriter, r *http.Request) {
		s.oauth.mu.Lock()
		defer s.oauth.mu.Unlock()
		out := []map[string]any{}
		for _, g := range s.oauth.grants {
			if g.tenant == r.PathValue("tenant") {
				out = append(out, map[string]any{"person": g.person, "clientId": g.client, "scope": g.scope, "refreshes": g.refreshes, "ended": g.ended})
			}
		}
		respond(w, http.StatusOK, map[string]any{"grants": out})
	})
}
