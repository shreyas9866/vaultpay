package currency

import (
	"context"
	"errors"
	"math"
	"time"
)

// We store rates multiplied by 10,000 to keep exchange rates as integers!
// e.g., 1 USD = 83.50 INR -> 835000
var exchangeRates = map[string]int64{
	"USD": 10000,
	"INR": 835000,
}

type ConversionResult struct {
	Amount int64
	Err    error
}

// Convert runs in a separate Goroutine and uses Channels to return data safely
func Convert(ctx context.Context, amount int64, from, to string) (int64, error) {
	if from == to {
		return amount, nil // No conversion needed
	}

	rateFrom, okFrom := exchangeRates[from]
	rateTo, okTo := exchangeRates[to]
	if !okFrom || !okTo {
		return 0, errors.New("unsupported currency")
	}

	// Create a channel to catch the result of our background worker
	resultChan := make(chan ConversionResult, 1)

	// Fire off a background worker (Goroutine)
	go func() {
		// Simulate a slow 3rd party Forex API call
		time.Sleep(200 * time.Millisecond)

		// Safely calculate the exchange rate conversion
		baseAmount := float64(amount) / float64(rateFrom)
		convertedFloat := baseAmount * float64(rateTo)

		// Round to the nearest minor integer unit (cents/paise)
		converted := int64(math.Round(convertedFloat))

		// Send the result back through the channel
		resultChan <- ConversionResult{Amount: converted, Err: nil}
	}()

	// The 'select' statement blocks until one of these channels receives data
	select {
	case res := <-resultChan:
		// The worker finished successfully
		return res.Amount, res.Err
	case <-ctx.Done():
		// The parent request timed out or was cancelled! Kill the process.
		return 0, errors.New("currency conversion timed out or was cancelled")
	}
}
