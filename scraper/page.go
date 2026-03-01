package scraper

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/use-agent/purify/engine"
	"github.com/use-agent/purify/models"
	"github.com/ysmood/gson"
)

// DoScrape is the top-level orchestrator.
//
// If the multi-engine dispatcher is configured AND the request has no Actions
// AND no CDPURL, it delegates to the dispatcher for a faster path (HTTP-first
// with Rod fallback via engine racing). Otherwise it falls through to the
// direct Rod-based scraping path.
func (s *Scraper) DoScrape(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	// ── 0. Multi-engine dispatch ────────────────────────────────────
	// If the dispatcher is configured AND the request has no Actions AND
	// no CDPURL, delegate to the multi-engine dispatcher for a faster path.
	if s.dispatcher != nil && len(req.Actions) == 0 && req.CDPURL == "" {
		timeout := time.Duration(req.Timeout) * time.Second
		if timeout > s.scraperCfg.MaxTimeout {
			timeout = s.scraperCfg.MaxTimeout
		}

		cookies := make([]http.Cookie, len(req.Cookies))
		for i, c := range req.Cookies {
			cookies[i] = http.Cookie{
				Name:   c.Name,
				Value:  c.Value,
				Domain: c.Domain,
				Path:   c.Path,
			}
		}

		fetchReq := &engine.FetchRequest{
			URL:     req.URL,
			Headers: req.Headers,
			Cookies: cookies,
			Timeout: timeout,
			Stealth: req.Stealth,
		}

		dispatchCtx, dispatchCancel := context.WithTimeout(ctx, timeout)
		defer dispatchCancel()

		result, err := s.dispatcher.Dispatch(dispatchCtx, fetchReq)
		if err == nil {
			return &ScrapeResult{
				RawHTML:     result.HTML,
				Title:       result.Title,
				StatusCode:  result.StatusCode,
				FinalURL:    result.FinalURL,
				EngineUsed:  result.EngineName,
				FetchMethod: result.EngineName,
			}, nil
		}
		// Dispatcher failed entirely — fall through to existing rod logic.
		slog.Warn("dispatcher failed, falling back to direct rod scrape",
			"url", req.URL, "error", err)
	}

	return s.doScrapeRod(ctx, req)
}

// DoScrapeRod is the direct rod-based scraping path. It is exported so
// that the engine.RodEngine callback in main.go can call it without
// triggering the dispatcher (avoiding infinite recursion).
func (s *Scraper) DoScrapeRod(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	return s.doScrapeRod(ctx, req)
}

