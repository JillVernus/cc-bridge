package forwardproxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/utils"
)

// StreamParserWriter wraps StreamSynthesizer as an io.Writer for use with io.TeeReader.
// Raw SSE bytes are buffered into lines and fed to the synthesizer for metric extraction.
type StreamParserWriter struct {
	synthesizer *utils.StreamSynthesizer
	buf         bytes.Buffer
	messageID   string // extracted from message_start for log ID
}

// NewStreamParserWriter creates a parser that delegates to StreamSynthesizer.
func NewStreamParserWriter() *StreamParserWriter {
	return &StreamParserWriter{
		synthesizer: utils.NewStreamSynthesizer("claude"),
	}
}

// Write implements io.Writer. Bytes are buffered and parsed as SSE lines.
func (p *StreamParserWriter) Write(data []byte) (int, error) {
	n := len(data)
	p.buf.Write(data)
	p.parseBufferedLines()
	return n, nil
}

// GetUsage returns the collected usage metrics after the stream ends.
// Flushes any remaining buffered data before returning.
func (p *StreamParserWriter) GetUsage() *utils.StreamUsage {
	// Flush any remaining partial line
	if p.buf.Len() > 0 {
		remaining := strings.TrimRight(p.buf.String(), "\r\n")
		if remaining != "" {
			p.synthesizer.ProcessLine(remaining)
		}
		p.buf.Reset()
	}
	return p.synthesizer.GetUsage()
}

// GetMessageID returns the message ID extracted from the stream.
func (p *StreamParserWriter) GetMessageID() string {
	return p.messageID
}

func (p *StreamParserWriter) parseBufferedLines() {
	for {
		line, err := p.buf.ReadString('\n')
		if err != nil {
			// Incomplete line — put it back for the next Write() call
			if len(line) > 0 {
				var newBuf bytes.Buffer
				newBuf.WriteString(line)
				newBuf.ReadFrom(&p.buf)
				p.buf = newBuf
			}
			return
		}

		line = strings.TrimRight(line, "\r\n")

		// Extract message ID from data lines (StreamSynthesizer doesn't track this)
		if p.messageID == "" && strings.HasPrefix(line, "data: ") {
			p.extractMessageID(strings.TrimPrefix(line, "data: "))
		}

		// Delegate all SSE parsing to StreamSynthesizer
		p.synthesizer.ProcessLine(line)
	}
}

// extractMessageID extracts the message ID from a message_start event's data.
func (p *StreamParserWriter) extractMessageID(data string) {
	var envelope struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return
	}
	if envelope.Message.ID != "" {
		p.messageID = envelope.Message.ID
	}
}

// parseJSONResponse extracts metrics from a non-streaming Anthropic JSON response.
func parseJSONResponse(body []byte) (messageID string, usage *utils.StreamUsage) {
	usage = &utils.StreamUsage{}

	var resp struct {
		ID    string                 `json:"id"`
		Model string                 `json:"model"`
		Usage map[string]interface{} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", usage
	}

	messageID = resp.ID
	usage.Model = resp.Model

	if resp.Usage != nil {
		if v, ok := resp.Usage["input_tokens"].(float64); ok {
			usage.InputTokens = int(v)
		}
		if v, ok := resp.Usage["output_tokens"].(float64); ok {
			usage.OutputTokens = int(v)
		}
		if v, ok := resp.Usage["cache_creation_input_tokens"].(float64); ok {
			usage.CacheCreationInputTokens = int(v)
		}
		if v, ok := resp.Usage["cache_read_input_tokens"].(float64); ok {
			usage.CacheReadInputTokens = int(v)
		}
	}

	return messageID, usage
}

// createCompletionRecord creates a record with response metrics for updating the pending log.
func createCompletionRecord(usage *utils.StreamUsage, httpStatus int, startTime, endTime time.Time, hostOnly string) *requestlog.RequestLog {
	status := requestlog.StatusCompleted
	if httpStatus >= 400 {
		status = requestlog.StatusError
	}

	durationMs := endTime.Sub(startTime).Milliseconds()

	totalTokens := usage.InputTokens + usage.OutputTokens +
		usage.CacheCreationInputTokens + usage.CacheReadInputTokens

	// Calculate cost using pricing manager
	var totalCost float64
	var inputCost, outputCost, cacheCreationCost, cacheReadCost float64
	if pm := pricing.GetManager(); pm != nil {
		breakdown := pm.CalculateCostWithBreakdown(
			usage.Model,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheCreationInputTokens,
			usage.CacheReadInputTokens,
			nil, // no multipliers
		)
		totalCost = breakdown.TotalCost
		inputCost = breakdown.InputCost
		outputCost = breakdown.OutputCost
		cacheCreationCost = breakdown.CacheCreationCost
		cacheReadCost = breakdown.CacheReadCost
	}

	return &requestlog.RequestLog{
		Status:                   status,
		CompleteTime:             endTime,
		DurationMs:               durationMs,
		Type:                     "claude",
		ProviderName:             hostOnly,
		ResponseModel:            usage.Model,
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		TotalTokens:              totalTokens,
		Price:                    totalCost,
		InputCost:                inputCost,
		OutputCost:               outputCost,
		CacheCreationCost:        cacheCreationCost,
		CacheReadCost:            cacheReadCost,
		HTTPStatus:               httpStatus,
		ChannelUID:               "subscription:forward-proxy",
		ChannelName:              "Subscription (Forward Proxy)",
	}
}

// extractClientSession extracts clientID and sessionID from the Anthropic request body.
// Claude  Code sends a compound user_id in metadata: "user_<hash>_account__session_<uuid>"
func extractClientSession(body []byte) (clientID, sessionID string) {
	if len(body) == 0 {
		return "", ""
	}
	var req struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", ""
	}
	return parseClaudeCodeUserID(req.Metadata.UserID)
}

// extractRequestMeta extracts model and stream flag from the Anthropic request body.
func extractRequestMeta(body []byte) (model string, stream bool) {
	if len(body) == 0 {
		return "", false
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false
	}
	return req.Model, req.Stream
}

// createPendingLog creates a minimal pending request log entry for immediate UI visibility.
func createPendingLog(req *http.Request, startTime time.Time, hostOnly string, reqBody []byte) *requestlog.RequestLog {
	clientID, sessionID := extractClientSession(reqBody)
	model, stream := extractRequestMeta(reqBody)

	return &requestlog.RequestLog{
		Status:       requestlog.StatusPending,
		InitialTime:  startTime,
		Type:         "claude",
		ProviderName: hostOnly,
		Model:        model,
		Stream:       stream,
		Endpoint:     req.URL.Path,
		ChannelID:    0,
		ChannelUID:   "subscription:forward-proxy",
		ChannelName:  "Subscription (Forward Proxy)",
		ClientID:     clientID,
		SessionID:    sessionID,
		CreatedAt:    time.Now(),
	}
}

// parseClaudeCodeUserID splits the compound user_id format used by Claude  Code.
// Format: "user_<hash>_account__session_<session_uuid>"
func parseClaudeCodeUserID(compoundUserID string) (userID, sessionID string) {
	compoundUserID = strings.TrimSpace(compoundUserID)
	if compoundUserID == "" {
		return "", ""
	}
	const delimiter = "_account__session_"
	idx := strings.Index(compoundUserID, delimiter)
	if idx == -1 {
		return compoundUserID, ""
	}
	return strings.TrimSpace(compoundUserID[:idx]), strings.TrimSpace(compoundUserID[idx+len(delimiter):])
}
