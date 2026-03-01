package models

// MapRequest is the payload for POST /api/v1/map.
type MapRequest struct {
	// URL is the target site to discover URLs for. Required.
	URL string `json:"url" binding:"required,url"`
}

// MapResponse is the response for POST /api/v1/map.
type MapResponse struct {
	Success bool     `json:"success"`
	URLs    []string `json:"urls"`
	Total   int      `json:"total"`
	Error   *ErrorDetail `json:"error,omitempty"`
}
