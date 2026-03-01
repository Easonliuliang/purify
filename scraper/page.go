package scraper

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/use-agent/purify/models"
	"github.com/ysmood/gson"
)

// DoScrape fetches the fully-rendered HTML and page title for the given URL.
//
// Lifecycle (numbered steps match the inline comments):
//
//  1. Timeout guard          – hard deadline on the entire operation
//  2. Acquire page           – borrow a tab from the pool (or create one)
//  3. DEFER: cleanup         – about:blank + return to pool (leak prevention)
//  4. Stealth injection      – mask navigator.webdriver etc. (before navigation!)
//  5. Hijack mount           – block images/CSS/fonts/media (before navigation!)
//  6. Context binding        – propagate timeout to all Rod operations
//  7. Idle listener setup    – MUST be registered before Navigate to capture all requests
//  8. Navigate               – triggers page load
//  9. Wait                   – network idle or DOM stable
//  10. Extract               – page.HTML() + document.title
//
// Why this order matters:
//   - Steps 4-5 MUST happen before step 8: stealth JS and resource blocking only
//     take effect for navigations that happen after they are installed.
//   - Step 7 MUST happen before step 8: WaitRequestIdle sets up a CDP listener;
//     if we set it up after Navigate, we would miss in-flight requests and the
//     wait would return instantly (false idle).
//   - Step 3's about:blank uses the ORIGINAL page reference (without request
//     context), so cleanup succeeds even if the request context has expired.
// ScrapeResult holds the output of a single scrape operation.
type ScrapeResult struct {
	RawHTML    string
	Title      string
	StatusCode int
	FinalURL   string
}

