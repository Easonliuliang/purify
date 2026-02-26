package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
func Scrape(sc *scraper.Scraper, cl *cleaner.Cleaner) gin.HandlerFunc {
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

		// ── 2. Scrape ───────────────────────────────────────────────
		navStart := time.Now()
		rawHTML, jsTitle, err := sc.DoScrape(c.Request.Context(), &req)
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
		resp, err := cl.Clean(rawHTML, req.URL, req.OutputFormat, req.ExtractMode)
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
		// Readability usually extracts a better title, but on fallback
		// (raw-HTML passthrough) it will be empty. Use the JS-evaluated
		// document.title as the safety net.
		if resp.Metadata.Title == "" {
			resp.Metadata.Title = jsTitle
		}

		// ── 5. Fill timing and respond ──────────────────────────────
		resp.Timing = models.TimingInfo{
			TotalMs:      time.Since(totalStart).Milliseconds(),
			NavigationMs: navigationMs,
			CleaningMs:   cleaningMs,
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
