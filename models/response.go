package models

// ScrapeResponse is the response for POST /api/v1/scrape.
type ScrapeResponse struct {
	// Success indicates whether the scrape completed without errors.
	Success bool `json:"success"`

	// StatusCode is the HTTP status code from the scraped page.
	StatusCode int `json:"status_code"`

	// FinalURL is the URL after following all redirects.
	FinalURL string `json:"final_url"`

	// Content is the cleaned output in the requested format.
	Content string `json:"content"`

	// Metadata contains extracted page metadata.
	Metadata Metadata `json:"metadata"`

	// Links contains internal and external links extracted from the page.
	Links LinksResult `json:"links"`

	// Images contains image src and alt text extracted from the page.
	Images []Image `json:"images"`

	// OGMetadata contains Open Graph meta tags from the page.
	OGMetadata OGMetadata `json:"og_metadata"`

	// Tokens provides token estimates before and after cleaning.
	Tokens TokenInfo `json:"tokens"`

	// Timing provides duration breakdowns for the operation.
	Timing TimingInfo `json:"timing"`

	// CacheStatus indicates whether the response was served from cache.
	// Values: "hit", "miss", or empty (caching not requested).
	CacheStatus string `json:"cache_status,omitempty"`

	// EngineUsed indicates which fetch engine produced the result
	// (e.g. "http", "rod", "rod-stealth"). Empty when multi-engine is disabled.
	EngineUsed string `json:"engine_used,omitempty"`

	// Error is populated only when Success is false.
	Error *ErrorDetail `json:"error,omitempty"`
}

// LinksResult separates extracted links into internal and external groups.
type LinksResult struct {
	Internal []Link `json:"internal"`
	External []Link `json:"external"`
}

// Link represents a hyperlink extracted from the page.
type Link struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

// Image represents an image element extracted from the page.
type Image struct {
	Src string `json:"src"`
	Alt string `json:"alt,omitempty"`
}

// OGMetadata contains Open Graph protocol meta tags.
type OGMetadata struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	Type        string `json:"type,omitempty"`
}

// Metadata holds page-level information extracted during scraping.
type Metadata struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	Author      string `json:"author,omitempty"`
	Language    string `json:"language,omitempty"`
	SourceURL   string `json:"source_url"`
}

// TokenInfo provides before/after token estimates to show cleaning efficacy.
type TokenInfo struct {
	// OriginalEstimate is the estimated token count of the raw HTML.
	OriginalEstimate int `json:"original_estimate"`

	// CleanedEstimate is the estimated token count of the cleaned output.
	CleanedEstimate int `json:"cleaned_estimate"`

	// SavingsPercent is the percentage of tokens removed (0-100).
	SavingsPercent float64 `json:"savings_percent"`
}

// TimingInfo breaks down the time spent in each phase.
type TimingInfo struct {
	// TotalMs is the end-to-end duration in milliseconds.
	TotalMs int64 `json:"total_ms"`

	// NavigationMs is the time spent navigating and rendering the page.
	NavigationMs int64 `json:"navigation_ms"`

	// CleaningMs is the time spent extracting content and converting to markdown.
	CleaningMs int64 `json:"cleaning_ms"`
}

// HealthResponse is the response for GET /api/v1/health.
type HealthResponse struct {
	Status    string    `json:"status"`      // "healthy" or "degraded"
	Uptime    string    `json:"uptime"`
	PoolStats PoolStats `json:"pool_stats"`
	Version   string    `json:"version"`
}

// PoolStats reports the state of the browser page pool.
type PoolStats struct {
	MaxPages    int `json:"max_pages"`
	ActivePages int `json:"active_pages"`
	BrowserPID  int `json:"browser_pid"`
}
