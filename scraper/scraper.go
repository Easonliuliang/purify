package scraper

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/use-agent/purify/config"
	"github.com/use-agent/purify/models"
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
}

// NewScraper launches a headless browser and initialises the reusable page pool.
func NewScraper(browserCfg config.BrowserConfig, scraperCfg config.ScraperConfig) (*Scraper, error) {
	l := launcher.New().
		Headless(browserCfg.Headless).
		NoSandbox(browserCfg.NoSandbox)

	if browserCfg.BrowserBin != "" {
		l = l.Bin(browserCfg.BrowserBin)
	}
	if browserCfg.DefaultProxy != "" {
		l = l.Proxy(browserCfg.DefaultProxy)
	}

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
	}, nil
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
	slog.Info("scraper shutdown complete")
}
