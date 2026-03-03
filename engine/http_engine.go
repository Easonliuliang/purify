package engine

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tls "github.com/refraction-networking/utls"
	"golang.org/x/net/html"
	"golang.org/x/net/proxy"
)

// HTTPEngine is a lightweight Layer 1 engine that uses pure net/http.
// It is the fastest option, suitable for static pages that don't need
// JavaScript rendering.
type HTTPEngine struct {
	client   *http.Client
	proxyURL string
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
// If proxyURL is non-empty, all connections are routed through the proxy
// (SOCKS5 with optional username/password auth is supported).
func NewHTTPEngine(proxyURL string) *HTTPEngine {
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var conn net.Conn
			var err error

			if proxyURL != "" {
				conn, err = dialViaProxy(ctx, proxyURL, network, addr)
			} else {
				dialer := &net.Dialer{Timeout: 10 * time.Second}
				conn, err = dialer.DialContext(ctx, network, addr)
			}
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

	// Route plain HTTP through the proxy via Go's standard Proxy mechanism.
	if proxyURL != "" {
		if pu, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}

	if proxyURL != "" {
		slog.Info("http_engine: proxy configured", "proxy", redactProxy(proxyURL))
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
		proxyURL: proxyURL,
	}
}

// dialViaProxy creates a TCP connection through a proxy (SOCKS5 or HTTP CONNECT).
func dialViaProxy(ctx context.Context, rawProxyURL, network, addr string) (net.Conn, error) {
	u, err := url.Parse(rawProxyURL)
	if err != nil {
		return nil, fmt.Errorf("http_engine: parse proxy url: %w", err)
	}

	switch u.Scheme {
	case "socks5", "socks5h":
		return dialViaSocks5(ctx, u, network, addr)
	case "http", "https":
		return dialViaHTTPConnect(ctx, u, addr)
	default:
		return nil, fmt.Errorf("http_engine: unsupported proxy scheme: %s", u.Scheme)
	}
}

// dialViaSocks5 connects through a SOCKS5 proxy with optional auth.
func dialViaSocks5(ctx context.Context, u *url.URL, network, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: pass,
		}
	}

	forward := &net.Dialer{Timeout: 10 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, forward)
	if err != nil {
		return nil, fmt.Errorf("http_engine: create socks5 dialer: %w", err)
	}

	if cd, ok := dialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, addr)
	}
	return dialer.Dial(network, addr)
}

// dialViaHTTPConnect creates a tunnel through an HTTP proxy using CONNECT.
func dialViaHTTPConnect(ctx context.Context, u *url.URL, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("http_engine: connect to http proxy: %w", err)
	}

	// Build CONNECT request with Basic auth.
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if u.User != nil {
		pass, _ := u.User.Password()
		creds := base64.StdEncoding.EncodeToString(
			[]byte(u.User.Username() + ":" + pass))
		req += "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("http_engine: send CONNECT: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("http_engine: read CONNECT response: %w", err)
	}
	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("http_engine: CONNECT rejected: %s", resp.Status)
	}

	return conn, nil
}

// redactProxy returns a proxy URL with credentials masked for logging.
func redactProxy(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "***"
	}
	if u.User != nil {
		u.User = url.UserPassword("***", "***")
	}
	return u.String()
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
