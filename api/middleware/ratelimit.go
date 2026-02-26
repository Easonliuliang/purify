package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/models"
	"golang.org/x/time/rate"
)

// RateLimit returns per-identity (API key or IP) token-bucket rate limiting
// middleware powered by golang.org/x/time/rate.
//
// For MVP we store limiters in a map guarded by sync.Mutex.
// TODO: add TTL-based eviction to prevent unbounded memory growth from
// many unique IPs in a production deployment.
func RateLimit(cfg config.RateLimitConfig) gin.HandlerFunc {
	var mu sync.Mutex
	limiters := make(map[string]*rate.Limiter)

	getLimiter := func(identity string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		if lim, ok := limiters[identity]; ok {
			return lim
		}
		lim := rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst)
		limiters[identity] = lim
		return lim
	}

	return func(c *gin.Context) {
		// Prefer API key as identity (set by auth middleware); fall back to IP.
		identity, exists := c.Get("api_key")
		if !exists {
			identity = c.ClientIP()
		}

		limiter := getLimiter(identity.(string))
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, models.ScrapeResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeRateLimited,
					Message: "rate limit exceeded, please slow down",
				},
			})
			return
		}

		c.Next()
	}
}
