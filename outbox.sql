CREATE TABLE IF NOT EXISTS outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type VARCHAR(50) NOT NULL, -- e.g., 'charge'
    aggregate_id VARCHAR(255) NOT NULL,  -- The ID of the charge
    event_type VARCHAR(50) NOT NULL,     -- e.g., 'charge.created'
    payload JSONB NOT NULL,              -- The actual JSON data of the charge
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, processing, completed, failed
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);