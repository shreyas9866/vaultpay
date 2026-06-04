package models

import "time"

// 1. Define the custom type explicitly so Go is happy
type ChargeStatus string

// 2. Define all states using that specific type (this fixes the 'undefined' error)
const (
	StatusCreated    ChargeStatus = "created"
	StatusProcessing ChargeStatus = "processing"
	StatusPaid       ChargeStatus = "paid"
	StatusRefunded   ChargeStatus = "refunded"
	StatusDisputed   ChargeStatus = "disputed" 
)

type Charge struct {
	ID             string       `db:"id" json:"id"`
	Amount         int64        `db:"amount" json:"amount"`
	Currency       string       `db:"currency" json:"currency"`
	Status         ChargeStatus `db:"status" json:"status"` // Strictly typed!
	IdempotencyKey string       `db:"idempotency_key" json:"idempotency_key"`
	CreatedAt      time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time    `db:"updated_at" json:"updated_at"`
}

// 3. The map MUST use ChargeStatus, not string
var validTransitions = map[ChargeStatus]map[ChargeStatus]bool{
	StatusCreated: {
		StatusProcessing: true,
		StatusPaid:       true,
	},
	StatusProcessing: {
		StatusPaid:     true,
		StatusDisputed: true,
	},
	StatusPaid: {
		StatusRefunded: true,
		StatusDisputed: true,
	},
	StatusRefunded: {},
	StatusDisputed: {},
}

// 4. Method signature MUST use ChargeStatus
func (c *Charge) IsValidTransition(newStatus ChargeStatus) bool {
	if c.Status == newStatus {
		return true
	}

	allowedStates, exists := validTransitions[c.Status]
	if !exists {
		return false
	}

	return allowedStates[newStatus]
}