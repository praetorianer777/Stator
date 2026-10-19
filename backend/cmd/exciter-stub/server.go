package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	apiPrefix  = "/api/v1"
	stubPrefix = "/_stub"
	// metaPrefix is where a suite tells the stub how /meta answers by host,
	// apart from the /_stub/{tenant} controls, since /meta knows no tenant.
	metaPrefix = "/_stub-meta"
	// maxBody bounds what the stub reads of a request.
	maxBody = 1 << 20
	// webhookTimeout bounds a delivery the stub sends.
	webhookTimeout = 10 * time.Second
	// stubVersion is the build the stub names at /meta.
	stubVersion = "0.0.0+stub"

	tokenPrefix = "exciter_pat_"
	// revokedToken is always refused, as a token revoked in Exciter is.
	revokedToken = "exciter_pat_revoked"
	// adminName may still find a trashed requirement in the trash; to
	// everybody else it is not found, as Exciter answers.
	adminName = "alice"
)

// exciterCapabilities are what Exciter reports at /meta on the main branch
// api/exciter/SOURCE pins (backend/internal/httpapi/capabilities.go there).
var exciterCapabilities = []string{
	"mcp@1", "requirement-embed@1", "requirement-list@1", "specifications@1", "theme-export@1", "trace-links@1", "webhooks@1", "webhooks@2",
}

// route is one operation the stub serves. Path is Exciter's, relative to
// /api/v1, which the contract test finds in Exciter's document.
type route struct {
	Method, Path string
	// Public routes need no token.
	Public  bool
	handler func(*stub, *call)
}

// routes are the part of Exciter's API Stator calls.
var routes = []route{
	{Method: "GET", Path: "/openapi.json", Public: true, handler: (*stub).openAPI},
	{Method: "GET", Path: "/meta", Public: true, handler: (*stub).meta},
	{Method: "GET", Path: "/auth/me", handler: (*stub).me},
	{Method: "GET", Path: "/requirements/{reqKey}", handler: (*stub).getRequirement},
	{Method: "GET", Path: "/search/quick", handler: (*stub).quickSearch},
	{Method: "GET", Path: "/projects", handler: (*stub).listProjects},
	{Method: "GET", Path: "/projects/{projectKey}/requirements", handler: (*stub).listRequirements},
	{Method: "GET", Path: "/projects/{projectKey}/requirements/suggest", handler: (*stub).suggest},
}

// namespace makes the stub's ids from names, so they are the same on every run.
var namespace = uuid.MustParse("1c0f6a52-6f8e-4b0d-9e1b-3c7a2d5e8f40")

func idOf(parts ...string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(strings.Join(parts, "/")))
}

// seeded is when the fixed requirements were made and last changed.
var seeded = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

type status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type requirement struct {
	ID         uuid.UUID
	Key        string
	Title      string
	Status     *status
	Revision   int
	FormerKeys []string
	// HiddenFrom are the people Exciter answers 404 for it.
	HiddenFrom map[string]bool
	TrashedAt  *time.Time
	// Owner is the person currentUser() finds it for, empty for nobody.
	Owner string
}

func (r *requirement) project() string { return r.Key[:strings.LastIndex(r.Key, "-R")] }

func (r *requirement) number() int {
	n, _ := strconv.Atoi(r.Key[strings.LastIndex(r.Key, "-R")+2:])
	return n
}

// visibleTo says whether person finds it, as Exciter decides for a reader.
func (r *requirement) visibleTo(person string) bool {
	if r.HiddenFrom[person] {
		return false
	}
	return r.TrashedAt == nil || person == adminName
}

// tenant is one suite's pretend Exciter, made on first use with the same
// requirements, so every test starts from the same world.
type tenant struct {
	Name         string
	requirements []*requirement
}

func newTenant(name string) *tenant {
	trashed := seeded.Add(48 * time.Hour)
	return &tenant{Name: name, requirements: []*requirement{
		{ID: idOf(name, "CHK-R1"), Key: "CHK-R1", Title: "The basket keeps its items for 30 days", Status: &status{"Approved", "approved"}, Revision: 3, Owner: adminName},
		{ID: idOf(name, "CHK-R2"), Key: "CHK-R2", Title: "Payment providers are tried in the order set", Status: &status{"In review", "review"}, Revision: 1,
			HiddenFrom: map[string]bool{"bob": true}, Owner: adminName},
		{ID: idOf(name, "CHK-R3"), Key: "CHK-R3", Title: "Gift wrapping is offered at checkout", Status: &status{"Draft", "draft"}, Revision: 1, TrashedAt: &trashed},
		{ID: idOf(name, "CHK-R4"), Key: "CHK-R4", Title: "An order is confirmed by mail", Status: &status{"Draft", "draft"}, Revision: 2, FormerKeys: []string{"OLD-R7"}, Owner: "bob"},
		{ID: idOf(name, "SEC-R1"), Key: "SEC-R1", Title: "A session ends after 15 minutes idle", Status: &status{"Approved", "approved"}, Revision: 1,
			HiddenFrom: map[string]bool{"bob": true}, Owner: adminName},
	}}
}

