CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL, 
    plan_id VARCHAR(50) NOT NULL, -- e.g., 'basic_monthly', 'pro_monthly'
    status VARCHAR(20) NOT NULL, -- 'active', 'past_due', 'canceled'
    current_period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    current_period_end TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for quick lookups when the cron job runs to renew subscriptions
CREATE INDEX idx_subscriptions_period_end ON subscriptions(current_period_end);