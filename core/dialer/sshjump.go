package dialer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// JumpHostConfig describes a single SSH bastion / jump host hop.
type JumpHostConfig struct {
	Addr                  string
	User                  string
	Password              string
	PrivateKeyPEM         []byte
	PrivateKeyPassphrase  []byte
	AuthMethods           []gossh.AuthMethod
	HostKeyCallback       gossh.HostKeyCallback
	HandshakeTimeout      time.Duration
}

// SSHJumpDialer dials target TCP endpoints through an SSH bastion host ("direct-tcpip" channel).
// Multiple SSHJumpDialers can be nested to form an arbitrary N-hop bastion chain.
type SSHJumpDialer struct {
	upstream Dialer
	cfg      JumpHostConfig
	auth     []gossh.AuthMethod

	mu     sync.Mutex
	client *gossh.Client
	closed bool
}

// NewSSHJumpDialer validates cfg and returns a reusable SSHJumpDialer layered over upstream.
func NewSSHJumpDialer(upstream Dialer, cfg JumpHostConfig) (*SSHJumpDialer, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, errors.New("dialer: jump host address must not be empty")
	}
	if strings.TrimSpace(cfg.User) == "" {
		return nil, errors.New("dialer: jump host user must not be empty")
	}
	if cfg.HostKeyCallback == nil {
		return nil, errors.New("dialer: jump host HostKeyCallback is required")
	}

	auth, err := buildJumpAuthMethods(cfg)
	if err != nil {
		return nil, err
	}
	if len(auth) == 0 {
		return nil, errors.New("dialer: jump host requires at least one authentication method")
	}

	cfg.Addr = ensureHostPort(strings.TrimSpace(cfg.Addr), "22")
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 15 * time.Second
	}

	return &SSHJumpDialer{
		upstream: upstreamOrDirect(upstream),
		cfg:      cfg,
		auth:     auth,
	}, nil
}

func buildJumpAuthMethods(cfg JumpHostConfig) ([]gossh.AuthMethod, error) {
	var methods []gossh.AuthMethod
	methods = append(methods, cfg.AuthMethods...)

	if len(cfg.PrivateKeyPEM) > 0 {
		var (
			signer gossh.Signer
			err    error
		)
		if len(cfg.PrivateKeyPassphrase) > 0 {
			signer, err = gossh.ParsePrivateKeyWithPassphrase(cfg.PrivateKeyPEM, cfg.PrivateKeyPassphrase)
		} else {
			signer, err = gossh.ParsePrivateKey(cfg.PrivateKeyPEM)
		}
		if err != nil {
			return nil, fmt.Errorf("dialer: parse jump host private key: %w", err)
		}
		methods = append(methods, gossh.PublicKeys(signer))
	}

	if cfg.Password != "" {
		pw := cfg.Password
		methods = append(methods, gossh.Password(pw))
	}
	return methods, nil
}

func (j *SSHJumpDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}

	client, err := j.getOrConnectClient(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := client.DialContext(ctx, "tcp", addr)
	if err == nil {
		return conn, nil
	}

	// If the cached jump SSH client died (e.g. broken pipe after sleep/wake),
	// probe whether the transport is dead; if so, reconnect once.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, _, probeErr := client.SendRequest("keepalive@shnex.dev", true, nil); probeErr == nil {
		// Jump host SSH connection is healthy; target itself refused or timed out.
		return nil, fmt.Errorf("dialer: jump host %s -> %s: %w", j.cfg.Addr, addr, err)
	}

	j.invalidateClient(client)
	client, reconnErr := j.getOrConnectClient(ctx)
	if reconnErr != nil {
		return nil, reconnErr
	}
	conn, err = client.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dialer: jump host %s -> %s: %w", j.cfg.Addr, addr, err)
	}
	return conn, nil
}

func (j *SSHJumpDialer) getOrConnectClient(ctx context.Context) (*gossh.Client, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil, errors.New("dialer: ssh jump dialer is closed")
	}
	if j.client != nil {
		return j.client, nil
	}

	rawConn, err := j.upstream.Dial(ctx, j.cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("dialer: connect jump host %s: %w", j.cfg.Addr, err)
	}

	sshCfg := &gossh.ClientConfig{
		User:            j.cfg.User,
		Auth:            j.auth,
		HostKeyCallback: j.cfg.HostKeyCallback,
		Timeout:         j.cfg.HandshakeTimeout,
	}

	var (
		c     gossh.Conn
		chans <-chan gossh.NewChannel
		reqs  <-chan *gossh.Request
	)
	err = runHandshakeWithContext(ctx, rawConn, func() error {
		var hsErr error
		c, chans, reqs, hsErr = gossh.NewClientConn(rawConn, j.cfg.Addr, sshCfg)
		return hsErr
	})
	if err != nil {
		return nil, fmt.Errorf("dialer: ssh handshake with jump host %s: %w", j.cfg.Addr, err)
	}

	j.client = gossh.NewClient(c, chans, reqs)
	return j.client, nil
}

func (j *SSHJumpDialer) invalidateClient(stale *gossh.Client) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.client == stale {
		_ = j.client.Close()
		j.client = nil
	}
}

// Close tears down the bastion SSH client and any closable upstream dialer.
func (j *SSHJumpDialer) Close() error {
	j.mu.Lock()
	j.closed = true
	client := j.client
	j.client = nil
	j.mu.Unlock()

	var errs []error
	if client != nil {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if closer, ok := j.upstream.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NewSSHJumpChain builds a multi-hop SSH bastion pipeline:
// base -> hops[0] -> hops[1] -> ... -> hops[n-1].
// Closing the returned io.Closer cleanly closes every jump host in reverse order.
func NewSSHJumpChain(base Dialer, hops ...JumpHostConfig) (Dialer, io.Closer, error) {
	cur := upstreamOrDirect(base)
	var topCloser io.Closer = nopCloser{}

	for i, hop := range hops {
		jd, err := NewSSHJumpDialer(cur, hop)
		if err != nil {
			_ = topCloser.Close()
			return nil, nil, fmt.Errorf("jump hop[%d] (%s): %w", i, hop.Addr, err)
		}
		cur = jd
		topCloser = jd
	}
	return cur, topCloser, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
