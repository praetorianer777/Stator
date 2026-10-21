package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Exciter as the token-exchange server Stator connects people at sign-in
// with (Cloudster1/Exciter#273, built beside this): a partner app an
// administrator registered with its Keycloak client trades a person's
// access token of the shared realm for a grant, at POST /oauth/token, as
// Armature does since Cloudster1/Armature#280.

const (
	oauthPrefix        = "/_stub-oauth"
	accessPrefix       = "exciter_oat_"
	refreshPrefix      = "exciter_ort_"
	clientSecretOf     = "exciter_ocs_"
	grantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenTypeAccess    = "urn:ietf:params:oauth:token-type:access_token"
	// defaultAudience is the client id of Exciter's own sign-in in the realm dynamo.
	defaultAudience = "exciter"
	accessLifetime  = time.Hour
	secretBytes     = 32
	// exchangeScope is what Exciter grants a partner here: everything its
	// person may, as a personal access token without the read-only mark.
	exchangeScope = "read write"
	endedSentence = "This connection has ended. Connect Exciter again."
)

type oauthClient struct {
	ID, Secret, Name string
	Exchange         *exchangeSetting
}

// exchangeSetting is an app's Keycloak client, as Exciter's administrator
// names it, and who in the stub's tenant each verified address is.
type exchangeSetting struct {
	KeycloakClientID string `json:"keycloakClientId"`
	// Audience is the client id Exciter's own sign-in uses, which a token to
	// exchange must name in aud.
	Audience string `json:"audience"`
	Tenant   string `json:"tenant"`
	// People maps a verified address to the person of Tenant who has it;
	// anybody else has no Exciter account.
	People map[string]string `json:"people"`
}

type oauthGrant struct {
	client, tenant, person string
	access, refresh        string
	refreshes              int
	ended                  bool
}

type oauthServer struct {
	mu      sync.Mutex
	clients map[string]*oauthClient
	access  map[string]*oauthGrant
	refresh map[string]*oauthGrant
	// rotated are refresh tokens already traded; one presented again ends its grant.
	rotated map[string]*oauthGrant
	grants  []*oauthGrant
}

func newOAuth() *oauthServer {
	return &oauthServer{clients: map[string]*oauthClient{}, access: map[string]*oauthGrant{},
		refresh: map[string]*oauthGrant{}, rotated: map[string]*oauthGrant{}}
}

func secretWith(prefix string) string {
	buf := make([]byte, secretBytes)
	_, _ = rand.Read(buf)
	return prefix + base64.RawURLEncoding.EncodeToString(buf)
}

// holder is whom a grant's access token acts for.
func (o *oauthServer) holder(token string) (string, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.access[token]
	if !ok || g.ended {
		return "", "", false
	}
	return g.tenant, g.person, true
}

func oauthRefuse(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	respond(w, status, map[string]string{"error": code, "error_description": description})
}

