package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/api/handler"
	"github.com/use-agent/purify/api/middleware"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/llm"
	"github.com/use-agent/purify/scraper"
)

// NewRouter creates a configured Gin engine with all routes and middleware.
//
// Middleware chain:
//
//	Global:  Recovery → Logger
//	/scrape: Auth (if enabled) → RateLimit
//
// Health endpoint is intentionally outside auth so monitoring probes always work.
func NewRouter(sc *scraper.Scraper, cl *cleaner.Cleaner, llmClient *llm.Client, cfg *config.Config, startTime time.Time) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	v1 := r.Group("/api/v1")

	// Health — no auth required.
	v1.GET("/health", handler.Health(sc, startTime))

	// Scrape — protected by auth + rate limit.
	scrapeGroup := v1.Group("")
	if cfg.Auth.Enabled {
		scrapeGroup.Use(middleware.Auth(cfg.Auth.APIKeys))
	}
	scrapeGroup.Use(middleware.RateLimit(cfg.RateLimit))
	scrapeGroup.POST("/scrape", handler.Scrape(sc, cl))
	scrapeGroup.POST("/extract", handler.Extract(sc, cl, llmClient))

	return r
}
