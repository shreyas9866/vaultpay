package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shreyas9866/vaultpay/internal/database"
)

type TransferHandler struct {
	store *database.Store
}

func NewTransferHandler(store *database.Store) *TransferHandler {
	return &TransferHandler{store: store}
}

func (h *TransferHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	// 1. Define the expected JSON payload
	var req struct {
		SenderWalletID   string `json:"sender_wallet_id"`
		ReceiverWalletID string `json:"receiver_wallet_id"`
		Amount           int64  `json:"amount"` // In cents!
	}

	// 2. Parse the request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	// 3. Basic validation
	if req.Amount <= 0 {
		http.Error(w, `{"error": "Transfer amount must be greater than zero"}`, http.StatusBadRequest)
		return
	}
	if req.SenderWalletID == req.ReceiverWalletID {
		http.Error(w, `{"error": "Cannot transfer money to yourself"}`, http.StatusBadRequest)
		return
	}

	// 4. Trigger the ACID Database Transaction
	err := h.store.TransferFunds(r.Context(), req.SenderWalletID, req.ReceiverWalletID, req.Amount)
	if err != nil {
		// If the database rejects it (e.g., insufficient funds), tell the user
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusUnprocessableEntity)
		return
	}

	// 5. Success!
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "success", "message": "Transfer completed successfully"}`))
}