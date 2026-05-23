-- Migration 015: Add authoritative monotonic config revision metadata

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'general',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_settings_category ON settings(category);

INSERT INTO settings (key, value, category)
VALUES ('config_revision', '1', 'config_meta')
ON CONFLICT(key) DO NOTHING;