// doScrapeRod contains the full rod-based scraping logic (timeout, pool,
// stealth, navigation, extraction). This is the original DoScrape path.
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
func (s *Scraper) doScrapeRod(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	// ── 1. Timeout guard ──────────────────────────────────────────────
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout > s.scraperCfg.MaxTimeout {
		timeout = s.scraperCfg.MaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// ── 1b. Per-request CDP URL: connect to user's own Chrome ────────
	if req.CDPURL != "" {
		return s.doScrapeWithCDP(ctx, req)
	}

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

	// ── 4b. Build extra headers (custom + Google Referer) ────────────
	extraHeaders := make(map[string]string, len(req.Headers)+1)
	if _, hasReferer := req.Headers["Referer"]; !hasReferer {
		if u, parseErr := url.Parse(req.URL); parseErr == nil {
			extraHeaders["Referer"] = "https://www.google.com/search?q=" + url.QueryEscape(u.Hostname())
		}
	}
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

	// ── 5. Mount hijack router (blocks Image/Stylesheet/Font/Media + ads) ──
	router := setupHijack(page, s.scraperCfg.BlockedResourceTypes, req.BlockAds)
	if router != nil {
		defer func() { _ = router.Stop() }()
	}

	// ── 6. Bind request context to page ───────────────────────────────
	p := page.Context(ctx)

	// ── 7. Set up network idle waiter BEFORE navigation ───────────────
	// NOTE: WaitRequestIdle uses the Fetch domain which conflicts with
	// HijackRequests on Chromium 145+. Use WaitDOMStable as fallback.
	var waitIdle func()

	// ── 7b. Status code capture ──────────────────────────────────────
	// NOTE: page.EachEvent(NetworkResponseReceived) causes ERR_BLOCKED_BY_CLIENT
	// on Chromium 145+ because it internally enables Network domain interception
	// which conflicts with the Fetch domain used by HijackRequests/WaitRequestIdle.
	// Instead, we capture the status code AFTER navigation from the page's
	// NavigationHistory, which is always available without any event listeners.
	var statusCode int

	// ── 8. Navigate ───────────────────────────────────────────────────
	var navErr error
	if navErr = p.Navigate(req.URL); navErr != nil {
		return nil, categorizeError(navErr, "navigation to target URL failed")
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

	// ── 9b. Collect status code via JS (best-effort) ────────────────
	// Use performance.getEntriesByType("navigation") to get the HTTP status
	// code without needing CDP event listeners.
	if res, err := p.Eval(`() => {
		try {
			const entries = performance.getEntriesByType("navigation");
			if (entries.length > 0) return entries[0].responseStatus || 0;
		} catch(e) {}
		return 0;
	}`); err == nil {
		statusCode = res.Value.Int()
	}

	// ── 9c. Remove overlays (cookie banners, popups) ────────────────
	if req.RemoveOverlays {
		removeOverlays(p)
	}

	// ── 9d. Execute browser actions ─────────────────────────────────
	if len(req.Actions) > 0 {
		if err := executeActions(ctx, page, req.Actions); err != nil {
			return nil, err
		}
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
		RawHTML:      rawHTML,
		Title:        title,
		StatusCode:   statusCode,
		FinalURL:     finalURL,
		FetchMethod:  "browser",
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

// doScrapeWithCDP connects to a user-provided CDP endpoint, creates a
// temporary page, scrapes it, and disconnects (without killing the browser).
func (s *Scraper) doScrapeWithCDP(ctx context.Context, req *models.ScrapeRequest) (*ScrapeResult, error) {
	browser := rod.New().ControlURL(req.CDPURL)
	if err := browser.Connect(); err != nil {
		return nil, models.NewScrapeError(
			models.ErrCodeBrowserCrash,
			"failed to connect to CDP URL",
			err,
		)
	}
	// Disconnect closes the WebSocket but does NOT kill the browser process.
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, models.NewScrapeError(
			models.ErrCodeBrowserCrash,
			"failed to create page on CDP browser",
			err,
		)
	}
	defer func() {
		_ = page.Close()
	}()

	// Bind context for timeout.
	p := page.Context(ctx)

	// Navigate.
	if err := p.Navigate(req.URL); err != nil {
		return nil, categorizeError(err, "navigation to target URL failed")
	}

	// Wait for network idle or DOM stable.
	if req.WaitForNetworkIdle != nil && *req.WaitForNetworkIdle {
		waitIdle := p.WaitRequestIdle(300*time.Millisecond, nil, nil, nil)
		waitIdle()
	} else {
		_ = p.WaitDOMStable(300*time.Millisecond, 0.1)
	}

	// Remove overlays if requested.
	if req.RemoveOverlays {
		removeOverlays(p)
	}

	// Execute actions if any.
	if len(req.Actions) > 0 {
		if err := executeActions(ctx, page, req.Actions); err != nil {
			return nil, err
		}
	}

	// Extract.
	rawHTML, htmlErr := p.HTML()
	if htmlErr != nil {
		return nil, categorizeError(htmlErr, "failed to extract page HTML")
	}

	title := evalStringOrEmpty(p, `() => document.title`)
	finalURL := evalStringOrEmpty(p, `() => window.location.href`)
	if finalURL == "" {
		finalURL = req.URL
	}

	return &ScrapeResult{
		RawHTML:  rawHTML,
		Title:    title,
		FinalURL: finalURL,
	}, nil
}

// removeOverlays injects JS to remove fixed/sticky positioned elements with
// high z-index, which are typically cookie consent banners and popup overlays.
func removeOverlays(p *rod.Page) {
	const js = `() => {
		const els = document.querySelectorAll('*');
		for (const el of els) {
			const style = window.getComputedStyle(el);
			const pos = style.position;
			if (pos === 'fixed' || pos === 'sticky') {
				const z = parseInt(style.zIndex, 10);
				if (z >= 900 || style.zIndex === 'auto') {
					el.remove();
				}
			}
		}
		// Also remove common overlay class patterns.
		const selectors = [
			'[class*="cookie"]', '[class*="consent"]', '[class*="overlay"]',
			'[id*="cookie"]', '[id*="consent"]', '[id*="overlay"]',
			'[class*="popup"]', '[id*="popup"]',
			'[class*="gdpr"]', '[id*="gdpr"]',
		];
		for (const sel of selectors) {
			document.querySelectorAll(sel).forEach(el => {
				const style = window.getComputedStyle(el);
				if (style.position === 'fixed' || style.position === 'sticky' || style.position === 'absolute') {
					el.remove();
				}
			});
		}
		// Remove any overflow:hidden on body/html (often set by modals).
		document.documentElement.style.overflow = '';
		document.body.style.overflow = '';
	}`
	_, _ = p.Eval(js)
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
