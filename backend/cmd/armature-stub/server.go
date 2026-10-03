package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/theme"
)

const (
	apiPrefix  = "/api/v1"
	stubPrefix = "/_stub"
	// maxSummary is Armature's bound on a summary.
	maxSummary = 255
	// defaultLimit and maxLimit bound a page of issues, as Armature's do.
	defaultLimit = 50
	maxLimit     = 100
	// maxBody bounds what the stub reads of a request.
	maxBody = 1 << 20
	// webhookTimeout bounds a delivery the stub sends.
	webhookTimeout = 10 * time.Second
	// uploadBytes is what /auth/me says Armature takes; nothing is uploaded.
	uploadBytes = 25 << 20
)

// route is one operation the stub serves. Path is Armature's, relative to
// /api/v1, which the contract test finds in Armature's document.
type route struct {
	Method, Path string
	// Public routes need no token.
	Public  bool
	handler func(*stub, *call)
}

// routes are the part of Armature's API Stator calls; see
// docs/api-contract-m3.md. Each issue of M3 adds what it calls.
var routes = []route{
	{Method: "GET", Path: "/openapi.json", Public: true, handler: (*stub).openAPI},
	{Method: "GET", Path: "/auth/me", handler: (*stub).me},
	{Method: "GET", Path: "/access/me", handler: (*stub).access},
	{Method: "GET", Path: "/projects", handler: (*stub).projects},
	{Method: "GET", Path: "/issue-types", handler: (*stub).issueTypes},
	{Method: "GET", Path: "/issues", handler: (*stub).search},
	{Method: "POST", Path: "/issues", handler: (*stub).createIssue},
	{Method: "GET", Path: "/issues/{issueKey}", handler: (*stub).getIssue},
	{Method: "GET", Path: "/issues/{issueKey}/remote-links", handler: (*stub).listLinks},
	{Method: "POST", Path: "/issues/{issueKey}/remote-links", handler: (*stub).putLink},
	{Method: "DELETE", Path: "/issues/{issueKey}/remote-links/{remoteLinkID}", handler: (*stub).deleteLink},
	{Method: "GET", Path: "/projects/{projectKey}/reports/{kind}", handler: (*stub).report},
	{Method: "GET", Path: "/themes/active", handler: (*stub).activeTheme},
	{Method: "GET", Path: "/themes/{themeID}/export", handler: (*stub).exportTheme},
}

type stub struct {
	world   *world
	openapi []byte
	client  *http.Client
	hooks   *bins
}

// call is one request as a person of a tenant.
type call struct {
	w      http.ResponseWriter
	r      *http.Request
	tenant *tenant
	person *person
}

func newStub(openapi []byte) *stub {
	return &stub{world: newWorld(), openapi: openapi, client: &http.Client{Timeout: webhookTimeout}, hooks: newBins()}
}

// handler routes Armature's operations and the /_stub/ controls.
func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Method+" "+apiPrefix+rt.Path, s.serve(rt))
	}
	mux.HandleFunc("GET "+stubPrefix+"/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("PATCH "+stubPrefix+"/{tenant}/issues/{issueKey}", s.control((*stub).changeIssue))
	mux.HandleFunc("POST "+stubPrefix+"/{tenant}/issues/{issueKey}/move", s.control((*stub).moveIssue))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/theme", s.control((*stub).setTheme))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/theme-delay", s.control((*stub).setThemeDelay))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/remote-links", s.control((*stub).allLinks))
	mux.HandleFunc("GET "+stubPrefix+"/{tenant}/issues/{issueKey}", s.control((*stub).heldIssue))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/people/{person}/read-only-projects", s.control((*stub).setReads))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/refused-summary", s.control((*stub).setRefused))
	mux.HandleFunc("PUT "+stubPrefix+"/{tenant}/remote-links/outage", s.control((*stub).setOutage))
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
	s.hooks.routes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not_found", "The stub does not serve this. Add it to cmd/armature-stub when Stator starts calling it.")
	})
	return mux
}

