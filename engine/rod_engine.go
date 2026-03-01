package engine

import (
	"context"
	"fmt"
)

// RodFetchFunc is the callback type that wraps the existing scraper.DoScrape logic.
// It is injected from main.go to avoid a circular import (engine/ -> scraper/).
type RodFetchFunc func(ctx context.Context, req *FetchRequest) (*FetchResult, error)

// RodEngine is a browser-based engine that delegates to the existing rod scraper
// logic via a callback function. The forceStealth flag distinguishes between
// Layer 3 (rod) and Layer 4 (rod-stealth).
type RodEngine struct {
	fetchFunc    RodFetchFunc
	forceStealth bool
	name         string
}

// NewRodEngine creates a RodEngine.
//   - fetchFunc: callback that invokes the rod-based scraper (injected from main.go).
//   - forceStealth: when true, the engine always sets Stealth=true on requests.
func NewRodEngine(fetchFunc RodFetchFunc, forceStealth bool) *RodEngine {
	name := "rod"
	if forceStealth {
		name = "rod-stealth"
	}
	return &RodEngine{
		fetchFunc:    fetchFunc,
		forceStealth: forceStealth,
		name:         name,
	}
}

func (e *RodEngine) Name() string { return e.name }

func (e *RodEngine) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	if e.fetchFunc == nil {
		return nil, fmt.Errorf("%s: fetchFunc not configured", e.name)
	}

	// Clone the request so we don't mutate the caller's copy.
	r := *req
	if e.forceStealth {
		r.Stealth = true
	}

	result, err := e.fetchFunc(ctx, &r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.name, err)
	}

	result.EngineName = e.name
	return result, nil
}
