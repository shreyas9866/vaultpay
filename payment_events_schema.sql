CREATE TABLE payment_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    charge_id UUID NOT NULL, -- Links back to your main charges table
    previous_status VARCHAR(50), -- What was it before? (Can be null for the very first event)
    new_status VARCHAR(50) NOT NULL, -- What did it change to?
    event_reason TEXT, -- Optional context (e.g., "webhook received", "manual refund")
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Crucial index: We will frequently query "Show me all events for Charge X"
CREATE INDEX idx_payment_events_charge_id ON payment_events(charge_id);