func (s *stub) serve(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{w: w, r: r}
		if !rt.Public {
			token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if rt.Path == "/themes/active" {
				if delay := s.themeDelay(strings.TrimSpace(token)); delay > 0 {
					select {
					case <-time.After(delay):
					case <-r.Context().Done():
						return
					}
				}
			}
			s.world.mu.Lock()
			defer s.world.mu.Unlock()
			t, p, ok := s.world.caller(strings.TrimSpace(token))
			if !found || !ok {
				refuse(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
				return
			}
			c.tenant, c.person = t, p
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

type apiError struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Fields   map[string]string `json:"fields,omitempty"`
	Position *int              `json:"position,omitempty"`
}

func refuse(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func refuseField(w http.ResponseWriter, field, message string) {
	respond(w, http.StatusUnprocessableEntity, map[string]apiError{"error": {Code: "validation_failed", Message: "Some fields need attention.", Fields: map[string]string{field: message}}})
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

func (c *call) readOnly() bool {
	if c.person.ReadOnly {
		refuse(c.w, http.StatusForbidden, "read_only_token", "This token can only read. Use a token without the read scope, or sign in, to make changes.")
		return true
	}
	return false
}

func (s *stub) openAPI(c *call) {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = c.w.Write(s.openapi)
}

type userView struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Timezone string    `json:"timezone"`
	Locale   string    `json:"locale"`
	IsActive bool      `json:"isActive"`
}

type orgView struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
}

func orgName(t *tenant) string { return "Stub " + t.Slug }

func roleOf(p *person) string {
	if p.Name == adminName {
		return "admin"
	}
	return "member"
}

func (s *stub) me(c *call) {
	p, t := c.person, c.tenant
	respond(c.w, http.StatusOK, map[string]any{
		"principal": map[string]any{
			"user": userView{ID: p.ID, Email: p.Email, Name: p.Display, Timezone: "UTC", Locale: "en", IsActive: true},
			"org":  orgView{ID: t.OrgID, Slug: t.Slug, Name: orgName(t)},
			"role": roleOf(p),
		},
		"organizations": []map[string]any{{"orgId": t.OrgID, "orgSlug": t.Slug, "orgName": orgName(t), "role": roleOf(p)}},
		"limits":        map[string]any{"uploadBytes": uploadBytes},
	})
}

// issuePermissions are what Armature grants a member on a project they work in.
var issuePermissions = []string{"issue.read", "issue.write", "comment.write"}

func (s *stub) access(c *call) {
	projects := map[string][]string{}
	var keys []string
	var grants []map[string]string
	for _, pr := range c.tenant.projects {
		if c.person.sees(pr) {
			projects[pr.Key] = issuePermissions
			if !c.person.writes(pr) {
				projects[pr.Key] = []string{"issue.read"}
			}
			keys = append(keys, pr.Key)
			grants = append(grants, map[string]string{"projectKey": pr.Key, "role": "developer"})
		}
	}
	org := []string{}
	if c.person.Name == adminName {
		org = []string{"org.administer"}
	}
	respond(c.w, http.StatusOK, map[string]any{
		"canAdministerOrg": c.person.Name == adminName,
		"canCreateProject": c.person.Name == adminName,
		"grants":           grants,
		"projects":         keys,
		"permissions":      map[string]any{"org": org, "projects": projects},
	})
}

func (s *stub) projects(c *call) {
	out := []map[string]any{}
	for _, pr := range c.tenant.projects {
		if !c.person.sees(pr) {
			continue
		}
		count, open := 0, 0
		for _, is := range c.tenant.issues {
			if is.Project == pr {
				count++
				if is.Status.Category != "done" {
					open++
				}
			}
		}
		out = append(out, map[string]any{
			"id": pr.ID, "key": pr.Key, "name": pr.Name, "description": "", "kind": "software",
			"issueCount": count, "openIssueCount": open, "portalVerifies": false,
			"trustedDomains": []string{}, "features": []string{"board"}, "createdAt": seeded, "updatedAt": seeded,
		})
	}
	respond(c.w, http.StatusOK, map[string]any{"projects": out})
}

func typeView(k *issueType) map[string]any {
	return map[string]any{"id": k.ID, "name": k.Name, "icon": k.Icon, "level": k.Level, "isSubtask": k.IsSubtask}
}

func (s *stub) issueTypes(c *call) {
	out := []map[string]any{}
	for _, k := range c.tenant.types {
		v := typeView(k)
		v["position"], v["description"] = k.Position, ""
		out = append(out, v)
	}
	respond(c.w, http.StatusOK, map[string]any{"issueTypes": out})
}

func personRef(p *person) any {
	if p == nil {
		return nil
	}
	return map[string]any{"id": p.ID, "name": p.Display}
}

func issueView(is *issue) map[string]any {
	v := map[string]any{
		"id": is.ID, "key": is.Key, "type": typeView(is.Type), "projectId": is.Project.ID, "projectKey": is.Project.Key,
		"summary": is.Summary, "priority": is.Priority, "timeSpentMinutes": 0,
		"status":      map[string]any{"id": is.Status.ID, "name": is.Status.Name, "category": is.Status.Category, "position": is.Status.Position},
		"labels":      []any{},
		"fixVersions": []any{}, "affectsVersions": []any{}, "components": []any{},
		"commentCount": 0, "childCount": 0,
		"createdAt": is.CreatedAt, "updatedAt": is.UpdatedAt,
	}
	if is.Assignee != nil {
		v["assignee"] = personRef(is.Assignee)
	}
	if is.Reporter != nil {
		v["reporter"] = personRef(is.Reporter)
	}
	if is.DueDate != nil {
		v["dueDate"] = is.DueDate
	}
	if is.ResolvedAt != nil {
		v["resolvedAt"] = is.ResolvedAt
	}
	return v
}

func (s *stub) search(c *call) {
	q := c.r.URL.Query()
	limit, offset := defaultLimit, 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			refuseField(c.w, "limit", "Ask for 1 to 100 issues.")
			return
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			refuseField(c.w, "offset", "Start at 0 or later.")
			return
		}
		offset = n
	}
	parsed, err := parseQuery(q.Get("q"))
	var bad *queryError
	if errors.As(err, &bad) {
		pos := bad.Pos
		respond(c.w, http.StatusBadRequest, map[string]apiError{"error": {Code: "bad_query", Message: bad.Msg, Position: &pos}})
		return
	}
	found := parsed.run(c.tenant, c.person)
	page := []map[string]any{}
	for i := offset; i < len(found) && i < offset+limit; i++ {
		page = append(page, issueView(found[i]))
	}
	respond(c.w, http.StatusOK, map[string]any{"issues": page, "total": len(found), "limit": limit, "offset": offset})
}

