package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/cache"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// Scrape returns a handler for POST /api/v1/scrape.
//
// Orchestration flow:
//  1. Parse & validate request, apply defaults.
//  2. Scraper.DoScrape → raw HTML + JS title   (records navigation_ms)
//  3. Cleaner.Clean    → Markdown/HTML/text     (records cleaning_ms)
//  4. Merge metadata (readability title → JS title fallback).
//  5. Fill Timing, return 200.
func Scrape(sc *scraper.Scraper, cl *cleaner.Cleaner, cc *cache.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		totalStart := time.Now()

		// ── 1. Parse request ────────────────────────────────────────
		var req models.ScrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ScrapeResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: err.Error(),
				},
			})
			return
		}
		req.Defaults()

		// SSE mode: stream progress events instead of JSON response.
		if c.GetHeader("Accept") == "text/event-stream" {
			handleScrapeSSE(c, sc, cl, cc, &req)
			return
		}

		// ── 1b. Cache lookup ───────────────────────────────────────
		if cc != nil && req.MaxAge > 0 {
			cacheKey := cache.Key(req.URL, req.OutputFormat, req.ExtractMode)
			if cached, hit := cc.Get(cacheKey, req.MaxAge); hit {
				cached.CacheStatus = "hit"
				cached.Timing = models.TimingInfo{
					TotalMs: time.Since(totalStart).Milliseconds(),
				}
				c.JSON(http.StatusOK, cached)
				return
			}
		}

		// ── 2. Scrape ───────────────────────────────────────────────
		navStart := time.Now()
		result, err := sc.DoScrape(c.Request.Context(), &req)
		navigationMs := time.Since(navStart).Milliseconds()

		if err != nil {
			respondError(c, err, models.TimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
			})
			return
		}

		// ── 3. Clean ────────────────────────────────────────────────
		cleanStart := time.Now()
		var cleanOpts []cleaner.CleanOptions
		if len(req.IncludeTags) > 0 || len(req.ExcludeTags) > 0 || req.CSSSelector != "" {
			cleanOpts = append(cleanOpts, cleaner.CleanOptions{
				IncludeTags: req.IncludeTags,
				ExcludeTags: req.ExcludeTags,
				CSSSelector: req.CSSSelector,
			})
		}
		resp, err := cl.Clean(result.RawHTML, req.URL, req.OutputFormat, req.ExtractMode, cleanOpts...)
		cleaningMs := time.Since(cleanStart).Milliseconds()

		if err != nil {
			respondError(c, err, models.TimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
				CleaningMs:   cleaningMs,
			})
			return
		}

		// ── 4. Title fallback ───────────────────────────────────────
		if resp.Metadata.Title == "" {
			resp.Metadata.Title = result.Title
		}
		resp.Metadata.FetchMethod = result.FetchMethod

		// ── 5. Fill scrape result fields + timing and respond ───────
		resp.StatusCode = result.StatusCode
		resp.FinalURL = result.FinalURL
		resp.EngineUsed = result.EngineUsed
		resp.Timing = models.TimingInfo{
			TotalMs:      time.Since(totalStart).Milliseconds(),
			NavigationMs: navigationMs,
			CleaningMs:   cleaningMs,
		}

		// ── 6. Cache store ──────────────────────────────────────────
		if cc != nil && req.MaxAge > 0 {
			cacheKey := cache.Key(req.URL, req.OutputFormat, req.ExtractMode)
			cc.Set(cacheKey, resp)
			resp.CacheStatus = "miss"
		}

		c.JSON(http.StatusOK, resp)
	}
}

// respondError maps a ScrapeError to the correct HTTP status code and writes
// a structured JSON error response.
func respondError(c *gin.Context, err error, timing models.TimingInfo) {
	scrapeErr, ok := err.(*models.ScrapeError)
	if !ok {
		scrapeErr = models.NewScrapeError(models.ErrCodeInternal, err.Error(), err)
	}

	c.JSON(mapErrorToStatus(scrapeErr), models.ScrapeResponse{
		Success: false,
		Error:   scrapeErr.ToDetail(),
		Timing:  timing,
	})
}

