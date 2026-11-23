package dialer

import (
	"context"
	"errors"
	"net"
)

// TSNetNode is satisfied by *tsnet.Server as well as Shnex's *tailnet.Manager,
// keeping core/dialer decoupled from concrete Tailscale control lifecycle internals.
type TSNetNode interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
}

// TSNetDialer dials TCP endpoints over a userspace Tailscale / Headscale mesh (tsnet).
type TSNetDialer struct {
	node TSNetNode
}

// NewTSNetDialer constructs a Dialer backed by a userspace tsnet node.
func NewTSNetDialer(node TSNetNode) (*TSNetDialer, error) {
	if node == nil {
		return nil, errors.New("dialer: tsnet node must not be nil")
	}
	return &TSNetDialer{node: node}, nil
}

func (t *TSNetDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}
	if t == nil || t.node == nil {
		return nil, errors.New("dialer: tsnet dialer not initialized")
	}
	return t.node.Dial(ctx, "tcp", addr)
}
