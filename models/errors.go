package models

import "fmt"

// Error codes used in API responses and internal error handling.
const (
	ErrCodeTimeout      = "SCRAPE_TIMEOUT"
	ErrCodeNavigation   = "NAVIGATION_FAILED"
	ErrCodeReadability  = "CONTENT_EXTRACTION_FAILED"
	ErrCodeBrowserCrash = "BROWSER_CRASH"
	ErrCodeInvalidInput = "INVALID_INPUT"
	ErrCodeRateLimited  = "RATE_LIMITED"
	ErrCodeUnauthorized = "UNAUTHORIZED"
	ErrCodeInternal     = "INTERNAL_ERROR"

	// LLM-related error codes for /api/v1/extract.
	ErrCodeLLMFailure     = "LLM_FAILURE"
	ErrCodeLLMAuthFailure = "LLM_AUTH_FAILURE"
	ErrCodeLLMRateLimited = "LLM_RATE_LIMITED"
)

// ErrorDetail is the structured error in API responses.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ScrapeError is the internal error type carrying an error code.
// It implements the error interface and supports error wrapping via Unwrap.
type ScrapeError struct {
	Code    string
	Message string
	Err     error // wrapped original error
}

func (e *ScrapeError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ScrapeError) Unwrap() error {
	return e.Err
}

// NewScrapeError creates a new ScrapeError.
func NewScrapeError(code, message string, err error) *ScrapeError {
	return &ScrapeError{Code: code, Message: message, Err: err}
}

// ToDetail converts an internal error to an API-facing ErrorDetail.
func (e *ScrapeError) ToDetail() *ErrorDetail {
	return &ErrorDetail{Code: e.Code, Message: e.Message}
}
