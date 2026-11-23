package dialer

import (
	"context"
	"errors"
	"net"
	"time"
)

// ErrEmptyAddress is returned when an empty target address is passed to Dial.
var ErrEmptyAddress = errors.New("dialer: target address must not be empty")

// Dialer is the composable, transport-agnostic TCP stream dialer interface.
// Implementations can be freely stacked (e.g. Direct -> SOCKS5 -> SSH Jump1 -> SSH Jump2,
// or TSNet -> SSH Jump -> Target).
type Dialer interface {
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

// DialFunc adapts an ordinary function to the Dialer interface.
type DialFunc func(ctx context.Context, addr string) (net.Conn, error)

func (f DialFunc) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return f(ctx, addr)
}

const (
	defaultDialTimeout = 15 * time.Second
	defaultKeepAlive   = 30 * time.Second
)

// DirectDialer dials TCP targets directly via the host OS network stack.
type DirectDialer struct {
	Timeout   time.Duration
	KeepAlive time.Duration
}

// NewDirectDialer constructs a DirectDialer with sensible defaults if zero values are passed.
func NewDirectDialer(timeout, keepAlive time.Duration) *DirectDialer {
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	if keepAlive == 0 {
		keepAlive = defaultKeepAlive
	}
	return &DirectDialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}
}

func (d *DirectDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}
	timeout := defaultDialTimeout
	keepAlive := defaultKeepAlive
	if d != nil {
		if d.Timeout > 0 {
			timeout = d.Timeout
		}
		if d.KeepAlive != 0 {
			keepAlive = d.KeepAlive
		}
	}
	nd := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}
	return nd.DialContext(ctx, "tcp", addr)
}

func upstreamOrDirect(upstream Dialer) Dialer {
	if upstream != nil {
		return upstream
	}
	return NewDirectDialer(0, 0)
}

// runHandshakeWithContext executes a blocking protocol handshake on conn while
// enforcing ctx cancellation and deadline semantics. On error, conn is closed.
func runHandshakeWithContext(ctx context.Context, conn net.Conn, fn func() error) error {
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	if err := fn(); err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}

// ensureHostPort appends defaultPort if addr does not specify a port.
func ensureHostPort(addr, defaultPort string) string {
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, defaultPort)
}
