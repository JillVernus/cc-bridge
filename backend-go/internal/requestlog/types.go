package requestlog

import (
	"time"
)

// Request status constants
const (
	StatusPending   = "pending"   // Request received, waiting for response
	StatusCompleted = "completed" // Response received successfully
	StatusError     = "error"     // Request failed with error
	StatusTimeout   = "timeout"   // Request timed out (stale pending)
)

// RequestLog represents a single API request/response record
type RequestLog struct {
	ID                       string    `json:"id"`
	Status                   string    `json:"status"` // pending, completed, error, timeout
	InitialTime              time.Time `json:"initialTime"`
	CompleteTime             time.Time `json:"completeTime"`
	DurationMs               int64     `json:"durationMs"`
	Type                     string    `json:"type"`                      // claude, openai, gemini (service type)
	ProviderName             string    `json:"providerName"`              // actual provider/channel name
	Model                    string    `json:"model"`                     // 请求的模型名称
	ResponseModel            string    `json:"responseModel"`             // 响应中的模型名称（可能与请求不同）
	ReasoningEffort          string    `json:"reasoningEffort,omitempty"` // Codex reasoning effort (low/medium/high/xhigh)
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	CacheCreationInputTokens int       `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int       `json:"cacheReadInputTokens"`
	TotalTokens              int       `json:"totalTokens"`
	Price                    float64   `json:"price"`
	// 成本明细
	InputCost         float64 `json:"inputCost"`
	OutputCost        float64 `json:"outputCost"`
	CacheCreationCost float64 `json:"cacheCreationCost"`
	CacheReadCost     float64 `json:"cacheReadCost"`
	// 其他字段
	HTTPStatus    int       `json:"httpStatus"`
	Stream        bool      `json:"stream"`
	ChannelID     int       `json:"channelId"`
	ChannelName   string    `json:"channelName"`
	Endpoint      string    `json:"endpoint"` // /v1/messages or /v1/responses
	ClientID      string    `json:"clientId,omitempty"` // Client/machine identifier
	SessionID     string    `json:"sessionId,omitempty"` // Claude Code conversation session ID
	APIKeyID      *int64    `json:"apiKeyId"`  // API key ID for tracking (nil = not set, 0 = master key)
	Error         string    `json:"error,omitempty"`
	UpstreamError string    `json:"upstreamError,omitempty"` // 上游服务原始错误信息
	CreatedAt     time.Time `json:"createdAt"`
}

// UsageData represents normalized usage data across all providers
type UsageData struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens"`
	TotalTokens              int `json:"totalTokens"`
}

// RequestLogFilter represents filter options for querying request logs
type RequestLogFilter struct {
	Provider   string     `json:"provider,omitempty"`
	Model      string     `json:"model,omitempty"`
	HTTPStatus int        `json:"httpStatus,omitempty"`
	Endpoint   string     `json:"endpoint,omitempty"`
	ClientID   string     `json:"clientId,omitempty"`
	SessionID  string     `json:"sessionId,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	Offset     int        `json:"offset,omitempty"`
}

// RequestLogStats represents aggregated statistics
type RequestLogStats struct {
	TotalRequests int64                    `json:"totalRequests"`
	TotalTokens   UsageData                `json:"totalTokens"`
	TotalCost     float64                  `json:"totalCost"`
	ByProvider    map[string]ProviderStats `json:"byProvider"`
	ByModel       map[string]ModelStats    `json:"byModel"`
	ByClient      map[string]GroupStats    `json:"byClient"`
	BySession     map[string]GroupStats    `json:"bySession"`
	ByAPIKey      map[string]GroupStats    `json:"byApiKey"`
	TimeRange     TimeRange                `json:"timeRange"`
}

// GroupStats represents statistics for a generic group (user, session, etc.)
type GroupStats struct {
	Count                    int64   `json:"count"`
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	Cost                     float64 `json:"cost"`
}

// ProviderStats represents statistics for a single provider
type ProviderStats struct {
	Count                    int64   `json:"count"`
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	Cost                     float64 `json:"cost"`
}

// ModelStats represents statistics for a single model
type ModelStats struct {
	Count                    int64   `json:"count"`
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	Cost                     float64 `json:"cost"`
}

// TimeRange represents a time range for statistics
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// RequestLogListResponse represents the API response for listing request logs
type RequestLogListResponse struct {
	Requests []RequestLog `json:"requests"`
	Total    int64        `json:"total"`
	HasMore  bool         `json:"hasMore"`
}

// ActiveSession represents an active session with aggregated statistics
type ActiveSession struct {
	SessionID                string    `json:"sessionId"`
	Type                     string    `json:"type"` // claude, openai, codex, responses
	FirstRequestTime         time.Time `json:"firstRequestTime"`
	LastRequestTime          time.Time `json:"lastRequestTime"`
	Count                    int64     `json:"count"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	CacheCreationInputTokens int       `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int       `json:"cacheReadInputTokens"`
	Cost                     float64   `json:"cost"`
}

// StatsHistoryDataPoint represents a single data point in time-series stats
type StatsHistoryDataPoint struct {
	Timestamp                time.Time `json:"timestamp"`
	Requests                 int64     `json:"requests"`
	Success                  int64     `json:"success"`
	Failure                  int64     `json:"failure"`
	InputTokens              int64     `json:"inputTokens"`
	OutputTokens             int64     `json:"outputTokens"`
	CacheCreationInputTokens int64     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int64     `json:"cacheReadInputTokens"`
	Cost                     float64   `json:"cost"`
}

// StatsHistorySummary represents aggregated summary for the time range
type StatsHistorySummary struct {
	TotalRequests            int64   `json:"totalRequests"`
	TotalSuccess             int64   `json:"totalSuccess"`
	TotalFailure             int64   `json:"totalFailure"`
	TotalInputTokens         int64   `json:"totalInputTokens"`
	TotalOutputTokens        int64   `json:"totalOutputTokens"`
	TotalCacheCreationTokens int64   `json:"totalCacheCreationTokens"`
	TotalCacheReadTokens     int64   `json:"totalCacheReadTokens"`
	TotalCost                float64 `json:"totalCost"`
	AvgSuccessRate           float64 `json:"avgSuccessRate"`
	Duration                 string  `json:"duration"`
}

// StatsHistoryResponse represents the response for stats history API
type StatsHistoryResponse struct {
	DataPoints []StatsHistoryDataPoint `json:"dataPoints"`
	Summary    StatsHistorySummary     `json:"summary"`
}

// ChannelStatsHistoryResponse represents per-channel stats history
type ChannelStatsHistoryResponse struct {
	ChannelID   int                     `json:"channelId"`
	ChannelName string                  `json:"channelName"`
	DataPoints  []StatsHistoryDataPoint `json:"dataPoints"`
	Summary     StatsHistorySummary     `json:"summary"`
}
