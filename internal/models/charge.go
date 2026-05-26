package models

import (
	"time"
)

// ChargeStatus represents the strict state machine of a payment
type ChargeStatus string

const (
	StatusCreated    ChargeStatus = "created"
	StatusProcessing ChargeStatus = "processing"
	StatusPaid       ChargeStatus = "paid"
	StatusFailed     ChargeStatus = "failed"
	StatusRefunded   ChargeStatus = "refunded"
)

// Charge represents a single payment transaction
type Charge struct {
	ID             string       `db:"id" json:"id"`
	Amount         int64        `db:"amount" json:"amount"`                 // Stored in minor units (e.g., cents, paise)
	Currency       string       `db:"currency" json:"currency"`             // 3-letter ISO code (e.g., USD, INR)
	Status         ChargeStatus `db:"status" json:"status"`                 // strict state machine
	IdempotencyKey string       `db:"idempotency_key" json:"idempotency_key"` // Prevents double-charging
	CreatedAt      time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time    `db:"updated_at" json:"updated_at"`
}