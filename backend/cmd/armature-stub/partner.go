package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A connected app's own webhook (docs/oauth.md, "The partner's own
// webhook"): one per app, managed with an access token whose grant holds
// webhooks.partner, of the administrator. The outbound guard is told by a
// suite which hosts it refuses, as Armature's would refuse its own network.

const (
	webhookSecretOf = "armature_whs_"
	topicAny        = "*"
	topicPing       = "ping"
)

// partnerTopics are the events the stub's catalog lists, as Armature's does.
var partnerTopics = []string{"issue.created", "issue.updated", "issue.deleted", "issue.transitioned",
	"issue.remote_link.added", "issue.remote_link.removed", "comment.added"}

type partnerHook struct {
	ID              uuid.UUID
	Name, URL       string
	Secret          string
	Topics          []string
	Version         int
	Enabled         bool
	Created, Update time.Time
	// tenant is whose events it takes, the administrator's who made it.
	tenant uuid.UUID
}

func (h *partnerHook) view(client *oauthClient, withSecret bool) map[string]any {
	out := map[string]any{
		"id": h.ID, "name": h.Name, "url": h.URL, "topics": h.Topics, "enabled": h.Enabled, "version": h.Version,
		"createdAt": h.Created, "updatedAt": h.Update, "partner": map[string]any{"clientId": client.ID, "name": client.Name},
	}
	if withSecret {
		out["secret"] = h.Secret
	}
	return out
}

// within says whether every scope asked is one the app registered.
func within(asked, registered string) bool {
	may := strings.Fields(registered)
	for _, scope := range strings.Fields(asked) {
		if !slices.Contains(may, scope) {
			return false
		}
	}
	return true
}

// partnerApp is the app a partner webhook call acts for, or a refusal as
// Armature's requirePartner answers it.
func (s *stub) partnerApp(c *call) *oauthClient {
	token := strings.TrimSpace(strings.TrimPrefix(c.r.Header.Get("Authorization"), "Bearer "))
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	g, ok := s.oauth.access[token]
	if !ok || g.ended {
		refuse(c.w, http.StatusForbidden, "forbidden", "Only a connected app manages its own webhook here. Manage webhooks under Settings, Webhooks.")
		return nil
	}
	client := s.oauth.clients[g.client]
	switch {
	case !slices.Contains(strings.Fields(g.scope), webhooksScope):
		respond(c.w, http.StatusForbidden, map[string]apiError{"error": {Code: "insufficient_scope",
			Message: client.Name + " was not allowed to manage its webhook. An Armature administrator connects it again."}})
		return nil
	case c.person.Name != adminName:
		refuse(c.w, http.StatusForbidden, "forbidden", c.person.Display+" is no longer an Armature administrator. An administrator connects "+client.Name+" again to manage its webhook.")
		return nil
	}
	return client
}

type partnerBody struct {
	URL     *string   `json:"url"`
	Topics  *[]string `json:"topics"`
	Version *int      `json:"version"`
	Enabled *bool     `json:"enabled"`
}

// judge holds an address and topics to what Armature takes, answering
// whether it refused them.
func (s *stub) judge(c *call, client *oauthClient, address string, topics []string, checkAddress bool) bool {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		refuseField(c.w, "url", "Enter an http or https address.")
		return true
	}
	if len(topics) == 0 {
		refuseField(c.w, "topics", "Name the topics "+client.Name+" takes, such as issue.updated.")
		return true
	}
	for _, t := range topics {
		if t == topicAny {
			refuseField(c.w, "topics", "Name the topics "+client.Name+" takes; a connected app cannot take every event.")
			return true
		}
		if !slices.Contains(partnerTopics, t) {
			refuseField(c.w, "topics", t+" is not an event a webhook can take. GET /automation/catalog lists them.")
			return true
		}
	}
	if !checkAddress {
		return false
	}
	host := strings.ToLower(u.Hostname())
	s.oauth.mu.Lock()
	how := s.oauth.outbound[strings.ToLower(c.r.Host)]
	s.oauth.mu.Unlock()
	switch how {
	case "outbound_refused":
		allow := "Add " + host + " to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)"
		respond(c.w, http.StatusUnprocessableEntity, map[string]apiError{"error": {Code: "outbound_refused",
			Message: "Armature may not reach " + host + ", which is inside its own network. " + allow + " and try again.",
			Fields:  map[string]string{"url": allow + "."}}})
		return true
	case "invalid":
		sentence := host + " does not resolve from Armature. Check the address, or Armature's DNS."
		respond(c.w, http.StatusUnprocessableEntity, map[string]apiError{"error": {Code: "invalid", Message: sentence, Fields: map[string]string{"url": sentence}}})
		return true
	}
	return false
}

func sortedTopics(topics []string) []string {
	out := slices.Clone(topics)
	slices.Sort(out)
	return slices.Compact(out)
}

func (s *stub) getPartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	if h := s.oauth.hooks[client.ID]; h != nil {
		respond(c.w, http.StatusOK, map[string]any{"webhook": h.view(client, false)})
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"webhook": nil})
}

