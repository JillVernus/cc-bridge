package requestlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
)

const (
	pgNotifyChannel = "request_log_events"
)

// NotificationPayload represents the data sent via PostgreSQL NOTIFY
type NotificationPayload struct {
	EventType string `json:"type"`   // "created" or "updated"
	LogID     string `json:"log_id"` // Request log ID
	Timestamp int64  `json:"ts"`     // Unix timestamp
}

// startPGListener starts listening for PostgreSQL NOTIFY events
// This enables cross-instance SSE broadcasting
func (m *Manager) startPGListener(ctx context.Context) error {
	if !m.isPostgres() {
		return nil // Only for PostgreSQL
	}

	// Get connection string from the database
	// We need a dedicated connection for LISTEN
	connStr, err := m.getConnectionString()
	if err != nil {
		return err
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("⚠️ PostgreSQL listener error: %v", err)
		}
	})

	if err := listener.Listen(pgNotifyChannel); err != nil {
		return err
	}

	log.Printf("📡 PostgreSQL LISTEN started on channel: %s", pgNotifyChannel)

	// Start listening goroutine
	go func() {
		defer listener.Close()

		for {
			select {
			case <-ctx.Done():
				log.Printf("📡 PostgreSQL LISTEN stopped")
				return

			case notification := <-listener.Notify:
				if notification == nil {
					continue
				}

				// Parse notification payload
				var payload NotificationPayload
				if err := json.Unmarshal([]byte(notification.Extra), &payload); err != nil {
					log.Printf("⚠️ Failed to parse notification: %v", err)
					continue
				}

				// Handle the notification
				m.handleNotification(&payload)

			case <-time.After(90 * time.Second):
				// Send periodic ping to keep connection alive
				if err := listener.Ping(); err != nil {
					log.Printf("⚠️ PostgreSQL listener ping failed: %v", err)
				}
			}
		}
	}()

	return nil
}

// handleNotification processes a notification from another instance
func (m *Manager) handleNotification(payload *NotificationPayload) {
	// Skip if no SSE clients connected (save resources)
	if m.broadcaster == nil || !m.broadcaster.HasClients() {
		return
	}

	// Small delay to ensure the transaction is fully committed and visible
	// This handles PostgreSQL's transaction isolation
	time.Sleep(10 * time.Millisecond)

	// Fetch the log record from database and broadcast to local SSE clients
	switch payload.EventType {
	case "created":
		// Fetch partial record for log:created event
		if record, err := m.getPartialRecordForSSE(payload.LogID); err == nil {
			m.broadcaster.Broadcast(NewLogCreatedEvent(record))
		} else {
			log.Printf("⚠️ Failed to fetch partial record for SSE: %v", err)
		}

	case "updated":
		// Fetch complete record for log:updated event
		if record, err := m.getCompleteRecordForSSE(payload.LogID); err == nil {
			m.broadcaster.Broadcast(NewLogUpdatedEvent(payload.LogID, record))
		} else {
			log.Printf("⚠️ Failed to fetch complete record for SSE: %v", err)
		}
	}
}

// notifyOtherInstances sends a NOTIFY to other instances via PostgreSQL
func (m *Manager) notifyOtherInstances(eventType string, logID string) {
	if !m.isPostgres() {
		return // Only for PostgreSQL
	}

	payload := NotificationPayload{
		EventType: eventType,
		LogID:     logID,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("⚠️ Failed to marshal notification: %v", err)
		return
	}

	// Send NOTIFY synchronously to ensure it happens after the transaction commits
	// Use a short timeout to avoid blocking
	query := "SELECT pg_notify($1, $2)"
	if _, err := m.db.Exec(query, pgNotifyChannel, string(data)); err != nil {
		log.Printf("⚠️ Failed to send NOTIFY: %v", err)
	}
}

