package scraper

// ScrapeResult is the unified return type for all scraping methods.
type ScrapeResult struct {
	// HTML is the raw page HTML.
	HTML string

	// Title is the page title.
	Title string

	// FetchMethod records how the page was fetched: "http" or "browser".
	FetchMethod string
}
