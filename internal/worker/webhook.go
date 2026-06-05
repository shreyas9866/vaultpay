package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
)

// Task Type Identifier
const TaskTypeWebhookDelivery = "webhook:deliver"

// WebhookPayload is the data we serialize and drop into the Redis queue
type WebhookPayload struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Data      []byte `json:"data"`
}

// NewWebhookDeliveryTask is a helper to quickly create a new job
func NewWebhookDeliveryTask(eventID, eventType string, data []byte) (*asynq.Task, error) {
	payload := WebhookPayload{
		EventID:   eventID,
		EventType: eventType,
		Data:      data,
	}
	
	bytesPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	
	// We set a max retry limit of 5. Asynq handles the exponential backoff automatically!
	return asynq.NewTask(TaskTypeWebhookDelivery, bytesPayload, asynq.MaxRetry(5)), nil
}

// ProcessWebhookDelivery is the consumer that runs when a job is pulled from Redis
func ProcessWebhookDelivery(ctx context.Context, t *asynq.Task) error {
	var p WebhookPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("json unmarshal failed: %v", err) // Fails job immediately
	}

	log.Printf("📦 [Asynq] Processing Webhook: %s | Type: %s", p.EventID, p.EventType)

	targetURL := "http://localhost:8081/webhook-receiver-test"
	webhookSecret := "whsec_vaultpay_super_secret_123"

	signature := generateHMAC(p.Data, webhookSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(p.Data))
	if err != nil {
		return fmt.Errorf("failed to build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("VaultPay-Signature", signature)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		// Returning an error tells Asynq: "I failed, please backoff and retry me later!"
		return fmt.Errorf("network error sending webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("✅ [Asynq] Webhook delivered successfully: %s", p.EventID)
		return nil // Returning nil tells Asynq to delete the job from Redis
	}

	return fmt.Errorf("server returned non-200 status: %d", resp.StatusCode)
}

func generateHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}