func (s *Scraper) DoScrape(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	// ── 1. Timeout guard ──────────────────────────────────────────────
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout > s.scraperCfg.MaxTimeout {
		timeout = s.scraperCfg.MaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// ── 2. Acquire page from pool ─────────────────────────────────────
	s.activePages.Add(1)
	defer s.activePages.Add(-1)

	page, acquireErr := s.pagePool.Get(func() (*rod.Page, error) {
		return s.browser.Page(proto.TargetCreateTarget{})
	})
	if acquireErr != nil {
		return nil, models.NewScrapeError(
			models.ErrCodeBrowserCrash,
			"failed to acquire page from pool",
			acquireErr,
		)
	}

	// ── 3. CRITICAL DEFER: prevent DOM memory leak + guarantee pool return
	//
	// Navigate to about:blank to discard the entire DOM tree of the scraped
	// page.  Without this, each reused tab accumulates the previous page's
	// DOM in Chrome's renderer process memory (observed growth: ~5-20 MB per
	// complex page).
	//
	// We use the ORIGINAL `page` reference here — not the context-bound `p`
	// created in step 6 — because the request context may have already
	// expired (DeadlineExceeded).  The original page still carries the
	// browser's background context, so the cleanup Navigate always succeeds.
	defer func() {
		if navErr := page.Navigate("about:blank"); navErr != nil {
			slog.Warn("cleanup: failed to navigate to about:blank",
				"error", navErr,
			)
		}
		s.pagePool.Put(page)
	}()

	// ── 4. Stealth injection ──────────────────────────────────────────
	// EvalOnNewDocument injects JS that runs before ANY script on every
	// subsequent navigation.  It masks navigator.webdriver, overrides
	// chrome.runtime, spoofs plugins array, etc.
	//
	// Non-fatal: if injection fails we still attempt the scrape.
	if req.Stealth {
		if _, evalErr := page.EvalOnNewDocument(stealth.JS); evalErr != nil {
			slog.Warn("stealth injection failed, proceeding without stealth",
				"error", evalErr,
			)
		}
	}

	// ── 4b. Build extra headers (custom + Google Referer) ────────────
	// Google Referer: many sites give Google-referred traffic looser
	// anti-bot policies (afraid of hurting SEO). We set a Google search
	// referer by default, unless the user provided their own Referer.
	extraHeaders := make(map[string]string, len(req.Headers)+1)
	if _, hasReferer := req.Headers["Referer"]; !hasReferer {
		if u, parseErr := url.Parse(req.URL); parseErr == nil {
			extraHeaders["Referer"] = "https://www.google.com/search?q=" + url.QueryEscape(u.Hostname())
		}
	}
	// User-provided headers override defaults (including Referer).
	for k, v := range req.Headers {
		extraHeaders[k] = v
	}
	if len(extraHeaders) > 0 {
		_ = proto.NetworkSetExtraHTTPHeaders{
			Headers: toHeadersMap(extraHeaders),
		}.Call(page)
	}

	// ── 4c. Custom cookies ──────────────────────────────────────────
	for _, cookie := range req.Cookies {
		domain := cookie.Domain
		if domain == "" {
			if u, parseErr := url.Parse(req.URL); parseErr == nil {
				domain = u.Host
			}
		}
		path := cookie.Path
		if path == "" {
			path = "/"
		}
		_, _ = proto.NetworkSetCookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Domain: domain,
			Path:   path,
		}.Call(page)
	}

	// ── 5. Mount hijack router (blocks Image/Stylesheet/Font/Media) ──
	// setupHijack returns nil if the blocked list is empty.
	router := setupHijack(page, s.scraperCfg.BlockedResourceTypes)
	if router != nil {
		defer func() { _ = router.Stop() }()
	}

	// ── 6. Bind request context to page ───────────────────────────────
	// page.Context(ctx) returns a shallow clone that shares the same
	// underlying CDP session but respects the new context's deadline.
	// All subsequent Rod operations on `p` will abort with
	// context.DeadlineExceeded if the timeout fires.
	p := page.Context(ctx)

	// ── 7. Set up network idle waiter BEFORE navigation ───────────────
	// WaitRequestIdle registers a CDP Fetch listener that tracks in-flight
	// requests.  The returned `waitIdle` function blocks until no new
	// requests have fired for `d` (300 ms).
	//
	// If we registered it AFTER Navigate, we would miss the initial burst
	// of requests and the waiter would return immediately (false positive).
	var waitIdle func()
	if req.WaitForNetworkIdle != nil && *req.WaitForNetworkIdle {
		waitIdle = p.WaitRequestIdle(
			300*time.Millisecond, // idle threshold
			nil,                  // includes (nil = all URLs)
			nil,                  // excludes
			nil,                  // excludeTypes
		)
	}

	// ── 7b. Status code capture ──────────────────────────────────────
	// Listen for the first main-frame document response to capture the
	// HTTP status code. Uses a channel so we never block the main flow.
	statusCh := make(chan int, 1)
	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		if e.Type == proto.NetworkResourceTypeDocument {
			select {
			case statusCh <- e.Response.Status:
			default:
			}
			return true
		}
		return false
	})()

	// ── 8. Navigate ───────────────────────────────────────────────────
	var navErr error
	if navErr = p.Navigate(req.URL); navErr != nil {
		return nil, categorizeError(navErr, "navigation to target URL failed")
	}

	// ── 9. Wait strategy ──────────────────────────────────────────────
	if waitIdle != nil {
		// Block until the page has had no network activity for 300 ms.
		// If context expires, the underlying Rod watcher unblocks and
		// subsequent operations return DeadlineExceeded.
		waitIdle()
	} else {
		// Lightweight fallback: wait until the DOM tree stops mutating.
		// diff=0.1 means the outer HTML must change by less than 10% between
		// two consecutive checks separated by 300 ms.
		if stableErr := p.WaitDOMStable(300*time.Millisecond, 0.1); stableErr != nil {
			// Non-fatal — the DOM may still be usable.
			slog.Debug("WaitDOMStable did not converge, proceeding with current DOM",
				"error", stableErr,
			)
		}
	}

	// ── 9b. Collect status code (best-effort) ────────────────────────
	var statusCode int
	select {
	case statusCode = <-statusCh:
	default:
		// Event didn't fire (e.g. intercepted by hijack router).
	}

	// ── 10. Extract rendered HTML ─────────────────────────────────────
	rawHTML, htmlErr := p.HTML()
	if htmlErr != nil {
		return nil, categorizeError(htmlErr, "failed to extract page HTML")
	}

	// ── 11. Extract title and final URL (best-effort) ────────────────
	title := evalStringOrEmpty(p, `() => document.title`)
	finalURL := evalStringOrEmpty(p, `() => window.location.href`)
	if finalURL == "" {
		finalURL = req.URL
	}

	return &ScrapeResult{
		RawHTML:    rawHTML,
		Title:      title,
		StatusCode: statusCode,
		FinalURL:   finalURL,
	}, nil
}

// evalStringOrEmpty evaluates a JS expression and returns the string result,
// swallowing any errors (useful for optional metadata extraction).
func evalStringOrEmpty(page *rod.Page, js string) string {
	res, err := page.Eval(js)
	if err != nil {
		return ""
	}
	return res.Value.Str()
}

// toHeadersMap converts a plain string map to the proto.NetworkHeaders type
// (map[string]gson.JSON) required by NetworkSetExtraHTTPHeaders.
func toHeadersMap(headers map[string]string) proto.NetworkHeaders {
	m := make(proto.NetworkHeaders, len(headers))
	for k, v := range headers {
		m[k] = gson.New(v)
	}
	return m
}

// categorizeError wraps raw errors into typed ScrapeErrors so the API layer
// can map them to appropriate HTTP status codes.
func categorizeError(err error, msg string) *models.ScrapeError {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return models.NewScrapeError(models.ErrCodeTimeout, msg, err)
	case errors.Is(err, context.Canceled):
		return models.NewScrapeError(models.ErrCodeTimeout, "request canceled", err)
	default:
		return models.NewScrapeError(models.ErrCodeNavigation, msg, err)
	}
}
