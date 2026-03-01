package models

// CrawlRequest is the payload for POST /api/v1/crawl.
type CrawlRequest struct {
	// URL is the starting page to crawl. Required.
	URL string `json:"url" binding:"required,url"`

	// MaxDepth limits the crawl depth from the starting URL.
	// Default: 3. Max: 10.
	MaxDepth int `json:"max_depth,omitempty" binding:"omitempty,min=1,max=10"`

	// MaxPages limits the total number of pages to crawl.
	// Default: 100. Max: 500.
	MaxPages int `json:"max_pages,omitempty" binding:"omitempty,min=1,max=500"`

	// Scope controls which links are followed.
	// "domain" (same domain), "subdomain" (same base domain), "page" (single page only).
	// Default: "subdomain".
	Scope string `json:"scope,omitempty" binding:"omitempty,oneof=domain subdomain page"`

	// ExcludePatterns is a list of glob patterns for paths to skip.
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`

	// Options contains shared scrape options for each crawled page.
	Options CrawlOptions `json:"options"`

	WebhookURL    string `json:"webhook_url,omitempty" binding:"omitempty,url"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// CrawlOptions are the scrape settings for each crawled page.
type CrawlOptions struct {
	OutputFormat string `json:"output_format,omitempty" binding:"omitempty,oneof=markdown html text"`
	ExtractMode  string `json:"extract_mode,omitempty" binding:"omitempty,oneof=readability raw"`
}

// CrawlResponse is the immediate response for POST /api/v1/crawl.
type CrawlResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// CrawlStatusResponse is the response for GET /api/v1/crawl/:id.
type CrawlStatusResponse struct {
	ID        string           `json:"id"`
	Status    string           `json:"status"`
	Completed int              `json:"completed"`
	Total     int              `json:"total"`
	Results   []*ScrapeResponse `json:"results,omitempty"`
}

// CrawlJob tracks an in-progress crawl operation.
type CrawlJob struct {
	ID            string
	Status        string // "processing", "completed", "failed", "partial"
	Total         int
	Completed     int
	Results       []*ScrapeResponse
	CreatedAt     int64 // unix timestamp
	WebhookURL    string
	WebhookSecret string
}
