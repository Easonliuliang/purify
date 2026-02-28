package scraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	tls2 "github.com/refraction-networking/utls"
	"golang.org/x/net/html"
)

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// httpFetcher performs HTTP requests with a Chrome TLS fingerprint (utls).
type httpFetcher struct {
	defaultProxy string
}

// newHTTPFetcher creates a new HTTP fetcher.
func newHTTPFetcher(defaultProxy string) *httpFetcher {
	return &httpFetcher{defaultProxy: defaultProxy}
}

// fetch retrieves the URL via plain HTTP with a Chrome TLS fingerprint.
// proxyOverride, if non-empty, overrides the default proxy.
func (f *httpFetcher) fetch(ctx context.Context, targetURL, proxyOverride string) ([]byte, error) {
	proxy := proxyOverride
	if proxy == "" {
		proxy = f.defaultProxy
	}

	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialTLSChrome(ctx, network, addr, proxy)
		},
	}
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") {
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
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
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

// dialTLSChrome establishes a TLS connection using a Chrome fingerprint via utls.
func dialTLSChrome(ctx context.Context, network, addr, proxy string) (net.Conn, error) {
	var rawConn net.Conn
	var err error

	dialer := &net.Dialer{}

	if proxy != "" {
		proxyURL, parseErr := url.Parse(proxy)
		if parseErr == nil && (proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h") {
			// For SOCKS5, the dialer handles the proxy connection.
			// We dial through the proxy to the target.
			socksConn, socksErr := dialer.DialContext(ctx, "tcp", proxyURL.Host)
			if socksErr != nil {
				return nil, fmt.Errorf("socks5 dial: %w", socksErr)
			}
			rawConn = socksConn
		}
	}

	if rawConn == nil {
		rawConn, err = dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
	}

	host, _, _ := net.SplitHostPort(addr)
	tlsConn := tls2.UClient(rawConn, &tls2.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
	}, tls2.HelloChrome_Auto)

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// needsBrowser uses heuristics to decide if the HTTP-fetched HTML likely needs
// JS rendering (SPA shell, heavy JS dependency, noscript warnings).
func needsBrowser(body []byte) bool {
	bodyText := extractVisibleText(body)

	// 1. Very little visible text in <body> → likely SPA shell
	if len(bodyText) < 200 {
		return true
	}

	lower := strings.ToLower(string(body))

	// 2. Empty SPA root containers
	if strings.Contains(lower, `<div id="root"></div>`) ||
		strings.Contains(lower, `<div id="app"></div>`) ||
		strings.Contains(lower, `<div id="__next"></div>`) ||
		strings.Contains(lower, `<div id="root">`) && !strings.Contains(lower, `<div id="root"><div`) {
		// Check for truly empty root — the last condition avoids false positives
		// when SSR has pre-rendered content inside #root
	} else {
		goto checkNoscript
	}
	return true

checkNoscript:
	// 3. <noscript> with JS-required warnings
	if reNoscript.MatchString(lower) {
		return true
	}

	// 4. Many <script> tags + little body text → JS-heavy page
	scriptCount := strings.Count(lower, "<script")
	if scriptCount > 10 && len(bodyText) < 500 {
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
