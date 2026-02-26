package models

// ScrapeResponse is the response for POST /api/v1/scrape.
type ScrapeResponse struct {
	// Success indicates whether the scrape completed without errors.
	Success bool `json:"success"`

	// Content is the cleaned output in the requested format.
	Content string `json:"content"`

	// Metadata contains extracted page metadata.
	Metadata Metadata `json:"metadata"`

	// Tokens provides token estimates before and after cleaning.
	Tokens TokenInfo `json:"tokens"`

	// Timing provides duration breakdowns for the operation.
	Timing TimingInfo `json:"timing"`

	// Error is populated only when Success is false.
	Error *ErrorDetail `json:"error,omitempty"`
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
