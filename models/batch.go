package models

// BatchRequest is the payload for POST /api/v1/batch/scrape.
type BatchRequest struct {
	// URLs is the list of target pages to scrape. Required.
	URLs []string `json:"urls" binding:"required,min=1,max=100"`

	// Options contains shared scrape options applied to all URLs.
	Options BatchOptions `json:"options"`
}

// BatchOptions are the shared scrape settings applied to every URL in a batch.
type BatchOptions struct {
	OutputFormat       string `json:"output_format,omitempty" binding:"omitempty,oneof=markdown html text"`
	ExtractMode        string `json:"extract_mode,omitempty" binding:"omitempty,oneof=readability raw"`
	WaitForNetworkIdle *bool  `json:"wait_for_network_idle,omitempty"`
	Timeout            int    `json:"timeout,omitempty" binding:"omitempty,min=1,max=120"`
	Stealth            bool   `json:"stealth,omitempty"`
}

// BatchResponse is the immediate response for POST /api/v1/batch/scrape.
type BatchResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Total  int    `json:"total"`
}

// BatchStatusResponse is the response for GET /api/v1/batch/:id.
type BatchStatusResponse struct {
	ID        string           `json:"id"`
	Status    string           `json:"status"`
	Completed int              `json:"completed"`
	Total     int              `json:"total"`
	Results   []*ScrapeResponse `json:"results,omitempty"`
}

// BatchJob tracks an in-progress batch scrape operation.
type BatchJob struct {
	ID        string
	Status    string // "processing", "completed", "failed", "partial"
	Total     int
	Completed int
	Results   []*ScrapeResponse
	CreatedAt int64 // unix timestamp
}
