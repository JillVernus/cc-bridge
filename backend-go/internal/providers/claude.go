package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// ClaudeProvider Claude 提供商（直接透传）
type ClaudeProvider struct{}

func truncateForLog(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func remapClaudeRequestModel(bodyBytes []byte, upstream *config.UpstreamConfig) ([]byte, error) {
	if len(bodyBytes) == 0 || upstream == nil || len(upstream.ModelMapping) == 0 {
		return bodyBytes, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		// Keep passthrough behavior for non-JSON bodies.
		return bodyBytes, nil
	}

	rawModel, ok := payload["model"]
	if !ok {
		return bodyBytes, nil
	}

	var model string
	if err := json.Unmarshal(rawModel, &model); err != nil {
		return bodyBytes, nil
	}

	redirectedModel := config.RedirectModel(model, upstream)
	if redirectedModel == model {
		return bodyBytes, nil
	}
	log.Printf("🔀 [Model Mapping] channel=%q %q → %q",
		upstream.Name, truncateForLog(model, 50), truncateForLog(redirectedModel, 50))

	encodedModel, err := json.Marshal(redirectedModel)
	if err != nil {
		return nil, err
	}
	payload["model"] = encodedModel

	remappedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return remappedBody, nil
}

// ConvertToProviderRequest 转换为 Claude 请求（实现真正的透传）
func (p *ClaudeProvider) ConvertToProviderRequest(c *gin.Context, upstream *config.UpstreamConfig, apiKey string) (*http.Request, []byte, error) {
	var bodyBytes []byte
	var err error

	// 读取原始请求体用于日志与转发
	bodyBytes, err = io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes)) // 恢复body

	// 仅重写 model 字段，保留其余原始 payload 结构
	bodyBytes, err = remapClaudeRequestModel(bodyBytes, upstream)
	if err != nil {
		return nil, nil, err
	}

	// 构建目标URL
	// 智能拼接逻辑：
	// 1. 如果 baseURL 已包含版本号后缀（如 /v1, /v2, /v3），直接拼接端点路径
	// 2. 如果 baseURL 不包含版本号后缀，自动添加 /v1 再拼接端点路径
	endpoint := strings.TrimPrefix(c.Request.URL.Path, "/v1")
	baseURL := strings.TrimSuffix(upstream.BaseURL, "/")

	// 使用正则表达式检测 baseURL 是否以版本号结尾（/v1, /v2, /v1beta, /v2alpha等）
	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)

	var targetURL string
	if versionPattern.MatchString(baseURL) {
		// baseURL 已包含版本号，直接拼接
		targetURL = baseURL + endpoint
	} else {
		// baseURL 不包含版本号，添加 /v1
		targetURL = baseURL + "/v1" + endpoint
	}

	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	// 创建请求
	var req *http.Request
	if len(bodyBytes) > 0 {
		req, err = http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	} else {
		// 如果 bodyBytes 为空（例如 GET 请求或原始请求体为空），则直接使用 nil Body
		req, err = http.NewRequest(c.Request.Method, targetURL, nil)
	}
	if err != nil {
		return nil, nil, err
	}

	// 使用统一的头部处理逻辑
	req.Header = utils.PrepareUpstreamHeaders(c, req.URL.Host)
	utils.SetAuthenticationHeader(req.Header, apiKey)
	utils.EnsureCompatibleUserAgent(req.Header, "claude")

	return req, bodyBytes, nil
}

// ConvertToClaudeResponse 转换为 Claude 响应（直接透传）
func (p *ClaudeProvider) ConvertToClaudeResponse(providerResp *types.ProviderResponse) (*types.ClaudeResponse, error) {
	var claudeResp types.ClaudeResponse
	if err := json.Unmarshal(providerResp.Body, &claudeResp); err != nil {
		return nil, err
	}
	return &claudeResp, nil
}

// HandleStreamResponse 处理流式响应（直接透传）
func (p *ClaudeProvider) HandleStreamResponse(body io.ReadCloser) (<-chan string, <-chan error, error) {
	eventChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)
		defer body.Close()

		// Use Reader.ReadString('\n') instead of Scanner to avoid hard token limits.
		// Some upstream SSE lines (e.g., large signatures / tool args) can exceed scanner caps.
		reader := bufio.NewReaderSize(body, 64*1024)
		toolUseStopEmitted := false

		for {
			line, err := reader.ReadString('\n')
			if len(line) == 0 && err == io.EOF {
				break
			}

			trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
			outLine := ""

			switch {
			case strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:"):
				// Standard SSE lines (allow either "data:..." or "data: ...").
				outLine = trimmed
			case trimmed == "":
				// SSE event delimiter.
				outLine = ""
			case json.Valid([]byte(trimmed)):
				// Some upstreams occasionally emit bare JSON lines in stream mode
				// (missing "data:" prefix). Normalize to valid SSE data line.
				outLine = "data: " + trimmed
			default:
				// Ignore non-SSE/non-JSON noise lines to preserve passthrough safety.
				continue
			}

			eventChan <- outLine + "\n"

			// 检测是否发送了 tool_use 相关的 stop_reason
			if strings.Contains(outLine, `"stop_reason":"tool_use"`) ||
				strings.Contains(outLine, `"stop_reason": "tool_use"`) {
				toolUseStopEmitted = true
			}

			if err != nil {
				// 在 tool_use 场景下，客户端主动断开是正常行为
				// 如果已经发送了 tool_use stop 事件，并且错误是连接断开相关的，则忽略该错误
				errMsg := err.Error()
				if toolUseStopEmitted && (strings.Contains(errMsg, "broken pipe") ||
					strings.Contains(errMsg, "connection reset") ||
					strings.Contains(errMsg, "EOF")) {
					// 这是预期的客户端行为，不报告错误
					return
				}
				if err != io.EOF {
					errChan <- err
				}
				return
			}
		}
	}()

	return eventChan, errChan, nil
}

// OpenAIOldProvider 旧版 OpenAI 提供商
type OpenAIOldProvider struct {
	OpenAIProvider
}
