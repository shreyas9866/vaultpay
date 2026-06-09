package models

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCharge_IsValidTransition(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus ChargeStatus // <-- Changed from string
		newStatus     ChargeStatus // <-- Changed from string
		expectValid   bool
	}{
		// The Happy Paths
		{"Created to Processing", StatusCreated, StatusProcessing, true},
		{"Processing to Paid", StatusProcessing, StatusPaid, true},
		{"Paid to Refunded", StatusPaid, StatusRefunded, true},

		// The Forbidden Paths
		{"Cannot go backwards to Created", StatusPaid, StatusCreated, false},
		{"Refunded is terminal", StatusRefunded, StatusPaid, false},
		{"Disputed is terminal", StatusDisputed, StatusProcessing, false},
		{"Cannot skip to Refunded", StatusCreated, StatusRefunded, false},

		// Idempotent (Same state)
		{"Same state is allowed", StatusPaid, StatusPaid, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charge := &Charge{Status: tt.currentStatus}

			isValid := charge.IsValidTransition(tt.newStatus)

			assert.Equal(t, tt.expectValid, isValid)
		})
	}
}
