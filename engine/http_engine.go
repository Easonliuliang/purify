package engine

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	tls "github.com/refraction-networking/utls"
	"golang.org/x/net/html"
)

// HTTPEngine is a lightweight Layer 1 engine that uses pure net/http.
// It is the fastest option, suitable for static pages that don't need
// JavaScript rendering.
type HTTPEngine struct {
	client *http.Client
}

// chromeH1Spec is a Chrome-like TLS ClientHello with ALPN forced to http/1.1
// only. Computed once at init time and reused for every connection.
var chromeH1Spec tls.ClientHelloSpec

func init() {
	spec, err := tls.UTLSIdToSpec(tls.HelloChrome_Auto)
	if err != nil {
		// Fallback: if spec generation fails, use HelloChrome_Auto as-is.
		// (Should never happen with a valid utls version.)
		return
	}
	// Replace h2 with http/1.1 only in the ALPN extension so the server
	// never negotiates HTTP/2 (which Go's http.Transport cannot handle
	// over a utls connection).
	for i, ext := range spec.Extensions {
		if alpn, ok := ext.(*tls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
			spec.Extensions[i] = alpn
			break
		}
	}
	chromeH1Spec = spec
}

// NewHTTPEngine creates an HTTPEngine with a Chrome-like TLS fingerprint.
// ALPN is locked to http/1.1 to avoid the HTTP/2 framing mismatch that
// occurs when utls negotiates h2 but Go's http.Transport only speaks h1.
func NewHTTPEngine() *HTTPEngine {
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, _ := net.SplitHostPort(addr)
			tlsConn := tls.UClient(conn, &tls.Config{ServerName: host}, tls.HelloCustom)
			if err := tlsConn.ApplyPreset(&chromeH1Spec); err != nil {
				conn.Close()
				return nil, fmt.Errorf("http_engine: apply tls spec: %w", err)
			}
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
		ForceAttemptHTTP2: false,
	}
	return &HTTPEngine{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (e *HTTPEngine) Name() string { return "http" }

func (e *HTTPEngine) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("http_engine: build request: %w", err)
	}

	// Simulate browser-like headers.
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	httpReq.Header.Set("Accept-Encoding", "identity") // no compression for simplicity

	// Apply custom headers (override defaults if provided).
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Apply cookies.
	for i := range req.Cookies {
		httpReq.AddCookie(&req.Cookies[i])
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http_engine: do request: %w", err)
	}
	defer resp.Body.Close()

	// Read body with a 10 MB limit to prevent unbounded memory use.
	const maxBody = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("http_engine: read body: %w", err)
	}

	bodyStr := string(body)

	// If the response isn't successful HTML, treat it as a failure so the
	// dispatcher can escalate to a browser engine.
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 || !isHTMLContentType(ct) {
		return nil, fmt.Errorf("http_engine: non-html or error status %d (content-type: %s)", resp.StatusCode, ct)
	}

	title := extractTitle(bodyStr)
	finalURL := resp.Request.URL.String()

	return &FetchResult{
		HTML:       bodyStr,
		Title:      title,
		StatusCode: resp.StatusCode,
		FinalURL:   finalURL,
		EngineName: e.Name(),
	}, nil
}

// isHTMLContentType returns true if the content-type header looks like HTML.
func isHTMLContentType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml")
}

// extractTitle uses the Go HTML tokenizer to find the first <title> element.
func extractTitle(htmlStr string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlStr))
	inTitle := false
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				inTitle = true
			}
		case html.TextToken:
			if inTitle {
				return strings.TrimSpace(string(tokenizer.Text()))
			}
		case html.EndTagToken:
			if inTitle {
				return ""
			}
		}
	}
}