// authenticate finds the app a request comes from, by HTTP Basic or the form.
func (o *oauthServer) authenticate(w http.ResponseWriter, r *http.Request) *oauthClient {
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	client, ok := o.clients[id]
	if !ok || client.Secret != secret {
		if basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="exciter"`)
		}
		oauthRefuse(w, http.StatusUnauthorized, "invalid_client", "Exciter does not know this app, or its secret is wrong.")
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
		"refresh_token": g.refresh, "scope": exchangeScope})
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
	o := s.oauth
	o.mu.Lock()
	defer o.mu.Unlock()
	client := o.authenticate(c.w, c.r)
	if client == nil {
		return
	}
	form := c.r.PostForm
	switch form.Get("grant_type") {
	case grantTokenExchange:
		o.exchange(c.w, client, form)
	case "refresh_token":
		presented := form.Get("refresh_token")
		if g, reused := o.rotated[presented]; reused {
			o.end(g)
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_grant", endedSentence)
			return
		}
		g, ok := o.refresh[presented]
		if !ok || g.ended || g.client != client.ID {
			oauthRefuse(c.w, http.StatusBadRequest, "invalid_grant", endedSentence)
			return
		}
		g.refreshes++
		o.issue(c.w, g)
	default:
		oauthRefuse(c.w, http.StatusBadRequest, "unsupported_grant_type", "The stub trades identity provider tokens and refresh tokens.")
	}
}

// exchange trades a person's token from the shared identity provider for a
// grant, as Exciter does for an app whose Keycloak client is named.
func (o *oauthServer) exchange(w http.ResponseWriter, client *oauthClient, form url.Values) {
	if form.Get("subject_token") == "" || form.Get("subject_token_type") != tokenTypeAccess {
		oauthRefuse(w, http.StatusBadRequest, "invalid_request", "Send subject_token, and subject_token_type "+tokenTypeAccess+".")
		return
	}
	if client.Exchange == nil || client.Exchange.KeycloakClientID == "" {
		oauthRefuse(w, http.StatusBadRequest, "unauthorized_client",
			client.Name+" may not exchange identity provider tokens. An Exciter administrator names its Keycloak client for the app.")
		return
	}
	claims, err := providerClaims(form.Get("subject_token"))
	if err == nil {
		err = claims.heldTo(client.Exchange)
	}
	if err != nil {
		oauthRefuse(w, http.StatusBadRequest, "invalid_grant", "The identity provider's token was refused: "+err.Error()+".")
		return
	}
	person := client.Exchange.People[strings.ToLower(claims.Email)]
	if person == "" {
		oauthRefuse(w, http.StatusBadRequest, "invalid_grant", claims.Name+" has no Exciter account yet. Sign in to Exciter once, or ask an administrator to let them in.")
		return
	}
	var g *oauthGrant
	for _, each := range o.grants {
		if !each.ended && each.client == client.ID && each.tenant == client.Exchange.Tenant && each.person == person {
			g = each
		}
	}
	if g == nil {
		g = &oauthGrant{client: client.ID, tenant: client.Exchange.Tenant, person: person}
		o.grants = append(o.grants, g)
	}
	o.issue(w, g)
}

// tokenClaims are what Exciter reads of an identity provider's access token.
type tokenClaims struct {
	AuthorizedBy  string          `json:"azp"`
	Audience      json.RawMessage `json:"aud"`
	Expires       int64           `json:"exp"`
	Email         string          `json:"email"`
	EmailVerified bool            `json:"email_verified"`
	Name          string          `json:"name"`
}

// providerClaims reads a token's claims without checking its signature: the
// stub stands in for Exciter's answers, and Exciter checks the signature
// against its provider's keys, which the stub's network may not reach under
// the token's issuer.
func providerClaims(token string) (*tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("it is not a JSON web token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("its claims are not base64url")
	}
	var claims tokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, errors.New("its claims are not JSON")
	}
	return &claims, nil
}

// heldTo checks the claims as Exciter does: unexpired, issued to the app's
// Keycloak client, meant for Exciter, and naming a verified address.
func (c *tokenClaims) heldTo(e *exchangeSetting) error {
	var audience []string
	if json.Unmarshal(c.Audience, &audience) != nil {
		var one string
		_ = json.Unmarshal(c.Audience, &one)
		audience = []string{one}
	}
	want := e.Audience
	if want == "" {
		want = defaultAudience
	}
	switch {
	case time.Now().Unix() >= c.Expires:
		return errors.New("it has expired")
	case c.AuthorizedBy != e.KeycloakClientID:
		return fmt.Errorf("it was issued to %s, not to %s", c.AuthorizedBy, e.KeycloakClientID)
	case !slices.Contains(audience, want):
		return fmt.Errorf("its audience does not hold %s", want)
	case c.Email == "" || !c.EmailVerified:
		return errors.New("it names no verified address")
	}
	return nil
}

// revoke is RFC 7009: a grant's access or refresh token ends the grant.
func (s *stub) revoke(c *call) {
	if err := c.r.ParseForm(); err != nil {
		oauthRefuse(c.w, http.StatusBadRequest, "invalid_request", "Send the request as a form.")
		return
	}
	o := s.oauth
	o.mu.Lock()
	defer o.mu.Unlock()
	client := o.authenticate(c.w, c.r)
	if client == nil {
		return
	}
	token := c.r.PostForm.Get("token")
	for _, g := range []*oauthGrant{o.access[token], o.refresh[token]} {
		if g != nil && g.client == client.ID {
			o.end(g)
		}
	}
	c.w.WriteHeader(http.StatusOK)
}

// oauthControls are how a suite steers it: register an app as Exciter's
// administrator does, end a person's access token so the next call renews
// it, end their grants, and read what the grants went through.
func (s *stub) oauthControls(mux *http.ServeMux) {
	o := s.oauth
	mux.HandleFunc("POST "+oauthPrefix+"/clients", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string           `json:"name"`
			Exchange *exchangeSetting `json:"exchange"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&body); err != nil || body.Name == "" {
			refuse(w, http.StatusBadRequest, "bad_request", "Send the app's name and how it exchanges tokens.")
			return
		}
		if body.Exchange != nil {
			people := map[string]string{}
			for email, person := range body.Exchange.People {
				people[strings.ToLower(email)] = person
			}
			body.Exchange.People = people
		}
		client := &oauthClient{ID: uuid.NewString(), Secret: secretWith(clientSecretOf), Name: body.Name, Exchange: body.Exchange}
		o.mu.Lock()
		o.clients[client.ID] = client
		o.mu.Unlock()
		respond(w, http.StatusCreated, map[string]any{"clientId": client.ID, "clientSecret": client.Secret})
	})
	each := func(fn func(*oauthGrant)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			o.mu.Lock()
			defer o.mu.Unlock()
			for _, g := range o.grants {
				if g.tenant == r.PathValue("tenant") && g.person == r.PathValue("person") && !g.ended {
					fn(g)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("POST "+oauthPrefix+"/{tenant}/people/{person}/expire", each(func(g *oauthGrant) { delete(o.access, g.access) }))
	mux.HandleFunc("POST "+oauthPrefix+"/{tenant}/people/{person}/end", each(o.end))
	mux.HandleFunc("GET "+oauthPrefix+"/{tenant}/grants", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		defer o.mu.Unlock()
		out := []map[string]any{}
		for _, g := range o.grants {
			if g.tenant == r.PathValue("tenant") {
				out = append(out, map[string]any{"person": g.person, "clientId": g.client, "refreshes": g.refreshes, "ended": g.ended})
			}
		}
		respond(w, http.StatusOK, map[string]any{"grants": out})
	})
}
