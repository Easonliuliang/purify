package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/llm"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// Extract returns a handler for POST /api/v1/extract.
//
// Flow:
//  1. Parse & validate ExtractRequest, apply defaults.
//  2. DoScrape → raw HTML + JS title.
//  3. Clean (with optional CSS selector) → content.
//  4. LLM Extract → structured JSON.
//  5. Assemble response with timing and LLM usage.
func Extract(sc *scraper.Scraper, cl *cleaner.Cleaner, llmClient *llm.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		totalStart := time.Now()

		// ── 1. Parse request ────────────────────────────────────────
		var req models.ExtractRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ExtractResponse{
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
		scrapeReq := req.ToScrapeRequest()
		scrapeReq.Defaults()

		navStart := time.Now()
		rawHTML, jsTitle, err := sc.DoScrape(c.Request.Context(), scrapeReq)
		navigationMs := time.Since(navStart).Milliseconds()

		if err != nil {
			respondExtractError(c, err, models.ExtractTimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
			})
			return
		}

		// ── 3. Clean ────────────────────────────────────────────────
		cleanStart := time.Now()
		scrapeResp, err := cl.Clean(rawHTML, req.URL, req.OutputFormat, req.ExtractMode, req.CSSSelector)
		cleaningMs := time.Since(cleanStart).Milliseconds()

		if err != nil {
			respondExtractError(c, err, models.ExtractTimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
				CleaningMs:   cleaningMs,
			})
			return
		}

		// Title fallback.
		if scrapeResp.Metadata.Title == "" {
			scrapeResp.Metadata.Title = jsTitle
		}

		// ── 4. LLM Extract ──────────────────────────────────────────
		extractStart := time.Now()
		result, err := llmClient.Extract(c.Request.Context(), scrapeResp.Content, req.Schema, llm.ExtractParams{
			APIKey:  req.LLMAPIKey,
			Model:   req.LLMModel,
			BaseURL: req.LLMBaseURL,
		})
		extractionMs := time.Since(extractStart).Milliseconds()

		if err != nil {
			respondExtractError(c, err, models.ExtractTimingInfo{
				TotalMs:        time.Since(totalStart).Milliseconds(),
				NavigationMs:   navigationMs,
				CleaningMs:     cleaningMs,
				ExtractionMs:   extractionMs,
			})
			return
		}

		// ── 5. Assemble response ────────────────────────────────────
		c.JSON(http.StatusOK, models.ExtractResponse{
			Success:  true,
			Data:     result.Data,
			Metadata: scrapeResp.Metadata,
			Tokens:   scrapeResp.Tokens,
			Timing: models.ExtractTimingInfo{
				TotalMs:        time.Since(totalStart).Milliseconds(),
				NavigationMs:   navigationMs,
				CleaningMs:     cleaningMs,
				ExtractionMs:   extractionMs,
			},
			LLMUsage: result.Usage,
		})
	}
}

// respondExtractError maps a ScrapeError to the correct HTTP status and writes
// a structured JSON error response for the extract endpoint.
func respondExtractError(c *gin.Context, err error, timing models.ExtractTimingInfo) {
	scrapeErr, ok := err.(*models.ScrapeError)
	if !ok {
		scrapeErr = models.NewScrapeError(models.ErrCodeInternal, err.Error(), err)
	}

	c.JSON(mapExtractErrorToStatus(scrapeErr), models.ExtractResponse{
		Success: false,
		Error:   scrapeErr.ToDetail(),
		Timing:  timing,
	})
}

// mapExtractErrorToStatus translates error codes to HTTP status codes,
// including LLM-specific codes.
func mapExtractErrorToStatus(e *models.ScrapeError) int {
	switch e.Code {
	case models.ErrCodeTimeout:
		return http.StatusGatewayTimeout
	case models.ErrCodeNavigation:
		return http.StatusBadGateway
	case models.ErrCodeInvalidInput:
		return http.StatusBadRequest
	case models.ErrCodeRateLimited, models.ErrCodeLLMRateLimited:
		return http.StatusTooManyRequests
	case models.ErrCodeUnauthorized, models.ErrCodeLLMAuthFailure:
		return http.StatusUnauthorized
	case models.ErrCodeLLMFailure:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
