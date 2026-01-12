package requestlog

import (
	"encoding/json"
	"time"
)

// Event type constants
const (
	EventLogCreated   = "log:created"
	EventLogUpdated   = "log:updated"
	EventLogDebugData = "log:debugdata"
	EventLogStats     = "log:stats"
	EventHeartbeat    = "heartbeat"
)

// LogEvent represents an SSE event to be sent to clients
type LogEvent struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// LogCreatedPayload contains data for log:created events
type LogCreatedPayload struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	ProviderName string    `json:"providerName"`
	Model        string    `json:"model"`
	ChannelID    int       `json:"channelId"`
	ChannelName  string    `json:"channelName"`
	Endpoint     string    `json:"endpoint"`
	Stream       bool      `json:"stream"`
	ClientID     string    `json:"clientId,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	InitialTime  time.Time `json:"initialTime"`
}

// LogUpdatedPayload contains data for log:updated events
type LogUpdatedPayload struct {
	ID                       string    `json:"id"`
	Status                   string    `json:"status"`
	DurationMs               int64     `json:"durationMs"`
	HTTPStatus               int       `json:"httpStatus"`
	Type                     string    `json:"type"`                   // claude, openai, gemini
	ProviderName             string    `json:"providerName"`           // channel name
	ChannelID                int       `json:"channelId"`
	ChannelName              string    `json:"channelName"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	CacheCreationInputTokens int       `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int       `json:"cacheReadInputTokens"`
	TotalTokens              int       `json:"totalTokens"`
	Price                    float64   `json:"price"`
	// Cost breakdown
	InputCost         float64 `json:"inputCost"`
	OutputCost        float64 `json:"outputCost"`
	CacheCreationCost float64 `json:"cacheCreationCost"`
	CacheReadCost     float64 `json:"cacheReadCost"`
	// Other fields
	APIKeyID      *int64    `json:"apiKeyId"`
	HasDebugData  bool      `json:"hasDebugData"`
	Error         string    `json:"error,omitempty"`
	UpstreamError string    `json:"upstreamError,omitempty"`
	FailoverInfo  string    `json:"failoverInfo,omitempty"`
	ResponseModel string    `json:"responseModel,omitempty"`
	CompleteTime  time.Time `json:"completeTime"`
}

// StatsPayload contains data for log:stats events
type StatsPayload struct {
	TotalRequests  int64                    `json:"totalRequests"`
	TotalCost      float64                  `json:"totalCost"`
	ActiveSessions []ActiveSession          `json:"activeSessions"`
	ByProvider     map[string]ProviderStats `json:"byProvider,omitempty"`
}

// NewLogCreatedEvent creates an event for a new request log
func NewLogCreatedEvent(record *RequestLog) *LogEvent {
	return &LogEvent{
		Type: EventLogCreated,
		Data: LogCreatedPayload{
			ID:           record.ID,
			Status:       record.Status,
			ProviderName: record.ProviderName,
			Model:        record.Model,
			ChannelID:    record.ChannelID,
			ChannelName:  record.ChannelName,
			Endpoint:     record.Endpoint,
			Stream:       record.Stream,
			ClientID:     record.ClientID,
			SessionID:    record.SessionID,
			InitialTime:  record.InitialTime,
		},
		Timestamp: time.Now(),
	}
}

// NewLogUpdatedEvent creates an event for an updated request log
func NewLogUpdatedEvent(id string, record *RequestLog) *LogEvent {
	return &LogEvent{
		Type: EventLogUpdated,
		Data: LogUpdatedPayload{
			ID:                       id,
			Status:                   record.Status,
			DurationMs:               record.DurationMs,
			HTTPStatus:               record.HTTPStatus,
			Type:                     record.Type,
			ProviderName:             record.ProviderName,
			ChannelID:                record.ChannelID,
			ChannelName:              record.ChannelName,
			InputTokens:              record.InputTokens,
			OutputTokens:             record.OutputTokens,
			CacheCreationInputTokens: record.CacheCreationInputTokens,
			CacheReadInputTokens:     record.CacheReadInputTokens,
			TotalTokens:              record.TotalTokens,
			Price:                    record.Price,
			InputCost:                record.InputCost,
			OutputCost:               record.OutputCost,
			CacheCreationCost:        record.CacheCreationCost,
			CacheReadCost:            record.CacheReadCost,
			APIKeyID:                 record.APIKeyID,
			HasDebugData:             record.HasDebugData,
			Error:                    record.Error,
			UpstreamError:            record.UpstreamError,
			FailoverInfo:             record.FailoverInfo,
			ResponseModel:            record.ResponseModel,
			CompleteTime:             record.CompleteTime,
		},
		Timestamp: time.Now(),
	}
}

// NewStatsEvent creates an event for periodic stats update
func NewStatsEvent(totalRequests int64, totalCost float64, activeSessions []ActiveSession, byProvider map[string]ProviderStats) *LogEvent {
	return &LogEvent{
		Type: EventLogStats,
		Data: StatsPayload{
			TotalRequests:  totalRequests,
			TotalCost:      totalCost,
			ActiveSessions: activeSessions,
			ByProvider:     byProvider,
		},
		Timestamp: time.Now(),
	}
}

// NewHeartbeatEvent creates a heartbeat event
func NewHeartbeatEvent() *LogEvent {
	return &LogEvent{
		Type:      EventHeartbeat,
		Data:      nil,
		Timestamp: time.Now(),
	}
}

// LogDebugDataPayload contains data for log:debugdata events
type LogDebugDataPayload struct {
	ID           string `json:"id"`
	HasDebugData bool   `json:"hasDebugData"`
}

// NewLogDebugDataEvent creates an event to notify that debug data is available
func NewLogDebugDataEvent(id string) *LogEvent {
	return &LogEvent{
		Type: EventLogDebugData,
		Data: LogDebugDataPayload{
			ID:           id,
			HasDebugData: true,
		},
		Timestamp: time.Now(),
	}
}

// ToSSE formats the event as an SSE message
func (e *LogEvent) ToSSE() (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return "event: " + e.Type + "\ndata: " + string(data) + "\n\n", nil
}
