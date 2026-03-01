package scraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// httpFetcher performs HTTP requests with Chrome-like headers.
type httpFetcher struct {
	defaultProxy string
}

// newHTTPFetcher creates a new HTTP fetcher.
func newHTTPFetcher(defaultProxy string) *httpFetcher {
	return &httpFetcher{defaultProxy: defaultProxy}
}

// fetch retrieves the URL via plain HTTP with Chrome-like headers.
// proxyOverride, if non-empty, overrides the default proxy.
func (f *httpFetcher) fetch(ctx context.Context, targetURL, proxyOverride string) ([]byte, error) {
	proxy := proxyOverride
	if proxy == "" {
		proxy = f.defaultProxy
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{Transport: transport}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("httpfetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpfetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("httpfetch: HTTP %d for %s", resp.StatusCode, targetURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MB cap
	if err != nil {
		return nil, fmt.Errorf("httpfetch: read body: %w", err)
	}

	return body, nil
}

// needsBrowser uses heuristics to decide if the HTTP-fetched HTML likely needs
// JS rendering (SPA shell, heavy JS dependency, noscript warnings).
func needsBrowser(body []byte) bool {
	bodyText := extractVisibleText(body)

	// 1. Very little visible text in <body> → likely SPA shell
	if len(bodyText) < 50 {
		return true
	}

	lower := strings.ToLower(string(body))

	// 2. Empty SPA root containers
	if strings.Contains(lower, `<div id="root"></div>`) ||
		strings.Contains(lower, `<div id="app"></div>`) ||
		strings.Contains(lower, `<div id="__next"></div>`) {
		return true
	}

	// 3. <noscript> with JS-required warnings
	if reNoscript.MatchString(lower) {
		return true
	}

	// 4. Many <script> tags + little body text → JS-heavy page
	scriptCount := strings.Count(lower, "<script")
	if scriptCount > 10 && len(bodyText) < 200 {
		return true
	}

	return false
}

var reNoscript = regexp.MustCompile(`<noscript[^>]*>[^<]*(enable|activate|turn on|requires?)\s+javascript`)

// extractTitle extracts the <title> content from raw HTML bytes.
func extractTitle(body []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				if tokenizer.Next() == html.TextToken {
					return strings.TrimSpace(string(tokenizer.Text()))
				}
				return ""
			}
		}
	}
}

// extractVisibleText extracts the visible text from within <body>, stripping
// all tags and <script>/<style> content. Used for heuristic analysis only.
func extractVisibleText(body []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var buf strings.Builder
	inBody := false
	skipDepth := 0

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return buf.String()
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "body" {
				inBody = true
			}
			if tag == "script" || tag == "style" || tag == "noscript" {
				skipDepth++
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" || tag == "style" || tag == "noscript" {
				if skipDepth > 0 {
					skipDepth--
				}
			}
		case html.TextToken:
			if inBody && skipDepth == 0 {
				text := strings.TrimSpace(string(tokenizer.Text()))
				if text != "" {
					buf.WriteString(text)
					buf.WriteByte(' ')
				}
			}
		}
	}
}
