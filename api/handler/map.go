package handler

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/use-agent/purify/cleaner"
	"github.com/use-agent/purify/models"
	"github.com/use-agent/purify/scraper"
)

// sitemapIndex represents a sitemap index XML file.
type sitemapIndex struct {
	XMLName  xml.Name       `xml:"sitemapindex"`
	Sitemaps []sitemapEntry `xml:"sitemap"`
}

// sitemapEntry is an entry in a sitemap index.
type sitemapEntry struct {
	Loc string `xml:"loc"`
}

// urlset represents a sitemap URL set XML file.
type urlset struct {
	XMLName xml.Name   `xml:"urlset"`
	URLs    []urlEntry `xml:"url"`
}

// urlEntry is a single URL in a sitemap.
type urlEntry struct {
	Loc string `xml:"loc"`
}

// PostMap returns a handler for POST /api/v1/map.
// It discovers URLs for a site using sitemaps, robots.txt, and link extraction.
func PostMap(sc *scraper.Scraper, cl *cleaner.Cleaner) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.MapRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.MapResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: err.Error(),
				},
			})
			return
		}

		parsed, err := url.Parse(req.URL)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.MapResponse{
				Success: false,
				Error: &models.ErrorDetail{
					Code:    models.ErrCodeInvalidInput,
					Message: "invalid URL",
				},
			})
			return
		}

		baseOrigin := parsed.Scheme + "://" + parsed.Host

		// Collect URLs from all sources.
		allURLs := make(map[string]struct{})

		// 1. Try fetching /sitemap.xml
		sitemapURLs := fetchSitemap(baseOrigin + "/sitemap.xml")
		for _, u := range sitemapURLs {
			allURLs[u] = struct{}{}
		}

		// 2. Try fetching /robots.txt for Sitemap: directives
		robotsSitemaps := fetchRobotsSitemaps(baseOrigin + "/robots.txt")
		for _, sitemapURL := range robotsSitemaps {
			urls := fetchSitemap(sitemapURL)
			for _, u := range urls {
				allURLs[u] = struct{}{}
			}
		}

		// 3. Scrape the homepage and extract same-domain links
		homeLinks := scrapeHomeLinks(sc, cl, req.URL, parsed.Host)
		for _, u := range homeLinks {
			allURLs[u] = struct{}{}
		}

		// Convert to slice.
		urls := make([]string, 0, len(allURLs))
		for u := range allURLs {
			urls = append(urls, u)
		}

		c.JSON(http.StatusOK, models.MapResponse{
			Success: true,
			URLs:    urls,
			Total:   len(urls),
		})
	}
}

// fetchSitemap fetches and parses a sitemap XML URL, returning discovered URLs.
// It handles both regular sitemaps and sitemap index files.
func fetchSitemap(sitemapURL string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return nil
	}

	var urls []string

	// Try parsing as sitemap index first.
	var idx sitemapIndex
	if err := xml.Unmarshal(body, &idx); err == nil && len(idx.Sitemaps) > 0 {
		for _, s := range idx.Sitemaps {
			if s.Loc != "" {
				// Recursively fetch each sub-sitemap.
				urls = append(urls, fetchSitemap(s.Loc)...)
			}
		}
		return urls
	}

	// Try parsing as regular sitemap.
	var us urlset
	if err := xml.Unmarshal(body, &us); err == nil {
		for _, u := range us.URLs {
			if u.Loc != "" {
				urls = append(urls, u.Loc)
			}
		}
	}

	return urls
}

// fetchRobotsSitemaps fetches robots.txt and extracts Sitemap: directives.
func fetchRobotsSitemaps(robotsURL string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024)) // 1MB limit
	if err != nil {
		return nil
	}

	var sitemaps []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
			sitemapURL := strings.TrimSpace(strings.TrimPrefix(line, line[:len("sitemap:")]))
			if sitemapURL != "" {
				sitemaps = append(sitemaps, sitemapURL)
			}
		}
	}

	return sitemaps
}

// scrapeHomeLinks scrapes the homepage and returns same-domain links.
func scrapeHomeLinks(sc *scraper.Scraper, cl *cleaner.Cleaner, homeURL string, host string) []string {
	sreq := &models.ScrapeRequest{
		URL:          homeURL,
		OutputFormat: "markdown",
		ExtractMode:  "raw",
	}
	sreq.Defaults()

	result, err := sc.DoScrape(context.Background(), sreq)
	if err != nil {
		slog.Debug("map: failed to scrape homepage for links", "url", homeURL, "error", err)
		return nil
	}

	links := cleaner.ExtractLinks(result.RawHTML, homeURL)
	var sameDomain []string
	for _, l := range links.Internal {
		sameDomain = append(sameDomain, l.Href)
	}

	return sameDomain
}
