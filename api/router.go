package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/api/handler"
	"github.com/use-agent/purify/api/middleware"
	"github.com/use-agent/purify/cache"
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
//	API:     Auth (if enabled) → RateLimit
//
// Health endpoint is intentionally outside auth so monitoring probes always work.
func NewRouter(sc *scraper.Scraper, cl *cleaner.Cleaner, llmClient *llm.Client, cfg *config.Config, cc *cache.Cache, startTime time.Time) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	v1 := r.Group("/api/v1")

	// Health — no auth required.
	v1.GET("/health", handler.Health(sc, startTime))

	// Protected group — auth + rate limit.
	protected := v1.Group("")
	if cfg.Auth.Enabled {
		protected.Use(middleware.Auth(cfg.Auth.APIKeys))
	}
	protected.Use(middleware.RateLimit(cfg.RateLimit))

	// Scrape
	protected.POST("/scrape", handler.Scrape(sc, cl, cc))

	// Extract (structured extraction via LLM)
	protected.POST("/extract", handler.Extract(sc, cl, llmClient))

	// Batch
	protected.POST("/batch/scrape", handler.PostBatch(sc, cl))
	protected.GET("/batch/:id", handler.GetBatch())

	// Crawl
	protected.POST("/crawl", handler.PostCrawl(sc, cl))
	protected.GET("/crawl/:id", handler.GetCrawl())

	// Map
	protected.POST("/map", handler.PostMap(sc, cl))

	return r
}
