package models

import "encoding/json"

// ExtractRequest is the payload for POST /api/v1/extract.
// It wraps a scrape operation with LLM-based structured data extraction.
type ExtractRequest struct {
	// URL is the target page to scrape. Required.
	URL string `json:"url" binding:"required,url"`

	// Schema is the JSON schema describing the desired output structure. Required.
	Schema json.RawMessage `json:"schema" binding:"required"`

	// LLMAPIKey is the user's own LLM API key (BYOK). Required.
	LLMAPIKey string `json:"llm_api_key" binding:"required"`

	// LLMModel is the model to use for extraction. Default: "gpt-4o-mini".
	LLMModel string `json:"llm_model,omitempty"`

	// LLMBaseURL is the base URL for the LLM API. Default: "https://api.openai.com/v1".
	// Supports any OpenAI-compatible API (DeepSeek, Groq, Azure, etc.).
	LLMBaseURL string `json:"llm_base_url,omitempty"`

	// CSSSelector is an optional CSS selector to filter HTML before cleaning.
	CSSSelector string `json:"css_selector,omitempty"`

	// OutputFormat controls the intermediate format before LLM extraction.
	// Default: "markdown".
	OutputFormat string `json:"output_format,omitempty" binding:"omitempty,oneof=markdown html text"`

	// ExtractMode controls the content extraction strategy.
	// Default: "readability".
	ExtractMode string `json:"extract_mode,omitempty" binding:"omitempty,oneof=readability raw"`

	// WaitForNetworkIdle instructs the scraper to wait for network idle.
	// Default: true.
	WaitForNetworkIdle *bool `json:"wait_for_network_idle,omitempty"`

	// Timeout is the max duration in seconds for the scrape operation.
	// Default: 30. Max: 120.
	Timeout int `json:"timeout,omitempty" binding:"omitempty,min=1,max=120"`

	// Stealth enables anti-bot-detection evasions.
	Stealth bool `json:"stealth,omitempty"`

	// ProxyURL overrides the default proxy for this request.
	ProxyURL string `json:"proxy_url,omitempty" binding:"omitempty,url"`

	// FetchMode controls the fetching strategy.
	// "auto" (default), "http", "browser".
	FetchMode string `json:"fetch_mode,omitempty" binding:"omitempty,oneof=auto browser http"`
}

// Defaults applies default values to unset fields.
func (r *ExtractRequest) Defaults() {
	if r.LLMModel == "" {
		r.LLMModel = "gpt-4o-mini"
	}
	if r.LLMBaseURL == "" {
		r.LLMBaseURL = "https://api.openai.com/v1"
	}
	if r.OutputFormat == "" {
		r.OutputFormat = "markdown"
	}
	if r.ExtractMode == "" {
		r.ExtractMode = "readability"
	}
	if r.WaitForNetworkIdle == nil {
		t := true
		r.WaitForNetworkIdle = &t
	}
	if r.Timeout == 0 {
		r.Timeout = 30
	}
	if r.FetchMode == "" {
		r.FetchMode = "auto"
	}
}

// ToScrapeRequest converts an ExtractRequest into a ScrapeRequest for reuse.
func (r *ExtractRequest) ToScrapeRequest() *ScrapeRequest {
	return &ScrapeRequest{
		URL:                r.URL,
		WaitForNetworkIdle: r.WaitForNetworkIdle,
		Timeout:            r.Timeout,
		Stealth:            r.Stealth,
		ProxyURL:           r.ProxyURL,
		OutputFormat:       r.OutputFormat,
		ExtractMode:        r.ExtractMode,
		CSSSelector:        r.CSSSelector,
		FetchMode:          r.FetchMode,
	}
}

// ExtractResponse is the response for POST /api/v1/extract.
type ExtractResponse struct {
	// Success indicates whether the extraction completed without errors.
	Success bool `json:"success"`

	// Data is the structured JSON extracted by the LLM.
	Data json.RawMessage `json:"data,omitempty"`

	// Metadata contains extracted page metadata.
	Metadata Metadata `json:"metadata"`

	// Tokens provides token estimates for the scrape pipeline.
	Tokens TokenInfo `json:"tokens"`

	// Timing provides duration breakdowns for the operation.
	Timing ExtractTimingInfo `json:"timing"`

	// LLMUsage reports the LLM token consumption.
	LLMUsage *LLMUsage `json:"llm_usage,omitempty"`

	// Error is populated only when Success is false.
	Error *ErrorDetail `json:"error,omitempty"`
}

// ExtractTimingInfo extends TimingInfo with extraction timing.
type ExtractTimingInfo struct {
	TotalMs        int64 `json:"total_ms"`
	NavigationMs   int64 `json:"navigation_ms"`
	CleaningMs     int64 `json:"cleaning_ms"`
	ExtractionMs   int64 `json:"extraction_ms"`
}

// LLMUsage reports token consumption from the LLM call.
type LLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
