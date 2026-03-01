package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// batchStore holds all in-flight and completed batch jobs.
var batchStore sync.Map

func init() {
	// Background goroutine to expire batch jobs older than 1 hour.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-1 * time.Hour).Unix()
			batchStore.Range(func(key, value any) bool {
				job := value.(*models.BatchJob)
				if job.CreatedAt < cutoff {
					batchStore.Delete(key)
				}
				return true
			})
		}
	}()
}

// PostBatch returns a handler for POST /api/v1/batch/scrape.
// It validates the request, creates a batch job, and launches goroutines
// to scrape each URL concurrently.
func PostBatch(sc *scraper.Scraper, cl *cleaner.Cleaner) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.BatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.BatchResponse{
				Status: "failed",
			})
			return
		}

		if len(req.URLs) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: "maximum 100 URLs per batch",
				},
			})
			return
		}

		jobID := "batch-" + randomID()
		job := &models.BatchJob{
			ID:        jobID,
			Status:    "processing",
			Total:     len(req.URLs),
			Completed: 0,
			Results:   make([]*models.ScrapeResponse, len(req.URLs)),
			CreatedAt: time.Now().Unix(),
		}
		batchStore.Store(jobID, job)

		// Launch scraping in background.
		go runBatch(sc, cl, job, req)

		c.JSON(http.StatusOK, models.BatchResponse{
			ID:     jobID,
			Status: "processing",
			Total:  len(req.URLs),
		})
	}
}

// GetBatch returns a handler for GET /api/v1/batch/:id.
func GetBatch() gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		val, ok := batchStore.Load(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"error": models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: "batch job not found",
				},
			})
			return
		}

		job := val.(*models.BatchJob)
		c.JSON(http.StatusOK, models.BatchStatusResponse{
			ID:        job.ID,
			Status:    job.Status,
			Completed: job.Completed,
			Total:     job.Total,
			Results:   job.Results,
		})
	}
}

// runBatch processes all URLs in a batch job with concurrency limited by a semaphore.
func runBatch(sc *scraper.Scraper, cl *cleaner.Cleaner, job *models.BatchJob, req models.BatchRequest) {
	// Use a semaphore to limit concurrency.
	maxConcurrent := sc.Stats().MaxPages
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	var completed atomic.Int32
	var failed atomic.Int32

	for i, rawURL := range req.URLs {
		wg.Add(1)
		go func(idx int, targetURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resp := scrapeOne(sc, cl, targetURL, req.Options)
			job.Results[idx] = resp

			if resp.Success {
				completed.Add(1)
			} else {
				failed.Add(1)
			}
			job.Completed = int(completed.Load()) + int(failed.Load())
		}(i, rawURL)
	}

	wg.Wait()

	failedCount := int(failed.Load())
	completedCount := int(completed.Load())

	switch {
	case failedCount == job.Total:
		job.Status = "failed"
	case failedCount > 0:
		job.Status = "partial"
	default:
		job.Status = "completed"
	}
	job.Completed = completedCount + failedCount

	slog.Info("batch job finished",
		"id", job.ID,
		"status", job.Status,
		"completed", completedCount,
		"failed", failedCount,
		"total", job.Total,
	)
}

// scrapeOne performs a single scrape+clean for one URL using shared batch options.
func scrapeOne(sc *scraper.Scraper, cl *cleaner.Cleaner, targetURL string, opts models.BatchOptions) *models.ScrapeResponse {
	totalStart := time.Now()

	// Build a ScrapeRequest from shared options.
	sreq := &models.ScrapeRequest{
		URL:                targetURL,
		OutputFormat:       opts.OutputFormat,
		ExtractMode:        opts.ExtractMode,
		WaitForNetworkIdle: opts.WaitForNetworkIdle,
		Timeout:            opts.Timeout,
		Stealth:            opts.Stealth,
	}
	sreq.Defaults()

	// Scrape.
	navStart := time.Now()
	result, err := sc.DoScrape(context.Background(), sreq)
	navigationMs := time.Since(navStart).Milliseconds()

	if err != nil {
		scrapeErr, ok := err.(*models.ScrapeError)
		if !ok {
			scrapeErr = models.NewScrapeError(models.ErrCodeInternal, err.Error(), err)
		}
		return &models.ScrapeResponse{
			Success: false,
			Error:   scrapeErr.ToDetail(),
			Timing: models.TimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
			},
		}
	}

	// Clean.
	cleanStart := time.Now()
	resp, err := cl.Clean(result.RawHTML, sreq.URL, sreq.OutputFormat, sreq.ExtractMode)
	cleaningMs := time.Since(cleanStart).Milliseconds()

	if err != nil {
		scrapeErr, ok := err.(*models.ScrapeError)
		if !ok {
			scrapeErr = models.NewScrapeError(models.ErrCodeInternal, err.Error(), err)
		}
		return &models.ScrapeResponse{
			Success: false,
			Error:   scrapeErr.ToDetail(),
			Timing: models.TimingInfo{
				TotalMs:      time.Since(totalStart).Milliseconds(),
				NavigationMs: navigationMs,
				CleaningMs:   cleaningMs,
			},
		}
	}

	// Title fallback.
	if resp.Metadata.Title == "" {
		resp.Metadata.Title = result.Title
	}

	resp.StatusCode = result.StatusCode
	resp.FinalURL = result.FinalURL
	resp.Timing = models.TimingInfo{
		TotalMs:      time.Since(totalStart).Milliseconds(),
		NavigationMs: navigationMs,
		CleaningMs:   cleaningMs,
	}

	return resp
}

// randomID generates a short random hex string for job IDs.
func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
