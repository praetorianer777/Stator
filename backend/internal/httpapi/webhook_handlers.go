package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// Webhooks: where the organization's content events are posted, kept by its
// administrators as in Armature. Adapted from Armature's handlers.

// errWebhooksOff answers for a server built without webhooks, which only a
// test does.
var errWebhooksOff = &APIError{Status: http.StatusServiceUnavailable, Code: "webhooks_unavailable",
	Message: "Webhooks are not set up on this server. Ask its operator to configure them."}

func (s *Server) webhooksOn(w http.ResponseWriter, r *http.Request) bool {
	if s.Webhooks == nil {
		respondError(w, r, errWebhooksOff)
		return false
	}
	return true
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	hooks, err := s.Webhooks.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhooks": hooks})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	var req webhook.WebhookInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Webhooks.Create(r.Context(), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"webhook": made})
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req webhook.WebhookInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Webhooks.Update(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": updated})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Webhooks.Delete(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	rotated, lsn, err := s.Webhooks.RotateSecret(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": rotated})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	sent, lsn, err := s.Webhooks.Test(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": sent})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, _, apiErr := window(r, webhook.DefaultDeliveries, webhook.MaxDeliveries)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	deliveries, err := s.Webhooks.Deliveries(r.Context(), id, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *Server) handleRedeliverWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhooksOn(w, r) {
		return
	}
	id, apiErr := pathUUID(r, "webhookID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	deliveryID, apiErr := pathUUID(r, "deliveryID", "delivery")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	sent, lsn, err := s.Webhooks.Redeliver(r.Context(), id, deliveryID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": sent})
}
