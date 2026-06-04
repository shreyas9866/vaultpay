package handlers

import (
	"context" // Make sure context is imported
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/models"
)

// 1. NEW: The Interface Contract
type ChargeStore interface {
	CreateCharge(ctx context.Context, charge *models.Charge) error
	RefundCharge(ctx context.Context, chargeID string) (*models.Charge, error) 
}

// 2. UPDATED: We now use the interface instead of the hardcoded *database.Store
type ChargeHandler struct {
	store ChargeStore 
	redis *redis.Client
}

// 3. UPDATED: The constructor now accepts the interface
func NewChargeHandler(store ChargeStore, rdb *redis.Client) *ChargeHandler {
	return &ChargeHandler{store: store, redis: rdb}
}

// ... (Keep your CreateChargeRequest struct and the entire Create method exactly the same) ...

// CreateChargeRequest represents the exact JSON we expect from the client.
type CreateChargeRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Create handles the POST /charges endpoint
func (h *ChargeHandler) Create(w http.ResponseWriter, r *http.Request) {
	// 1. Extract the Idempotency Key from the Headers
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	// --- 3. NEW: REDIS IDEMPOTENCY CACHE ---
	redisKey := "idemp:charge:" + idempotencyKey

	// SetNX (Set if Not eXists) creates the lock. 
	// If the key is already there, it returns false instantly.
	isNewRequest, err := h.redis.SetNX(r.Context(), redisKey, "processing", 24*time.Hour).Result()
	if err != nil {
		http.Error(w, "Cache failure", http.StatusInternalServerError)
		return
	}

	if !isNewRequest {
		// Redis intercepted the duplicate request before it ever reached PostgreSQL!
		http.Error(w, "Idempotency key already used (Caught by Redis)", http.StatusConflict)
		return
	}
	// ---------------------------------------

	// 4. Decode the incoming JSON body
	var req CreateChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 5. Basic Business Validation
	if req.Amount <= 0 {
		http.Error(w, "Amount must be greater than zero", http.StatusBadRequest)
		return
	}
	if len(req.Currency) != 3 {
		http.Error(w, "Currency must be a 3-letter ISO code", http.StatusBadRequest)
		return
	}

	// 6. Construct the Database Model
	charge := &models.Charge{
		Amount:         req.Amount,
		Currency:       strings.ToUpper(req.Currency),
		Status:         models.StatusCreated,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 7. Save to Database (Our ACID Transaction)
	if err := h.store.CreateCharge(r.Context(), charge); err != nil {
		// Fallback: If Redis failed to catch it, Postgres will still block it
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			http.Error(w, "Idempotency key already used for a previous request (Caught by DB)", http.StatusConflict)
			return
		}

		http.Error(w, "Failed to create charge", http.StatusInternalServerError)
		return
	}

	// 8. Return Success Response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charge)
}