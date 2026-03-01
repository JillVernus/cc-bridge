package types

// ============================================================================
// OpenAI Chat Completions API Types
// For /v1/chat/completions incoming endpoint
// ============================================================================

// ChatCompletionsRequest OpenAI Chat Completions 请求结构
type ChatCompletionsRequest struct {
	Model               string                   `json:"model"`
	Messages            []ChatCompletionsMessage `json:"messages"`
	Temperature         *float64                 `json:"temperature,omitempty"`
	TopP                *float64                 `json:"top_p,omitempty"`
	N                   int                      `json:"n,omitempty"`
	Stream              bool                     `json:"stream,omitempty"`
	StreamOptions       *StreamOptions           `json:"stream_options,omitempty"`
	MaxTokens           *int                     `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                     `json:"max_completion_tokens,omitempty"`
	FrequencyPenalty    *float64                 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64                 `json:"presence_penalty,omitempty"`
	Tools               []ChatCompletionsTool    `json:"tools,omitempty"`
	ToolChoice          interface{}              `json:"tool_choice,omitempty"` // string | object
	ParallelToolCalls   *bool                    `json:"parallel_tool_calls,omitempty"`
	ReasoningEffort     string                   `json:"reasoning_effort,omitempty"` // none, low, medium, high
	User                string                   `json:"user,omitempty"`
	Stop                interface{}              `json:"stop,omitempty"` // string | []string
}

// StreamOptions 流式选项
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatCompletionsMessage OpenAI Chat 消息
type ChatCompletionsMessage struct {
	Role       string                    `json:"role"`              // system, user, assistant, tool
	Content    interface{}               `json:"content,omitempty"` // string | null | []ContentPart
	Name       string                    `json:"name,omitempty"`
	ToolCalls  []ChatCompletionsToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                    `json:"tool_call_id,omitempty"` // For tool role
}

// ContentPart 多模态内容部分（仅支持文本）
type ContentPart struct {
	Type string `json:"type"` // text
	Text string `json:"text,omitempty"`
}

// ChatCompletionsTool OpenAI 工具定义
type ChatCompletionsTool struct {
	Type     string                      `json:"type"` // function
	Function ChatCompletionsToolFunction `json:"function"`
}

// ChatCompletionsToolFunction 工具函数定义
type ChatCompletionsToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"` // JSON Schema
	Strict      bool        `json:"strict,omitempty"`
}

// ChatCompletionsToolCall 工具调用
type ChatCompletionsToolCall struct {
	ID       string                          `json:"id"`
	Type     string                          `json:"type"` // function
	Function ChatCompletionsToolCallFunction `json:"function"`
	Index    *int                            `json:"index,omitempty"` // For streaming
}

// ChatCompletionsToolCallFunction 工具调用函数
type ChatCompletionsToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ============================================================================
// Response Types
// ============================================================================

// ChatCompletionsResponse 非流式响应
type ChatCompletionsResponse struct {
	ID                string                  `json:"id"`
	Object            string                  `json:"object"` // chat.completion
	Created           int64                   `json:"created"`
	Model             string                  `json:"model"`
	Choices           []ChatCompletionsChoice `json:"choices"`
	Usage             *ChatCompletionsUsage   `json:"usage,omitempty"`
	SystemFingerprint string                  `json:"system_fingerprint,omitempty"`
}

// ChatCompletionsChoice 响应选项
type ChatCompletionsChoice struct {
	Index        int                    `json:"index"`
	Message      ChatCompletionsMessage `json:"message"`
	FinishReason string                 `json:"finish_reason,omitempty"` // stop, length, content_filter, tool_calls
}

// ChatCompletionsUsage Token 使用统计
type ChatCompletionsUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ============================================================================
// Streaming Types
// ============================================================================

// ChatCompletionsChunk 流式响应块
type ChatCompletionsChunk struct {
	ID                string                       `json:"id"`
	Object            string                       `json:"object"` // chat.completion.chunk
	Created           int64                        `json:"created"`
	Model             string                       `json:"model"`
	Choices           []ChatCompletionsChunkChoice `json:"choices"`
	Usage             *ChatCompletionsUsage        `json:"usage,omitempty"` // Only in final chunk if include_usage=true
	SystemFingerprint string                       `json:"system_fingerprint,omitempty"`
}

// ChatCompletionsChunkChoice 流式响应选项
type ChatCompletionsChunkChoice struct {
	Index        int                       `json:"index"`
	Delta        ChatCompletionsChunkDelta `json:"delta"`
	FinishReason *string                   `json:"finish_reason"` // null until final chunk
}

// ChatCompletionsChunkDelta 流式增量内容
type ChatCompletionsChunkDelta struct {
	Role      string                    `json:"role,omitempty"`
	Content   *string                   `json:"content,omitempty"`
	ToolCalls []ChatCompletionsToolCall `json:"tool_calls,omitempty"`
}

// ============================================================================
// Tool Choice Types
// ============================================================================

// ToolChoiceFunction 指定工具选择
type ToolChoiceFunction struct {
	Type     string                   `json:"type"` // function
	Function ToolChoiceFunctionDetail `json:"function"`
}

// ToolChoiceFunctionDetail 工具选择详情
type ToolChoiceFunctionDetail struct {
	Name string `json:"name"`
}

// ============================================================================
// Helper Methods
// ============================================================================

// GetMaxTokens 获取最大 token 数（优先 max_completion_tokens）
func (r *ChatCompletionsRequest) GetMaxTokens() int {
	if r.MaxCompletionTokens != nil {
		return *r.MaxCompletionTokens
	}
	if r.MaxTokens != nil {
		return *r.MaxTokens
	}
	return 0
}

// GetTemperature 获取温度（默认 1.0）
func (r *ChatCompletionsRequest) GetTemperature() float64 {
	if r.Temperature != nil {
		return *r.Temperature
	}
	return 1.0
}

// GetTopP 获取 top_p（默认 1.0）
func (r *ChatCompletionsRequest) GetTopP() float64 {
	if r.TopP != nil {
		return *r.TopP
	}
	return 1.0
}

// ShouldIncludeUsage 检查是否应在流式响应中包含 usage
func (r *ChatCompletionsRequest) ShouldIncludeUsage() bool {
	return r.StreamOptions != nil && r.StreamOptions.IncludeUsage
}

// GetToolChoiceString 将 tool_choice 转换为字符串表示
// 返回: "none", "auto", "required", 或 function name
func (r *ChatCompletionsRequest) GetToolChoiceString() string {
	if r.ToolChoice == nil {
		return "auto"
	}

	// String value: "none", "auto", "required"
	if s, ok := r.ToolChoice.(string); ok {
		return s
	}

	// Object value: {"type": "function", "function": {"name": "..."}}
	if m, ok := r.ToolChoice.(map[string]interface{}); ok {
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return name
			}
		}
	}

	return "auto"
}
