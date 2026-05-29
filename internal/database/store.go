package database

import (
	"context"
	"encoding/json"
	"fmt"

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