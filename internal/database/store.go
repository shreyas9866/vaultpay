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
// Charge represents a single payment record in the database
type Charge struct {
	ID             string    `db:"id"`
	Amount         int64     `db:"amount"`
	Currency       string    `db:"currency"`
	Status         string    `db:"status"`
	IdempotencyKey *string   `db:"idempotency_key"` 
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
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
	defer tx.Rollback()

	// 2. Fetch the charge and lock the row using FOR UPDATE
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

	// 3. Validate the state transition
	if !charge.IsValidTransition(models.StatusRefunded) {
		return nil, fmt.Errorf("invalid state transition from %s to %s", charge.Status, models.StatusRefunded)
	}

	// Capture the old status BEFORE we change it so we can log it!
	oldStatus := charge.Status

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

	// Update our local memory model
	charge.Status = models.StatusRefunded

	// 5. Serialize the payload for our Outbox Event notification
	eventPayload, err := json.Marshal(charge)
	if err != nil {
		return nil, err
	}

	// 6. Record the event in the outbox table
	outboxQuery := `
		INSERT INTO outbox_events (event_type, payload) 
		VALUES ($1, $2)
	`
	_, err = tx.ExecContext(ctx, outboxQuery, "charge.refunded", eventPayload)
	if err != nil {
		return nil, err
	}

	// 7. Record the immutable audit log!
	auditQuery := `
		INSERT INTO payment_events (charge_id, previous_status, new_status, event_reason)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.ExecContext(ctx, auditQuery, chargeID, oldStatus, models.StatusRefunded, "Refund requested via API")
	if err != nil {
		return nil, err
	}

	// 8. Commit the entire transaction atomically
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
	ID             string    `db:"id" json:"id"`
	ChargeID       string    `db:"charge_id" json:"charge_id"`
	PreviousStatus *string   `db:"previous_status" json:"previous_status"`
	NewStatus      string    `db:"new_status" json:"new_status"`
	EventReason    string    `db:"event_reason" json:"event_reason"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
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
// GetCharge fetches a single charge by its ID
func (s *Store) GetCharge(id string) (*Charge, error) {
	var charge Charge
	// Removed idempotency_key from the SELECT statement so Postgres doesn't panic!
	query := `SELECT id, amount, currency, status, created_at, updated_at FROM charges WHERE id = $1`
	err := s.db.Get(&charge, query, id)
	return &charge, err
}

// UpdateChargeStatus changes the status of a charge (e.g., to 'disputed' or 'refunded')
func (s *Store) UpdateChargeStatus(id, status string) error {
	query := `UPDATE charges SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.db.Exec(query, status, id)
	return err
}
// LogPaymentEvent records state transitions for event sourcing
func (s *Store) LogPaymentEvent(id, chargeID, previousStatus, newStatus, reason string) error {
	query := `INSERT INTO payment_events (id, charge_id, previous_status, new_status, event_reason, created_at) 
	          VALUES ($1, $2, $3, $4, $5, NOW())`
	_, err := s.db.Exec(query, id, chargeID, previousStatus, newStatus, reason)
	return err
}

// GetUserByEmail fetches a user for login verification
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	// Removed updated_at so it perfectly matches your database schema!
	query := `SELECT id, email, created_at FROM users WHERE email = $1`
	err := s.db.GetContext(ctx, &user, query, email)
	return &user, err
}