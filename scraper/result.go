package scraper

// ScrapeResult holds the output of a single scrape operation.
type ScrapeResult struct {
	// RawHTML is the raw page HTML.
	RawHTML string

	// Title is the page title.
	Title string

	// StatusCode is the HTTP status code of the navigation response.
	StatusCode int

	// FinalURL is the URL after any redirects.
	FinalURL string

	// EngineUsed records which engine produced the result (e.g. "http", "rod", "rod-stealth").
	EngineUsed string

	// FetchMethod records how the page was fetched: "http" or "browser".
	// Used by the extract handler for metadata.
	FetchMethod string
}
