package dialer

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

const defaultProxyFallbackStagger = 800 * time.Millisecond

// SmartDirectDialer dials TCP targets directly first, and if an OS/environment
// proxy is active for a non-private target, automatically races the system proxy
// after StaggerDelay (or immediately if direct fails early).
type SmartDirectDialer struct {
	Timeout      time.Duration
	KeepAlive    time.Duration
	StaggerDelay time.Duration
}

// NewSmartDirectDialer constructs a SmartDirectDialer with sensible defaults.
func NewSmartDirectDialer(timeout, keepAlive time.Duration) *SmartDirectDialer {
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	if keepAlive == 0 {
		keepAlive = defaultKeepAlive
	}
	return &SmartDirectDialer{
		Timeout:      timeout,
		KeepAlive:    keepAlive,
		StaggerDelay: defaultProxyFallbackStagger,
	}
}

func (d *SmartDirectDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}
	direct := NewDirectDialer(d.Timeout, d.KeepAlive)
	proxyURLs := DetectSystemProxyURLs(addr)
	if len(proxyURLs) == 0 {
		return direct.Dial(ctx, addr)
	}

	stagger := d.StaggerDelay
	if stagger <= 0 {
		stagger = defaultProxyFallbackStagger
	}

	overallCtx := ctx
	var overallCancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && d.Timeout > 0 {
		overallCtx, overallCancel = context.WithTimeout(ctx, d.Timeout)
		defer overallCancel()
	}

	raceCtx, cancelRace := context.WithCancel(overallCtx)
	defer cancelRace()

	type dialResult struct {
		conn net.Conn
		err  error
	}

	winCh := make(chan net.Conn, 1)
	errCh := make(chan error, 2)
	directFailedEarly := make(chan struct{})

	var wonOnce sync.Once
	deliverWin := func(c net.Conn) {
		delivered := false
		wonOnce.Do(func() {
			delivered = true
			winCh <- c
			cancelRace()
		})
		if !delivered {
			_ = c.Close()
		}
	}

	// Branch 1: Direct TCP dial starts immediately at t=0
	go func() {
		c, err := direct.Dial(raceCtx, addr)
		if err == nil {
			deliverWin(c)
			return
		}
		select {
		case directFailedEarly <- struct{}{}:
		default:
		}
		errCh <- err
	}()

	// Wait up to stagger before starting system proxy fallback (or start right away if direct already failed)
	timer := time.NewTimer(stagger)
	defer timer.Stop()

	select {
	case c := <-winCh:
		return c, nil
	case <-overallCtx.Done():
		return nil, overallCtx.Err()
	case <-directFailedEarly:
		// Direct failed fast; launch proxy immediately
	case <-timer.C:
		// Direct still pending (e.g. SYN blackholed); launch proxy in parallel
	}

	// Branch 2: System Proxy fallback (HTTP CONNECT / SOCKS5)
	go func() {
		c, err := dialViaProxyCandidates(raceCtx, proxyURLs, d.Timeout, d.KeepAlive, addr)
		if err == nil {
			deliverWin(c)
			return
		}
		errCh <- err
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case c := <-winCh:
			return c, nil
		case err := <-errCh:
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, overallCtx.Err()
}

