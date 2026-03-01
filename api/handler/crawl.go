package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// crawlStore holds all in-flight and completed crawl jobs.
var crawlStore sync.Map

func init() {
	// Background goroutine to expire crawl jobs older than 1 hour.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-1 * time.Hour).Unix()
			crawlStore.Range(func(key, value any) bool {
				job := value.(*models.CrawlJob)
				if job.CreatedAt < cutoff {
					crawlStore.Delete(key)
				}
				return true
			})
		}
	}()
}

// PostCrawl returns a handler for POST /api/v1/crawl.
func PostCrawl(sc *scraper.Scraper, cl *cleaner.Cleaner) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.CrawlRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.CrawlResponse{
				Status: "failed",
			})
			return
		}

		// Apply defaults.
		if req.MaxDepth == 0 {
			req.MaxDepth = 3
		}
		if req.MaxPages == 0 {
			req.MaxPages = 100
		}
		if req.Scope == "" {
			req.Scope = "subdomain"
		}
		if req.Options.OutputFormat == "" {
			req.Options.OutputFormat = "markdown"
		}
		if req.Options.ExtractMode == "" {
			req.Options.ExtractMode = "readability"
		}

		jobID := "crawl-" + randomID()
		job := &models.CrawlJob{
			ID:        jobID,
			Status:    "processing",
			CreatedAt: time.Now().Unix(),
		}
		crawlStore.Store(jobID, job)

		// Launch BFS crawl in background.
		go runCrawl(sc, cl, job, req)

		c.JSON(http.StatusOK, models.CrawlResponse{
			ID:     jobID,
			Status: "processing",
		})
	}
}

// GetCrawl returns a handler for GET /api/v1/crawl/:id.
func GetCrawl() gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		val, ok := crawlStore.Load(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"error": models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: "crawl job not found",
				},
			})
			return
		}

		job := val.(*models.CrawlJob)
		c.JSON(http.StatusOK, models.CrawlStatusResponse{
			ID:        job.ID,
			Status:    job.Status,
			Completed: job.Completed,
			Total:     job.Total,
			Results:   job.Results,
		})
	}
}

// bfsItem represents a URL to be crawled at a given depth.
type bfsItem struct {
	url   string
	depth int
}

// runCrawl performs BFS crawling starting from the request URL.
func runCrawl(sc *scraper.Scraper, cl *cleaner.Cleaner, job *models.CrawlJob, req models.CrawlRequest) {
	baseURL, err := url.Parse(req.URL)
	if err != nil {
		job.Status = "failed"
		return
	}

	maxConcurrent := sc.Stats().MaxPages
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	sem := make(chan struct{}, maxConcurrent)

	visited := &sync.Map{}
	visited.Store(req.URL, struct{}{})

	var mu sync.Mutex
	var results []*models.ScrapeResponse
	var totalPages int

	queue := []bfsItem{{url: req.URL, depth: 0}}

	for len(queue) > 0 {
		// Check if we've hit the max pages limit.
		mu.Lock()
		if totalPages >= req.MaxPages {
			mu.Unlock()
			break
		}
		mu.Unlock()

		// Process current level in parallel.
		currentLevel := queue
		queue = nil

		var wg sync.WaitGroup
		var nextLevel []bfsItem
		var nextMu sync.Mutex

		for _, item := range currentLevel {
			mu.Lock()
			if totalPages >= req.MaxPages {
				mu.Unlock()
				break
			}
			totalPages++
			mu.Unlock()

			wg.Add(1)
			go func(it bfsItem) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Build scrape options.
				opts := models.BatchOptions{
					OutputFormat: req.Options.OutputFormat,
					ExtractMode:  req.Options.ExtractMode,
				}

				resp := scrapeOne(sc, cl, it.url, opts)

				mu.Lock()
				results = append(results, resp)
				job.Completed = len(results)
				job.Results = results
				mu.Unlock()

				// If within depth limit and successful, extract links for next level.
				if it.depth < req.MaxDepth && resp.Success {
					for _, link := range resp.Links.Internal {
						linkURL := link.Href

						// Check exclude patterns.
						if isExcluded(linkURL, req.ExcludePatterns) {
							continue
						}

						// Check scope.
						if !isInScope(linkURL, baseURL, req.Scope) {
							continue
						}

						// Deduplicate.
						if _, loaded := visited.LoadOrStore(linkURL, struct{}{}); loaded {
							continue
						}

						nextMu.Lock()
						nextLevel = append(nextLevel, bfsItem{url: linkURL, depth: it.depth + 1})
						nextMu.Unlock()
					}
				}
			}(item)
		}

		wg.Wait()
		queue = append(queue, nextLevel...)
	}

	mu.Lock()
	job.Total = len(results)
	failedCount := 0
	for _, r := range results {
		if !r.Success {
			failedCount++
		}
	}

	switch {
	case failedCount == len(results) && len(results) > 0:
		job.Status = "failed"
	case failedCount > 0:
		job.Status = "partial"
	default:
		job.Status = "completed"
	}
	mu.Unlock()

	slog.Info("crawl job finished",
		"id", job.ID,
		"status", job.Status,
		"total", job.Total,
	)
}

// isInScope checks whether a link URL is within the crawl scope relative to the base URL.
func isInScope(linkURL string, baseURL *url.URL, scope string) bool {
	parsed, err := url.Parse(linkURL)
	if err != nil {
		return false
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	switch scope {
	case "page":
		// Only the exact starting page.
		return false
	case "domain":
		// Same exact domain.
		return strings.EqualFold(parsed.Host, baseURL.Host)
	case "subdomain":
		// Same base domain (e.g., docs.example.com and www.example.com both match example.com).
		return sameBaseDomain(parsed.Host, baseURL.Host)
	default:
		return strings.EqualFold(parsed.Host, baseURL.Host)
	}
}

// sameBaseDomain checks if two hosts share the same base domain.
// For example, "docs.example.com" and "www.example.com" both have base domain "example.com".
func sameBaseDomain(host1, host2 string) bool {
	d1 := baseDomain(host1)
	d2 := baseDomain(host2)
	return strings.EqualFold(d1, d2)
}

// baseDomain extracts the base domain from a host.
// "docs.example.com" -> "example.com", "example.com" -> "example.com"
func baseDomain(host string) string {
	// Strip port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// isExcluded checks whether a URL path matches any of the exclude patterns.
func isExcluded(rawURL string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	for _, pattern := range patterns {
		// Match against the path.
		if matched, _ := path.Match(pattern, parsed.Path); matched {
			return true
		}
		// Also match against the full URL for patterns like "*.pdf".
		if matched, _ := path.Match(pattern, rawURL); matched {
			return true
		}
	}
	return false
}

