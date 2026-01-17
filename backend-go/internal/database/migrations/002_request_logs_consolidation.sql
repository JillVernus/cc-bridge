-- Migration 002: Consolidate request_logs.db tables into unified database
-- This adds tables that were previously in a separate request_logs.db file

-- User aliases table (for user ID to alias mappings)
CREATE TABLE IF NOT EXISTS user_aliases (
    user_id TEXT PRIMARY KEY,
    alias TEXT NOT NULL UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_user_aliases_alias ON user_aliases(alias);

-- API keys table (for access control)
CREATE TABLE IF NOT EXISTS api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    is_admin BOOLEAN NOT NULL DEFAULT 0,
    rate_limit_rpm INTEGER NOT NULL DEFAULT 0,
    allowed_endpoints TEXT,
    allowed_channels_msg TEXT,
    allowed_channels_resp TEXT,
    allowed_models TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);

-- Request logs table (main logging table)
CREATE TABLE IF NOT EXISTS request_logs (
    id TEXT PRIMARY KEY,
    status TEXT DEFAULT 'completed',
    initial_time DATETIME NOT NULL,
    complete_time DATETIME,
    duration_ms INTEGER DEFAULT 0,
    provider TEXT NOT NULL,
    provider_name TEXT,
    model TEXT NOT NULL,
    response_model TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cache_creation_input_tokens INTEGER DEFAULT 0,
    cache_read_input_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    price REAL DEFAULT 0,
    input_cost REAL DEFAULT 0,
    output_cost REAL DEFAULT 0,
    cache_creation_cost REAL DEFAULT 0,
    cache_read_cost REAL DEFAULT 0,
    http_status INTEGER DEFAULT 0,
    stream BOOLEAN NOT NULL,
    channel_id INTEGER,
    channel_name TEXT,
    endpoint TEXT,
    client_id TEXT,
    session_id TEXT,
    reasoning_effort TEXT,
    error TEXT,
    upstream_error TEXT,
    failover_info TEXT,
    api_key_id INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_logs_initial_time ON request_logs(initial_time);
CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status);
CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider);
CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model);
CREATE INDEX IF NOT EXISTS idx_request_logs_http_status ON request_logs(http_status);
CREATE INDEX IF NOT EXISTS idx_request_logs_endpoint ON request_logs(endpoint);
CREATE INDEX IF NOT EXISTS idx_request_logs_session_id ON request_logs(session_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_client_id ON request_logs(client_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_api_key_id ON request_logs(api_key_id);

-- Channel quota table (for Codex quota tracking)
CREATE TABLE IF NOT EXISTS channel_quota (
    channel_id INTEGER PRIMARY KEY,
    channel_name TEXT NOT NULL,
    plan_type TEXT,
    primary_used_percent INTEGER DEFAULT 0,
    primary_window_minutes INTEGER DEFAULT 0,
    primary_reset_at DATETIME,
    secondary_used_percent INTEGER DEFAULT 0,
    secondary_window_minutes INTEGER DEFAULT 0,
    secondary_reset_at DATETIME,
    credits_has_credits BOOLEAN DEFAULT 0,
    credits_unlimited BOOLEAN DEFAULT 0,
    credits_balance TEXT,
    is_exceeded BOOLEAN DEFAULT 0,
    exceeded_at DATETIME,
    recover_at DATETIME,
    exceeded_reason TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Channel suspensions table (for quota-exhausted channels)
CREATE TABLE IF NOT EXISTS channel_suspensions (
    channel_id INTEGER NOT NULL,
    channel_type TEXT NOT NULL,
    suspended_at DATETIME NOT NULL,
    suspended_until DATETIME NOT NULL,
    reason TEXT,
    PRIMARY KEY (channel_id, channel_type)
);
CREATE INDEX IF NOT EXISTS idx_suspensions_until ON channel_suspensions(suspended_until);

-- Request debug logs table (for full request/response data)
CREATE TABLE IF NOT EXISTS request_debug_logs (
    request_id TEXT PRIMARY KEY,
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    request_headers BLOB,
    request_body BLOB,
    request_body_size INTEGER DEFAULT 0,
    response_status INTEGER DEFAULT 0,
    response_headers BLOB,
    response_body BLOB,
    response_body_size INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (request_id) REFERENCES request_logs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_debug_logs_created ON request_debug_logs(created_at);