func dialViaProxyCandidates(ctx context.Context, proxyURLs []string, timeout, keepAlive time.Duration, targetAddr string) (net.Conn, error) {
	base := NewDirectDialer(timeout, keepAlive)
	var lastErr error
	for _, rawURL := range proxyURLs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pd, err := NewProxyDialerFromURL(rawURL, base)
		if err != nil {
			lastErr = err
			continue
		}
		conn, err := pd.Dial(ctx, targetAddr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// DetectSystemProxyURLs returns ordered candidate proxy URLs (e.g. ["http://127.0.0.1:10808", "socks5://127.0.0.1:10808"])
// from environment variables or OS proxy settings when targetAddr is a public host not matched by bypass rules.
func DetectSystemProxyURLs(targetAddr string) []string {
	host := strings.TrimSpace(targetAddr)
	if h, _, err := net.SplitHostPort(targetAddr); err == nil {
		host = strings.TrimSpace(h)
	}
	host = strings.Trim(host, "[]")
	if host == "" || isBypassedPrivateOrLocalHost(host) {
		return nil
	}

	// 1. Check standard environment variables first
	noProxy := firstNonEmptyEnv("NO_PROXY", "no_proxy")
	if noProxy != "" && matchesBypassList(host, noProxy, ",") {
		return nil
	}
	envProxy := firstNonEmptyEnv("ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy")
	if envProxy != "" {
		if cands := parseProxyServerSpec(envProxy); len(cands) > 0 {
			return cands
		}
	}

	// 2. Check OS-level system proxy (Windows WinINET Registry)
	osSrv, osBypass := readOSSystemProxy()
	if osSrv == "" {
		return nil
	}
	if osBypass != "" && matchesBypassList(host, osBypass, ";") {
		return nil
	}
	return parseProxyServerSpec(osSrv)
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func isBypassedPrivateOrLocalHost(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" ||
		strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") ||
		strings.HasSuffix(lower, ".ts.net") {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() {
			return true
		}
		// Tailscale / Carrier-Grade NAT 100.64.0.0/10
		if addr.Is4() {
			b := addr.As4()
			if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
				return true
			}
		}
	}
	return false
}

func matchesBypassList(host, rawList, sep string) bool {
	lowerHost := strings.ToLower(host)
	for _, item := range strings.Split(rawList, sep) {
		pat := strings.ToLower(strings.TrimSpace(item))
		if pat == "" {
			continue
		}
		if pat == "*" {
			return true
		}
		if pat == "<local>" && !strings.Contains(lowerHost, ".") && !strings.Contains(lowerHost, ":") {
			return true
		}
		pat = strings.TrimPrefix(pat, "*.")
		if strings.Contains(pat, "*") {
			if ok, _ := path.Match(pat, lowerHost); ok {
				return true
			}
			continue
		}
		if lowerHost == pat || strings.HasSuffix(lowerHost, "."+pat) {
			return true
		}
	}
	return false
}

func parseProxyServerSpec(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}

	var out []string
	addUnique := func(u string) {
		for _, existing := range out {
			if existing == u {
				return
			}
		}
		out = append(out, u)
	}

	// WinINET multi-protocol format: "http=127.0.0.1:10809;https=127.0.0.1:10809;socks=127.0.0.1:10808"
	if strings.Contains(spec, "=") {
		kv := map[string]string{}
		for _, part := range strings.Split(spec, ";") {
			part = strings.TrimSpace(part)
			if eq := strings.IndexByte(part, '='); eq > 0 {
				k := strings.ToLower(strings.TrimSpace(part[:eq]))
				v := strings.TrimSpace(part[eq+1:])
				if v != "" {
					kv[k] = v
				}
			}
		}
		for _, key := range []string{"https", "http"} {
			if v, ok := kv[key]; ok {
				if strings.Contains(v, "://") {
					addUnique(v)
				} else {
					addUnique("http://" + v)
				}
			}
		}
		if v, ok := kv["socks"]; ok {
			if strings.Contains(v, "://") {
				addUnique(v)
			} else {
				addUnique("socks5://" + v)
			}
		}
		return out
	}

	// Explicit URL scheme: "http://127.0.0.1:7890" or "socks5://127.0.0.1:1080"
	if strings.Contains(spec, "://") {
		lower := strings.ToLower(spec)
		if strings.HasPrefix(lower, "socks://") {
			spec = "socks5://" + spec[len("socks://"):]
		}
		addUnique(spec)
		return out
	}

	// Bare "host:port" (e.g. "127.0.0.1:10808" in Windows ProxyServer):
	// Try HTTP CONNECT first, then SOCKS5 on the same port as fallback.
	addUnique("http://" + spec)
	addUnique("socks5://" + spec)
	return out
}
