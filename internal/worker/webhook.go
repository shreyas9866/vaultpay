package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
)

// TaskTypeWebhookDelivery is the routing key for Redis
const TaskTypeWebhookDelivery = "webhook:delivery"

// WebhookPayload is the JSON data sent to the merchant
type WebhookPayload struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	ChargeID  string `json:"charge_id"`
	Status    string `json:"status"`
}

// WebhookTask is the internal structure saved in Redis
type WebhookTask struct {
	URL     string         `json:"url"`
	Payload WebhookPayload `json:"payload"`
}

// ProcessWebhookDelivery is the worker that actually fires the HTTP request
func ProcessWebhookDelivery(ctx context.Context, t *asynq.Task) error {
	var task WebhookTask
	if err := json.Unmarshal(t.Payload(), &task); err != nil {
		return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
	}

	requestBody, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %v: %w", err, asynq.SkipRetry)
	}

	log.Printf("🚀 Firing webhook [%s] to %s", task.Payload.EventType, task.URL)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "POST", task.URL, bytes.NewBuffer(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "VaultPay-Webhook-Worker/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook (server down or timeout): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("merchant server rejected the webhook with status: %d", resp.StatusCode)
	}

	log.Printf("✅ Webhook delivered successfully to %s", task.URL)
	return nil
}

// EnqueueWebhook drops the task into the Redis queue
func EnqueueWebhook(client *asynq.Client, url string, payload WebhookPayload) error {
	taskData := WebhookTask{
		URL:     url,
		Payload: payload,
	}

	payloadBytes, err := json.Marshal(taskData)
	if err != nil {
		return err
	}

	task := asynq.NewTask(TaskTypeWebhookDelivery, payloadBytes, asynq.MaxRetry(5))
	
	_, err = client.Enqueue(task)
	return err
}