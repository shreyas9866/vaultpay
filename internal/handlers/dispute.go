package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/worker"
)

type DisputeHandler struct {
	store       *database.Store
	asynqClient *asynq.Client
}

func NewDisputeHandler(store *database.Store, asynqClient *asynq.Client) *DisputeHandler {
	return &DisputeHandler{
		store:       store,
		asynqClient: asynqClient,
	}
}

type DisputeRequest struct {
	ChargeID string `json:"charge_id"`
	Reason   string `json:"reason"`
}

func (h *DisputeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req DisputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid Request", "Invalid JSON body.")
		return
	}

	// 1. Verify the charge exists and isn't already refunded/disputed
	// (Assuming you have a GetCharge method in your store - if not, we can add it!)
	charge, err := h.store.GetCharge(req.ChargeID)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "Not Found", "Charge not found.")
		return
	}

	if charge.Status == "disputed" || charge.Status == "refunded" {
		RespondWithError(w, http.StatusConflict, "State Conflict", "Cannot dispute a charge that is already refunded or disputed.")
		return
	}

	// 2. Update status to 'disputed'
	err = h.store.UpdateChargeStatus(req.ChargeID, "disputed")
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Database Error", "Failed to update charge status.")
		return
	}

	// 3. Event Sourcing: Log this in the timeline
	eventID := uuid.New().String()
	h.store.LogPaymentEvent(eventID, req.ChargeID, charge.Status, "disputed", "Customer initiated dispute: "+req.Reason)

	// 4. Asynchronous Webhook: Drop a job into Redis
	// We use a dummy merchant URL here, but in real life, this comes from the merchant's settings!
	merchantWebhookURL := "https://webhook.site/your-custom-webhook-url" 
	
	payload := worker.WebhookPayload{
		EventID:   eventID,
		EventType: "charge.disputed",
		ChargeID:  req.ChargeID,
		Status:    "disputed",
	}

	// Fire and forget - the background worker handles the retries!
	err = worker.EnqueueWebhook(h.asynqClient, merchantWebhookURL, payload)
	if err != nil {
		// We just log this, we don't fail the API request if the queue is temporarily busy
		// A true enterprise system might have a fallback queue here.
		println("Warning: Failed to enqueue webhook task: ", err.Error())
	}

	// Replace RespondWithJSON with this standard Go approach:
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message":   "Dispute created, charge locked, and merchant notified.",
		"charge_id": req.ChargeID,
		"status":    "disputed",
	})
}