package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/models"
	"github.com/shreyas9866/vaultpay/internal/worker"
)

// ChargeStore defines the database repository interface
type ChargeStore interface {
	CreateCharge(ctx context.Context, charge *models.Charge) error
	RefundCharge(ctx context.Context, chargeID string) (*models.Charge, error)
}

// ChargeHandler orchestrates transaction HTTP endpoints
type ChargeHandler struct {
	store       ChargeStore
	redis       *redis.Client
	asynqClient *asynq.Client
}

// NewChargeHandler initializes a new instance of ChargeHandler
func NewChargeHandler(store ChargeStore, rdb *redis.Client, asynqClient *asynq.Client) *ChargeHandler {
	return &ChargeHandler{
		store:       store,
		redis:       rdb,
		asynqClient: asynqClient,
	}
}

// CreateChargeRequest represents the expected payload for transaction initialization
type CreateChargeRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Create handles incoming charges at POST /charges
func (h *ChargeHandler) Create(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	redisKey := "idemp:charge:" + idempotencyKey

	isNewRequest, err := h.redis.SetNX(r.Context(), redisKey, "processing", 24*time.Hour).Result()
	if err != nil {
		http.Error(w, "Cache failure", http.StatusInternalServerError)
		return
	}

	if !isNewRequest {
		http.Error(w, "Idempotency key already used (Caught by Redis)", http.StatusConflict)
		return
	}

	var req CreateChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.Amount <= 0 {
		http.Error(w, "Amount must be greater than zero", http.StatusBadRequest)
		return
	}
	if len(req.Currency) != 3 {
		http.Error(w, "Currency must be a 3-letter ISO code", http.StatusBadRequest)
		return
	}

	charge := &models.Charge{
		Amount:         req.Amount,
		Currency:       strings.ToUpper(req.Currency),
		Status:         models.StatusCreated,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := h.store.CreateCharge(r.Context(), charge); err != nil {
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			http.Error(w, "Idempotency key already used for a previous request (Caught by DB)", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create charge", http.StatusInternalServerError)
		return
	}

	if h.asynqClient != nil {
		payloadBytes, _ := json.Marshal(charge)
		task, err := worker.NewWebhookDeliveryTask(charge.ID, "charge.created", payloadBytes)
		if err == nil {
			info, err := h.asynqClient.Enqueue(task)
			if err != nil {
				log.Printf("❌ Failed to enqueue Asynq task: %v", err)
			} else {
				log.Printf("📥 Job safely enqueued to Asynq [Job ID: %s]", info.ID)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charge)
}

// RefundRequest represents the payload structure for making a refund
type RefundRequest struct {
	ChargeID string `json:"charge_id"`
}

// Refund handles payment refunds at POST /v1/refunds
func (h *ChargeHandler) Refund(w http.ResponseWriter, r *http.Request) {
	var req RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.ChargeID == "" {
		http.Error(w, `{"error": "charge_id is required"}`, http.StatusBadRequest)
		return
	}

	refundedCharge, err := h.store.RefundCharge(r.Context(), req.ChargeID)
	if err != nil {
		if err.Error() == "charge not found" {
			http.Error(w, `{"error": "charge not found"}`, http.StatusNotFound)
			return
		}
		if err.Error() == "invalid state transition from paid to refunded" {
			http.Error(w, `{"error": "invalid state transition"}`, http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, `{"error": "failed to process refund"}`, http.StatusInternalServerError)
		return
	}

	if h.asynqClient != nil {
		payloadBytes, _ := json.Marshal(refundedCharge)
		task, err := worker.NewWebhookDeliveryTask(refundedCharge.ID, "charge.refunded", payloadBytes)
		if err == nil {
			info, err := h.asynqClient.Enqueue(task)
			if err != nil {
				log.Printf("❌ Failed to enqueue Asynq task: %v", err)
			} else {
				log.Printf("📥 Job safely enqueued to Asynq [Job ID: %s]", info.ID)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(refundedCharge)
}