-- Migration 001: Initial schema for configuration storage
-- This creates the base tables needed for JSON-to-DB migration

-- Settings table (key-value store for simple configs)
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'general',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_settings_category ON settings(category);

-- Channels table (unified for Messages and Responses API)
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id TEXT NOT NULL UNIQUE,
    channel_type TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    website TEXT,
    service_type TEXT NOT NULL,
    base_url TEXT,
    insecure_skip_verify BOOLEAN DEFAULT 0,
    status TEXT DEFAULT 'active',
    priority INTEGER DEFAULT 0,
    promotion_until DATETIME,
    response_header_timeout INTEGER DEFAULT 120,
    quota_type TEXT,
    quota_limit REAL DEFAULT 0,
    quota_reset_at DATETIME,
    quota_reset_interval INTEGER DEFAULT 0,
    quota_reset_unit TEXT,
    quota_reset_mode TEXT DEFAULT 'fixed',
    rate_limit_rpm INTEGER DEFAULT 0,
    queue_enabled BOOLEAN DEFAULT 0,
    queue_timeout INTEGER DEFAULT 60,
    key_load_balance TEXT,
    api_keys TEXT,
    model_mapping TEXT,
    price_multipliers TEXT,
    oauth_tokens TEXT,
    quota_models TEXT,
    composite_mappings TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(channel_type);
CREATE INDEX IF NOT EXISTS idx_channels_status ON channels(status);
CREATE INDEX IF NOT EXISTS idx_channels_channel_id ON channels(channel_id);

-- Model pricing table
CREATE TABLE IF NOT EXISTS model_pricing (
    model_id TEXT PRIMARY KEY,
    input_price REAL NOT NULL,
    output_price REAL NOT NULL,
    cache_creation_price REAL,
    cache_read_price REAL,
    description TEXT,
    export_to_models BOOLEAN DEFAULT 1,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Model aliases table
CREATE TABLE IF NOT EXISTS model_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias_type TEXT NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    sort_order INTEGER DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_model_aliases_type ON model_aliases(alias_type);

-- Channel usage table (replaces quota_usage.json)
CREATE TABLE IF NOT EXISTS channel_usage (
    channel_id INTEGER NOT NULL,
    channel_type TEXT NOT NULL,
    used REAL DEFAULT 0,
    last_reset_at DATETIME,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, channel_type)
);
