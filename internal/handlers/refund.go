package handlers

import (
	"encoding/json"
	"net/http"
)

type RefundRequest struct {
	ChargeID string `json:"charge_id"`
}

// Refund handles the POST /v1/refunds endpoint
func (h *ChargeHandler) Refund(w http.ResponseWriter, r *http.Request) {
	// 1. Decode incoming JSON payload
	var req RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.ChargeID == "" {
		http.Error(w, "charge_id is required", http.StatusBadRequest)
		return
	}

	// 2. Execute the ACID-protected refund transaction
	charge, err := h.store.RefundCharge(r.Context(), req.ChargeID)
	if err != nil {
		// Handle business logic errors cleanly
		if err.Error() == "charge not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if jsonErr := err.Error(); jsonErr != "" && (err.Error()[:13] == "invalid state") {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		http.Error(w, "Internal server error processing refund", http.StatusInternalServerError)
		return
	}

	// 3. Return the fully updated charge object back to the client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(charge)
}