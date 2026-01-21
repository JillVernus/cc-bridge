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
		SELECT id, status, provider, model, channel_id, channel_name,
		       endpoint, stream, client_id, session_id, initial_time
		FROM request_logs
		WHERE id = ?
	`)

	var r RequestLog
	var channelID sql.NullInt64
	var channelName, endpoint, clientID, sessionID sql.NullString

	err := m.db.QueryRow(query, id).Scan(
		&r.ID, &r.Status, &r.ProviderName, &r.Model, &channelID, &channelName,
		&endpoint, &r.Stream, &clientID, &sessionID, &r.InitialTime,
	)

	if err != nil {
		return nil, err
	}

	if channelID.Valid {
		r.ChannelID = int(channelID.Int64)
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

	return &r, nil
}