// mapErrorToStatus translates error codes to HTTP status codes.
func mapErrorToStatus(e *models.ScrapeError) int {
	switch e.Code {
	case models.ErrCodeTimeout:
		return http.StatusGatewayTimeout // 504
	case models.ErrCodeNavigation:
		return http.StatusBadGateway // 502
	case models.ErrCodeInvalidInput:
		return http.StatusBadRequest // 400
	case models.ErrCodeRateLimited:
		return http.StatusTooManyRequests // 429
	case models.ErrCodeUnauthorized:
		return http.StatusUnauthorized // 401
	default:
		return http.StatusInternalServerError // 500
	}
}

// handleScrapeSSE processes a scrape request and streams SSE events.
func handleScrapeSSE(c *gin.Context, sc *scraper.Scraper, cl *cleaner.Cleaner, cc *cache.Cache, req *models.ScrapeRequest) {
	totalStart := time.Now()

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 1. Send started event.
	writeSSE(c, "scrape.started", map[string]interface{}{
		"url": req.URL,
	})

	// 2. Cache lookup.
	if cc != nil && req.MaxAge > 0 {
		cacheKey := cache.Key(req.URL, req.OutputFormat, req.ExtractMode)
		if cached, hit := cc.Get(cacheKey, req.MaxAge); hit {
			cached.CacheStatus = "hit"
			cached.Timing = models.TimingInfo{
				TotalMs: time.Since(totalStart).Milliseconds(),
			}
			writeSSE(c, "scrape.completed", cached)
			return
		}
	}

	// 3. Scrape.
	navStart := time.Now()
	result, err := sc.DoScrape(c.Request.Context(), req)
	navigationMs := time.Since(navStart).Milliseconds()

	if err != nil {
		writeSSE(c, "scrape.error", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// 4. Send navigated event.
	writeSSE(c, "scrape.navigated", map[string]interface{}{
		"status_code":   result.StatusCode,
		"final_url":     result.FinalURL,
		"engine_used":   result.EngineUsed,
		"navigation_ms": navigationMs,
	})

	// 5. Clean.
	cleanStart := time.Now()
	var cleanOpts []cleaner.CleanOptions
	if len(req.IncludeTags) > 0 || len(req.ExcludeTags) > 0 || req.CSSSelector != "" {
		cleanOpts = append(cleanOpts, cleaner.CleanOptions{
			IncludeTags: req.IncludeTags,
			ExcludeTags: req.ExcludeTags,
			CSSSelector: req.CSSSelector,
		})
	}
	resp, err := cl.Clean(result.RawHTML, req.URL, req.OutputFormat, req.ExtractMode, cleanOpts...)
	cleaningMs := time.Since(cleanStart).Milliseconds()

	if err != nil {
		writeSSE(c, "scrape.error", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// 6. Title fallback + fill fields.
	if resp.Metadata.Title == "" {
		resp.Metadata.Title = result.Title
	}
	resp.Metadata.FetchMethod = result.FetchMethod
	resp.StatusCode = result.StatusCode
	resp.FinalURL = result.FinalURL
	resp.EngineUsed = result.EngineUsed
	resp.Timing = models.TimingInfo{
		TotalMs:      time.Since(totalStart).Milliseconds(),
		NavigationMs: navigationMs,
		CleaningMs:   cleaningMs,
	}

	// 7. Cache store.
	if cc != nil && req.MaxAge > 0 {
		cacheKey := cache.Key(req.URL, req.OutputFormat, req.ExtractMode)
		cc.Set(cacheKey, resp)
		resp.CacheStatus = "miss"
	}

	// 8. Send completed event with full response.
	writeSSE(c, "scrape.completed", resp)
}

// writeSSE writes a single SSE event to the response.
func writeSSE(c *gin.Context, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, jsonData)
	c.Writer.Flush()
}
