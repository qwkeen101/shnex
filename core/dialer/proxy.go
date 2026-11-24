package dialer

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/shnex/shnex/core/sharelink"
	"golang.org/x/net/proxy"
)

// SOCKS5Config configures a SOCKS5 proxy dialer.
type SOCKS5Config struct {
	ProxyAddr string
	Username  string
	Password  string
	Upstream  Dialer
}

// SOCKS5Dialer tunnels TCP streams through a SOCKS5 proxy (RFC 1928 / RFC 1929).
type SOCKS5Dialer struct {
	cfg SOCKS5Config
}

// NewSOCKS5Dialer creates a composable SOCKS5Dialer.
func NewSOCKS5Dialer(cfg SOCKS5Config) (*SOCKS5Dialer, error) {
	if strings.TrimSpace(cfg.ProxyAddr) == "" {
		return nil, errors.New("dialer: socks5 proxy address must not be empty")
	}
	cfg.ProxyAddr = ensureHostPort(strings.TrimSpace(cfg.ProxyAddr), "1080")
	cfg.Upstream = upstreamOrDirect(cfg.Upstream)
	return &SOCKS5Dialer{cfg: cfg}, nil
}

func (s *SOCKS5Dialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}
	var auth *proxy.Auth
	if s.cfg.Username != "" || s.cfg.Password != "" {
		auth = &proxy.Auth{
			User:     s.cfg.Username,
			Password: s.cfg.Password,
		}
	}

	forward := &xnetContextAdapter{upstream: s.cfg.Upstream}
	pd, err := proxy.SOCKS5("tcp", s.cfg.ProxyAddr, auth, forward)
	if err != nil {
		return nil, fmt.Errorf("dialer: socks5 init: %w", err)
	}
	if cd, ok := pd.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, "tcp", addr)
	}
	return pd.Dial("tcp", addr)
}

type xnetContextAdapter struct {
	upstream Dialer
}

func (a *xnetContextAdapter) Dial(network, addr string) (net.Conn, error) {
	return a.DialContext(context.Background(), network, addr)
}

func (a *xnetContextAdapter) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("dialer: unsupported network %q", network)
	}
	conn, err := a.upstream.Dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	// Wrap conn so if ctx is cancelled while x/net/proxy performs the SOCKS5 handshake,
	// the underlying socket unblocks immediately.
	return &ctxBoundHandshakeConn{Conn: conn, ctx: ctx}, nil
}

type ctxBoundHandshakeConn struct {
	net.Conn
	ctx context.Context
}

func (c *ctxBoundHandshakeConn) Read(p []byte) (int, error) {
	stop := context.AfterFunc(c.ctx, func() { _ = c.Conn.Close() })
	defer stop()
	n, err := c.Conn.Read(p)
	if err != nil && c.ctx.Err() != nil {
		return n, c.ctx.Err()
	}
	return n, err
}

func (c *ctxBoundHandshakeConn) Write(p []byte) (int, error) {
	stop := context.AfterFunc(c.ctx, func() { _ = c.Conn.Close() })
	defer stop()
	n, err := c.Conn.Write(p)
	if err != nil && c.ctx.Err() != nil {
		return n, c.ctx.Err()
	}
	return n, err
}

// HTTPProxyConfig configures an HTTP/HTTPS CONNECT tunnel proxy dialer.
type HTTPProxyConfig struct {
	ProxyAddr string // host:port
	Scheme    string // "http" (default) or "https"
	Username  string
	Password  string
	TLSConfig *tls.Config
	Upstream  Dialer
}

// HTTPProxyDialer tunnels TCP streams using HTTP CONNECT.
type HTTPProxyDialer struct {
	cfg HTTPProxyConfig
}

// NewHTTPProxyDialer creates an HTTP CONNECT proxy dialer.
func NewHTTPProxyDialer(cfg HTTPProxyConfig) (*HTTPProxyDialer, error) {
	scheme := strings.ToLower(strings.TrimSpace(cfg.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("dialer: unsupported http proxy scheme %q", scheme)
	}
	if strings.TrimSpace(cfg.ProxyAddr) == "" {
		return nil, errors.New("dialer: http proxy address must not be empty")
	}
	defaultPort := "8080"
	if scheme == "https" {
		defaultPort = "443"
	}
	cfg.Scheme = scheme
	cfg.ProxyAddr = ensureHostPort(strings.TrimSpace(cfg.ProxyAddr), defaultPort)
	cfg.Upstream = upstreamOrDirect(cfg.Upstream)
	return &HTTPProxyDialer{cfg: cfg}, nil
}

func (h *HTTPProxyDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}

	rawConn, err := h.cfg.Upstream.Dial(ctx, h.cfg.ProxyAddr)
	if err != nil {
		return nil, fmt.Errorf("dialer: connect http proxy %s: %w", h.cfg.ProxyAddr, err)
	}

	conn := rawConn
	if h.cfg.Scheme == "https" {
		host, _, _ := net.SplitHostPort(h.cfg.ProxyAddr)
		var tlsCfg *tls.Config
		if h.cfg.TLSConfig != nil {
			tlsCfg = h.cfg.TLSConfig.Clone()
		} else {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		if tlsCfg.ServerName == "" {
			tlsCfg.ServerName = host
		}
		tlsConn := tls.Client(rawConn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("dialer: https proxy tls handshake: %w", err)
		}
		conn = tlsConn
	}

	var resultConn net.Conn
	err = runHandshakeWithContext(ctx, conn, func() error {
		req := &http.Request{
			Method: http.MethodConnect,
			URL:    &url.URL{Opaque: addr},
			Host:   addr,
			Header: make(http.Header),
		}
		if h.cfg.Username != "" || h.cfg.Password != "" {
			cred := base64.StdEncoding.EncodeToString([]byte(h.cfg.Username + ":" + h.cfg.Password))
			req.Header.Set("Proxy-Authorization", "Basic "+cred)
		}

		if err := req.Write(conn); err != nil {
			return fmt.Errorf("write CONNECT request: %w", err)
		}

		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			return fmt.Errorf("read CONNECT response: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("proxy rejected CONNECT with HTTP %d (%s)", resp.StatusCode, resp.Status)
		}

		if br.Buffered() > 0 {
			resultConn = &bufferedConn{Conn: conn, r: io.MultiReader(io.LimitReader(br, int64(br.Buffered())), conn)}
		} else {
			resultConn = conn
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dialer: http proxy handshake: %w", err)
	}
	return resultConn, nil
}

type bufferedConn struct {
	net.Conn
	r io.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

// NewProxyDialerFromURL constructs either a SOCKS5Dialer or HTTPProxyDialer from
// a URL such as "socks5://user:pass@127.0.0.1:1080" or "http://127.0.0.1:7890".
func NewProxyDialerFromURL(rawURL string, upstream Dialer) (Dialer, error) {
	if sharelink.IsShareLink(rawURL) {
		// vless:// trojan:// ss:// ... go through xray; it dials the node itself
		return NewXrayDialer(rawURL)
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("dialer: invalid proxy url: %w", err)
	}
	var user, pass string
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		return NewSOCKS5Dialer(SOCKS5Config{
			ProxyAddr: u.Host,
			Username:  user,
			Password:  pass,
			Upstream:  upstream,
		})
	case "http", "https":
		return NewHTTPProxyDialer(HTTPProxyConfig{
			ProxyAddr: u.Host,
			Scheme:    u.Scheme,
			Username:  user,
			Password:  pass,
			Upstream:  upstream,
		})
	default:
		return nil, fmt.Errorf("dialer: unsupported proxy scheme %q (expected socks5, http, or https)", u.Scheme)
	}
}
