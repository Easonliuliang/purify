package scraper

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/use-agent/purify/models"
)

// actionTimeout is the per-action deadline.
const actionTimeout = 10 * time.Second

// executeActions runs the ordered list of browser actions on the page.
// If any action fails, it returns an error describing which action failed
// and how many completed successfully.
func executeActions(ctx context.Context, page *rod.Page, actions []models.Action) error {
	for i, action := range actions {
		if err := executeSingleAction(ctx, page, action); err != nil {
			return models.NewScrapeError(
				models.ErrCodeActionFailed,
				fmt.Sprintf("action %d (%s) failed after %d completed: %v", i, action.Type, i, err),
				err,
			)
		}
	}
	return nil
}

// executeSingleAction dispatches a single action with its own timeout.
func executeSingleAction(ctx context.Context, page *rod.Page, action models.Action) error {
	actionCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	p := page.Context(actionCtx)

	switch action.Type {
	case "wait":
		return execWait(p, action)
	case "click":
		return execClick(p, action)
	case "scroll":
		return execScroll(p, action)
	case "execute_js":
		return execJS(p, action)
	case "scrape":
		// "scrape" is a no-op marker for multi-step scraping; the caller
		// handles capturing page state. For now we just succeed.
		return nil
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// execWait either sleeps for a duration or waits for a CSS selector to appear.
func execWait(p *rod.Page, action models.Action) error {
	if action.Selector != "" {
		// Wait for at least one element matching the selector to appear.
		return p.WaitElementsMoreThan(action.Selector, 0)
	}
	if action.Milliseconds > 0 {
		d := time.Duration(action.Milliseconds) * time.Millisecond
		select {
		case <-time.After(d):
			return nil
		case <-p.GetContext().Done():
			return p.GetContext().Err()
		}
	}
	return nil
}

// execClick finds the element matching the selector and clicks it.
func execClick(p *rod.Page, action models.Action) error {
	if action.Selector == "" {
		return fmt.Errorf("click action requires a selector")
	}
	el, err := p.Element(action.Selector)
	if err != nil {
		return fmt.Errorf("element %q not found: %w", action.Selector, err)
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// execScroll scrolls the page up or down by the specified number of viewports.
func execScroll(p *rod.Page, action models.Action) error {
	amount := action.Amount
	if amount <= 0 {
		amount = 1
	}

	// Get the viewport height to calculate scroll distance.
	res, err := p.Eval(`() => window.innerHeight`)
	if err != nil {
		return fmt.Errorf("failed to get viewport height: %w", err)
	}
	viewportHeight := res.Value.Int()

	for i := 0; i < amount; i++ {
		var scrollDelta int
		if action.Direction == "up" {
			scrollDelta = -viewportHeight
		} else {
			scrollDelta = viewportHeight
		}

		// Use Mouse.Scroll for precise pixel-level scrolling.
		err := p.Mouse.Scroll(0, float64(scrollDelta), 0)
		if err != nil {
			return fmt.Errorf("scroll step %d failed: %w", i, err)
		}

		// Brief pause between scroll steps to let lazy-loaded content trigger.
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// execJS evaluates arbitrary JavaScript in the page context.
func execJS(p *rod.Page, action models.Action) error {
	if action.Code == "" {
		return fmt.Errorf("execute_js action requires code")
	}
	_, err := p.Eval(action.Code)
	return err
}
