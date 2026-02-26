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
}
