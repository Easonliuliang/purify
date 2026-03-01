package engine

import (
	"context"
	"net/http"
	"time"
)

// Engine is the interface that all fetch engines must implement.
type Engine interface {
	// Name returns the engine identifier (e.g. "http", "rod", "rod-stealth").
	Name() string

	// Fetch retrieves the page content for the given request.
	Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error)
}

// FetchRequest contains everything an engine needs to fetch a page.
type FetchRequest struct {
	URL     string
	Headers map[string]string
	Cookies []http.Cookie
	Timeout time.Duration
	Stealth bool
}

// FetchResult is the output of a successful engine fetch.
type FetchResult struct {
	HTML       string
	Title      string
	StatusCode int
	FinalURL   string
	EngineName string
}
