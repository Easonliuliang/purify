package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// Health returns a handler for GET /api/v1/health.
//
// Reports pool utilisation and degrades status when > 80% of pages are active.
func Health(sc *scraper.Scraper, startTime time.Time) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats := sc.Stats()

		status := "healthy"
		if stats.MaxPages > 0 && stats.ActivePages > int(float64(stats.MaxPages)*0.8) {
			status = "degraded"
		}

		c.JSON(http.StatusOK, models.HealthResponse{
			Status:    status,
			Uptime:    time.Since(startTime).Round(time.Second).String(),
			PoolStats: stats,
			Version:   "0.1.0",
		})
	}
}
