package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"github.com/shreyas9866/vaultpay/internal/database"
)

type WebhookWorker struct {
	store *database.Store
}

func NewWebhookWorker(store *database.Store) *WebhookWorker {
	return &WebhookWorker{store: store}
}

func (w *WebhookWorker) Start(ctx context.Context) {
	log.Println("⚙️ Webhook worker started...")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Webhook worker shutting down...")
			return
		case <-ticker.C:
			w.processNextEvent(ctx)
		}
	}
}

func (w *WebhookWorker) processNextEvent(ctx context.Context) {
	event, err := w.store.FetchNextOutboxEvent(ctx)
	if err != nil {
		log.Printf("❌ Error fetching outbox event: %v", err)
		return
	}
	if event == nil {
		return 
	}

	// Updated to %s for the UUID string
	log.Printf("📦 Processing event ID: %s | Type: %s", event.ID, event.EventType)

	targetURL := "http://localhost:8081/webhook-receiver-test" 
	webhookSecret := "whsec_vaultpay_super_secret_123"

	signature := generateHMAC(event.Payload, webhookSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(event.Payload))
	if err != nil {
		log.Printf("❌ Error building request: %v", err)
		return
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("VaultPay-Signature", signature)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		w.store.UpdateOutboxEventStatus(ctx, event.ID, "delivered", event.Attempts+1, sql.NullTime{})
		log.Printf("✅ Webhook delivered successfully: Event %s", event.ID)
		if resp != nil {
			resp.Body.Close()
		}
		return
	}

	if resp != nil {
		resp.Body.Close()
	}

	attempts := event.Attempts + 1
	if attempts >= 5 {
		w.store.UpdateOutboxEventStatus(ctx, event.ID, "failed", attempts, sql.NullTime{})
		log.Printf("☠️ Webhook permanently failed after 5 attempts: Event %s", event.ID)
		return
	}

	backoffSeconds := (1 << attempts) * 5 
	nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	w.store.UpdateOutboxEventStatus(ctx, event.ID, "pending", attempts, sql.NullTime{Time: nextRetry, Valid: true})
	log.Printf("⚠️ Webhook failed (Attempt %d). Retrying at %v: Event %s", attempts, nextRetry.Format(time.TimeOnly), event.ID)
}

func generateHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}