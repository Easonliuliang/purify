package scraper

import (
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/engine"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/proxy"
)

// Scraper manages the global browser lifecycle and the page pool.
// It is safe for concurrent use.
type Scraper struct {
	browser     *rod.Browser
	pagePool    rod.Pool[rod.Page]
	browserCfg  config.BrowserConfig
	scraperCfg  config.ScraperConfig
	httpFetcher *httpFetcher
	activePages atomic.Int32
	startTime   time.Time
	dispatcher  *engine.Dispatcher
	relay       *proxy.Relay
}

// NewScraper launches a headless browser and initialises the reusable page pool.
func NewScraper(browserCfg config.BrowserConfig, scraperCfg config.ScraperConfig) (*Scraper, error) {
	// ── Proxy strategy ──────────────────────────────────────────────
	// When the proxy requires auth (user:pass in URL), Chrome cannot handle
	// it directly (HandleAuth conflicts with HijackRequests, SOCKS5 auth
	// is unsupported). Solution: start a local SOCKS5 relay (no auth) that
	// forwards to the external proxy (with auth). Chrome connects to the
	// relay, which handles authentication transparently.
	var relay *proxy.Relay
	proxyForChrome := ""

	if browserCfg.DefaultProxy != "" {
		if u, err := url.Parse(browserCfg.DefaultProxy); err == nil && u.User != nil {
			// Proxy has auth → start local relay for Chrome.
			r, err := proxy.StartRelay(browserCfg.DefaultProxy)
			if err != nil {
				slog.Warn("failed to start proxy relay, Chrome will go direct",
					"error", err)
			} else {
				relay = r
				proxyForChrome = "socks5://127.0.0.1:" + addrPort(r.Addr())
				slog.Info("Chrome will use proxy via local relay",
					"relay", r.Addr())
			}
		} else {
			// No auth: Chrome can use it directly.
			proxyForChrome = browserCfg.DefaultProxy
		}
	}

	l := launcher.New().
		Headless(browserCfg.Headless).
		NoSandbox(browserCfg.NoSandbox)

	if browserCfg.BrowserBin != "" {
		l = l.Bin(browserCfg.BrowserBin)
	}
	if proxyForChrome != "" {
		l = l.Proxy(proxyForChrome)
	}

	// ── Stealth flags ────────────────────────────────────────────────
	l.Set(flags.Flag("disable-blink-features"), "AutomationControlled")
	l.Delete(flags.Flag("enable-automation"))
	l.Set(flags.Flag("disable-features"), "AudioServiceOutOfProcess,TranslateUI")
	l.Set(flags.Flag("disable-ipc-flooding-protection"))
	l.Set(flags.Flag("disable-popup-blocking"))
	l.Set(flags.Flag("disable-prompt-on-repost"))
	l.Set(flags.Flag("disable-renderer-backgrounding"))
	l.Set(flags.Flag("disable-background-timer-throttling"))
	l.Set(flags.Flag("disable-backgrounding-occluded-windows"))
	l.Set(flags.Flag("disable-component-update"))
	l.Set(flags.Flag("disable-default-apps"))
	l.Set(flags.Flag("disable-dev-shm-usage"))
	l.Set(flags.Flag("disable-extensions"))
	l.Set(flags.Flag("no-first-run"))

	controlURL, err := l.Launch()
	if err != nil {
		return nil, models.NewScrapeError(
			models.ErrCodeBrowserCrash,
			"failed to launch browser",
			err,
		)
	}
	slog.Info("browser launched", "controlURL", controlURL)

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return nil, models.NewScrapeError(
			models.ErrCodeBrowserCrash,
			"failed to connect to browser",
			err,
		)
	}

	pool := rod.NewPagePool(browserCfg.MaxPages)
	slog.Info("page pool created", "maxPages", browserCfg.MaxPages)

	return &Scraper{
		browser:     browser,
		pagePool:    pool,
		browserCfg:  browserCfg,
		scraperCfg:  scraperCfg,
		httpFetcher: newHTTPFetcher(browserCfg.DefaultProxy),
		startTime:   time.Now(),
		relay:       relay,
	}, nil
}

// addrPort extracts the port from a "host:port" address string.
func addrPort(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return addr
}

// SetDispatcher sets the multi-engine dispatcher. When set, DoScrape will
// delegate simple requests (no Actions, no CDPURL) to the dispatcher.
func (s *Scraper) SetDispatcher(d *engine.Dispatcher) {
	s.dispatcher = d
}

// Stats returns a snapshot of the pool's current state.
func (s *Scraper) Stats() models.PoolStats {
	return models.PoolStats{
		MaxPages:    s.browserCfg.MaxPages,
		ActivePages: int(s.activePages.Load()),
	}
}

// Close drains the page pool and kills the browser process.
// Call this on graceful shutdown to prevent zombie Chrome processes.
func (s *Scraper) Close() {
	slog.Info("scraper shutting down: draining page pool")
	s.pagePool.Cleanup(func(p *rod.Page) {
		_ = p.Close()
	})
	slog.Info("scraper shutting down: closing browser")
	s.browser.MustClose()
	if s.relay != nil {
		s.relay.Close()
	}
	slog.Info("scraper shutdown complete")
}
