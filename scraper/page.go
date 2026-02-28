package scraper

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/use-agent/purify/models"
)

// DoScrape is the top-level orchestrator that dispatches to the appropriate
// fetching strategy based on req.FetchMode:
//
//	"http"    → doScrapeHTTP     (pure HTTP with Chrome TLS fingerprint)
//	"browser" → doScrapeBrowser  (headless Chrome via Rod)
//	"auto"    → doScrapeAuto     (HTTP first, fall back to Rod if JS needed)
func (s *Scraper) DoScrape(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	switch req.FetchMode {
	case "http":
		return s.doScrapeHTTP(ctx, req)
	case "browser":
		return s.doScrapeBrowser(ctx, req)
	default: // "auto"
		return s.doScrapeAuto(ctx, req)
	}
}

// doScrapeAuto tries HTTP first; if the response looks like an SPA shell that
// needs JS rendering, it falls back to the full browser pipeline.
func (s *Scraper) doScrapeAuto(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	result, err := s.doScrapeHTTP(ctx, req)
	if err != nil {
		// HTTP failed entirely → fall back to browser
		slog.Debug("auto: HTTP fetch failed, falling back to browser",
			"url", req.URL,
			"error", err,
		)
		return s.doScrapeBrowser(ctx, req)
	}

	if needsBrowser([]byte(result.HTML)) {
		slog.Debug("auto: HTTP response needs JS rendering, falling back to browser",
			"url", req.URL,
		)
		return s.doScrapeBrowser(ctx, req)
	}

	return result, nil
}

// doScrapeHTTP fetches the page via plain HTTP with a Chrome TLS fingerprint.
func (s *Scraper) doScrapeHTTP(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout > s.scraperCfg.MaxTimeout {
		timeout = s.scraperCfg.MaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := s.httpFetcher.fetch(ctx, req.URL, req.ProxyURL)
	if err != nil {
		slog.Info("HTTP fetch failed", "url", req.URL, "error", err)
		return nil, categorizeError(err, "HTTP fetch failed")
	}

	return &ScrapeResult{
		HTML:        string(body),
		Title:       extractTitle(body),
		FetchMethod: "http",
	}, nil
}

// doScrapeBrowser fetches the fully-rendered HTML via headless Chrome (Rod).
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
func (s *Scraper) doScrapeBrowser(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
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
	defer func() {
		if navErr := page.Navigate("about:blank"); navErr != nil {
			slog.Warn("cleanup: failed to navigate to about:blank",
				"error", navErr,
			)
		}
		s.pagePool.Put(page)
	}()

	// ── 4. Stealth injection ──────────────────────────────────────────
	if req.Stealth {
		if _, evalErr := page.EvalOnNewDocument(stealth.JS); evalErr != nil {
			slog.Warn("stealth injection failed, proceeding without stealth",
				"error", evalErr,
			)
		}
	}

	// ── 5. Mount hijack router (blocks Image/Stylesheet/Font/Media) ──
	router := setupHijack(page, s.scraperCfg.BlockedResourceTypes)
	if router != nil {
		defer func() { _ = router.Stop() }()
	}

	// ── 6. Bind request context to page ───────────────────────────────
	p := page.Context(ctx)

	// ── 7. Set up network idle waiter BEFORE navigation ───────────────
	var waitIdle func()
	if req.WaitForNetworkIdle != nil && *req.WaitForNetworkIdle {
		waitIdle = p.WaitRequestIdle(
			300*time.Millisecond,
			nil, nil, nil,
		)
	}

	// ── 8. Navigate ───────────────────────────────────────────────────
	if err := p.Navigate(req.URL); err != nil {
		return nil, categorizeError(err, "navigation to target URL failed")
	}

	// ── 9. Wait strategy ──────────────────────────────────────────────
	if waitIdle != nil {
		waitIdle()
	} else {
		if stableErr := p.WaitDOMStable(300*time.Millisecond, 0.1); stableErr != nil {
			slog.Debug("WaitDOMStable did not converge, proceeding with current DOM",
				"error", stableErr,
			)
		}
	}

	// ── 10. Extract rendered HTML ─────────────────────────────────────
	rawHTML, err := p.HTML()
	if err != nil {
		return nil, categorizeError(err, "failed to extract page HTML")
	}

	// ── 11. Extract title (best-effort) ───────────────────────────────
	title := evalStringOrEmpty(p, `() => document.title`)

	return &ScrapeResult{
		HTML:        rawHTML,
		Title:       title,
		FetchMethod: "browser",
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
