package requestlog

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// DebugLogEntry represents full request/response data for debugging
type DebugLogEntry struct {
	RequestID        string            `json:"requestId"`
	RequestMethod    string            `json:"requestMethod"`
	RequestPath      string            `json:"requestPath"`
	RequestHeaders   map[string]string `json:"requestHeaders"`
	RequestBody      string            `json:"requestBody"`
	RequestBodySize  int               `json:"requestBodySize"`
	ResponseStatus   int               `json:"responseStatus"`
	ResponseHeaders  map[string]string `json:"responseHeaders"`
	ResponseBody     string            `json:"responseBody"`
	ResponseBodySize int               `json:"responseBodySize"`
	CreatedAt        time.Time         `json:"createdAt"`
}

// sensitiveHeaders are headers that should be masked in debug logs
var sensitiveHeaders = []string{
	"authorization",
	"x-api-key",
	"cookie",
	"set-cookie",
	"proxy-authorization",
}

// compressData compresses data using gzip
func compressData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, fmt.Errorf("gzip write failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("gzip close failed: %w", err)
	}
	return buf.Bytes(), nil
}

// decompressData decompresses gzip data
func decompressData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader failed: %w", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("gzip read failed: %w", err)
	}
	return result, nil
}

// maskSensitiveHeaders masks sensitive header values
func maskSensitiveHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		lowerKey := strings.ToLower(k)
		isSensitive := false
		for _, sensitive := range sensitiveHeaders {
			if lowerKey == sensitive {
				isSensitive = true
				break
			}
		}
		if isSensitive && len(v) > 12 {
			// Show first 8 and last 4 characters
			result[k] = v[:8] + "..." + v[len(v)-4:]
		} else if isSensitive {
			result[k] = "***"
		} else {
			result[k] = v
		}
	}
	return result
}

// HttpHeadersToMap converts http.Header to map[string]string
func HttpHeadersToMap(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			result[k] = strings.Join(v, ", ")
		}
	}
	return result
}

// truncateBody truncates body if it exceeds maxSize
func truncateBody(body []byte, maxSize int) []byte {
	if maxSize <= 0 || len(body) <= maxSize {
		return body
	}
	truncated := body[:maxSize]
	return append(truncated, []byte("\n... [truncated]")...)
}

