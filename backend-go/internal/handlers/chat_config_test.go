package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
)

func TestGetChatChannelMetrics_ReturnsSharedMetricsShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		ChatUpstream: []config.UpstreamConfig{
			{
				ID:          "chat-primary",
				Name:        "Chat Primary",
				ServiceType: "openai",
				Status:      "active",
				APIKeys:     []string{"chat-key"},
			},
		},
	})

	sch := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		session.NewTraceAffinityManager(),
	)

	chatMetrics := sch.GetChatMetricsManager()
	chatMetrics.RecordSuccessWithStatusDetailByIdentity(0, "chat-primary", http.StatusOK, "gpt-4.1", "Chat Primary")

	r := gin.New()
	r.GET("/api/chat/channels/metrics", GetChatChannelMetrics(cfgManager, sch))

	req := httptest.NewRequest(http.MethodGet, "/api/chat/channels/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp []struct {
		ChannelIndex        int                    `json:"channelIndex"`
		RequestCount        int64                  `json:"requestCount"`
		SuccessCount        int64                  `json:"successCount"`
		FailureCount        int64                  `json:"failureCount"`
		ConsecutiveFailures int64                  `json:"consecutiveFailures"`
		SuccessRate         float64                `json:"successRate"`
		ErrorRate           float64                `json:"errorRate"`
		RecentCalls         []metrics.RecentCall   `json:"recentCalls"`
		TimeWindows         map[string]interface{} `json:"timeWindows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected array response, got decode error: %v body=%s", err, w.Body.String())
	}

	if len(resp) != 1 {
		t.Fatalf("expected 1 metrics item, got %d body=%s", len(resp), w.Body.String())
	}

	item := resp[0]
	if item.ChannelIndex != 0 {
		t.Fatalf("expected channelIndex 0, got %d", item.ChannelIndex)
	}
	if item.RequestCount != 1 || item.SuccessCount != 1 || item.FailureCount != 0 {
		t.Fatalf("unexpected counters: %+v", item)
	}
	if item.SuccessRate != 100 {
		t.Fatalf("expected successRate 100, got %v", item.SuccessRate)
	}
	if item.ErrorRate != 0 {
		t.Fatalf("expected errorRate 0, got %v", item.ErrorRate)
	}
	if len(item.RecentCalls) != 1 {
		t.Fatalf("expected 1 recent call, got %d", len(item.RecentCalls))
	}
	if item.RecentCalls[0].ChannelName != "Chat Primary" {
		t.Fatalf("expected recent call channel name Chat Primary, got %q", item.RecentCalls[0].ChannelName)
	}
	if item.TimeWindows == nil {
		t.Fatalf("expected time windows to be present")
	}
}
