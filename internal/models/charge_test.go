package models

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCharge_IsValidTransition(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus ChargeStatus 
		newStatus     ChargeStatus 
		expectValid   bool
	}{
		// The Happy Paths
		{"Created to Processing", StatusCreated, StatusProcessing, true},
		{"Processing to Succeeded", StatusProcessing, StatusSucceeded, true}, // FIXED
		{"Succeeded to Refunded", StatusSucceeded, StatusRefunded, true},       // FIXED

		// The Forbidden Paths
		{"Cannot go backwards to Created", StatusSucceeded, StatusCreated, false}, // FIXED
		{"Refunded is terminal", StatusRefunded, StatusSucceeded, false},          // FIXED
		{"Disputed is terminal", StatusDisputed, StatusProcessing, false},
		{"Cannot skip to Refunded", StatusCreated, StatusRefunded, false},

		// Idempotent (Same state)
		{"Same state is allowed", StatusSucceeded, StatusSucceeded, true}, // FIXED
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charge := &Charge{Status: tt.currentStatus}

			isValid := charge.IsValidTransition(tt.newStatus)

			assert.Equal(t, tt.expectValid, isValid)
		})
	}
}
