package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/models"
	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimit returns per-identity (API key or IP) token-bucket rate limiting
// middleware powered by golang.org/x/time/rate.
//
// Entries unused for 1 hour are evicted by a background goroutine that runs
// every 5 minutes, preventing unbounded memory growth.
func RateLimit(cfg config.RateLimitConfig) gin.HandlerFunc {
	var mu sync.Mutex
	limiters := make(map[string]*limiterEntry)

	getLimiter := func(identity string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		entry, ok := limiters[identity]
		if !ok {
			entry = &limiterEntry{
				limiter: rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst),
			}
			limiters[identity] = entry
		}
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	// Background cleanup goroutine: evict entries not seen in the last hour.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-1 * time.Hour)
			mu.Lock()
			for id, entry := range limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(limiters, id)
				}
			}
			mu.Unlock()
		}
	}()

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
