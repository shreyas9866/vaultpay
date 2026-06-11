package handlers

import (
	"encoding/json"
	"net/http"
	"time"
    "github.com/shreyas9866/vaultpay/internal/billing"
	"github.com/shreyas9866/vaultpay/internal/database"
)

type SubscriptionHandler struct {
	// We will inject your database store here in the next step
	store *database.Store
}

func NewSubscriptionHandler(store *database.Store) *SubscriptionHandler {
	return &SubscriptionHandler{store: store}
}
func (h *SubscriptionHandler) Upgrade(w http.ResponseWriter, r *http.Request) {
	// In a real app, you get this from the JWT/Auth middleware.
	// We will hardcode a mock UUID for testing purposes.
	userID := "123e4567-e89b-12d3-a456-426614174000"

	// 1. Fetch the REAL subscription from the database
	sub, err := h.store.GetActiveSubscription(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to fetch active subscription: "+err.Error(), http.StatusNotFound)
		return
	}

	// 2. Define the plan costs (Cents)
	basicPlanCents := int64(1000) // $10.00
	proPlanCents := int64(3000)   // $30.00

	// 3. THE ENGINE: Calculate the real prorated difference based on DB timestamps
	upgradeTime := time.Now()
	netDue, err := billing.CalculateUpgradeProration(
		basicPlanCents, 
		proPlanCents, 
		sub.CurrentPeriodStart, 
		sub.CurrentPeriodEnd, 
		upgradeTime,
	)
	if err != nil {
		http.Error(w, "Proration error: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 4. Return the exact receipt
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":               "Proration calculated successfully",
		"current_plan":          sub.PlanID,
		"new_plan_cents":        proPlanCents,
		"net_amount_due_cents":  netDue,
		"billing_cycle_end":     sub.CurrentPeriodEnd.Format(time.RFC3339),
	})
}