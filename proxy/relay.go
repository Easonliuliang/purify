package proxy

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Relay is a local SOCKS5 proxy (no auth) that forwards all connections
// through an external authenticated proxy. This allows Chrome (which cannot
// handle SOCKS5 auth or HTTP proxy auth without CDP conflicts) to use
// authenticated proxies transparently.
type Relay struct {
	listener    net.Listener
	externalURL string
	done        chan struct{}
}

// StartRelay creates a local SOCKS5 relay on 127.0.0.1 (random port)
// that forwards connections through the given external proxy URL.
// Supports both socks5:// and http:// external proxies with auth.
func StartRelay(externalProxyURL string) (*Relay, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy relay: listen: %w", err)
	}

	r := &Relay{
		listener:    listener,
		externalURL: externalProxyURL,
		done:        make(chan struct{}),
	}

	go r.serve()
	slog.Info("proxy relay started", "addr", r.Addr())
	return r, nil
}

// Addr returns the local listen address (e.g., "127.0.0.1:12345").
func (r *Relay) Addr() string {
	return r.listener.Addr().String()
}

// Close stops the relay.
func (r *Relay) Close() error {
	close(r.done)
	return r.listener.Close()
}

func (r *Relay) serve() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				continue
			}
		}
		go r.handle(conn)
	}
}

func (r *Relay) handle(client net.Conn) {
	defer client.Close()

	// ── SOCKS5 handshake (no auth) ──────────────────────────────────
	buf := make([]byte, 258)

	// 1. Read greeting: [VER, NMETHODS, METHODS...]
	if _, err := io.ReadFull(client, buf[:2]); err != nil || buf[0] != 0x05 {
		return
	}
	nMethods := int(buf[1])
	if _, err := io.ReadFull(client, buf[:nMethods]); err != nil {
		return
	}

	// 2. Reply: no auth required
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 3. Read request: [VER, CMD, RSV, ATYP, ...]
	if _, err := io.ReadFull(client, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 { // only CONNECT
		client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Parse target address based on ATYP
	var target string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(client, buf[:6]); err != nil {
			return
		}
		target = fmt.Sprintf("%d.%d.%d.%d:%d",
			buf[0], buf[1], buf[2], buf[3],
			binary.BigEndian.Uint16(buf[4:6]))
	case 0x03: // Domain
		if _, err := io.ReadFull(client, buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(client, buf[:domainLen+2]); err != nil {
			return
		}
		target = fmt.Sprintf("%s:%d",
			string(buf[:domainLen]),
			binary.BigEndian.Uint16(buf[domainLen:domainLen+2]))
	case 0x04: // IPv6
		if _, err := io.ReadFull(client, buf[:18]); err != nil {
			return
		}
		ip := net.IP(buf[:16])
		port := binary.BigEndian.Uint16(buf[16:18])
		target = fmt.Sprintf("[%s]:%d", ip.String(), port)
	default:
		client.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// ── Connect through external proxy ──────────────────────────────
	remote, err := dialExternal(r.externalURL, target)
	if err != nil {
		slog.Debug("proxy relay: dial failed", "target", target, "error", err)
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// 4. Reply: success
	client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// ── Relay data ──────────────────────────────────────────────────
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(remote, client) }()
	go func() { defer wg.Done(); io.Copy(client, remote) }()
	wg.Wait()
}

// dialExternal connects to the target through the external proxy.
func dialExternal(proxyURL, target string) (net.Conn, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "socks5", "socks5h":
		return dialExternalSocks5(u, target)
	case "http", "https":
		return dialExternalHTTPConnect(u, target)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
	}
}

func dialExternalSocks5(u *url.URL, target string) (net.Conn, error) {
	var auth *xproxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
	}
	forward := &net.Dialer{Timeout: 10 * time.Second}
	dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, forward)
	if err != nil {
		return nil, err
	}
	return dialer.Dial("tcp", target)
}

func dialExternalHTTPConnect(u *url.URL, target string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy: %w", err)
	}

	// Build CONNECT request with Basic auth
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if u.User != nil {
		pass, _ := u.User.Password()
		creds := base64.StdEncoding.EncodeToString(
			[]byte(u.User.Username() + ":" + pass))
		req += "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send CONNECT: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}
	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("CONNECT rejected: %s", resp.Status)
	}

	return conn, nil
}