func (s *stub) createPartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	var req partnerBody
	if !decode(c, &req) {
		return
	}
	if req.URL == nil || req.Topics == nil {
		refuseField(c.w, "url", "Name the address and the topics.")
		return
	}
	s.oauth.mu.Lock()
	_, exists := s.oauth.hooks[client.ID]
	s.oauth.mu.Unlock()
	if exists {
		refuse(c.w, http.StatusConflict, "partner_webhook_exists", client.Name+" has a webhook here already. Change it with PATCH /webhooks/partner, or remove it first.")
		return
	}
	if s.judge(c, client, *req.URL, *req.Topics, true) {
		return
	}
	now := time.Now().UTC()
	h := &partnerHook{ID: uuid.New(), Name: client.Name + " (connected app)", URL: *req.URL, Secret: secretWith(webhookSecretOf),
		Topics: sortedTopics(*req.Topics), Version: envelopeVersion, Enabled: true, Created: now, Update: now, tenant: c.tenant.OrgID}
	if req.Version != nil {
		h.Version = *req.Version
	}
	s.oauth.mu.Lock()
	s.oauth.hooks[client.ID] = h
	s.oauth.mu.Unlock()
	respond(c.w, http.StatusCreated, map[string]any{"webhook": h.view(client, true)})
}

func (s *stub) updatePartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	var req partnerBody
	if !decode(c, &req) {
		return
	}
	s.oauth.mu.Lock()
	h := s.oauth.hooks[client.ID]
	s.oauth.mu.Unlock()
	if h == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That webhook was not found.")
		return
	}
	address, topics := h.URL, h.Topics
	if req.URL != nil {
		address = *req.URL
	}
	if req.Topics != nil {
		topics = *req.Topics
	}
	if s.judge(c, client, address, topics, address != h.URL) {
		return
	}
	s.oauth.mu.Lock()
	h.URL, h.Topics, h.Update = address, sortedTopics(topics), time.Now().UTC()
	if req.Version != nil {
		h.Version = *req.Version
	}
	if req.Enabled != nil {
		h.Enabled = *req.Enabled
	}
	s.oauth.mu.Unlock()
	respond(c.w, http.StatusOK, map[string]any{"webhook": h.view(client, false)})
}

func (s *stub) deletePartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	s.oauth.mu.Lock()
	delete(s.oauth.hooks, client.ID)
	s.oauth.mu.Unlock()
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) rotatePartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	h := s.oauth.hooks[client.ID]
	if h == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That webhook was not found.")
		return
	}
	h.Secret, h.Update = secretWith(webhookSecretOf), time.Now().UTC()
	respond(c.w, http.StatusOK, map[string]any{"webhook": h.view(client, true)})
}

// testPartnerWebhook posts a ping now, as Armature's Test does, and answers
// the attempt as logged.
func (s *stub) testPartnerWebhook(c *call) {
	client := s.partnerApp(c)
	if client == nil {
		return
	}
	s.oauth.mu.Lock()
	h := s.oauth.hooks[client.ID]
	var hook partnerHook
	if h != nil {
		hook = *h
	}
	s.oauth.mu.Unlock()
	if h == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That webhook was not found.")
		return
	}
	created, event := time.Now().UTC(), uuid.New()
	delivery := map[string]any{"id": uuid.New(), "endpointId": hook.ID, "eventId": event, "topic": topicPing, "attempt": 1, "createdAt": created}
	status, err := s.deliver(c.r.Context(), webhookRequest{URL: hook.URL, Secret: hook.Secret, Topic: topicPing, Version: hook.Version,
		Payload: json.RawMessage(`{"message":"Armature can reach this address."}`)}, event, hook.tenant)
	if err != nil {
		delivery["error"] = err.Error()
	} else {
		delivery["status"] = status
		delivery["deliveredAt"] = time.Now().UTC()
	}
	respond(c.w, http.StatusOK, map[string]any{"delivery": delivery})
}

// partnerControls let a suite see each app's webhook, its secret included,
// have the outbound guard of the Armature reached by a host refuse every
// address, and drop a webhook as an Armature
// administrator removing it under Settings, Webhooks would.
func (s *stub) partnerControls(mux *http.ServeMux) {
	mux.HandleFunc("GET "+oauthPrefix+"/webhooks", func(w http.ResponseWriter, _ *http.Request) {
		s.oauth.mu.Lock()
		defer s.oauth.mu.Unlock()
		out := []map[string]any{}
		for id, h := range s.oauth.hooks {
			v := h.view(s.oauth.clients[id], true)
			v["clientId"] = id
			out = append(out, v)
		}
		respond(w, http.StatusOK, map[string]any{"webhooks": out})
	})
	mux.HandleFunc("DELETE "+oauthPrefix+"/webhooks/{clientID}", func(w http.ResponseWriter, r *http.Request) {
		s.oauth.mu.Lock()
		delete(s.oauth.hooks, r.PathValue("clientID"))
		s.oauth.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT "+oauthPrefix+"/hosts/{host}/outbound", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Refuse string `json:"refuse"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&body); err != nil ||
			(body.Refuse != "" && body.Refuse != "outbound_refused" && body.Refuse != "invalid") {
			refuse(w, http.StatusBadRequest, "bad_request", "Send refuse outbound_refused, invalid, or empty to let the host through.")
			return
		}
		s.oauth.mu.Lock()
		if body.Refuse == "" {
			delete(s.oauth.outbound, strings.ToLower(r.PathValue("host")))
		} else {
			s.oauth.outbound[strings.ToLower(r.PathValue("host"))] = body.Refuse
		}
		s.oauth.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
}