// find is the requirement a key or a key it had names.
func (t *tenant) find(key string) *requirement {
	for _, r := range t.requirements {
		if strings.EqualFold(r.Key, key) {
			return r
		}
	}
	for _, r := range t.requirements {
		for _, former := range r.FormerKeys {
			if strings.EqualFold(former, key) {
				return r
			}
		}
	}
	return nil
}

type world struct {
	mu      sync.Mutex
	tenants map[string]*tenant
}

func (w *world) tenant(name string) *tenant {
	t, ok := w.tenants[name]
	if !ok {
		t = newTenant(name)
		w.tenants[name] = t
	}
	return t
}

// caller is the tenant and person a token names: exciter_pat_<tenant>_<person>.
func (w *world) caller(token string) (*tenant, string, bool) {
	rest, ok := strings.CutPrefix(token, tokenPrefix)
	at := strings.LastIndex(rest, "_")
	if !ok || token == revokedToken || at <= 0 || at == len(rest)-1 {
		return nil, "", false
	}
	return w.tenant(rest[:at]), rest[at+1:], true
}

type metaOverride struct {
	Capabilities []string `json:"capabilities"`
}

type stub struct {
	world   *world
	openapi []byte
	client  *http.Client
	mu      sync.Mutex
	metas   map[string]metaOverride
}

// call is one request as a person of a tenant.
type call struct {
	w      http.ResponseWriter
	r      *http.Request
	tenant *tenant
	person string
}

func newStub(openapi []byte) *stub {
	return &stub{world: &world{tenants: map[string]*tenant{}}, openapi: openapi,
		client: &http.Client{Timeout: webhookTimeout}, metas: map[string]metaOverride{}}
}

// handler routes Exciter's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("PUT "+metaPrefix+"/{host}", s.setMeta)
	mux.HandleFunc("DELETE "+metaPrefix+"/{host}", s.forgetMeta)
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/requirements/{reqKey}", s.control((*stub).putRequirement))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/requirements/{reqKey}/move", s.control((*stub).moveRequirement))
	// Unlocked while it posts, so a receiver that calls back finds the stub free.
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/webhooks", func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		t := s.world.tenant(r.PathValue("tenant"))
		s.world.mu.Unlock()
		s.sendWebhook(&call{w: w, r: r, tenant: t})
	})
	mux.HandleFunc("DELETE "+stubPrefix+"/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		delete(s.world.tenants, r.PathValue("tenant"))
		s.world.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not_found", "The stub does not serve this. Add it to cmd/exciter-stub when Stator starts calling it.")
	})
	return mux
}

func (s *stub) serve(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{w: w, r: r}
		if !rt.Public {
			token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			s.world.mu.Lock()
			defer s.world.mu.Unlock()
			t, person, ok := s.world.caller(strings.TrimSpace(token))
			if !found || !ok {
				refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
				return
			}
			c.tenant, c.person = t, person
		}
		rt.handler(s, c)
	}
}

