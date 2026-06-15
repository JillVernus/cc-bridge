CREATE TABLE IF NOT EXISTS request_debug_logs (
    request_id TEXT PRIMARY KEY,
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    request_headers BLOB,
    request_removed_headers BLOB,
    request_modified_headers BLOB,
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

ALTER TABLE request_debug_logs
ADD COLUMN request_modified_headers BLOB;
