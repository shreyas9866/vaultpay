-- Upgrade the outbox table safely (Idempotent Migration)
ALTER TABLE outbox_events 
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS attempts INT DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMP;