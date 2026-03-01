package models

// ScrapeRequest is the payload for POST /api/v1/scrape.
type ScrapeRequest struct {
	// URL is the target page to scrape. Required.
	URL string `json:"url" binding:"required,url"`

	// WaitForNetworkIdle instructs the scraper to wait until the page
	// has no more than 2 in-flight network requests for 500ms.
	// Useful for SPAs that load data asynchronously.
	// Default: true.
	WaitForNetworkIdle *bool `json:"wait_for_network_idle,omitempty"`

	// Timeout is the maximum duration in seconds for the entire
	// scrape operation (navigation + rendering + extraction).
	// Default: 30. Max: 120.
	Timeout int `json:"timeout,omitempty" binding:"omitempty,min=1,max=120"`

	// Stealth enables anti-bot-detection evasions (e.g. navigator.webdriver masking).
	// Default: false.
	Stealth bool `json:"stealth,omitempty"`

	// ProxyURL overrides the default proxy for this request.
	// Format: "http://user:pass@host:port" or "socks5://host:port".
	ProxyURL string `json:"proxy_url,omitempty" binding:"omitempty,url"`

	// OutputFormat controls the response body format.
	// Allowed: "markdown" (default), "html", "text".
	OutputFormat string `json:"output_format,omitempty" binding:"omitempty,oneof=markdown html text"`

	// ExtractMode controls the content extraction strategy.
	// "readability" (default): two-stage pipeline, readability extracts main body → format conversion.
	// "raw": skip readability, pass full rendered HTML directly to format conversion.
	ExtractMode string `json:"extract_mode,omitempty" binding:"omitempty,oneof=readability raw"`

	// Headers sets custom HTTP headers for the request.
	// Example: {"Authorization": "Bearer xxx", "Accept-Language": "en-US"}
	Headers map[string]string `json:"headers,omitempty"`

	// Cookies sets cookies on the browser before navigation.
	Cookies []Cookie `json:"cookies,omitempty"`
}

// Cookie represents a browser cookie to set before scraping.
type Cookie struct {
	Name   string `json:"name" binding:"required"`
	Value  string `json:"value" binding:"required"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}

// Defaults applies default values to unset fields.
func (r *ScrapeRequest) Defaults() {
	if r.WaitForNetworkIdle == nil {
		t := true
		r.WaitForNetworkIdle = &t
	}
	if r.Timeout == 0 {
		r.Timeout = 30
	}
	if r.OutputFormat == "" {
		r.OutputFormat = "markdown"
	}
	if r.ExtractMode == "" {
		r.ExtractMode = "readability"
	}
}
