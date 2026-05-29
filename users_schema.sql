-- 1. The Users Table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. The API Keys Table
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    key_prefix VARCHAR(50) NOT NULL, -- We store the first few chars (e.g., 'sk_test_') so users can identify their keys in the UI
    key_hash VARCHAR(255) NOT NULL,  -- The secure bcrypt hash of the actual secret key
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast lookups when a user makes a request
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);