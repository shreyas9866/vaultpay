package models

import "time"

// User represents a merchant/developer account
type User struct {
	ID        string    `db:"id" json:"id"`
	Email     string    `db:"email" json:"email"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// APIKey represents the hashed security credentials for a user
type APIKey struct {
	ID        string    `db:"id" json:"id"`
	UserID    string    `db:"user_id" json:"user_id"`
	KeyPrefix string    `db:"key_prefix" json:"key_prefix"` // e.g., "sk_test"
	KeyHash   string    `db:"key_hash" json:"-"`            // The "-" means NEVER send this in JSON responses!
	IsActive  bool      `db:"is_active" json:"is_active"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}