// AddDebugLog stores compressed request/response data
func (m *Manager) AddDebugLog(entry *DebugLogEntry) error {
	if entry == nil || entry.RequestID == "" {
		return fmt.Errorf("invalid debug log entry")
	}

	// Mask sensitive headers
	maskedReqHeaders := maskSensitiveHeaders(entry.RequestHeaders)
	maskedRespHeaders := maskSensitiveHeaders(entry.ResponseHeaders)

	// Serialize headers to JSON
	reqHeadersJSON, err := json.Marshal(maskedReqHeaders)
	if err != nil {
		return fmt.Errorf("failed to marshal request headers: %w", err)
	}
	respHeadersJSON, err := json.Marshal(maskedRespHeaders)
	if err != nil {
		return fmt.Errorf("failed to marshal response headers: %w", err)
	}

	// Compress data
	compReqHeaders, err := compressData(reqHeadersJSON)
	if err != nil {
		return fmt.Errorf("failed to compress request headers: %w", err)
	}
	compReqBody, err := compressData([]byte(entry.RequestBody))
	if err != nil {
		return fmt.Errorf("failed to compress request body: %w", err)
	}
	compRespHeaders, err := compressData(respHeadersJSON)
	if err != nil {
		return fmt.Errorf("failed to compress response headers: %w", err)
	}
	compRespBody, err := compressData([]byte(entry.ResponseBody))
	if err != nil {
		return fmt.Errorf("failed to compress response body: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	query := `
	INSERT OR REPLACE INTO request_debug_logs (
		request_id, request_method, request_path,
		request_headers, request_body, request_body_size,
		response_status, response_headers, response_body, response_body_size,
		created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = m.db.Exec(query,
		entry.RequestID,
		entry.RequestMethod,
		entry.RequestPath,
		compReqHeaders,
		compReqBody,
		entry.RequestBodySize,
		entry.ResponseStatus,
		compRespHeaders,
		compRespBody,
		entry.ResponseBodySize,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert debug log: %w", err)
	}

	// Broadcast debug data available event (skip if no clients to save memory)
	if m.broadcaster != nil && m.broadcaster.HasClients() {
		m.broadcaster.Broadcast(NewLogDebugDataEvent(entry.RequestID))
	}

	return nil
}

// GetDebugLog retrieves and decompresses debug data
func (m *Manager) GetDebugLog(requestID string) (*DebugLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `
	SELECT request_id, request_method, request_path,
		   request_headers, request_body, request_body_size,
		   response_status, response_headers, response_body, response_body_size,
		   created_at
	FROM request_debug_logs
	WHERE request_id = ?
	`

	var entry DebugLogEntry
	var reqHeadersBlob, reqBodyBlob, respHeadersBlob, respBodyBlob []byte

	err := m.db.QueryRow(query, requestID).Scan(
		&entry.RequestID,
		&entry.RequestMethod,
		&entry.RequestPath,
		&reqHeadersBlob,
		&reqBodyBlob,
		&entry.RequestBodySize,
		&entry.ResponseStatus,
		&respHeadersBlob,
		&respBodyBlob,
		&entry.ResponseBodySize,
		&entry.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query debug log: %w", err)
	}

	// Decompress and unmarshal headers
	if reqHeadersData, err := decompressData(reqHeadersBlob); err == nil && len(reqHeadersData) > 0 {
		json.Unmarshal(reqHeadersData, &entry.RequestHeaders)
	}
	if respHeadersData, err := decompressData(respHeadersBlob); err == nil && len(respHeadersData) > 0 {
		json.Unmarshal(respHeadersData, &entry.ResponseHeaders)
	}

	// Decompress bodies
	if reqBodyData, err := decompressData(reqBodyBlob); err == nil {
		entry.RequestBody = string(reqBodyData)
	}
	if respBodyData, err := decompressData(respBodyBlob); err == nil {
		entry.ResponseBody = string(respBodyData)
	}

	return &entry, nil
}

// HasDebugLog checks if debug data exists for a request
func (m *Manager) HasDebugLog(requestID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var count int
	err := m.db.QueryRow(`SELECT COUNT(*) FROM request_debug_logs WHERE request_id = ?`, requestID).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// PurgeExpiredDebugLogs removes logs older than retention period
func (m *Manager) PurgeExpiredDebugLogs(retentionHours int) (int64, error) {
	if retentionHours <= 0 {
		retentionHours = 24
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour)
	result, err := m.db.Exec(`DELETE FROM request_debug_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to purge expired debug logs: %w", err)
	}

	return result.RowsAffected()
}

// PurgeAllDebugLogs removes all debug logs
func (m *Manager) PurgeAllDebugLogs() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.Exec(`DELETE FROM request_debug_logs`)
	if err != nil {
		return 0, fmt.Errorf("failed to purge all debug logs: %w", err)
	}

	return result.RowsAffected()
}

// GetDebugLogCount returns the number of debug log entries
func (m *Manager) GetDebugLogCount() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var count int64
	err := m.db.QueryRow(`SELECT COUNT(*) FROM request_debug_logs`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// StartDebugLogCleanup starts a background goroutine to periodically clean up expired debug logs
func (m *Manager) StartDebugLogCleanup(getRetentionHours func() int) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			retentionHours := getRetentionHours()
			if retentionHours > 0 {
				deleted, err := m.PurgeExpiredDebugLogs(retentionHours)
				if err != nil {
					log.Printf("⚠️ Failed to purge expired debug logs: %v", err)
				} else if deleted > 0 {
					log.Printf("🧹 Purged %d expired debug logs (retention: %d hours)", deleted, retentionHours)
				}
			}
		}
	}()
}

// CreateDebugLogEntry is a helper to create a DebugLogEntry from HTTP request/response
func CreateDebugLogEntry(
	requestID string,
	req *http.Request,
	reqBody []byte,
	respStatus int,
	respHeaders http.Header,
	respBody []byte,
	maxBodySize int,
) *DebugLogEntry {
	entry := &DebugLogEntry{
		RequestID:        requestID,
		RequestMethod:    req.Method,
		RequestPath:      req.URL.Path,
		RequestHeaders:   HttpHeadersToMap(req.Header),
		RequestBodySize:  len(reqBody),
		ResponseStatus:   respStatus,
		ResponseHeaders:  HttpHeadersToMap(respHeaders),
		ResponseBodySize: len(respBody),
	}

	// Truncate bodies if needed
	if maxBodySize > 0 {
		entry.RequestBody = string(truncateBody(reqBody, maxBodySize))
		entry.ResponseBody = string(truncateBody(respBody, maxBodySize))
	} else {
		entry.RequestBody = string(reqBody)
		entry.ResponseBody = string(respBody)
	}

	return entry
}