func (s *stub) control(fn func(*stub, *call)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.world.mu.Lock()
		defer s.world.mu.Unlock()
		fn(s, &call{w: w, r: r, tenant: s.world.tenant(r.PathValue("tenant"))})
	}
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func refuse(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func decode(c *call, into any) bool {
	dec := json.NewDecoder(io.LimitReader(c.r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON: "+err.Error()+".")
		return false
	}
	return true
}

func (s *stub) openAPI(c *call) {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = c.w.Write(s.openapi)
}

func (s *stub) meta(c *call) {
	capabilities := exciterCapabilities
	s.mu.Lock()
	if o, ok := s.metas[strings.ToLower(c.r.Host)]; ok {
		capabilities = o.Capabilities
	}
	s.mu.Unlock()
	if capabilities == nil {
		capabilities = []string{}
	}
	respond(c.w, http.StatusOK, map[string]any{
		"product": "exciter", "build": map[string]any{"version": stubVersion}, "apiVersion": "1", "capabilities": capabilities,
	})
}

// setMeta is PUT /_stub-meta/{host}: what /meta answers when reached by host.
func (s *stub) setMeta(w http.ResponseWriter, r *http.Request) {
	var o metaOverride
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&o); err != nil {
		refuse(w, http.StatusBadRequest, "bad_request", "Send the capabilities to report.")
		return
	}
	s.mu.Lock()
	s.metas[strings.ToLower(r.PathValue("host"))] = o
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) forgetMeta(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	delete(s.metas, strings.ToLower(r.PathValue("host")))
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func display(person string) string {
	if person == "" {
		return person
	}
	runes := []rune(person)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func (s *stub) me(c *call) {
	role := "member"
	if c.person == adminName {
		role = "admin"
	}
	respond(c.w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": idOf(c.tenant.Name, "person", c.person), "email": c.person + "@suite.test", "name": display(c.person), "locale": "en",
		},
		"installation": map[string]any{"name": "Exciter " + c.tenant.Name, "oauthConnections": false},
		"role":         role,
		"token":        nil,
		"signsWith":    "password",
	})
}

// requirementView is a requirement in Exciter's Requirement shape.
func requirementView(t *tenant, r *requirement) map[string]any {
	var st any
	if r.Status != nil {
		st = map[string]any{"id": idOf(t.Name, "status", r.Status.Name), "name": r.Status.Name, "category": r.Status.Category}
	}
	former := r.FormerKeys
	if former == nil {
		former = []string{}
	}
	return map[string]any{
		"id": r.ID, "key": r.Key, "projectKey": r.project(), "number": r.number(), "kind": "requirement",
		"title": r.Title, "statement": statementOf(r), "rationale": rationaleOf(r), "type": nil,
		"attributes": map[string]any{}, "computed": map[string]any{}, "testCase": nil, "risk": nil,
		"revision": r.Revision, "status": st, "owner": ownerOf(t, r), "priority": nil, "labels": []string{}, "parentId": nil,
		"outline": strconv.Itoa(r.number()), "formerKeys": former, "copiedFrom": nil, "reuse": nil,
		"createdAt": seeded, "createdByName": "Alice", "updatedAt": seeded, "updatedByName": "Alice", "trashedAt": r.TrashedAt,
		"can":      map[string]any{"edit": false, "transition": false, "delete": false, "link": false, "comment": true},
		"comments": map[string]any{"threads": 0, "open": 0},
		"watching": map[string]any{"requirement": false, "project": false},
		"approval": map[string]any{"pendingReviewId": nil, "approvedRevision": nil, "current": false},
		"lock":     nil,
		"quality":  map[string]any{"score": nil, "active": 0, "accepted": 0, "stale": false},
	}
}

func (s *stub) getRequirement(c *call) {
	r := c.tenant.find(c.r.PathValue("reqKey"))
	if r == nil || !r.visibleTo(c.person) {
		refuse(c.w, http.StatusNotFound, "not_found", "That requirement was not found.")
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"requirement": requirementView(c.tenant, r)})
}

// putRequirement is PUT /_stub/{tenant}/requirements/{reqKey}: a requirement
// made or replaced by a suite.
func (s *stub) putRequirement(c *call) {
	var req struct {
		Title      string   `json:"title"`
		Status     *status  `json:"status"`
		Revision   int      `json:"revision"`
		FormerKeys []string `json:"formerKeys"`
		HiddenFrom []string `json:"hiddenFrom"`
		Trashed    bool     `json:"trashed"`
	}
	if !decode(c, &req) {
		return
	}
	key := strings.ToUpper(c.r.PathValue("reqKey"))
	if !strings.Contains(key, "-R") || req.Title == "" {
		refuse(c.w, http.StatusBadRequest, "bad_request", "Name a key such as CHK-R9 and send a title.")
		return
	}
	r := &requirement{ID: idOf(c.tenant.Name, key), Key: key, Title: req.Title, Status: req.Status, Revision: max(req.Revision, 1),
		FormerKeys: req.FormerKeys, HiddenFrom: map[string]bool{}}
	for _, p := range req.HiddenFrom {
		r.HiddenFrom[p] = true
	}
	if req.Trashed {
		at := time.Now().UTC()
		r.TrashedAt = &at
	}
	for i, old := range c.tenant.requirements {
		if old.Key == key {
			// What a suite leaves out stays as it was, as a revision in Exciter keeps it.
			r.ID = old.ID
			if req.Status == nil {
				r.Status = old.Status
			}
			if req.FormerKeys == nil {
				r.FormerKeys = old.FormerKeys
			}
			if req.HiddenFrom == nil {
				r.HiddenFrom = old.HiddenFrom
			}
			c.tenant.requirements[i] = r
			respond(c.w, http.StatusOK, map[string]any{"requirement": requirementView(c.tenant, r)})
			return
		}
	}
	c.tenant.requirements = append(c.tenant.requirements, r)
	respond(c.w, http.StatusOK, map[string]any{"requirement": requirementView(c.tenant, r)})
}

