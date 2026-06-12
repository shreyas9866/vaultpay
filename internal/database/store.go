package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
    "time"
	"github.com/jmoiron/sqlx"
	"github.com/shreyas9866/vaultpay/internal/models"
)

// Store wraps our database connection
type Store struct {
	db *sqlx.DB
}

func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// CreateCharge safely inserts a new payment and an outbox event in a single atomic transaction.
func (s *Store) CreateCharge(ctx context.Context, charge *models.Charge) error {
	// 1. Begin the Transaction
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// 2. Defer a Rollback. If the function returns before tx.Commit(), Postgres completely undoes everything.
	defer tx.Rollback()

	// 3. Insert the Charge
	chargeQuery := `
        INSERT INTO charges (amount, currency, status, idempotency_key, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `
	err = tx.QueryRowxContext(ctx, chargeQuery,
		charge.Amount,
		charge.Currency,
		charge.Status,
		charge.IdempotencyKey,
		charge.CreatedAt,
		charge.UpdatedAt,
	).Scan(&charge.ID)

	if err != nil {
		return fmt.Errorf("failed to insert charge: %w", err)
	}

	// 4. Prepare the Outbox Event payload
	payloadBytes, err := json.Marshal(charge)
	if err != nil {
		return fmt.Errorf("failed to marshal charge payload: %w", err)
	}

	// 5. Insert the Outbox Event
	outboxQuery := `
        INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
        VALUES ($1, $2, $3, $4)
    `
	_, err = tx.ExecContext(ctx, outboxQuery,
		"charge",
		charge.ID,
		"charge.created",
		payloadBytes,
	)

	if err != nil {
		return fmt.Errorf("failed to insert outbox event: %w", err)
	}

	// 6. Commit the Transaction (Locks everything in permanently)
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateChargeStatus safely advances the state machine in the database.
// UpdateChargeStatus safely advances the state machine AND appends to the immutable audit log.
func (s *Store) UpdateChargeStatus(ctx context.Context, chargeID string, newStatus models.ChargeStatus) error {
	// 1. Begin the atomic transaction
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 2. Fetch the CURRENT status and lock the row (FOR UPDATE).
	// This prevents race conditions if two webhooks hit at the exact same millisecond.
	var oldStatus string
	query := `SELECT status FROM charges WHERE id = $1 FOR UPDATE`
	err = tx.QueryRowContext(ctx, query, chargeID).Scan(&oldStatus)
	if err != nil {
		return fmt.Errorf("failed to fetch and lock charge: %w", err)
	}

	// 3. Update the main charges table
	updateQuery := `UPDATE charges SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = tx.ExecContext(ctx, updateQuery, newStatus, chargeID)
	if err != nil {
		return fmt.Errorf("failed to update charge status: %w", err)
	}

	// 4. THE EVENT SOURCING MAGIC: Insert the immutable audit log
	eventQuery := `
		INSERT INTO payment_events (charge_id, previous_status, new_status, event_reason)
		VALUES ($1, $2, $3, $4)
	`
	reason := "System state transition" // You can make this dynamic later!
	_, err = tx.ExecContext(ctx, eventQuery, chargeID, oldStatus, newStatus, reason)
	if err != nil {
		return fmt.Errorf("failed to insert audit log event: %w", err)
	}

	// 5. Commit everything permanently
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CreateUser provisions a new developer account in the system.
func (s *Store) CreateUser(ctx context.Context, user *models.User) error {
	query := `
        INSERT INTO users (email)
        VALUES ($1)
        RETURNING id, created_at
    `
	// Using QueryRowContext to instantly capture the Postgres-generated UUID and timestamp
	return s.db.QueryRowContext(ctx, query, user.Email).Scan(&user.ID, &user.CreatedAt)
}

// CreateAPIKey maps a securely hashed API key to a specific user account.
func (s *Store) CreateAPIKey(ctx context.Context, apiKey *models.APIKey) error {
	query := `
        INSERT INTO api_keys (user_id, key_prefix, key_hash)
        VALUES ($1, $2, $3)
        RETURNING id, is_active, created_at
    `
	return s.db.QueryRowContext(ctx, query,
		apiKey.UserID,
		apiKey.KeyPrefix,
		apiKey.KeyHash,
	).Scan(&apiKey.ID, &apiKey.IsActive, &apiKey.CreatedAt)
}

// RefundCharge executes a concurrent-safe ACID transaction to refund a payment
func (s *Store) RefundCharge(ctx context.Context, chargeID string) (*models.Charge, error) {
	// 1. Begin the database transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Defer a rollback. If the function returns early due to an error,
	// the transaction safely aborts without leaving dirty data.
	defer tx.Rollback()

	// 2. Fetch the charge and lock the row using FOR UPDATE
	// This prevents any other concurrent request from modifying this specific charge
	query := `
		SELECT id, amount, currency, status, idempotency_key, created_at, updated_at 
		FROM charges 
		WHERE id = $1 
		FOR UPDATE
	`

	charge := &models.Charge{}
	err = tx.QueryRowContext(ctx, query, chargeID).Scan(
		&charge.ID,
		&charge.Amount,
		&charge.Currency,
		&charge.Status,
		&charge.IdempotencyKey,
		&charge.CreatedAt,
		&charge.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("charge not found")
		}
		return nil, err
	}

	// 3. Validate the state transition using our Go State Machine
	if !charge.IsValidTransition(models.StatusRefunded) {
		return nil, fmt.Errorf("invalid state transition from %s to %s", charge.Status, models.StatusRefunded)
	}

	// 4. Update the charge status in the database
	updateQuery := `
		UPDATE charges 
		SET status = $1, updated_at = CURRENT_TIMESTAMP 
		WHERE id = $2
	`
	_, err = tx.ExecContext(ctx, updateQuery, models.StatusRefunded, chargeID)
	if err != nil {
		return nil, err
	}

	// Update our local memory model to reflect the change for the API response
	charge.Status = models.StatusRefunded

	// 5. Serialize the payload for our Outbox Event notification
	eventPayload, err := json.Marshal(charge)
	if err != nil {
		return nil, err
	}

	// 6. Record the event in the outbox table within the SAME transaction
	outboxQuery := `
		INSERT INTO outbox_events (event_type, payload) 
		VALUES ($1, $2)
	`
	_, err = tx.ExecContext(ctx, outboxQuery, "charge.refunded", eventPayload)
	if err != nil {
		return nil, err
	}

	// 7. Commit the entire transaction atomically
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return charge, nil
}

// OutboxEvent represents a pending webhook notification
type OutboxEvent struct {
	ID          string
	EventType   string
	Payload     []byte
	Attempts    int
	NextRetryAt sql.NullTime
}

// FetchNextOutboxEvent grabs the oldest pending event and locks it for processing.
// "SKIP LOCKED" ensures that if another worker is already processing an event,
// this query will instantly skip it and grab the next available one.
func (s *Store) FetchNextOutboxEvent(ctx context.Context) (*OutboxEvent, error) {
	query := `
		SELECT id, event_type, payload, attempts, next_retry_at 
		FROM outbox_events 
		WHERE status = 'pending' 
		  AND (next_retry_at IS NULL OR next_retry_at <= CURRENT_TIMESTAMP)
		ORDER BY created_at ASC 
		LIMIT 1 
		FOR UPDATE SKIP LOCKED
	`

	event := &OutboxEvent{}
	err := s.db.QueryRowContext(ctx, query).Scan(
		&event.ID,
		&event.EventType,
		&event.Payload,
		&event.Attempts,
		&event.NextRetryAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No pending events right now, this is normal
		}
		return nil, err
	}

	return event, nil
}

// UpdateOutboxEventStatus marks an event as delivered or sets it up for a retry.
func (s *Store) UpdateOutboxEventStatus(ctx context.Context, id string, status string, attempts int, nextRetryAt sql.NullTime) error {
	query := `
		UPDATE outbox_events 
		SET status = $1, attempts = $2, next_retry_at = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $4
	`
	_, err := s.db.ExecContext(ctx, query, status, attempts, nextRetryAt, id)
	return err
}
// --- SUBSCRIPTION BILLING ---

// Subscription represents a row in the subscriptions table
type Subscription struct {
	ID                 string    
	UserID             string    
	PlanID             string    
	Status             string    
	CurrentPeriodStart time.Time 
	CurrentPeriodEnd   time.Time 
}

// GetActiveSubscription fetches the active subscription for a specific user.
// We use LIMIT 1 to ensure we only ever grab one active billing cycle.
func (s *Store) GetActiveSubscription(ctx context.Context, userID string) (*Subscription, error) {
	query := `
		SELECT id, user_id, plan_id, status, current_period_start, current_period_end 
		FROM subscriptions 
		WHERE user_id = $1 AND status = 'active' 
		LIMIT 1
	`

	var sub Subscription
	// Using standard Scan to map the SQL columns directly into our Go struct
	err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&sub.ID, 
		&sub.UserID, 
		&sub.PlanID, 
		&sub.Status,
		&sub.CurrentPeriodStart, 
		&sub.CurrentPeriodEnd,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no active subscription found for user")
		}
		return nil, err
	}

	return &sub, nil
}
// --- AUDIT LOGS ---

type PaymentEvent struct {
	ID             string    `json:"id"`
	ChargeID       string    `json:"charge_id"`
	PreviousStatus *string   `json:"previous_status"` // Pointer because it can be NULL initially
	NewStatus      string    `json:"new_status"`
	EventReason    string    `json:"event_reason"`
	CreatedAt      time.Time `json:"created_at"`
}

// GetAuditTrail fetches the complete, chronological history of a specific payment
func (s *Store) GetAuditTrail(ctx context.Context, chargeID string) ([]PaymentEvent, error) {
	query := `
		SELECT id, charge_id, previous_status, new_status, event_reason, created_at 
		FROM payment_events 
		WHERE charge_id = $1 
		ORDER BY created_at ASC
	`
	
	var events []PaymentEvent
	// sqlx allows us to easily map multiple rows into a slice!
	err := s.db.SelectContext(ctx, &events, query, chargeID)
	if err != nil {
		return nil, err
	}
	
	return events, nil
}