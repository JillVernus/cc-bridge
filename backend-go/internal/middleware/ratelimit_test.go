package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

func TestRateLimiter_ZeroLimitBehavesAsDisabled(t *testing.T) {
	rl := NewRateLimiterWithConfig(ratelimit.EndpointRateLimit{
		Enabled:           true,
		RequestsPerMinute: 0,
	})
	defer rl.Stop()

	clientKey := "ip:127.0.0.1"
	for i := 0; i < 10; i++ {
		if !rl.Allow(clientKey) {
			t.Fatalf("Allow() = false, want true (iteration %d)", i)
		}
	}

	info := rl.CheckWithCustomLimit(clientKey, 0)
	if !info.Allowed {
		t.Fatalf("CheckWithCustomLimit().Allowed = false, want true")
	}
	if info.Limit != 0 {
		t.Fatalf("CheckWithCustomLimit().Limit = %d, want 0", info.Limit)
	}
}

func TestRateLimiter_CheckWithCustomLimit_UsesCustomRPM(t *testing.T) {
	rl := NewRateLimiterWithConfig(ratelimit.EndpointRateLimit{
		Enabled:           true,
		RequestsPerMinute: 10,
	})
	defer rl.Stop()

	clientKey := "key:test"

	info := rl.CheckWithCustomLimit(clientKey, 2)
	if !info.Allowed || info.Limit != 2 || info.Remaining != 1 {
		t.Fatalf("first call = %+v, want Allowed=true Limit=2 Remaining=1", info)
	}

	info = rl.CheckWithCustomLimit(clientKey, 2)
	if !info.Allowed || info.Remaining != 0 {
		t.Fatalf("second call = %+v, want Allowed=true Remaining=0", info)
	}

	info = rl.CheckWithCustomLimit(clientKey, 2)
	if info.Allowed {
		t.Fatalf("third call Allowed=true, want false (limit exceeded)")
	}
}

func TestAPIRateLimitMiddleware_UsesContextCustomRPMAndSetsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rl := NewRateLimiterWithConfig(ratelimit.EndpointRateLimit{
		Enabled:           true,
		RequestsPerMinute: 100,
	})
	defer rl.Stop()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(ContextKeyAPIKeyName, "k1")
		c.Set(ContextKeyRateLimitRPM, 1)
	})
	router.Use(APIRateLimitMiddleware(rl))
	router.GET("/v1/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("X-RateLimit-Limit"); got != "1" {
		t.Fatalf("X-RateLimit-Limit = %q, want %q", got, "1")
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Fatalf("X-RateLimit-Remaining = %q, want %q", got, "0")
	}
	if got := w.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Fatalf("X-RateLimit-Reset is empty, want non-empty")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w2.Code, http.StatusTooManyRequests)
	}
	if got := w2.Header().Get("Retry-After"); got == "" {
		t.Fatalf("Retry-After is empty, want non-empty")
	}
}