// moveRequirement is POST /_stub/{tenant}/requirements/{reqKey}/move: the
// requirement takes the key to, and keeps its old one among its former keys.
func (s *stub) moveRequirement(c *call) {
	var req struct {
		To string `json:"to"`
	}
	if !decode(c, &req) {
		return
	}
	r := c.tenant.find(c.r.PathValue("reqKey"))
	if r == nil || !strings.Contains(req.To, "-R") {
		refuse(c.w, http.StatusBadRequest, "bad_request", "Name a requirement the tenant has and the key it takes.")
		return
	}
	r.FormerKeys = append(r.FormerKeys, r.Key)
	r.Key = strings.ToUpper(req.To)
	respond(c.w, http.StatusOK, map[string]any{"requirement": requirementView(c.tenant, r)})
}

// webhookRequest is what a suite asks the stub to deliver: a topic about
// the requirement key names, or a payload of its own.
type webhookRequest struct {
	URL     string          `json:"url"`
	Secret  string          `json:"secret"`
	Topic   string          `json:"topic"`
	Key     string          `json:"key,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	ID      *uuid.UUID      `json:"id,omitempty"`
	// Version and Source, when set, make a delivery Stator should refuse or
	// ignore.
	Version int    `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	// SentAt signs another time than now, for a replay.
	SentAt *time.Time `json:"sentAt,omitempty"`
}

// sign is Exciter's: sha256= and the hex HMAC-SHA256 of what is signed.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// payloadFor is the payload Exciter posts about a requirement: a reference
// read as the webhook's owner, which the stub takes to be adminName.
func payloadFor(t *tenant, base string, r *requirement) map[string]any {
	var st any
	if r.Status != nil {
		st = map[string]any{"name": r.Status.Name, "category": r.Status.Category}
	}
	return map[string]any{
		"requirement": map[string]any{
			"id": r.ID, "key": r.Key, "kind": "requirement", "title": r.Title, "revision": r.Revision, "status": st,
			"project": map[string]any{"key": r.project(), "name": r.project()},
			"url":     base + "/p/" + url.PathEscape(r.project()) + "/r/" + url.PathEscape(r.Key), "trashed": r.TrashedAt != nil,
		},
		"revision": r.Revision,
		"actor":    map[string]any{"id": idOf(t.Name, "person", adminName), "name": display(adminName)},
	}
}

func (s *stub) sendWebhook(c *call) {
	var req webhookRequest
	if !decode(c, &req) {
		return
	}
	id := uuid.New()
	if req.ID != nil {
		id = *req.ID
	}
	payload := req.Payload
	if req.Key != "" {
		s.world.mu.Lock()
		r := c.tenant.find(req.Key)
		var made map[string]any
		if r != nil {
			made = payloadFor(c.tenant, "http://"+c.r.Host, r)
		}
		s.world.mu.Unlock()
		if made == nil {
			refuse(c.w, http.StatusBadRequest, "bad_request", "The tenant has no requirement "+req.Key+".")
			return
		}
		payload, _ = json.Marshal(made)
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{"message":"ping"}`)
	}
	version := cmp.Or(req.Version, 1)
	body, err := json.Marshal(map[string]any{
		"version": version, "id": id, "source": cmp.Or(req.Source, "exciter"), "topic": req.Topic, "occurredAt": time.Now().UTC(), "payload": payload,
	})
	if err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.r.Context(), webhookTimeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(body))
	if err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", "Name the address to deliver to.")
		return
	}
	sentAt := time.Now()
	if req.SentAt != nil {
		sentAt = *req.SentAt
	}
	sent := strconv.FormatInt(sentAt.Unix(), 10)
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("User-Agent", "Exciter-Webhook")
	out.Header.Set("X-Exciter-Event", req.Topic)
	out.Header.Set("X-Exciter-Delivery", uuid.NewString())
	out.Header.Set("X-Exciter-Webhook-Version", strconv.Itoa(version))
	out.Header.Set("X-Exciter-Signature-256", sign(body, req.Secret))
	out.Header.Set("X-Exciter-Timestamp", sent)
	out.Header.Set("X-Exciter-Signature-Timestamped-256", sign(append([]byte(sent+"."), body...), req.Secret))
	resp, err := s.client.Do(out)
	if err != nil {
		refuse(c.w, http.StatusBadGateway, "delivery_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	respond(c.w, http.StatusOK, map[string]any{"id": id, "status": resp.StatusCode, "body": json.RawMessage(body)})
}