func (s *stub) getIssue(c *call) {
	is := c.tenant.lookup(c.person, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

type createIssueRequest struct {
	ProjectKey  string          `json:"projectKey"`
	TypeID      *uuid.UUID      `json:"typeId,omitempty"`
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description,omitempty"`
}

func (s *stub) createIssue(c *call) {
	var req createIssueRequest
	if !decode(c, &req) || c.readOnly() {
		return
	}
	pr := c.tenant.project(req.ProjectKey)
	if pr == nil || !c.person.sees(pr) {
		refuse(c.w, http.StatusNotFound, "not_found", "That project was not found.")
		return
	}
	if !c.person.writes(pr) {
		refuse(c.w, http.StatusForbidden, "forbidden", "You may not file issues in this project.")
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" || utf8.RuneCountInString(summary) > maxSummary {
		refuseField(c.w, "summary", "Give the issue a summary of 1 to 255 characters.")
		return
	}
	if c.tenant.refused != "" && summary == c.tenant.refused {
		refuseField(c.w, "summary", "Armature refuses this summary.")
		return
	}
	kind := c.tenant.typeNamed("Task")
	if req.TypeID != nil {
		if kind = c.tenant.typeByID(*req.TypeID); kind == nil || kind.IsSubtask {
			refuseField(c.w, "typeId", "Choose an issue type of this project that is not a sub-task.")
			return
		}
	}
	is := c.tenant.add(pr, kind, summary, c.person, time.Now().UTC())
	is.Description = req.Description
	respond(c.w, http.StatusCreated, map[string]any{"issue": issueView(is)})
}

func linkView(l *remoteLink) map[string]any {
	return map[string]any{
		"id": l.ID, "url": l.URL, "title": l.Title, "source": l.Source, "iconUrl": l.IconURL,
		"createdBy": l.CreatedBy, "createdAt": l.CreatedAt, "updatedAt": l.UpdatedAt,
	}
}

func (s *stub) linkedIssue(c *call) *issue {
	if c.tenant.outage > 0 {
		c.tenant.outage--
		refuse(c.w, c.tenant.outageStatus, "unavailable", "Armature is not available right now. Try again in a moment.")
		return nil
	}
	is := c.tenant.lookup(c.person, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
	}
	return is
}

// linkWritable refuses a change to an issue's links in a project the person
// may only read, as Armature asks for issue.write there.
func (s *stub) linkWritable(c *call, is *issue) bool {
	if !c.person.writes(is.Project) {
		refuse(c.w, http.StatusForbidden, "forbidden", "You may not change the links of issues in this project.")
		return false
	}
	return true
}

func (s *stub) listLinks(c *call) {
	is := s.linkedIssue(c)
	if is == nil {
		return
	}
	out := []map[string]any{}
	for _, l := range c.tenant.links {
		if l.IssueID == is.ID {
			out = append(out, linkView(l))
		}
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}

type remoteLinkRequest struct {
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Source  string  `json:"source"`
	IconURL *string `json:"iconUrl,omitempty"`
}

// putLink puts a link on an issue, or retitles the one with the same url, as
// Armature does, so sending the same link again is harmless.
func (s *stub) putLink(c *call) {
	var req remoteLinkRequest
	if !decode(c, &req) || c.readOnly() {
		return
	}
	is := s.linkedIssue(c)
	if is == nil || !s.linkWritable(c, is) {
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		refuseField(c.w, "url", "Link to an http or https address.")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		refuseField(c.w, "title", "Give the link a title.")
		return
	}
	now := time.Now().UTC()
	for _, l := range c.tenant.links {
		if l.IssueID == is.ID && l.URL == req.URL {
			l.Title, l.Source, l.IconURL, l.UpdatedAt = req.Title, req.Source, req.IconURL, now
			respond(c.w, http.StatusOK, map[string]any{"remoteLink": linkView(l)})
			return
		}
	}
	l := &remoteLink{ID: uuid.New(), IssueID: is.ID, URL: req.URL, Title: req.Title, Source: req.Source, IconURL: req.IconURL,
		CreatedBy: c.person.ID, CreatedAt: now, UpdatedAt: now}
	c.tenant.links = append(c.tenant.links, l)
	respond(c.w, http.StatusCreated, map[string]any{"remoteLink": linkView(l)})
}

func (s *stub) deleteLink(c *call) {
	if c.readOnly() {
		return
	}
	is := s.linkedIssue(c)
	if is == nil || !s.linkWritable(c, is) {
		return
	}
	id, err := uuid.Parse(c.r.PathValue("remoteLinkID"))
	i := slices.IndexFunc(c.tenant.links, func(l *remoteLink) bool { return err == nil && l.ID == id && l.IssueID == is.ID })
	if i < 0 {
		refuse(c.w, http.StatusNotFound, "not_found", "That link was not found.")
		return
	}
	c.tenant.links = slices.Delete(c.tenant.links, i, i+1)
	c.w.WriteHeader(http.StatusNoContent)
}

func themeView(t *tenant, p *person, e *theme.Example) map[string]any {
	return map[string]any{
		"id": themeID(t, p, e.Key), "ownerId": p.ID, "ownerName": p.Display, "name": e.Name, "shared": false,
		"spec": e.Spec, "assets": []any{}, "inUse": 1, "active": true, "default": false,
		"createdAt": seeded, "updatedAt": p.ThemeChanged,
	}
}

func (s *stub) activeTheme(c *call) {
	e := theme.ExampleByKey(c.person.Theme)
	if e == nil {
		respond(c.w, http.StatusOK, map[string]any{"theme": nil, "source": ""})
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"theme": themeView(c.tenant, c.person, e), "source": "chosen"})
}

func (s *stub) exportTheme(c *call) {
	e := theme.ExampleByKey(c.person.Theme)
	if e == nil || c.r.PathValue("themeID") != themeID(c.tenant, c.person, e.Key).String() {
		refuse(c.w, http.StatusNotFound, "not_found", "That theme was not found.")
		return
	}
	spec := e.Spec
	if c.person.ThemeBroken {
		light := map[string]string{}
		for name := range spec.Colors.Light {
			light[name] = "not a colour"
		}
		spec.Colors.Light = light
	}
	pkg := theme.Package{Format: theme.PackageFormat, Name: e.Name, Spec: spec, Assets: []theme.PackagedAsset{}}
	c.w.Header().Set("Content-Disposition", `attachment; filename="`+e.Key+`.armature-theme.json"`)
	respond(c.w, http.StatusOK, pkg)
}

// The controls below are the stub's own, never Armature's: tests change the
// world through them and read what Stator did to it.

type issueChange struct {
	Summary        *string `json:"summary,omitempty"`
	StatusCategory *string `json:"statusCategory,omitempty"`
	Priority       *string `json:"priority,omitempty"`
	// Assignee is a person's name, or empty for nobody.
	Assignee *string `json:"assignee,omitempty"`
}

func (s *stub) changeIssue(c *call) {
	var req issueChange
	if !decode(c, &req) {
		return
	}
	is := c.tenant.lookup(c.tenant.person(adminName), c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	if req.Summary != nil {
		is.Summary = *req.Summary
	}
	if req.StatusCategory != nil {
		if st := c.tenant.statusOf(*req.StatusCategory); st != nil {
			is.Status = st
			// Done is resolved, now; anything else is open again.
			is.ResolvedAt = nil
			if st.Category == "done" {
				now := time.Now().UTC()
				is.ResolvedAt = &now
			}
		}
	}
	if req.Priority != nil {
		if _, known := priorityRank[*req.Priority]; known {
			is.Priority = *req.Priority
		}
	}
	if req.Assignee != nil {
		is.Assignee = nil
		if *req.Assignee != "" {
			is.Assignee = c.tenant.person(*req.Assignee)
		}
	}
	is.UpdatedAt = time.Now().UTC()
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

func (s *stub) moveIssue(c *call) {
	var req struct {
		ProjectKey string `json:"projectKey"`
	}
	if !decode(c, &req) {
		return
	}
	is := c.tenant.lookup(c.tenant.person(adminName), c.r.PathValue("issueKey"))
	to := c.tenant.project(req.ProjectKey)
	if is == nil || to == nil || to == is.Project {
		refuse(c.w, http.StatusNotFound, "not_found", "Name an issue and another project to move it to.")
		return
	}
	c.tenant.move(is, to, time.Now().UTC())
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

func (s *stub) setTheme(c *call) {
	var req struct {
		// Theme is the key of an example theme, or empty for Armature's built-in one.
		Theme string `json:"theme"`
		// Broken exports it with colours no theme may hold, as a theme from a
		// later Armature might.
		Broken bool `json:"broken"`
	}
	if !decode(c, &req) {
		return
	}
	if req.Theme != "" && theme.ExampleByKey(req.Theme) == nil {
		refuseField(c.w, "theme", "Name one of Stator's example themes, or leave it empty.")
		return
	}
	p := c.tenant.person(c.r.PathValue("person"))
	p.Theme, p.ThemeBroken, p.ThemeChanged = req.Theme, req.Broken, time.Now().UTC()
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) setThemeDelay(c *call) {
	var req struct {
		// MS holds back every answer to the person's GET /themes/active.
		MS int `json:"ms"`
	}
	if !decode(c, &req) {
		return
	}
	if req.MS < 0 || time.Duration(req.MS)*time.Millisecond > webhookTimeout {
		refuseField(c.w, "ms", "Give a delay of 0 to 10000 milliseconds.")
		return
	}
	c.tenant.person(c.r.PathValue("person")).ThemeDelay = time.Duration(req.MS) * time.Millisecond
	c.w.WriteHeader(http.StatusNoContent)
}

// themeDelay is how long the caller's GET /themes/active is held back. It is
// waited out without the world's lock, so nothing else is held up meanwhile.
func (s *stub) themeDelay(token string) time.Duration {
	s.world.mu.Lock()
	defer s.world.mu.Unlock()
	if _, p, ok := s.world.caller(token); ok {
		return p.ThemeDelay
	}
	return 0
}

// heldIssue is an issue as the stub holds it, with the description it was
// filed with, whoever may see it.
func (s *stub) heldIssue(c *call) {
	is := c.tenant.lookup(c.tenant.person(adminName), c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	v := issueView(is)
	v["description"] = is.Description
	respond(c.w, http.StatusOK, map[string]any{"issue": v})
}

func (s *stub) setReads(c *call) {
	var req struct {
		// Projects are the keys of the projects the person may only read.
		Projects []string `json:"projects"`
	}
	if !decode(c, &req) {
		return
	}
	p := c.tenant.person(c.r.PathValue("person"))
	p.Reads = map[string]bool{}
	for _, key := range req.Projects {
		p.Reads[key] = true
	}
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) setRefused(c *call) {
	var req struct {
		// Summary is refused by every create from now on; empty refuses none.
		Summary string `json:"summary"`
	}
	if !decode(c, &req) {
		return
	}
	c.tenant.refused = req.Summary
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) setOutage(c *call) {
	var req struct {
		// Status answers the next Count remote link calls, a 5xx or a 429.
		Status int `json:"status"`
		Count  int `json:"count"`
	}
	if !decode(c, &req) {
		return
	}
	if req.Count > 0 && req.Status != http.StatusTooManyRequests && (req.Status < 500 || req.Status > 599) {
		refuseField(c.w, "status", "Name a 5xx status or 429.")
		return
	}
	c.tenant.outage, c.tenant.outageStatus = max(req.Count, 0), req.Status
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) allLinks(c *call) {
	out := []map[string]any{}
	byID := map[uuid.UUID]*issue{}
	for _, is := range c.tenant.issues {
		byID[is.ID] = is
	}
	names := map[uuid.UUID]string{}
	for _, p := range c.tenant.people {
		names[p.ID] = p.Display
	}
	for _, l := range c.tenant.links {
		v := linkView(l)
		if is := byID[l.IssueID]; is != nil {
			v["issueKey"] = is.Key
		}
		v["createdByName"] = names[l.CreatedBy]
		out = append(out, v)
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}

type webhookRequest struct {
	URL     string          `json:"url"`
	Secret  string          `json:"secret"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
	// ID repeats an earlier delivery's event when set, as a redelivery does.
	ID *uuid.UUID `json:"id,omitempty"`
}

// sign is Armature's webhook.Sign: sha256= and the hex HMAC of the body.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendWebhook posts one event exactly as Armature's webhook service does,
// and answers what the receiver said.
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
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	body, err := json.Marshal(map[string]any{
		"id": id, "topic": req.Topic, "orgId": c.tenant.OrgID, "occurredAt": time.Now().UTC(), "payload": payload,
	})
	if err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.r.Context(), webhookTimeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(body))
	if err != nil {
		refuseField(c.w, "url", "Name the address to deliver to.")
		return
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("User-Agent", "Armature-Webhook")
	out.Header.Set("X-Armature-Event", req.Topic)
	out.Header.Set("X-Armature-Delivery", uuid.NewString())
	out.Header.Set("X-Armature-Signature-256", sign(body, req.Secret))
	resp, err := s.client.Do(out)
	if err != nil {
		refuse(c.w, http.StatusBadGateway, "delivery_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	respond(c.w, http.StatusOK, map[string]any{"id": id, "status": resp.StatusCode})
}
