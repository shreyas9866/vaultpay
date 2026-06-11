package billing

import (
	"errors"
	"math"
	"time"
)

// CalculateUpgradeProration determines the net amount due when changing plans mid-cycle.
// All amounts must be in cents (e.g., $10.00 = 1000).
func CalculateUpgradeProration(oldPlanCents, newPlanCents int64, cycleStart, cycleEnd, upgradeTime time.Time) (int64, error) {
	if upgradeTime.Before(cycleStart) || upgradeTime.After(cycleEnd) {
		return 0, errors.New("upgrade time is outside the current billing cycle")
	}

	// 1. Calculate total duration and remaining duration
	totalCycleDuration := cycleEnd.Sub(cycleStart).Hours()
	remainingDuration := cycleEnd.Sub(upgradeTime).Hours()

	if totalCycleDuration == 0 {
		return 0, errors.New("invalid billing cycle duration")
	}

	// 2. Find the percentage of the cycle remaining
	percentageRemaining := remainingDuration / totalCycleDuration

	// 3. Calculate the credit for the unused time on the old plan
	// We use math.Round to handle sub-cent fractions accurately
	creditCents := int64(math.Round(float64(oldPlanCents) * percentageRemaining))

	// 4. Calculate the cost for the remaining time on the new plan
	chargeCents := int64(math.Round(float64(newPlanCents) * percentageRemaining))

	// 5. The net amount the user owes today to upgrade immediately
	netDueToday := chargeCents - creditCents

	// Ensure we don't accidentally return a negative charge (if downgrading, this would be a credit balance, but we are focusing on upgrades)
	if netDueToday < 0 {
		netDueToday = 0 
	}

	return netDueToday, nil
}