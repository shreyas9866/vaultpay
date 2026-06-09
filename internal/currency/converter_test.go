package currency

import (
	"context"
	"testing"
	"time"
)

func TestConvertINRtoUSD(t *testing.T) {
	// Give the test a 2-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 10,000 paise (100 INR) should calculate to exactly 120 cents ($1.20)
	result, err := Convert(ctx, 10000, "INR", "USD")
	
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
	
	if result != 120 {
		t.Errorf("Expected 120 cents, but got: %v", result)
	}
}