// getConnectionString returns the PostgreSQL connection string
func (m *Manager) getConnectionString() (string, error) {
	if m.connStr == "" {
		return "", fmt.Errorf("connection string not provided")
	}
	return m.connStr, nil
}

// Close stops the PostgreSQL listener (if running)
func (m *Manager) StopListener() {
	if m.listenerStop != nil {
		m.listenerStop()
	}
}

// getPartialRecordForSSE fetches minimal fields for log:created event
func (m *Manager) getPartialRecordForSSE(id string) (*RequestLog, error) {
	query := m.convertQuery(`
		SELECT r.id, r.status, r.provider, r.provider_name, r.model, r.response_model,
		       r.duration_ms, r.http_status, r.input_tokens, r.output_tokens,
		       r.cache_creation_input_tokens, r.cache_read_input_tokens, r.total_tokens,
		       r.price, r.input_cost, r.output_cost, r.cache_creation_cost, r.cache_read_cost,
		       r.api_key_id, r.error, r.upstream_error, r.failover_info, r.complete_time,
		       r.channel_id, r.channel_uid, r.channel_name, r.endpoint, r.stream,
		       r.client_id, r.session_id, r.reasoning_effort, r.initial_time,
		       CASE WHEN d.request_id IS NOT NULL THEN 1 ELSE 0 END as has_debug_data
		FROM request_logs r
		LEFT JOIN request_debug_logs d ON r.id = d.request_id
		WHERE r.id = ?
	`)

	var r RequestLog
	var hasDebugData int
	var channelID, apiKeyID sql.NullInt64
	var completeTime sql.NullTime
	var providerName, model, responseModel sql.NullString
	var channelUID, channelName, endpoint, clientID, sessionID, reasoningEffort sql.NullString
	var errorStr, upstreamErrorStr, failoverInfoStr sql.NullString

	err := m.db.QueryRow(query, id).Scan(
		&r.ID, &r.Status, &r.Type, &providerName, &model, &responseModel,
		&r.DurationMs, &r.HTTPStatus, &r.InputTokens, &r.OutputTokens,
		&r.CacheCreationInputTokens, &r.CacheReadInputTokens, &r.TotalTokens,
		&r.Price, &r.InputCost, &r.OutputCost, &r.CacheCreationCost, &r.CacheReadCost,
		&apiKeyID, &errorStr, &upstreamErrorStr, &failoverInfoStr, &completeTime,
		&channelID, &channelUID, &channelName, &endpoint, &r.Stream,
		&clientID, &sessionID, &reasoningEffort, &r.InitialTime,
		&hasDebugData,
	)

	if err != nil {
		return nil, err
	}

	r.HasDebugData = hasDebugData == 1

	if providerName.Valid {
		r.ProviderName = providerName.String
	} else {
		r.ProviderName = r.Type
	}
	if model.Valid {
		r.Model = model.String
	}
	if responseModel.Valid {
		r.ResponseModel = responseModel.String
	}
	if completeTime.Valid {
		r.CompleteTime = completeTime.Time
	}
	if apiKeyID.Valid {
		value := apiKeyID.Int64
		r.APIKeyID = &value
	}
	if channelID.Valid {
		r.ChannelID = int(channelID.Int64)
	}
	if channelUID.Valid {
		r.ChannelUID = channelUID.String
	}
	if channelName.Valid {
		r.ChannelName = channelName.String
	}
	if endpoint.Valid {
		r.Endpoint = endpoint.String
	}
	if clientID.Valid {
		r.ClientID = clientID.String
	}
	if sessionID.Valid {
		r.SessionID = sessionID.String
	}
	if reasoningEffort.Valid {
		r.ReasoningEffort = reasoningEffort.String
	}
	if errorStr.Valid {
		r.Error = errorStr.String
	}
	if upstreamErrorStr.Valid {
		r.UpstreamError = upstreamErrorStr.String
	}
	if failoverInfoStr.Valid {
		r.FailoverInfo = failoverInfoStr.String
	}

	return &r, nil
}
