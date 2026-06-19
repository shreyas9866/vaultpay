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
	"github.com/shreyas9866/vaultpay/internal/currency" // Our new converter!
	"github.com/shreyas9866/vaultpay/internal/metrics"  // Metrics Package
	"github.com/shreyas9866/vaultpay/internal/models"
	"github.com/shreyas9866/vaultpay/internal/worker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"github.com/go-chi/chi/v5"
	"github.com/shreyas9866/vaultpay/internal/database"
)

type ChargeStore interface {
	CreateCharge(ctx context.Context, charge *models.Charge) error
	RefundCharge(ctx context.Context, chargeID string) (*models.Charge, error)
	GetAuditTrail(ctx context.Context, chargeID string) ([]database.PaymentEvent, error)
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
	ctx, span := otel.Tracer("vaultpay-handlers").Start(r.Context(), "ChargeHandler.Create")
	defer span.End()

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	span.SetAttributes(attribute.String("charge.idempotency_key", idempotencyKey))

	redisKey := "idemp:charge:" + idempotencyKey

	isNewRequest, err := h.redis.SetNX(ctx, redisKey, "processing", 24*time.Hour).Result()
	if err != nil {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Cache failure", http.StatusInternalServerError)
		return
	}

	if !isNewRequest {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Idempotency key already used (Caught by Redis)", http.StatusConflict)
		return
	}

	var req CreateChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("charge.currency", req.Currency),
		attribute.Int64("charge.amount", req.Amount),
	)

	if req.Amount <= 0 {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Amount must be greater than zero", http.StatusBadRequest)
		return
	}

	// Strict Multi-Currency Validation
	req.Currency = strings.ToUpper(req.Currency)
	if req.Currency != "USD" && req.Currency != "INR" {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Only USD and INR are currently supported", http.StatusBadRequest)
		return
	}

	// The Concurrency Pipeline & Context Cancellation
	// Give the currency converter EXACTLY 500ms to finish, otherwise kill it.
	convertCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	usdEquivalent, err := currency.Convert(convertCtx, req.Amount, req.Currency, "USD")
	if err != nil {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		http.Error(w, "Currency exchange service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Attach the converted USD amount to our Jaeger Trace
	span.SetAttributes(attribute.Int64("charge.usd_equivalent", usdEquivalent))

	charge := &models.Charge{
		Amount:         req.Amount,
		Currency:       req.Currency, // Store the original currency requested
		Status:         models.StatusCreated,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := h.store.CreateCharge(ctx, charge); err != nil {
		metrics.ChargesTotal.WithLabelValues("failed").Inc()
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			http.Error(w, "Idempotency key already used (Caught by DB)", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create charge", http.StatusInternalServerError)
		return
	}

	// NEW: Use the updated Webhook Enqueue method
	if h.asynqClient != nil {
		payload := worker.WebhookPayload{
			EventID:   "evt_" + charge.ID, 
			EventType: "charge.created",
			ChargeID:  charge.ID,
			Status:    string(charge.Status),
		}
		// Fire and forget to the background worker
		worker.EnqueueWebhook(h.asynqClient, "https://webhook.site/your-custom-webhook-url", payload)
	}

	metrics.ChargesTotal.WithLabelValues("success").Inc()

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
		RespondWithError(w, http.StatusBadRequest, "Malformed Request", "The request body contains invalid or improperly formatted JSON.")
		return
	}

	if req.ChargeID == "" {
		RespondWithError(w, http.StatusBadRequest, "Invalid Request", "The charge_id field is required in the JSON body.")
		return
	}

	span.SetAttributes(attribute.String("refund.charge_id", req.ChargeID))

	refundedCharge, err := h.store.RefundCharge(ctx, req.ChargeID)
	if err != nil {
		log.Printf("❌ CRITICAL REFUND ERROR: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "Refund Processing Failed", "The system encountered an error while attempting to process this refund. Please try again later.")
		return
	}

	// NEW: Use the updated Webhook Enqueue method
	if h.asynqClient != nil {
		payload := worker.WebhookPayload{
			EventID:   "evt_" + refundedCharge.ID,
			EventType: "charge.refunded",
			ChargeID:  refundedCharge.ID,
			Status:    string(refundedCharge.Status),
		}
		worker.EnqueueWebhook(h.asynqClient, "https://webhook.site/your-custom-webhook-url", payload)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(refundedCharge)
}

// GetTimeline returns the complete, immutable audit log for a specific charge
func (h *ChargeHandler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	// Grab the charge ID right out of the URL (e.g., /v1/charges/123/timeline)
	chargeID := chi.URLParam(r, "id")
	if chargeID == "" {
		http.Error(w, "Charge ID is required", http.StatusBadRequest)
		return
	}

	// Fetch the timeline from your new database engine
	events, err := h.store.GetAuditTrail(r.Context(), chargeID)
	if err != nil {
		http.Error(w, "Failed to fetch timeline: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// If no events exist, return a friendly empty array instead of null
	if events == nil {
		events = []database.PaymentEvent{}
	}

	// Send the receipt
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"charge_id": chargeID,
		"timeline":  events,
	})
}