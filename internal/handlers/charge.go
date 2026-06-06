package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/models"
	"github.com/shreyas9866/vaultpay/internal/worker"

	// NEW: OpenTelemetry Imports
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type ChargeStore interface {
	CreateCharge(ctx context.Context, charge *models.Charge) error
	RefundCharge(ctx context.Context, chargeID string) (*models.Charge, error)
}

type ChargeHandler struct {
	store       ChargeStore
	redis       *redis.Client
	asynqClient *asynq.Client
}

func NewChargeHandler(store ChargeStore, rdb *redis.Client, asynqClient *asynq.Client) *ChargeHandler {
	return &ChargeHandler{
		store:       store,
		redis:       rdb,
		asynqClient: asynqClient,
	}
}

type CreateChargeRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func (h *ChargeHandler) Create(w http.ResponseWriter, r *http.Request) {
	// NEW: Start a Trace Span for the handler logic
	ctx, span := otel.Tracer("vaultpay-handlers").Start(r.Context(), "ChargeHandler.Create")
	defer span.End() // This automatically closes the span when the function finishes

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	// NEW: Attach custom data to our trace so we can search it in Jaeger later!
	span.SetAttributes(attribute.String("charge.idempotency_key", idempotencyKey))

	redisKey := "idemp:charge:" + idempotencyKey

	// NOTICE: We pass `ctx` instead of `r.Context()` so Redis joins our existing trace!
	isNewRequest, err := h.redis.SetNX(ctx, redisKey, "processing", 24*time.Hour).Result()
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

	// NEW: Add the business data to the trace
	span.SetAttributes(
		attribute.String("charge.currency", req.Currency),
		attribute.Int64("charge.amount", req.Amount),
	)

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

	// NOTICE: We pass `ctx` to the database so the DB query joins the trace!
	if err := h.store.CreateCharge(ctx, charge); err != nil {
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			http.Error(w, "Idempotency key already used (Caught by DB)", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create charge", http.StatusInternalServerError)
		return
	}

	if h.asynqClient != nil {
		payloadBytes, _ := json.Marshal(charge)
		task, err := worker.NewWebhookDeliveryTask(charge.ID, "charge.created", payloadBytes)
		if err == nil {
			h.asynqClient.Enqueue(task)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charge)
}

type RefundRequest struct {
	ChargeID string `json:"charge_id"`
}

func (h *ChargeHandler) Refund(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("vaultpay-handlers").Start(r.Context(), "ChargeHandler.Refund")
	defer span.End()

	var req RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.ChargeID == "" {
		http.Error(w, `{"error": "charge_id is required"}`, http.StatusBadRequest)
		return
	}

	span.SetAttributes(attribute.String("refund.charge_id", req.ChargeID))

	refundedCharge, err := h.store.RefundCharge(ctx, req.ChargeID)
	if err != nil {
		http.Error(w, `{"error": "failed to process refund"}`, http.StatusInternalServerError)
		return
	}

	if h.asynqClient != nil {
		payloadBytes, _ := json.Marshal(refundedCharge)
		task, _ := worker.NewWebhookDeliveryTask(refundedCharge.ID, "charge.refunded", payloadBytes)
		h.asynqClient.Enqueue(task)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(refundedCharge)
}